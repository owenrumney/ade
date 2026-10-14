package web

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/jobs"
	"github.com/owenrumney/ade/internal/memory"
	"github.com/owenrumney/ade/internal/transcripts"
)

// projectStore is the project's memory, resolved once. The identity is a git
// remote, which does not change under a running server.
func (s *Server) projectStore() *memory.Store {
	s.rememberedOnce.Do(func() {
		id, err := memory.Resolve(s.ctxOrBackground(), s.startRoot)
		if err != nil {
			slog.Debug("resolve project identity", "root", s.startRoot, "err", err)
			return
		}
		store, err := memory.Open(id)
		if err != nil {
			slog.Debug("open project store", "slug", id.Slug, "err", err)
			return
		}
		s.remembered = store
		// The checkout is live now, so this is the moment its path can be tied
		// to the project. A worktree removed later is resolved from here.
		if err := memory.Record(s.startRoot, id); err != nil {
			slog.Debug("record checkout path", "root", s.startRoot, "err", err)
		}
	})
	return s.remembered
}

// projectPrompt is what this project adds to a run's system prompt. memory
// false is a run whose job is too narrow for the notes.
func (s *Server) projectPrompt(withMemory bool) string {
	store := s.projectStore()
	if store == nil {
		return ""
	}
	return store.Prompt(withMemory)
}

func (s *Server) ctxOrBackground() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

// ProjectView is what the Project modal reads.
type ProjectView struct {
	Slug         string             `json:"slug"`
	Name         string             `json:"name"`
	Remote       string             `json:"remote,omitempty"`
	Remembered   bool               `json:"remembered"`
	Instructions string             `json:"instructions"`
	MaxLength    int                `json:"maxLength"`
	Entries      []memory.Entry     `json:"entries"`
	Proposed     []memory.Candidate `json:"proposed"`

	// Unread is how far behind the project is: a stat per transcript, so this
	// costs nothing to show.
	Unread   int      `json:"unread"`
	Bytes    int64    `json:"bytes"`
	Siblings []string `json:"siblings,omitempty"`

	// Jobs is the scheduled work defined for this project. The launcher fires
	// them; this is where a repository's own are read.
	Jobs []jobs.View `json:"jobs"`
}

func (s *Server) handleProject(w http.ResponseWriter, r *http.Request) {
	store := s.projectStore()
	if store == nil {
		writeError(w, http.StatusNotFound, "this checkout is not in a git repository ade can identify")
		return
	}
	writeJSON(w, http.StatusOK, s.projectView(r.Context(), store))
}

func (s *Server) projectView(ctx context.Context, store *memory.Store) ProjectView {
	id := store.Identity()
	view := ProjectView{
		Slug: id.Slug, Name: id.Name(), Remote: id.Remote,
		Remembered:   store.Remembered(),
		Instructions: store.Instructions(),
		MaxLength:    memory.MaxInstructions,
		Entries:      store.Entries(),
		Proposed:     store.Proposed(),
	}
	if view.Entries == nil {
		view.Entries = []memory.Entry{}
	}
	if view.Proposed == nil {
		view.Proposed = []memory.Candidate{}
	}
	view.Jobs = s.projectJobs(id.Remote)
	if view.Remembered {
		sessions := transcripts.Scan()
		for _, u := range store.Outstanding(ctx, sessions) {
			view.Unread++
			view.Bytes += u.Bytes()
		}
		view.Siblings = memory.Siblings(id, sessions)
	}
	return view
}

// projectJobs is the jobs that would run in this project: the repository's
// own, and the user's that name it.
func (s *Server) projectJobs(remote string) []jobs.View {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	var mine []jobs.Job
	for _, job := range jobs.Load(s.space().homeRoot, home) {
		// A job that names a project belongs to that one, wherever it was
		// committed; one that names none belongs to the checkout it came from.
		if job.Project != "" {
			if remote != "" && job.Project == remote {
				mine = append(mine, job)
			}
			continue
		}
		if job.Scope == jobs.ScopeRepo {
			mine = append(mine, job)
		}
	}
	return jobs.Views(mine)
}

