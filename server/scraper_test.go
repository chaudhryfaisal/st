package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jpillora/scraper/scraper"
)

func loadScraperConfig(t *testing.T) *scraper.Handler {
	t.Helper()
	data, err := os.ReadFile("default-scraper-config.json")
	if err != nil {
		t.Fatalf("failed to read scraper config: %v", err)
	}
	h := &scraper.Handler{
		Log: true,
	}
	if err := h.LoadConfig(data); err != nil {
		t.Fatalf("failed to load scraper config: %v", err)
	}
	return h
}

func scrapeWithTimeout(t *testing.T, endpoint *scraper.Endpoint, params map[string]string) ([]scraper.Result, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	type result struct {
		res []scraper.Result
		err error
	}
	done := make(chan result, 1)
	go func() {
		res, err := endpoint.Execute(params)
		done <- result{res: res, err: err}
	}()

	select {
	case r := <-done:
		return r.res, r.err
	case <-ctx.Done():
		return nil, fmt.Errorf("scraper timeout after 15s")
	}
}

func TestScrapers(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live scraper tests in short mode")
	}
	h := loadScraperConfig(t)

	for id, endpoint := range h.Config {
		endpoint := endpoint
		id := id
		t.Run(id, func(t *testing.T) {
			params := make(map[string]string)
			switch id {
			case "eztv":
				params["query"] = "house of the dragon"
			case "nyaa":
				params["query"] = "house of the dragon"
			case "thepiratebay":
				params["query"] = "house of the dragon"
			case "uindex":
				params["query"] = "house of the dragon"
			}

			if endpoint.FlareSolverrURL == "" {
				if fsURL := os.Getenv("FLARESOLVERR_URL"); fsURL != "" {
					endpoint.FlareSolverrURL = fsURL
				}
			}

			res, err := scrapeWithTimeout(t, endpoint, params)
			if err != nil {
				if endpoint.FlareSolverrURL != "" && (strings.Contains(err.Error(), "flaresolverr") || strings.Contains(err.Error(), "timeout")) {
					t.Logf("FlareSolverr failed for %s: %v", id, err)
					return
				}
				if !isNetworkError(err) {
					t.Fatalf("unexpected error for %s: %v", id, err)
				}
				t.Logf("network error for %s (site may be down): %v", id, err)
				return
			}
			if len(res) == 0 {
				t.Logf("no results for %s (site may have changed HTML structure)", id)
				return
			}
			t.Logf("%s: got %d results", id, len(res))
		})
	}
}

func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "EOF") ||
		strings.Contains(errStr, "context deadline exceeded") ||
		strings.Contains(errStr, "network is down") ||
		strings.Contains(errStr, "Temporary failure in name resolution")
}

func TestFlareSolverrIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping FlareSolverr test in short mode")
	}
	fsURL := os.Getenv("FLARESOLVERR_URL")
	if fsURL == "" {
		fsURL = "http://127.0.0.1:8191"
	}
	h := loadScraperConfig(t)
	for _, endpoint := range h.Config {
		endpoint := endpoint
		endpoint.FlareSolverrURL = fsURL
		endpoint.Client = &http.Client{
			Timeout: 20 * time.Second,
		}
	}

	flaresolverrTests := map[string]struct {
		params map[string]string
	}{
		"eztv": {
			params: map[string]string{"query": "ubuntu"},
		},
		"nyaa": {
			params: map[string]string{"query": "ubuntu"},
		},
	}

	for id, tc := range flaresolverrTests {
		endpoint := h.Config[id]
		if endpoint == nil {
			t.Skipf("endpoint %s not found", id)
			continue
		}
		t.Run(id, func(t *testing.T) {
			res, err := scrapeWithTimeout(t, endpoint, tc.params)
			if err != nil {
				if strings.Contains(err.Error(), "flaresolverr error: Method not allowed") ||
					strings.Contains(err.Error(), "flaresolverr error: Post") ||
					strings.Contains(err.Error(), "connection refused") ||
					strings.Contains(err.Error(), "no such host") {
					t.Skipf("FlareSolverr not available at %s: %v", fsURL, err)
				}
				if strings.Contains(err.Error(), "flaresolverr error") {
					t.Skipf("FlareSolverr error: %v", err)
				}
				t.Fatalf("FlareSolverr scrape failed for %s: %v", id, err)
			}
			if len(res) == 0 {
				t.Logf("no results for %s with FlareSolverr", id)
				return
			}
			t.Logf("%s with FlareSolverr: got %d results", id, len(res))
		})
	}
}

