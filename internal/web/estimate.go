package web

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/owenrumney/ade/internal/docs"
	"github.com/owenrumney/ade/internal/estimate"
	"github.com/owenrumney/ade/internal/limits"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/transcripts"
)

// overrunTick is how often a build is held against its range.
var overrunTick = 30 * time.Second

// BuildEstimate is what the Build button says, or nothing on a thin history.
type BuildEstimate struct {
	Estimate *estimate.Estimate `json:"estimate"`
}

// handleBuildEstimate is how long a build of the open design will take, and
// how much of the plan window, from how builds like it went before.
func (s *Server) handleBuildEstimate(w http.ResponseWriter, r *http.Request) {
	dir, err := settings.Dir()
	if err != nil || s.currentDoc() == "" {
		writeJSON(w, http.StatusOK, BuildEstimate{})
		return
	}
	provider := r.URL.Query().Get("provider")
	if provider == "" {
		provider = s.agent.State().Provider
	}
	history := s.estimates.Runs(dir)
	got, ok := estimate.For(history, s.buildTarget(r.URL.Query().Get("model")), s.calibration(provider))
	if !ok {
		writeJSON(w, http.StatusOK, BuildEstimate{})
		return
	}
	writeJSON(w, http.StatusOK, BuildEstimate{Estimate: &got})
}

// buildTarget is the build about to start, described for the history.
func (s *Server) buildTarget(model string) estimate.Target {
	t := estimate.Target{Kind: estimate.KindBuild, Model: model, Repo: estimate.Repo(s.space().homeRoot)}
	if file := s.currentDoc(); file != "" {
		if text, err := docs.Read(s.docsDir, file); err == nil {
			t.DocWords, t.ChecklistItems = estimate.Size(text)
		}
	}
	return t
}

// calibration reads the provider's 5h window against what was spent in it.
func (s *Server) calibration(provider string) *estimate.Calibration {
	state := s.limits.Snapshot(s.ctx)
	reading, agent := state.Claude, transcripts.AgentClaude
	if normalizeProvider(provider) == "codex" {
		reading, agent = state.Codex, transcripts.AgentCodex
	}
	win, ok := shortWindow(reading)
	if !ok {
		return nil
	}
	spent, known := s.estimates.SpentSince(agent, win.ResetsAt.Add(-5*time.Hour))
	if !known {
		return nil
	}
	return &estimate.Calibration{
		Name: win.Name, UsedPct: win.Percent, ResetsAt: win.ResetsAt,
		UnitsSinceReset: spent,
	}
}

func shortWindow(l *limits.Limits) (limits.Window, bool) {
	if l == nil {
		return limits.Window{}, false
	}
	for _, win := range l.Windows {
		if win.Name == "5h" && !win.ResetsAt.IsZero() {
			return win, true
		}
	}
	return limits.Window{}, false
}

// watchOverrun says in the conversation, once, that a build has run past the
// upper end of its range. The returned func stops the watch.
func (s *Server) watchOverrun(ctx context.Context, t estimate.Target, started time.Time) func() {
	done := make(chan struct{})
	go func() {
		dir, err := settings.Dir()
		if err != nil {
			return
		}
		watch, ok := estimate.NewOverrun(s.estimates.Runs(dir), t)
		if !ok {
			return
		}
		tick := time.NewTicker(overrunTick)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-tick.C:
				if line, due := watch.Check(time.Since(started)); due {
					s.agent.step(line)
					return
				}
			}
		}
	}()
	return func() { close(done) }
}

// buildFrom is where a build's transcript is.
type buildFrom struct {
	root, provider, session string
}

// recordBuild adds a finished build to the machine's history. A build is a
// fresh session, so the session's model and usage are the build's.
func (s *Server) recordBuild(t estimate.Target, from buildFrom, started, finished time.Time, failed bool) {
	run := estimate.Run{
		Kind: t.Kind, Model: t.Model, Repo: t.Repo, DocWords: t.DocWords, ChecklistItems: t.ChecklistItems,
		Started: started, Finished: finished, Failed: failed,
	}
	if sess := buildSession(from); sess != nil {
		if sess.Model != "" {
			run.Model = sess.Model
		}
		run.Usage = sess.Usage
	}
	dir, err := settings.Dir()
	if err != nil {
		return
	}
	if err := estimate.Record(dir, run); err != nil {
		slog.Warn("record build", "err", err)
		return
	}
	s.estimates.Add(run)
}

// buildSession is the build's transcript, loaded, for whichever agent ran it.
func buildSession(from buildFrom) *transcripts.Session {
	if from.session == "" {
		return nil
	}
	if normalizeProvider(from.provider) == "codex" {
		for _, found := range transcripts.Scan() {
			if found.Agent == transcripts.AgentCodex && found.CWD == from.root && found.ID == from.session {
				loaded, err := transcripts.LoadCopy(found)
				if err != nil {
					return nil
				}
				return loaded
			}
		}
		return nil
	}
	path := transcriptPath(from.root, from.session)
	if path == "" {
		return nil
	}
	if sess := transcripts.ClaudeSession(path); sess != nil && transcripts.Load(sess) == nil {
		return sess
	}
	return nil
}