func (s *Server) handleProjectSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Instructions *string `json:"instructions"`
		Remembered   *bool   `json:"remembered"`
	}
	if !decode(w, r, &req) {
		return
	}
	store := s.projectStore()
	if store == nil {
		writeError(w, http.StatusNotFound, "no project to remember")
		return
	}
	if req.Remembered != nil {
		if err := store.Remember(*req.Remembered); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if req.Instructions != nil {
		if err := store.SetInstructions(*req.Instructions); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	s.spawn(func() { s.refreshDebt() })
	s.hub.Broadcast("project", nil)
	writeJSON(w, http.StatusOK, s.projectView(r.Context(), store))
}

func (s *Server) handleProjectEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action  string `json:"action"`
		ID      string `json:"id"`
		Heading string `json:"heading"`
		Body    string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	store := s.projectStore()
	if store == nil {
		writeError(w, http.StatusNotFound, "no project")
		return
	}

	var err error
	switch req.Action {
	case "accept":
		_, err = store.Accept(req.ID, req.Heading, req.Body)
	case "reject":
		err = store.Reject(req.ID)
	case "edit":
		err = store.Edit(req.ID, req.Heading, req.Body)
	case "forget":
		err = store.Forget(req.ID)
	default:
		writeError(w, http.StatusBadRequest, "unknown action "+req.Action)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.Broadcast("project", nil)
	writeJSON(w, http.StatusOK, s.projectView(r.Context(), store))
}

// withProject appends the project's instructions to an agent definition's own
// system prompt, for a provider that takes one string rather than appending.
func withProject(own, project string) string {
	if project == "" {
		return own
	}
	if own == "" {
		return project
	}
	return own + "\n\n" + project
}

// handleProjectDistil reads the next batch of unread transcripts and puts what
// it found in the review queue. Nothing is remembered without being accepted.
func (s *Server) handleProjectDistil(w http.ResponseWriter, r *http.Request) {
	store := s.projectStore()
	if store == nil {
		writeError(w, http.StatusNotFound, "no project")
		return
	}
	if !store.Remembered() {
		writeError(w, http.StatusBadRequest, "this project is not remembered")
		return
	}
	runCtx, ok := s.agent.begin(s.ctx, "distil")
	if !ok {
		writeError(w, http.StatusConflict, "the agent is already running")
		return
	}
	found, err := s.distil(runCtx, store)
	s.agent.finish(err)
	s.hub.Broadcast("agent", nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	s.spawn(func() { s.refreshDebt() })
	s.hub.Broadcast("project", nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"read":     found.read,
		"proposed": found.proposed,
		"project":  s.projectView(r.Context(), store),
	})
}

// distilled is what one run got through.
type distilled struct {
	read     int
	proposed int
}

// distil runs one batch. The watermark advances over everything read: a
// transcript with nothing to remember has still been looked at.
func (s *Server) distil(ctx context.Context, store *memory.Store) (distilled, error) {
	sessions := transcripts.Scan()
	batch, ok := store.Distil(store.Outstanding(ctx, sessions), sessions)
	if !ok {
		return distilled{}, nil
	}
	if batch.Prompt == "" {
		return distilled{read: len(batch.Read)}, store.MarkRead(batch.Read...)
	}

	turn, err := s.oneShot(ctx, bridge.OneShotOpts{
		Bin:          s.agent.bin,
		Model:        s.agent.model,
		Dir:          s.space().homeRoot,
		Prompt:       batch.Prompt,
		Tools:        memory.Tools(),
		AllowedTools: memory.Tools(),
		// Its whole input is in the prompt, so the MCP servers are startup
		// cost and nothing else.
		NoMCP: true,
	})
	if err != nil {
		return distilled{}, err
	}

	found, confirms, err := memory.Parse(turn.Text, store.Accepted())
	if err != nil {
		return distilled{}, err
	}
	if err := store.Confirm(confirms...); err != nil {
		slog.Debug("confirm entries", "err", err)
	}
	added, err := store.Propose(found)
	if err != nil {
		return distilled{}, err
	}
	if err := store.MarkRead(batch.Read...); err != nil {
		return distilled{}, err
	}
	return distilled{read: len(batch.Read), proposed: added}, nil
}

// ProjectDebt is the badge on the Project button: what is waiting to be
// reviewed, and how far behind distillation has fallen.
type ProjectDebt struct {
	Remembered bool  `json:"remembered"`
	Proposed   int   `json:"proposed"`
	Unread     int   `json:"unread"`
	Bytes      int64 `json:"bytes"`
}

// projectDebt is the last count, or nothing when the checkout has no project.
func (s *Server) projectDebt() *ProjectDebt {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.debt
}

// refreshDebt recounts what is outstanding. It scans transcripts, so it is
// called when a run settles rather than from the state poll.
func (s *Server) refreshDebt() {
	store := s.projectStore()
	if store == nil {
		return
	}
	debt := ProjectDebt{Remembered: store.Remembered(), Proposed: len(store.Proposed())}
	if debt.Remembered {
		for _, u := range store.Outstanding(s.ctxOrBackground(), transcripts.Scan()) {
			debt.Unread++
			debt.Bytes += u.Bytes()
		}
	}

	s.mu.Lock()
	was := s.debt
	s.debt = &debt
	s.mu.Unlock()

	if was == nil || *was != debt {
		s.hub.Broadcast("state", nil)
	}
}