func startFlareSolverrMock(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := map[string]interface{}{
			"solution": map[string]interface{}{
				"cookies": []map[string]string{
					{"name": "session", "value": "st"},
				},
				"userAgent": "MockFlareSolverr/1.0",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	return "http://" + ln.Addr().String(), func() {
		srv.Close()
		ln.Close()
	}
}

func TestFlareSolverrMock(t *testing.T) {
	mockURL, cleanup := startFlareSolverrMock(t)
	defer cleanup()

	h := loadScraperConfig(t)
	endpoint := h.Config["eztv"]
	if endpoint == nil {
		t.Fatal("eztv endpoint not found")
	}
	endpoint.FlareSolverrURL = mockURL
	endpoint.Client = &http.Client{Timeout: 5 * time.Second}

	res, err := endpoint.Execute(map[string]string{"query": "ubuntu"})
	if err != nil {
		// The mock returns cookies but the actual target site may fail; that's okay.
		t.Logf("expected error after mock FlareSolverr: %v", err)
		return
	}
	t.Logf("got %d results", len(res))
}

func TestScraperServerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping server integration test in short mode")
	}
	h := loadScraperConfig(t)

	srv := &http.Server{Handler: h}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	go srv.Serve(listener)
	defer srv.Close()

	client := &http.Client{Timeout: 15 * time.Second}
	for id := range h.Config {
		endpoint := h.Config[id]
		url := fmt.Sprintf("http://%s/%s?query=ubuntu", listener.Addr().String(), id)
		if endpoint.FlareSolverrURL != "" {
			endpoint.FlareSolverrURL = ""
		}

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			t.Logf("skipping %s due to request creation error: %v", id, err)
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Logf("network error for %s: %v", id, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Logf("%s returned status %d: %s", id, resp.StatusCode, string(body))
			continue
		}
		t.Logf("%s returned status %d", id, resp.StatusCode)
	}
}

func TestScraperConfigValidation(t *testing.T) {
	data, err := os.ReadFile("default-scraper-config.json")
	if err != nil {
		t.Fatalf("failed to read scraper config: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("scraper config is empty")
	}
	for id, v := range raw {
		m, ok := v.(map[string]interface{})
		if !ok {
			t.Fatalf("endpoint %s is not an object", id)
		}
		url, ok := m["url"].(string)
		if !ok || url == "" {
			t.Fatalf("endpoint %s missing url", id)
		}
		if !strings.Contains(url, "{{") && !strings.Contains(url, "http") {
			t.Logf("endpoint %s url looks unusual: %s", id, url)
		}
		_, hasResult := m["result"]
		if !hasResult {
			t.Fatalf("endpoint %s missing result", id)
		}
	}
}

func TestScraperFlareSolverrConfigPropagation(t *testing.T) {
	data, err := os.ReadFile("default-scraper-config.json")
	if err != nil {
		t.Fatalf("failed to read scraper config: %v", err)
	}
	fsURL := "http://127.0.0.1:8191"
	h := &scraper.Handler{FlareSolverrURL: fsURL}
	if err := h.LoadConfig(data); err != nil {
		t.Fatalf("failed to load scraper config: %v", err)
	}
	for id, endpoint := range h.Config {
		if endpoint.FlareSolverrURL != fsURL {
			t.Errorf("endpoint %s did not inherit FlareSolverrURL", id)
		}
	}
}
