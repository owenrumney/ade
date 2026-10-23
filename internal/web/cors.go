package web

import (
	"net/http"
	"strings"
)

// The schemes a webview of ade's own calls itself by. The scheme is the
// whole rule: the rest is a per-window id nobody can enumerate.
const (
	webviewOrigin = "vscode-webview://"
	appOrigin     = "ade-webview://"
)

// editorOrigin reports whether an Origin is a webview ade put the page
// in. Neither scheme is reachable from a browser.
func editorOrigin(origin string) bool {
	return strings.HasPrefix(origin, webviewOrigin) ||
		strings.HasPrefix(origin, appOrigin)
}

// allowEditor lets a webview read a reply it was already allowed. The token is
// still the whole of the authorisation; this widens nothing.
func allowEditor(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if !editorOrigin(origin) {
		return
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", origin)
	h.Set("Access-Control-Allow-Headers", "X-Ade-Token, Content-Type")
	h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	h.Set("Access-Control-Max-Age", "600")
	// The origin decides the reply, so a cache keyed on the URL alone would
	// hand one window's headers to another.
	h.Add("Vary", "Origin")
}

// handlePreflight answers the OPTIONS before a request carrying a token header.
// Outside the guard, because a preflight never carries one.
func (s *Server) handlePreflight(w http.ResponseWriter, r *http.Request) {
	allowEditor(w, r)
	if !editorOrigin(r.Header.Get("Origin")) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
