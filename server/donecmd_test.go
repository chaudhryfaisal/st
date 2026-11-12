package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jpillora/cloud-torrent/engine"
)

func TestParseTorrentRequest(t *testing.T) {
	cases := map[string]TorrentRequest{
		`{"action":"stop","infohash":"abc"}`: {Action: "stop", InfoHash: "abc"},
		"stop:abc":                           {Action: "stop", InfoHash: "abc"},
		"delete:abc\n":                       {Action: "delete", InfoHash: "abc"},
	}
	for in, want := range cases {
		got, err := parseTorrentRequest([]byte(in))
		if err != nil || got != want {
			t.Errorf("%q => %+v, %v want %+v", in, got, err, want)
		}
	}
	if _, err := parseTorrentRequest([]byte("garbage")); err == nil {
		t.Error("expected error for malformed body")
	}
}

func TestParseMagnetRequest(t *testing.T) {
	for _, in := range []string{`{"uri":"magnet:?xt=urn:btih:abc"}`, "magnet:?xt=urn:btih:abc\n"} {
		got, err := parseMagnetRequest([]byte(in))
		if err != nil || got.URI != "magnet:?xt=urn:btih:abc" {
			t.Errorf("%q => %+v, %v", in, got, err)
		}
	}
}

func TestRunDoneCmdEnvironment(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "env.txt")
	script := filepath.Join(dir, "done.sh")
	body := "#!/bin/sh\nenv | sed -n 's/^\\(CLD_.*\\)$/\\1/p' | sort > " + out + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &Server{DoneCmd: script, restAPIAddr: "127.0.0.1:9999", engine: engine.New()}
	started := time.Unix(1700000000, 0)
	s.runDoneCmd(engine.DoneEvent{Type: "file", InfoHash: "abc", Path: "show/a.mkv", Size: 42, StartedAt: started, FileCount: 3})
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	env := string(b)
	for _, want := range []string{"CLD_HASH=abc", "CLD_PATH=show/a.mkv", "CLD_TYPE=file", "CLD_SIZE=42", "CLD_STARTTS=1700000000", "CLD_FILENUM=3", "CLD_RESTAPI=127.0.0.1:9999", "CLD_DIR="} {
		if !strings.Contains(env, want) {
			t.Errorf("missing %s in:\n%s", want, env)
		}
	}
}
