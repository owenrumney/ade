package launcher

import (
	"cmp"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"

	"github.com/owenrumney/ade/internal/docs"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/sessions"
)

// handleDesigns lists a checkout's design documents, so a session can start
// on one already written. Read-only: it never makes the directory.
func (s *Server) handleDesigns(w http.ResponseWriter, r *http.Request) {
	root, common, ok := s.designsDir(w, r)
	if !ok {
		return
	}
	list, err := designsIn(root, common)
	if err != nil {
		// Nowhere on this screen may report an empty list where it means it
		// could not look.
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"designs": list})
}

// handleDesign reads one document, which is how a design picked here is
// carried to a session started somewhere else.
func (s *Server) handleDesign(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	if file == "" {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	root, common, ok := s.designsDir(w, r)
	if !ok {
		return
	}
	dirs := docs.SearchDirs(root, common, "")
	if len(dirs) == 0 {
		writeError(w, http.StatusNotFound, "that checkout keeps no design documents")
		return
	}
	// Every directory the list was drawn from, in the same order: a document
	// under the git dir is as real as one under .scratch.
	var err error
	for _, dir := range dirs {
		var body string
		if body, err = docs.ReadBounded(dir, file); err == nil {
			writeJSON(w, http.StatusOK, map[string]any{"file": file, "body": body})
			return
		}
		// Absent here means look in the next one; anything else is the answer.
		if !errors.Is(err, fs.ErrNotExist) {
			break
		}
	}
	if errors.Is(err, fs.ErrNotExist) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}

// designsDir resolves the checkout a document request is about, forwarding to
// another machine's launcher when it names one. ok is false when it answered.
func (s *Server) designsDir(w http.ResponseWriter, r *http.Request) (root, common string, ok bool) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		writeError(w, http.StatusBadRequest, "dir is required")
		return "", "", false
	}
	if machine := r.URL.Query().Get("machine"); machine != "" {
		s.designsOnHost(w, r, machine)
		return "", "", false
	}

	all, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return "", "", false
	}
	found, listed := sessions.FindCheckout(all, dir)
	if !listed {
		browse, isCheckout := browsed(dir)
		if !isCheckout {
			writeError(w, http.StatusBadRequest, "that is not one of the checkouts on this machine")
			return "", "", false
		}
		found = browse
	}
	// The main checkout's, because that is where a worktree is made beside and
	// the documents live in the git dir they share.
	root = cmp.Or(found.Main, found.Path)
	common, err = gitdiff.CommonGitDir(r.Context(), root)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", "", false
	}
	return root, common, true
}

// designsIn is every document in every directory ade would look in. A checkout
// with nowhere to keep them has none; a directory that cannot be read is said.
func designsIn(root, common string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, dir := range docs.SearchDirs(root, common, "") {
		list, err := docs.List(dir)
		if err != nil {
			return nil, err
		}
		for _, file := range list {
			if !seen[file] {
				seen[file] = true
				out = append(out, file)
			}
		}
	}
	return out, nil
}

// designsOnHost asks the machine the checkout is on. The same route and the
// same query, so listing and reading both forward with one function.
func (s *Server) designsOnHost(w http.ResponseWriter, r *http.Request, machine string) {
	live := s.connection(machine)
	if live == nil {
		writeError(w, http.StatusBadRequest, "not connected to "+machine)
		return
	}
	asked := r.URL.Query()
	asked.Del("machine")
	asked.Del("t")
	path := r.URL.Path + "?" + asked.Encode()
	answer, err := sessions.AskFor(r.Context(), live.url, http.MethodGet, path, "", openWait)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var body map[string]any
	if err := json.Unmarshal(answer, &body); err != nil {
		writeError(w, http.StatusBadGateway, machine+" did not answer about its documents")
		return
	}
	if list, ok := body["designs"]; ok && list == nil {
		body["designs"] = []string{}
	}
	writeJSON(w, http.StatusOK, body)
}
