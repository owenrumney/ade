package launcher

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/owenrumney/ade/internal/runlog"
	"github.com/owenrumney/ade/internal/sessions"
)

// maxTags and maxTag bound what one session carries: a label, not a document.
const (
	maxTags = 12
	maxTag  = 40
)

// recordOf is where a listed session keeps its record. Off the scan, so a
// request cannot name a file.
func (s *Server) recordOf(id string) (string, bool) {
	id = strings.TrimSpace(id)
	repos, err := s.repos()
	if id == "" || err != nil {
		return "", false
	}
	for _, repo := range repos {
		for _, dir := range repo.Checkouts {
			if _, ok := sessions.Find(dir.Sessions, dir.Path, id); ok {
				return runlog.Path(sessions.GitDir(dir.Path)), true
			}
		}
	}
	return "", false
}

func (s *Server) handleSessionTags(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string   `json:"id"`
		Tags []string `json:"tags"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	path, ok := s.recordOf(req.ID)
	if !ok {
		writeError(w, http.StatusBadRequest, "that is not one of this machine's sessions")
		return
	}
	tags := cleanTags(req.Tags)
	if err := runlog.Update(path, func(held *runlog.Record) { held.Tags = tags }); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.Broadcast("repos", nil)
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

func cleanTags(in []string) []string {
	out, seen := []string{}, map[string]bool{}
	for _, tag := range in {
		tag = strings.TrimSpace(tag)
		if tag == "" || len(tag) > maxTag || seen[tag] || len(out) == maxTags {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	return out
}

// dropRecords removes the record of each listed session and counts them.
func (s *Server) dropRecords(ids []string) (int, error) {
	removed := 0
	for _, id := range ids {
		path, ok := s.recordOf(id)
		if !ok {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
