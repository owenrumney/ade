package launcher

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/diskuse"
	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
)

// diskShown is how many places are named; a longer list stops answering.
const diskShown = 5

// diskTTL is how long a reading is good for; this moves over days.
const diskTTL = 10 * time.Minute

// diskResponse is where ade's disk has gone, biggest first.
type diskResponse struct {
	Total  int64           `json:"total"`
	Stores []diskuse.Store `json:"stores"`
	At     time.Time       `json:"at"`
}

type diskCache struct {
	mu  sync.Mutex
	at  time.Time
	out diskResponse
}

func (s *Server) handleDisk(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.disk.read(s.diskPlaces))
}

// read is the last reading while it is warm, and a fresh walk otherwise.
func (c *diskCache) read(places func() []diskuse.Store) diskResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < diskTTL {
		return c.out
	}
	measured := diskuse.MeasureAll(places())
	// Every place, not just the shown ones: this is what ade really costs.
	out := diskResponse{At: time.Now(), Total: diskuse.Total(measured), Stores: measured}
	if len(out.Stores) > diskShown {
		out.Stores = out.Stores[:diskShown]
	}
	c.at, c.out = out.At, out
	return out
}

// diskPlaces is everywhere ade writes. A review worktree sits inside a
// checkout's ade directory and is counted with it, which is the point.
func (s *Server) diskPlaces() []diskuse.Store {
	var out []diskuse.Store
	if dir, err := settings.Dir(); err == nil {
		out = append(out, diskuse.Store{Label: "ade itself", Path: dir})
	}
	repos, err := s.repos()
	if err != nil {
		return out
	}
	seen := map[string]bool{}
	for _, repo := range repos {
		for _, checkout := range repo.Checkouts {
			path := diskuse.AdeDir(checkout.Path)
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			out = append(out, diskuse.Store{Label: label(repo, checkout), Path: path})
		}
	}
	return out
}

// label names a checkout the way the session list does.
func label(repo sessions.Repo, checkout sessions.Dir) string {
	if checkout.Branch != "" && checkout.Path != repo.Path {
		return repo.Name + " · " + checkout.Branch
	}
	return repo.Name
}

// diskDetail is one place, and what can be emptied inside it.
type diskDetail struct {
	diskuse.Store

	// CachePath is the build cache when this place holds it. Kept for a
	// browser that has not reloaded since Purgeable arrived.
	CachePath string `json:"cachePath,omitempty"`

	Purgeable []purgeable `json:"purgeable,omitempty"`
}

