package web

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/owenrumney/ade/internal/gitdiff"
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
	writeJSON(w, http.StatusOK, map[string]any{"path": name, "source": string(body), "sum": sourceSum(body)})
}

// handleSaveSource writes one file the window has open. Only a file that
// is already there, and only if nobody changed it since it was read.
func (s *Server) handleSaveSource(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path   string `json:"path"`
		Source string `json:"source"`
		Was    string `json:"was"`
	}
	if !decode(w, r, &req) {
		return
	}
	ws := s.space()
	if ws.snapshot {
		writeError(w, http.StatusConflict, "this pull request is not checked out; there is nothing to save into")
		return
	}
	name := strings.TrimSpace(req.Path)
	if ws.homeRoot == "" || name == "" || inGitDir(name) {
		writeError(w, http.StatusBadRequest, "path is a file in this checkout")
		return
	}
	path, err := safepath.Inside(ws.homeRoot, name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "that is not a file in this checkout")
		return
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, http.StatusNotFound, "no such file")
		return
	}
	if len(req.Source) > maxSource {
		writeError(w, http.StatusRequestEntityTooLarge, "too large to save from here")
		return
	}
	now, err := os.ReadFile(path) // #nosec G304 -- resolved inside the checkout
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if sourceSum(now) != req.Was {
		writeError(w, http.StatusConflict, name+" changed on disk since it was opened; reopen it before saving")
		return
	}
	if err := replace(path, []byte(req.Source), info.Mode().Perm()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": name, "sum": sourceSum([]byte(req.Source))})
}

func sourceSum(body []byte) string {
	at := sha256.Sum256(body)
	return hex.EncodeToString(at[:])
}

func inGitDir(name string) bool {
	for _, part := range strings.Split(filepath.ToSlash(filepath.Clean(name)), "/") {
		if part == ".git" {
			return true
		}
	}
	return false
}

// replace writes beside the file and renames over it, so a failed write
// never leaves half a file.
func replace(path string, body []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".ade-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
