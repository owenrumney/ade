package launcher

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/jobs"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/transcripts"
	"github.com/owenrumney/ade/internal/wiki"
)

// distillAfter is how long a repository has to stay quiet before what was said
// in it is distilled. Longer than wiki.DistillQuiet, which refuses a transcript
// written more recently than that: a fire inside it would read nothing.
var distillAfter = wiki.DistillQuiet + 30*time.Second

// handleQuiet arms a distillation of the checkout's repository. A session
// sends it on going idle and on the way out, so the wait is held here, in the
// process that outlives the session rather than the one that is leaving.
func (s *Server) handleQuiet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir string `json:"dir"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Dir == "" {
		writeError(w, http.StatusBadRequest, "which checkout went quiet")
		return
	}
	owner, name := gitdiff.Origin(r.Context(), req.Dir)
	if owner == "" || name == "" {
		// Not an error the session can act on, and not worth a retry.
		writeJSON(w, http.StatusOK, map[string]bool{"armed": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"armed": s.armDistill(owner + "/" + name)})
}

// armDistill starts or restarts one repository's wait. Restarting is the
// point: a session still being worked in keeps pushing the fire out, and two
// sessions in the same repository share the one timer.
func (s *Server) armDistill(repo string) bool {
	if s.wiki == nil || !s.wiki.Enabled() {
		return false
	}
	if config, err := settings.Load(); err != nil || !config.WikiDistill {
		return false
	}
	s.quietMu.Lock()
	defer s.quietMu.Unlock()
	if timer, ok := s.quiet[repo]; ok {
		timer.Reset(distillAfter)
		return true
	}
	if s.quiet == nil {
		s.quiet = map[string]*time.Timer{}
	}
	s.quiet[repo] = time.AfterFunc(distillAfter, func() { s.distillQuiet(repo) })
	return true
}

// stopDistilling drops every pending fire, so a launcher going down does not
// leave a timer holding a model call it cannot finish.
func (s *Server) stopDistilling() {
	s.quietMu.Lock()
	defer s.quietMu.Unlock()
	for repo, timer := range s.quiet {
		timer.Stop()
		delete(s.quiet, repo)
	}
}

// distillQuiet runs one repository's distillation, once its wait has elapsed.
func (s *Server) distillQuiet(repo string) {
	s.quietMu.Lock()
	delete(s.quiet, repo)
	s.quietMu.Unlock()

	ctx, cancel := context.WithTimeout(s.ctx, jobs.DistillPerRepo)
	defer cancel()

	out := s.distillOne(ctx, repo)
	switch {
	case out.State == wikiConflict:
		// Another machine published while this draft was being written. The
		// draft is discarded and the watermark has not moved, so a second go
		// from the new base is the whole of the repair.
		slog.Info("the wiki moved under this distillation; trying again", "repo", repo)
		s.armDistill(repo)
	case out.Error != "":
		slog.Info("distilling a quiet repository failed", "repo", repo, "err", out.Error)
	case out.State == "published":
		slog.Info("distilled a quiet repository", "repo", repo, "pages", out.Pages, "commit", out.Commit)
	}
}

// wikiConflict is the state Submit reports when the rebase onto origin failed.
const wikiConflict = "conflict"

// distillOne is distillWiki narrowed to a single repository: the same store,
// spend gate and model, and only the target the session named.
func (s *Server) distillOne(ctx context.Context, repo string) wiki.DistillOutcome {
	store := s.distillStore()
	if store == nil || !store.Enabled() {
		return wiki.DistillOutcome{Repo: repo}
	}
	all := transcripts.Scan()
	// Before the spend gate: marking where the transcripts end costs nothing
	// and calls no model.
	if !store.DistillStarted() {
		if err := store.StartDistilling(transcriptPaths(all)); err != nil {
			return wiki.DistillOutcome{Repo: repo, Error: err.Error()}
		}
		return wiki.DistillOutcome{Repo: repo, State: "started"}
	}
	if ok, why := jobs.WithinSpend(s.limits.Snapshot(ctx)); !ok {
		slog.Debug("distillation held by spend", "repo", repo, "why", why)
		return wiki.DistillOutcome{Repo: repo}
	}
	repos, err := s.repos()
	if err != nil {
		return wiki.DistillOutcome{Repo: repo, Error: err.Error()}
	}
	model := s.jobs.distill
	if model == nil {
		dir, err := settings.Dir()
		if err != nil {
			return wiki.DistillOutcome{Repo: repo, Error: err.Error()}
		}
		model = jobs.DistillModel("", dir)
	}
	for _, target := range jobs.DistillTargets(ctx, repos, all) {
		if target.Repo != repo {
			continue
		}
		out, err := store.Distill(ctx, wiki.DistillRun{Repo: repo, Sessions: target.Sessions, Model: model})
		if err != nil && out.Error == "" {
			out.Error = err.Error()
		}
		s.refreshWiki()
		return out
	}
	return wiki.DistillOutcome{Repo: repo}
}