// purgeable is a tree that can be emptied on its own. A cache rebuilds
// itself; an attachment is the only copy, so the two are offered differently.
type purgeable struct {
	Label string `json:"label"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Files int    `json:"files"`

	// Recoverable marks a tree that comes back on its own. A false here is
	// what asks the user twice.
	Recoverable bool `json:"recoverable"`
}

// handleDiskDetail is one place's whole breakdown, walked fresh. The path is
// matched against what diskPlaces offered rather than trusted.
func (s *Server) handleDiskDetail(w http.ResponseWriter, r *http.Request) {
	want := r.URL.Query().Get("path")
	for _, place := range s.diskPlaces() {
		if place.Path == want {
			out := diskDetail{Store: diskuse.Detail(place.Label, place.Path), Purgeable: s.purgeable(place)}
			if cache, err := buildCache(); err == nil && filepath.Dir(cache) == place.Path {
				out.CachePath = cache
			}
			writeJSON(w, http.StatusOK, out)
			return
		}
	}
	writeError(w, http.StatusBadRequest, "that is not one of the places ade writes")
}

// purgeable is what can be emptied inside one place: the build cache when it
// lives there, and the attachments of any checkout that has some.
func (s *Server) purgeable(place diskuse.Store) []purgeable {
	var out []purgeable
	if cache, err := buildCache(); err == nil && filepath.Dir(cache) == place.Path {
		if got := diskuse.Measure("cache", cache); got.Bytes > 0 {
			out = append(out, purgeable{
				Label: "sandbox build cache", Path: cache,
				Bytes: got.Bytes, Files: count(cache), Recoverable: true,
			})
		}
	}
	// place.Path is a checkout's ade directory, where attachments sit.
	dir := filepath.Join(place.Path, "attachments")
	if got := diskuse.Measure("attachments", dir); got.Bytes > 0 {
		out = append(out, purgeable{
			Label: "message attachments", Path: dir,
			Bytes: got.Bytes, Files: count(dir), Recoverable: false,
		})
	}
	return out
}

// buildCache is the sandbox build cache, which rebuilds itself.
func buildCache() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return sandbox.Cache(home), nil
}

// handleDiskPurge empties one tree a detail offered, matched against that
// list rather than trusted. With no path it empties the build cache.
func (s *Server) handleDiskPurge(w http.ResponseWriter, r *http.Request) {
	cache, err := buildCache()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	want := r.URL.Query().Get("path")
	// Always emptiable, offered or not: an empty cache is not listed.
	tree := purgeable{Label: "sandbox build cache", Path: cache, Recoverable: true}
	if want != "" && want != cache {
		found, ok := s.offered(want)
		if !ok {
			writeError(w, http.StatusBadRequest, "that is not one of the things ade can empty")
			return
		}
		tree = found
	} else {
		tree.Bytes = diskuse.Measure("cache", cache).Bytes
		tree.Files = count(cache)
	}
	// The cache is the container's, so only a sandboxed run holds it. An
	// attachment is read by whatever turn was sent it, sandboxed or not.
	if tree.Recoverable {
		if busy, where := s.sandboxRunning(r.Context()); busy {
			writeError(w, http.StatusConflict, "a sandboxed run is going in "+where+"; wait for it to finish")
			return
		}
	} else if busy, where := s.runningIn(r.Context(), tree.Path); busy {
		writeError(w, http.StatusConflict, "a run is going in "+where+"; wait for it to finish")
		return
	}
	if err := removeAll(tree.Path); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.disk.forget()
	writeJSON(w, http.StatusOK, map[string]any{"freed": tree.Bytes, "files": tree.Files, "label": tree.Label})
}

// offered finds a path among what the details offer, so a purge cannot name
// a directory ade never proposed.
func (s *Server) offered(want string) (purgeable, bool) {
	for _, place := range s.diskPlaces() {
		for _, tree := range s.purgeable(place) {
			if tree.Path == want {
				return tree, true
			}
		}
	}
	return purgeable{}, false
}

// count is how many files a tree holds, for a purge that has to say what goes.
func count(root string) int {
	var files int
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files++
		}
		return nil //nolint:nilerr // an unreadable entry is skipped, not fatal
	})
	return files
}

func removeAll(root string) error {
	makeWritable(root)
	return os.RemoveAll(root)
}

// makeWritable lets each directory's entries be unlinked. Best effort: one
// that will not budge fails at the remove instead, naming itself.
func makeWritable(root string) {
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() {
			_ = os.Chmod(path, 0o700)
		}
		return nil
	})
}

func (c *diskCache) forget() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = time.Time{}
}

// runningIn is whether the checkout that owns this tree has a run in flight.
// A turn was told the paths of what was attached to it and reads them itself.
func (s *Server) runningIn(ctx context.Context, tree string) (bool, string) {
	repos, err := s.repos()
	if err != nil {
		return true, "an unreadable checkout"
	}
	for _, repo := range sessions.Graph(ctx, repos, s.probe) {
		for _, dir := range repo.Checkouts {
			if diskuse.AdeDir(dir.Path) != filepath.Dir(tree) {
				continue
			}
			if dir.Server != nil && dir.Server.Agent != nil && dir.Server.Agent.Running {
				return true, dir.Name
			}
		}
	}
	return false, ""
}

// sandboxRunning is whether a containerised run is reading the cache now.
func (s *Server) sandboxRunning(ctx context.Context) (bool, string) {
	repos, err := s.repos()
	if err != nil {
		// Unable to tell is not permission to delete.
		return true, "an unreadable checkout"
	}
	graph := sessions.Graph(ctx, repos, s.probe)
	if busy, where := sandboxBusy(graph); busy {
		return true, where
	}
	return silentServer(graph)
}

// silentServer is a checkout whose recorded ade is still running but did not
// answer: a server deep in a sandboxed build is the slow one to answer.
func silentServer(graph []sessions.GraphRepo) (bool, string) {
	for _, repo := range graph {
		for _, dir := range repo.Checkouts {
			if dir.Server != nil {
				continue
			}
			rec, ok := session.Read(session.Path(sessions.GitDir(dir.Path)))
			if ok && session.Running(rec.PID) {
				return true, dir.Name + ", which did not answer"
			}
		}
	}
	return false, ""
}

// sandboxBusy is the rule: any checkout with a containerised run in flight.
func sandboxBusy(graph []sessions.GraphRepo) (bool, string) {
	for _, repo := range graph {
		for _, dir := range repo.Checkouts {
			if dir.Server == nil || dir.Server.Agent == nil {
				continue
			}
			if agent := dir.Server.Agent; agent.Running && agent.Sandbox {
				return true, dir.Name
			}
		}
	}
	return false, ""
}
