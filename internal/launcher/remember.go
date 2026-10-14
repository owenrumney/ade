package launcher

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/owenrumney/ade/internal/memory"
	"github.com/owenrumney/ade/internal/sessions"
)

// markRemembered fills in which repositories ade keeps a memory for. Only a
// checkout the ledger has never seen costs git: this runs on every list.
func (s *Server) markRemembered(repos []sessions.Repo) []sessions.Repo {
	var wg sync.WaitGroup
	for i, repo := range repos {
		id, known := memory.Lookup(repo.Path)
		if known {
			repos[i].Remembered = remembered(id)
			continue
		}
		wg.Go(func() {
			store, err := s.projectStore(repo.Path)
			if err != nil {
				return
			}
			repos[i].Remembered = store.Remembered()
		})
	}
	wg.Wait()
	return repos
}

func remembered(id memory.Identity) bool {
	store, err := memory.Open(id)
	if err != nil {
		return false
	}
	return store.Remembered()
}

// projectStore is the memory store for a checkout, resolving it through git and
// writing the answer to the ledger so the next list does not have to.
func (s *Server) projectStore(path string) (*memory.Store, error) {
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	id, err := memory.Resolve(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := memory.Record(path, id); err != nil {
		slog.Debug("record checkout path", "path", path, "err", err)
	}
	return memory.Open(id)
}

func (s *Server) handleRemember(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path       string `json:"path"`
		Remembered bool   `json:"remembered"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request body")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "no repository named")
		return
	}
	store, err := s.projectStore(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := store.Remember(req.Remembered); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.Broadcast("repos", nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"path": req.Path, "slug": store.Identity().Slug, "remembered": store.Remembered(),
	})
}
