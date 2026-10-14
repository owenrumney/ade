package web

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/owenrumney/ade/internal/gitdiff"
)

// handleBlame is the commit one line of one file last came from. The path is
// repository-relative: IsLocal refuses an absolute one and anything climbing out.
func (s *Server) handleBlame(w http.ResponseWriter, r *http.Request) {
	ws := s.space()
	if ws.snapshot {
		writeError(w, http.StatusBadRequest, "this is a fetched snapshot, so there is no checkout to blame in")
		return
	}
	path, ok := askedFile(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "path is a file in this checkout")
		return
	}
	line, err := strconv.Atoi(r.URL.Query().Get("line"))
	if err != nil || line < 1 {
		writeError(w, http.StatusBadRequest, "line is the 1-based line to blame")
		return
	}

	blame, err := gitdiff.BlameLine(r.Context(), ws.homeRoot, path, line)
	if err != nil {
		writeError(w, http.StatusBadRequest, "git has no blame for that line")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sha":       blame.SHA,
		"short":     blame.Short,
		"summary":   blame.Summary,
		"author":    blame.Author,
		"at":        blame.At,
		"committed": blame.Committed(),
	})
}

// handleHistory is the commits that touched one file, through its renames.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	ws := s.space()
	if ws.snapshot {
		writeError(w, http.StatusBadRequest, "this is a fetched snapshot, so there is no checkout to read history in")
		return
	}
	path, ok := askedFile(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "path is a file in this checkout")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	past, err := gitdiff.History(r.Context(), ws.homeRoot, path, limit)
	if err != nil {
		writeError(w, http.StatusBadRequest, "git has no history for that file")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"commits": past})
}

// handleFileAt is a file as one commit left it, for a read-only editor.
func (s *Server) handleFileAt(w http.ResponseWriter, r *http.Request) {
	ws := s.space()
	if ws.snapshot {
		writeError(w, http.StatusBadRequest, "this is a fetched snapshot, so there is no checkout to read from")
		return
	}
	path, ok := askedFile(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "path is a file in this checkout")
		return
	}
	sha := r.URL.Query().Get("sha")
	if !validSHA(sha) {
		writeError(w, http.StatusBadRequest, "sha is a commit in this repository")
		return
	}

	body, err := gitdiff.ShowAt(r.Context(), ws.homeRoot, sha, path)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("%s is not in %s", path, sha[:7]))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := io.WriteString(w, body); err != nil {
		slog.Debug("write file at revision", "err", err)
	}
}

// askedFile is the repository-relative path a request names. Cleaning first
// would turn an empty one into ".", which IsLocal then accepts.
func askedFile(r *http.Request) (string, bool) {
	asked := r.URL.Query().Get("path")
	if strings.TrimSpace(asked) == "" {
		return "", false
	}
	// "." is local and is the checkout itself, which git reads as every file.
	path := filepath.Clean(asked)
	if path == "." || !filepath.IsLocal(path) {
		return "", false
	}
	return path, true
}

// validSHA keeps a ref out of the argument: git resolves `HEAD@{1}` and
// `main^` too, and this reads what a history row answered, which is a SHA.
func validSHA(sha string) bool {
	if len(sha) < 7 || len(sha) > 40 {
		return false
	}
	for _, c := range sha {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}
