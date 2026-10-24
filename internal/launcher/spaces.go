package launcher

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/spaces"
)

// Space is one repository kept open, with the designs written anywhere in it.
type Space struct {
	Repo     string   `json:"repo"`
	Owner    string   `json:"owner,omitempty"`
	Name     string   `json:"name,omitempty"`
	Path     string   `json:"path,omitempty"`
	Branch   string   `json:"branch,omitempty"`
	Sessions int      `json:"sessions"`
	Designs  []string `json:"designs,omitempty"`

	// Here is false for a space whose clone this machine has not got: listed
	// anyway, so it can be cloned rather than quietly forgotten.
	Here bool `json:"here"`

	// Current marks the repository the window is working in. It is a space
	// whether or not it was opened, and it is not one to close.
	Current bool `json:"current,omitempty"`
}

// handleSpaces lists the open spaces and what is offerable beside them. One
// read: the picker and the sidebar want the same two lists.
func (s *Server) handleSpaces(w http.ResponseWriter, r *http.Request) {
	cfg, err := settings.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	all, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	open := spaces.Clean(cfg.Spaces)
	// Being in a checkout is having its repository open. Shown, never
	// written: a repository glanced at is not one to remember.
	here := spaces.KeyOf(repoHolding(all, r.URL.Query().Get("dir")))
	want := spaces.Open(open, here)
	out := make([]Space, 0, len(want))
	// Only what is open. Within reads an empty set as every repository,
	// which is right for scoping a list and wrong for listing the set.
	if len(want) > 0 {
		for _, repo := range spaces.Within(all, want) {
			space := s.spaceOf(r.Context(), repo)
			space.Current = space.Repo == here && here != ""
			out = append(out, space)
		}
	}
	for _, missing := range spaces.Missing(all, open) {
		out = append(out, Space{Repo: missing})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"spaces":  out,
		"offered": offered(spaces.Offerable(all, open)),
	})
}

func (s *Server) spaceOf(ctx context.Context, repo sessions.Repo) Space {
	main := mainOf(repo)
	space := Space{
		Repo:     spaces.KeyOf(repo),
		Owner:    repo.Owner,
		Name:     repo.Name,
		Path:     main,
		Branch:   repo.Branch,
		Sessions: repo.Sessions,
		Here:     true,
	}
	space.Designs = designsAnywhereIn(ctx, repo, main)
	return space
}

// designsAnywhereIn is every design in the repository, whichever worktree
// wrote it: a design belongs to the repository, not to one checkout.
func designsAnywhereIn(ctx context.Context, repo sessions.Repo, main string) []string {
	if main == "" {
		return nil
	}
	// Every worktree shares the repository's git dir, so this is asked once
	// and not thirteen times for a repository with twelve worktrees.
	common, err := gitdiff.CommonGitDir(ctx, main)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	out := []string{}
	for _, root := range append([]string{main}, worktreesOf(repo)...) {
		list, err := designsIn(root, common)
		if err != nil {
			continue
		}
		for _, file := range list {
			if !seen[file] {
				seen[file] = true
				out = append(out, file)
			}
		}
	}
	sort.Strings(out)
	return out
}

func worktreesOf(repo sessions.Repo) []string {
	out := []string{}
	for _, checkout := range repo.Checkouts {
		if checkout.Worktree {
			out = append(out, checkout.Path)
		}
	}
	return out
}

type offer struct {
	Repo     string `json:"repo"`
	Sessions int    `json:"sessions"`
	Branch   string `json:"branch,omitempty"`
}

func offered(all []sessions.Repo) []offer {
	out := make([]offer, 0, len(all))
	for _, repo := range all {
		out = append(out, offer{Repo: spaces.KeyOf(repo), Sessions: repo.Sessions, Branch: repo.Branch})
	}
	return out
}

// handleSpaceOpen and handleSpaceClose write the list. Closing removes the
// name and nothing else: no checkout, no worktree, no session.
func (s *Server) handleSpaceOpen(w http.ResponseWriter, r *http.Request) {
	s.writeSpaces(w, r, spaces.Open)
}

func (s *Server) handleSpaceClose(w http.ResponseWriter, r *http.Request) {
	s.writeSpaces(w, r, spaces.Close)
}

func (s *Server) writeSpaces(w http.ResponseWriter, r *http.Request, apply func([]string, string) []string) {
	var req struct {
		Repo string `json:"repo"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(spaces.Clean([]string{req.Repo})) == 0 {
		writeError(w, http.StatusBadRequest, "repo must be owner/name")
		return
	}
	cfg, err := settings.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg.Spaces = apply(cfg.Spaces, req.Repo)
	if err := settings.Save(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"spaces": cfg.Spaces})
}

// cloneSpace is ghpr.Clone unless a test stands one in.
var cloneSpace = ghpr.Clone

// handleSpaceClone fetches a space this machine has not got. An open space
// with no checkout is listed with this as its act, not quietly dropped.
func (s *Server) handleSpaceClone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repo string `json:"repo"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	key := spaces.Clean([]string{req.Repo})
	if len(key) == 0 {
		writeError(w, http.StatusBadRequest, "repo must be owner/name")
		return
	}
	cfg, err := settings.Load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	roots := cfg.Roots()
	if len(roots) == 0 {
		writeError(w, http.StatusConflict,
			"there is no scan directory to clone into; add one in settings")
		return
	}
	owner, name, _ := strings.Cut(key[0], "/")
	dest := filepath.Join(roots[0], owner, name)
	if err := cloneSpace(r.Context(), req.Repo, dest); err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("clone %s: %v", req.Repo, err))
		return
	}
	// Cloning one is opening it: nobody fetches a repository to leave it out.
	cfg.Spaces = spaces.Open(cfg.Spaces, key[0])
	if err := settings.Save(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": dest, "spaces": cfg.Spaces})
}

// repoHolding is the repository a checkout belongs to, by path: the window
// names the directory it is on and the launcher knows whose it is.
func repoHolding(all []sessions.Repo, dir string) sessions.Repo {
	if dir == "" {
		return sessions.Repo{}
	}
	for _, repo := range all {
		for _, checkout := range repo.Checkouts {
			if checkout.Path == dir {
				return repo
			}
		}
	}
	return sessions.Repo{}
}
