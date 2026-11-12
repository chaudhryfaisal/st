package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeCtxPath(t *testing.T) {
	for in, want := range map[string]string{"": "", "/": "", "st": "/st", "/st/": "/st", "/a//b/": "/a/b"} {
		if got := normalizeCtxPath(in); got != want {
			t.Errorf("normalizeCtxPath(%q)=%q want %q", in, got, want)
		}
	}
}

func TestContextPathRouting(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("path=" + r.URL.Path)) })
	cases := []struct {
		ctx, url string
		code     int
		body     string
		location string
	}{
		{"/st", "/st/", 200, "path=/", ""},
		{"/st", "/st/api/magnet", 200, "path=/api/magnet", ""},
		{"/st", "/st/sync?x=1", 200, "path=/sync", ""},
		{"/st", "/st", 301, "", "/st/"},
		{"/st", "/st?a=b", 301, "", "/st/?a=b"},
		{"/st", "/api/magnet", 404, "", ""},
		{"/st", "/", 404, "", ""},
		{"/stuff", "/st/", 404, "", ""},
		{"", "/api/magnet", 200, "path=/api/magnet", ""},
		{"/", "/x", 200, "path=/x", ""},
	}
	for _, c := range cases {
		s := &Server{CtxPath: c.ctx}
		rec := httptest.NewRecorder()
		s.withContextPath(inner).ServeHTTP(rec, httptest.NewRequest("GET", c.url, nil))
		if rec.Code != c.code || (c.body != "" && rec.Body.String() != c.body) || rec.Header().Get("Location") != c.location {
			t.Errorf("ctx=%q GET %s => %d %q loc=%q; want %d %q loc=%q", c.ctx, c.url, rec.Code, rec.Body.String(), rec.Header().Get("Location"), c.code, c.body, c.location)
		}
	}
}
