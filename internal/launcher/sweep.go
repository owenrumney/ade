package launcher

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/worktrees"
)

// sweepTTL is how long a sweep stands. A scan runs several git commands per
// worktree, so the panel reads the last one unless Refresh asks otherwise.
const sweepTTL = 2 * time.Minute

// sweepWorkers bounds the fan-out. Twenty repositories of four worktrees is
// hundreds of processes if they all go at once.
const sweepWorkers = 8

// sweepDeadline is how long one repository gets. A repository on a stalled
// network mount is a row saying so, not a sweep that never answers.
const sweepDeadline = 30 * time.Second

// sweepRepo is one repository and the linked worktrees found in it. Owner is
// carried because two scan roots hold two repositories of the same name.
type sweepRepo struct {
	Path  string               `json:"path"`
	Name  string               `json:"name"`
	Owner string               `json:"owner,omitempty"`
	Why   string               `json:"why,omitempty"`
	Tree  []worktrees.Worktree `json:"worktrees"`
}

// sweepResponse is every worktree on this machine, grouped by repository.
type sweepResponse struct {
	Repos []sweepRepo `json:"repos"`
	At    time.Time   `json:"at"`
}

type sweepCache struct {
	mu   sync.Mutex
	at   time.Time
	out  sweepResponse
	gen  uint64
	busy sync.Mutex
}

// handleWorktreesAll sweeps every repository under the scan roots. Only the
// launcher can: an ade serving a checkout knows only its own.
func (s *Server) handleWorktreesAll(w http.ResponseWriter, r *http.Request) {
	asked := time.Now()
	fresh := r.URL.Query().Get("fresh") == "1"
	if out, ok := s.sweep.warm(); ok && !fresh {
		writeJSON(w, http.StatusOK, out)
		return
	}

	s.sweep.busy.Lock()
	defer s.sweep.busy.Unlock()
	// A sweep taken since this request arrived answers it, fresh or not: a
	// held-down Refresh is one scan and not one each.
	if out, ok := s.sweep.warm(); ok && (!fresh || out.At.After(asked)) {
		writeJSON(w, http.StatusOK, out)
		return
	}

	// Everything(), not Recent(): the pile-up is precisely the repositories
	// you have not touched in months.
	repos, err := s.reposIn(sessions.Everything())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gen := s.sweep.mark()
	out := sweepResponse{Repos: sweepAll(r.Context(), repos), At: time.Now()}
	// A sweep the client walked away from is every repository failing at once.
	// That is not a reading, and it must not stand for the TTL.
	if r.Context().Err() == nil {
		s.sweep.put(out, gen)
	}
	writeJSON(w, http.StatusOK, out)
}

// sweepAll scans each repository on a bounded pool, dropping the ones with no
// linked worktrees. A repository that fails is kept, carrying its reason.
func sweepAll(ctx context.Context, repos []sessions.Repo) []sweepRepo {
	found := make([]sweepRepo, len(repos))
	gate := make(chan struct{}, sweepWorkers)
	var wg sync.WaitGroup
	for i, repo := range repos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			gate <- struct{}{}
			defer func() { <-gate }()
			found[i] = scanOne(ctx, repo)
		}()
	}
	wg.Wait()

	out := []sweepRepo{}
	for _, row := range found {
		if row.Why == "" && len(row.Tree) == 0 {
			continue
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func scanOne(ctx context.Context, repo sessions.Repo) sweepRepo {
	ctx, cancel := context.WithTimeout(ctx, sweepDeadline)
	defer cancel()
	row := sweepRepo{Path: repo.Path, Name: repo.Name, Owner: repo.Owner, Tree: []worktrees.Worktree{}}
	found, err := worktrees.Scan(ctx, repo.Path, false)
	if err != nil {
		row.Why = err.Error()
		return row
	}
	row.Tree = append(row.Tree, found...)
	return row
}

func (c *sweepCache) warm() (sweepResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.at.IsZero() || time.Since(c.at) > sweepTTL {
		return sweepResponse{}, false
	}
	return c.out, true
}

// mark is the generation a scan starts under. A forget while it runs bumps
// the generation, and put then drops a result taken before the removal.
func (c *sweepCache) mark() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

func (c *sweepCache) put(out sweepResponse, gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return
	}
	c.at, c.out = out.At, out
}

// forget drops the reading, so a removal is not followed by a list still
// holding what went.
func (c *sweepCache) forget() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = time.Time{}
	c.gen++
}
