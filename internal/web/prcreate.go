package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/owenrumney/ade/internal/bridge"

	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/prcreate"
)

// CreatedPR is a pull request opened from the browser this session.
type CreatedPR struct {
	Number string `json:"number"`
	URL    string `json:"url"`
}

// prCreateBlocked is why this branch cannot open a pull request, with the
// status to answer under. Read again when a queued entry's turn comes.
func (s *Server) prCreateBlocked() (int, string) {
	ws := s.space()
	if ws.snapshot {
		return http.StatusBadRequest, "this is a fetched snapshot, so there is no local branch to open a PR for"
	}
	if ws.pr != nil {
		return http.StatusConflict, fmt.Sprintf("this diff is already PR #%d", ws.pr.Number)
	}
	if existing := s.currentBranchPR(); existing != nil {
		return http.StatusConflict, fmt.Sprintf("%s already has PR #%d", ws.branch, existing.Number)
	}
	s.mu.RLock()
	commits := 0
	if s.diff != nil {
		commits = s.diff.Commits
	}
	s.mu.RUnlock()
	if commits == 0 {
		return http.StatusBadRequest,
			fmt.Sprintf("%s has no commits ahead of the base, so there is nothing to open a PR for", ws.branch)
	}
	return 0, ""
}

func (s *Server) handlePRCreate(w http.ResponseWriter, _ *http.Request) {
	if code, why := s.prCreateBlocked(); why != "" {
		writeError(w, code, why)
		return
	}
	runCtx, ok := s.agent.begin(s.ctx, kindPRCreate)
	if !ok {
		s.queueBehind(w, queued{kind: kindPRCreate})
		return
	}

	s.spawn(func() { s.runPRCreate(runCtx) })
	s.hub.Broadcast("agent", nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"running": true})
}

func (s *Server) runPRCreate(ctx context.Context) {
	defer s.recoverRun("pr-create")

	err := s.prCreateOnce(ctx)
	s.agent.finish(err)
	if err != nil {
		slog.Warn("create pr failed", "err", err)
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
}

func (s *Server) prCreateOnce(ctx context.Context) error {
	w := s.space()
	s.agent.step(fmt.Sprintf("opening a draft PR for %s", w.branch))

	// Found here rather than left for the agent to go looking for, so a template
	// cannot be missed just because a file was never opened.
	template, hasTemplate := prcreate.Template(w.homeRoot)
	if hasTemplate {
		s.agent.step("found a pull request template; it will be included")
		if prcreate.NeedsRiskScore(template) {
			s.agent.step("the template asks for a risk score; it will be estimated and filled in")
		}
	}

	turn, err := s.oneShot(ctx, bridge.OneShotOpts{
		Bin:          s.agent.bin,
		Model:        s.agent.model,
		Dir:          w.homeRoot,
		Prompt:       prcreate.Prompt(template),
		AllowedTools: prcreate.Tools(),
		// Needs git, and a permission prompt has nothing to answer it.
		OnEvent: s.onAgentEvent,
	})
	if err != nil {
		return err
	}

	url, err := prcreate.ParseURL(turn.Text)
	if err != nil {
		return err
	}
	if hasTemplate {
		s.warnIfTemplateDropped(ctx, w.homeRoot, template, url)
	}

	s.mu.Lock()
	s.created = &CreatedPR{Number: prcreate.Number(url), URL: url}
	s.mu.Unlock()

	s.agent.step(url)
	return nil
}

func (s *Server) currentCreatedPR() *CreatedPR {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.created
}

// warnIfTemplateDropped reads back what was actually submitted. The agent can
// drop the template while reporting success, and a warning in the log beats
// finding out on the pull request itself.
func (s *Server) warnIfTemplateDropped(ctx context.Context, root, template, url string) {
	number, err := strconv.Atoi(prcreate.Number(url))
	if err != nil {
		return
	}
	body, err := ghpr.Body(ctx, root, number)
	if err != nil {
		slog.Debug("read back pr body", "err", err)
		return
	}
	if !prcreate.Kept(template, body) {
		s.agent.step("warning: the submitted body is missing part of the pull request template")
	}
}
