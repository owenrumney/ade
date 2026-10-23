package web

import (
	"errors"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/lsp"
	"github.com/owenrumney/ade/internal/safepath"
)

// The checkout's own source, as a window shows it. Reading it off disk
// only works for a session on this machine, and one may be on another.

// Tracked and untracked both: a file you just wrote is the one you want,
// and .gitignore is honoured by construction.

// maxSource bounds one read. A file larger than this is not something
// to put in an editor pane over a proxy.
const maxSource = 4 << 20

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	root := s.space().homeRoot
	if root == "" {
		writeError(w, http.StatusConflict, "this session has no checkout")
		return
	}
	tracked, err := gitdiff.Tracked(r.Context(), root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	untracked, err := gitdiff.Untracked(r.Context(), root)
	if err != nil {
		// A repository with nothing new is the common case, and a
		// failure here is not a reason to show no files at all.
		untracked = nil
	}
	files := append(tracked, untracked...)
	sort.Strings(files)
	writeJSON(w, http.StatusOK, map[string]any{"root": root, "files": files})
}

// handleSource is one file of the checkout. The path is resolved inside
// it and nowhere else: it came from a page.
func (s *Server) handleSource(w http.ResponseWriter, r *http.Request) {
	root := s.space().homeRoot
	name := strings.TrimSpace(r.URL.Query().Get("path"))
	if root == "" || name == "" {
		writeError(w, http.StatusBadRequest, "path is a file in this checkout")
		return
	}
	path, err := safepath.Inside(root, name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "that is not a file in this checkout")
		return
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, http.StatusNotFound, "no such file")
		return
	}
	if info.Size() > maxSource {
		writeError(w, http.StatusRequestEntityTooLarge,
			"that file is "+strconv.FormatInt(info.Size()>>20, 10)+"MB; too large to open here")
		return
	}
	body, err := os.ReadFile(path) // #nosec G304 -- resolved inside the checkout
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": name, "source": string(body)})
}

// A file's history is `/api/history`, which was already here.

// handleDocFile serves a file beside the design documents. The editor
// resolves `![](thing.png)` against it, so it is a directory url.
func (s *Server) handleDocFile(w http.ResponseWriter, r *http.Request) {
	dir, _ := s.designPlace()
	name := r.PathValue("name")
	if dir == "" || name == "" {
		http.NotFound(w, r)
		return
	}
	path, err := safepath.Inside(dir, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Lstat before the open, so a link planted in the docs directory is
	// refused rather than followed out of it.
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, path)
}

// handleHover is what a language server says about the symbol under the
// cursor. The pool is this checkout's, started on the first ask.
func (s *Server) handleHover(w http.ResponseWriter, r *http.Request) {
	root := s.space().homeRoot
	name := strings.TrimSpace(r.URL.Query().Get("path"))
	if root == "" || name == "" {
		writeError(w, http.StatusBadRequest, "path is a file in this checkout")
		return
	}
	if _, err := safepath.Inside(root, name); err != nil {
		writeError(w, http.StatusBadRequest, "that is not a file in this checkout")
		return
	}
	line, _ := strconv.Atoi(r.URL.Query().Get("line"))
	col, _ := strconv.Atoi(r.URL.Query().Get("col"))
	said, err := s.language(root).Hover(r.Context(), name, line, col)
	if errors.Is(err, lsp.ErrNoServer) {
		// Not a failure: most files have no server, and the window asks
		// before it knows which do.
		writeJSON(w, http.StatusOK, map[string]any{"hover": ""})
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hover": said})
}

// handleDefinition is where the symbol under the cursor is defined, as
// a path in this checkout. Nothing found answers with nothing.
func (s *Server) handleDefinition(w http.ResponseWriter, r *http.Request) {
	root, name, line, col, ok := s.spotAsked(w, r)
	if !ok {
		return
	}
	found, err := s.language(root).Definition(r.Context(), name, line, col)
	if errors.Is(err, lsp.ErrNoServer) || found == nil {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, found)
}

// handleReferences is every use of the symbol under the cursor.
func (s *Server) handleReferences(w http.ResponseWriter, r *http.Request) {
	root, name, line, col, ok := s.spotAsked(w, r)
	if !ok {
		return
	}
	found, err := s.language(root).References(r.Context(), name, line, col)
	if errors.Is(err, lsp.ErrNoServer) {
		writeJSON(w, http.StatusOK, map[string]any{"spots": []lsp.Spot{}})
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"spots": found})
}

// spotAsked reads the file and position every language route takes, and
// answers the caller itself when they do not make sense.
func (s *Server) spotAsked(w http.ResponseWriter, r *http.Request) (root, name string, line, col int, ok bool) {
	root = s.space().homeRoot
	name = strings.TrimSpace(r.URL.Query().Get("path"))
	if root == "" || name == "" {
		writeError(w, http.StatusBadRequest, "path is a file in this checkout")
		return "", "", 0, 0, false
	}
	if _, err := safepath.Inside(root, name); err != nil {
		writeError(w, http.StatusBadRequest, "that is not a file in this checkout")
		return "", "", 0, 0, false
	}
	line, _ = strconv.Atoi(r.URL.Query().Get("line"))
	col, _ = strconv.Atoi(r.URL.Query().Get("col"))
	return root, name, line, col, true
}

// language is the checkout's pool of language servers, made once.
func (s *Server) language(root string) *lsp.Pool {
	s.lspOnce.Do(func() { s.lsp = lsp.New(root) })
	return s.lsp
}
