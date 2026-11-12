package server

import (
	"net/http"
	"net/url"
	"strings"
)

// normalizeCtxPath turns "st", "/st/" or "/a//b" into "/st" or "/a/b"; the root
// ("", "/") is "".
func normalizeCtxPath(p string) string {
	parts := make([]string, 0, 4)
	for _, part := range strings.Split(p, "/") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "/" + strings.Join(parts, "/")
}

// withContextPath serves h under the --ctx-path prefix. The prefix is stripped
// before routing, so every route (sync, API, downloads, SPA) works unchanged.
// The bare prefix redirects to the slash form so the SPA's relative URLs
// resolve beneath it. The frontend uses relative URLs, so the same build also
// works behind a proxy that strips the prefix, as long as the page URL ends in /.
func (s *Server) withContextPath(h http.Handler) http.Handler {
	prefix := normalizeCtxPath(s.CtxPath)
	if prefix == "" {
		return h
	}
	mux := http.NewServeMux()
	mux.Handle(prefix+"/", http.StripPrefix(prefix, h))
	mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
		target := url.URL{Path: prefix + "/", RawQuery: r.URL.RawQuery}
		http.Redirect(w, r, target.String(), http.StatusMovedPermanently)
	})
	return mux
}
