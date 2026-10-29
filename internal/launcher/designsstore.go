package launcher

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/designs"
	"github.com/owenrumney/ade/internal/docs"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/sessions"
)

// storeRow is one design as the tree draws it: the store's own facts, plus
// what this machine knows about work going on against it.
type storeRow struct {
	designs.Design
	// Repo is the first of the design's repositories, which is home.
	Repo string `json:"repo,omitempty"`
	// Here is the checkouts on this machine for its repositories, in order,
	// and Missing the ones it has none for.
	Here    []string `json:"here,omitempty"`
	Missing []string `json:"missing,omitempty"`

	Session string `json:"session,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Live    bool   `json:"live,omitempty"`
}

// handleStoreDesigns lists the shared store. Read off disk, so it answers
// whether or not the remote can be reached.
func (s *Server) handleStoreDesigns(w http.ResponseWriter, r *http.Request) {
	if s.designs == nil {
		writeError(w, http.StatusInternalServerError, "no designs store on this machine")
		return
	}
	list, err := s.designs.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	repos, _ := s.repos()
	out := make([]storeRow, 0, len(list))
	for _, design := range list {
		out = append(out, storeRowFor(design, repos))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"designs": out,
		"root":    s.designs.Root(),
		"shared":  s.designs.Shared(),
	})
}

// storeRowFor says where a design could be worked on here, and whether
// anything is on it. A session is matched by branch, as ade create names it.
func storeRowFor(design designs.Design, repos []sessions.Repo) storeRow {
	row := storeRow{Design: design}
	if len(design.Meta.Repos) > 0 {
		row.Repo = design.Meta.Repos[0]
	}
	for _, want := range design.Meta.Repos {
		if main, ok := checkoutNamed(repos, want); ok {
			row.Here = append(row.Here, main)
			continue
		}
		row.Missing = append(row.Missing, want)
	}
	for _, repo := range repos {
		for _, checkout := range repo.Checkouts {
			if !branchFor(checkout.Branch, design.Name) {
				continue
			}
			for _, session := range checkout.Sessions {
				if session.Archived {
					continue
				}
				row.Session, row.Branch = session.ID, checkout.Branch
				row.Live = row.Live || session.Live
			}
		}
	}
	return row
}

// branchFor is the branch ade create names for a design: the design's own
// name, with or without a prefix such as feat/.
func branchFor(branch, name string) bool {
	if branch == "" || name == "" {
		return false
	}
	return branch == name || strings.HasSuffix(branch, "/"+name)
}

// checkoutNamed resolves owner/name to the main checkout on this machine.
// The owner is matched when it is known and ignored when the scan has none.
func checkoutNamed(repos []sessions.Repo, nameWithOwner string) (string, bool) {
	owner, name, ok := strings.Cut(strings.TrimSpace(nameWithOwner), "/")
	if !ok {
		owner, name = "", strings.TrimSpace(nameWithOwner)
	}
	if name == "" {
		return "", false
	}
	loose := ""
	for _, repo := range repos {
		if !strings.EqualFold(repo.Name, name) {
			continue
		}
		main := mainOf(repo)
		if main == "" {
			continue
		}
		if owner == "" || strings.EqualFold(repo.Owner, owner) {
			return main, true
		}
		if loose == "" {
			loose = main
		}
	}
	return loose, loose != ""
}

// mainOf is the repository's main checkout, which is where a worktree is
// made beside and where its documents used to live.
func mainOf(repo sessions.Repo) string {
	for _, checkout := range repo.Checkouts {
		if !checkout.Worktree {
			return checkout.Path
		}
	}
	if len(repo.Checkouts) > 0 {
		return cmp.Or(repo.Checkouts[0].Main, repo.Checkouts[0].Path)
	}
	return ""
}

// handleDesignRead is one design's markdown, by name. The store is ade's
// own directory, so the name is a slug and never a path from a request.
func (s *Server) handleDesignRead(w http.ResponseWriter, r *http.Request) {
	if s.designs == nil {
		writeError(w, http.StatusInternalServerError, "no designs store on this machine")
		return
	}
	design, err := s.designs.Read(r.URL.Query().Get("name"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	body, err := os.ReadFile(design.File) // #nosec G304 -- the store's own file, found by slug
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": design.Name, "body": string(body), "meta": design.Meta,
	})
}

// handleDesignWrite puts a document back. The editor sends it whole,
// front matter and all, and the store parses the matter off again.
func (s *Server) handleDesignWrite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Body string `json:"body"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if s.designs == nil {
		writeError(w, http.StatusInternalServerError, "no designs store on this machine")
		return
	}
	// The name has to be one the store already has: this writes a
	// document, and making one is `ade designs import`.
	held, err := s.designs.Read(req.Name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	meta, body, err := designs.Split([]byte(req.Body))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.designs.Save(held.Name, meta, body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.Broadcast("designs", nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleDesignsSync brings the store level with its remote by hand, for when
// you do not want to wait for the sweep.
func (s *Server) handleDesignsSync(w http.ResponseWriter, r *http.Request) {
	if s.designs == nil {
		writeError(w, http.StatusInternalServerError, "no designs store on this machine")
		return
	}
	if err := s.designs.Sync(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDesignImport puts one of a checkout's documents into the store and
// answers its name. Only the store's documents can start.
func (s *Server) handleDesignImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File string `json:"file"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.File == "" {
		writeError(w, http.StatusBadRequest, "file is required")
		return
	}
	if s.designs == nil {
		writeError(w, http.StatusInternalServerError, "no designs store on this machine")
		return
	}
	root, common, ok := s.designsDir(w, r)
	if !ok {
		return
	}
	// Already there under that name: a second copy is not what was asked.
	name := designs.Slug(strings.TrimSuffix(req.File, ".md"))
	if held, err := s.designs.Read(name); err == nil {
		writeJSON(w, http.StatusOK, map[string]any{"name": held.Name, "held": true})
		return
	}
	var repo string
	if owner, named := gitdiff.Origin(r.Context(), root); named != "" {
		repo = strings.TrimPrefix(owner+"/"+named, "/")
	}
	for _, dir := range docs.SearchDirs(root, common, "") {
		path := filepath.Join(dir, req.File)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		took := designs.ImportFile(s.designs.Root(), path, repo)
		if !took.Took() {
			writeError(w, http.StatusBadRequest, took.Why)
			return
		}
		s.hub.Broadcast("designs", nil)
		writeJSON(w, http.StatusOK, map[string]any{"name": took.Name})
		return
	}
	writeError(w, http.StatusNotFound, req.File+" is not a document in that checkout")
}

// handleDesignStart begins work on a design: a worktree per repository it
// names, on a branch named for it, and one session across them.
func (s *Server) handleDesignStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Agent   string `json:"agent"`
		Model   string `json:"model"`
		Machine string `json:"machine,omitempty"`
		// Set by the machine that forwarded this, never by a person.
		Sync bool `json:"sync,omitempty"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if s.designs == nil {
		writeError(w, http.StatusInternalServerError, "no designs store on this machine")
		return
	}
	if req.Machine != "" {
		s.startDesignOnHost(w, r, req.Machine, req.Name, req.Agent, req.Model)
		return
	}
	// The clone may be behind the machine that forwarded this.
	if req.Sync && s.designs.Shared() {
		if err := s.designs.Sync(r.Context()); err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
	}
	design, err := s.designs.Read(req.Name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	repos, err := s.repos()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	home, also, err := placesFor(design, repos)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	url, path, err := sessions.CreateWork(r.Context(), home, sessions.Work{
		Name: design.Name, Agent: req.Agent, Model: req.Model,
		Also: also, Docs: design.Dir,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": url, "path": path, "also": also})
}

// startDesignOnHost begins the work on another machine.
func (s *Server) startDesignOnHost(w http.ResponseWriter, r *http.Request, machine, name, agent, model string) {
	live := s.connection(machine)
	if live == nil {
		writeError(w, http.StatusBadRequest, notConnectedTo(machine))
		return
	}
	if !s.designs.Shared() {
		writeError(w, http.StatusBadRequest,
			"this machine's designs store has no remote, so "+machine+" cannot read "+name+
				"; set designs_remote in ~/.ade/settings.json")
		return
	}
	// Push first: the far pull is the only way the design gets there.
	if err := s.designs.Sync(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	body, err := json.Marshal(map[string]any{
		"name": name, "agent": agent, "model": model, "sync": true,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	answer, err := sessions.AskFor(r.Context(), live.url, http.MethodPost, "/api/designs/start", string(body), openWait)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var got map[string]any
	if err := json.Unmarshal(answer, &got); err != nil {
		writeError(w, http.StatusBadGateway, machine+" did not answer about starting "+name)
		return
	}
	// The path is that machine's, so the caller opens a window over there.
	got["machine"] = machine
	writeJSON(w, http.StatusOK, got)
}

// placesFor is the checkouts a design's repositories resolve to here. One it
// cannot find is an error: starting in fewer than asked for says nothing.
func placesFor(design designs.Design, repos []sessions.Repo) (home string, also []string, err error) {
	if len(design.Meta.Repos) == 0 {
		return "", nil, fmt.Errorf("%s names no repositories; add repos: to its front matter", design.Name)
	}
	for i, want := range design.Meta.Repos {
		main, ok := checkoutNamed(repos, want)
		if !ok {
			return "", nil, fmt.Errorf("this machine has no checkout of %s", want)
		}
		if i == 0 {
			home = main
			continue
		}
		also = append(also, main)
	}
	return home, also, nil
}

// designsInterval: a design is written by a person, so minutes is the grain.
const designsInterval = 2 * time.Minute

// watchDesigns is how a design written on another machine turns up here.
func (s *Server) watchDesigns(ctx context.Context) {
	ticker := time.NewTicker(designsInterval)
	defer ticker.Stop()
	for {
		s.syncDesigns(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// syncDesigns is one at a time: a second waiter queues behind a stale fetch.
func (s *Server) syncDesigns(ctx context.Context) {
	if s.designs == nil || !s.designs.Shared() {
		return
	}
	if !s.designsSyncing.CompareAndSwap(false, true) {
		return
	}
	defer s.designsSyncing.Store(false)
	if err := s.designs.Sync(ctx); err != nil {
		slog.Debug("sync the designs store", "err", err)
	}
}
