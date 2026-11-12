package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/jpillora/cloud-torrent/engine"
)

const doneCmdTimeout = 30 * time.Minute

// startDoneCmd arms the operator-supplied done command (--done-cmd). The command
// is a startup-only setting, never part of the browser-editable configuration,
// so a web session cannot make the server execute programs.
//
// Scripts report back through the trusted REST listener (CLD_RESTAPI): the
// configured --rest-api address, or an ephemeral loopback port when unset.
func (s *Server) startDoneCmd() (func(), error) {
	if s.DoneCmd == "" {
		return func() {}, nil
	}
	if _, err := exec.LookPath(s.DoneCmd); err != nil {
		return nil, fmt.Errorf("done command: %w", err)
	}
	addr := s.RestAPI
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("rest api listener: %w", err)
	}
	s.restAPIAddr = listener.Addr().String()
	mux := http.NewServeMux()
	mux.Handle("/api/", s.protectIngress(http.HandlerFunc(s.handleAPI)))
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: s.ingressLimits().header, IdleTimeout: s.ingressLimits().idle}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("[RestAPI] %v", err)
		}
	}()
	log.Printf("[RestAPI] trusted API listening at %s (no auth); done command: %s", s.restAPIAddr, s.DoneCmd)
	s.engine.SetDoneHandler(s.runDoneCmd)
	return func() { _ = srv.Close() }, nil
}

// runDoneCmd executes the done command for one finished file or torrent. The
// environment matches simple-torrent: CLD_DIR, CLD_PATH, CLD_HASH, CLD_TYPE,
// CLD_SIZE, CLD_STARTTS, CLD_FILENUM and CLD_RESTAPI.
func (s *Server) runDoneCmd(ev engine.DoneEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), doneCmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.DoneCmd)
	cmd.Env = append(os.Environ(),
		"CLD_DIR="+s.engine.Config().DownloadDirectory,
		"CLD_RESTAPI="+s.restAPIAddr,
		"CLD_PATH="+ev.Path,
		"CLD_HASH="+ev.InfoHash,
		"CLD_TYPE="+ev.Type,
		fmt.Sprintf("CLD_SIZE=%d", ev.Size),
		fmt.Sprintf("CLD_STARTTS=%d", ev.StartedAt.Unix()),
		fmt.Sprintf("CLD_FILENUM=%d", ev.FileCount),
	)
	tag := fmt.Sprintf("[DoneCmd:%s]%s", ev.Type, ev.InfoHash)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	// The environment is deliberately not logged: it carries the operator's secrets.
	log.Printf("%s run %s path=%q size=%d", tag, s.DoneCmd, ev.Path, ev.Size)
	if err := cmd.Start(); err != nil {
		log.Printf("%s ERR: %v", tag, err)
		return
	}
	var wg sync.WaitGroup
	for prefix, r := range map[string]io.Reader{" O: ": stdout, " E: ": stderr} {
		wg.Add(1)
		go func(prefix string, r io.Reader) {
			defer wg.Done()
			sc := bufio.NewScanner(r)
			for sc.Scan() {
				if line := strings.TrimSpace(sc.Text()); line != "" {
					log.Printf("%s%s%s", tag, prefix, line)
				}
			}
		}(prefix, r)
	}
	wg.Wait()
	if err := cmd.Wait(); err != nil {
		log.Printf("%s ERR: %v", tag, err)
		return
	}
	log.Printf("%s exit code: %d", tag, cmd.ProcessState.ExitCode())
}

// parseTorrentRequest accepts the JSON request body, and the plain
// "<action>:<infohash>" form used by simple-torrent done-command scripts.
func parseTorrentRequest(body []byte) (TorrentRequest, error) {
	var req TorrentRequest
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		return req, json.Unmarshal(body, &req)
	}
	action, hash, ok := strings.Cut(string(trimmed), ":")
	if !ok {
		return req, fmt.Errorf("expecting JSON or <action>:<infohash>")
	}
	return TorrentRequest{Action: action, InfoHash: hash}, nil
}

// parseMagnetRequest accepts the JSON request body, or a raw magnet URI.
func parseMagnetRequest(body []byte) (MagnetRequest, error) {
	var req MagnetRequest
	trimmed := bytes.TrimSpace(body)
	if strings.HasPrefix(string(trimmed), "magnet:") {
		return MagnetRequest{URI: string(trimmed)}, nil
	}
	return req, json.Unmarshal(body, &req)
}
