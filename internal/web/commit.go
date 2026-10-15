package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/owenrumney/ade/internal/bridge"

	"github.com/owenrumney/ade/internal/commitmsg"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/outgoing"
)

// CommitDraft is a generated message plus the staged files it describes.
type CommitDraft struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Staged []string `json:"staged"`
	Raw    string   `json:"raw,omitempty"`
}

func (s *Server) handleCommitMessage(w http.ResponseWriter, r *http.Request) {
	staged, err := gitdiff.StagedFiles(r.Context(), s.space().diffRoot())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(staged) == 0 {
		writeError(w, http.StatusBadRequest, "nothing is staged; git add the changes you want described")
		return
	}
	runCtx, ok := s.agent.begin(s.ctx, kindCommitMessage)
	if !ok {
		// Asking while a turn is in flight is the usual case, not a mistake:
		// you stage and describe while the agent is still working.
		waiting := s.stack(queued{kind: kindCommitMessage})
		s.hub.Broadcast("agent", nil)
		s.hub.Broadcast("state", nil)
		writeJSON(w, http.StatusAccepted, map[string]any{"running": true, "queued": waiting})
		return
	}

	s.setDraft(nil)
	s.spawn(func() { s.runCommitMessage(runCtx, staged) })
	s.hub.Broadcast("agent", nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"running": true, "staged": len(staged)})
}

func (s *Server) runCommitMessage(ctx context.Context, staged []string) {
	defer s.recoverRun(kindCommitMessage)

	err := s.commitMessageOnce(ctx, staged)
	s.agent.finish(err)
	if err != nil {
		slog.Warn("commit message failed", "err", err)
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
}

func (s *Server) commitMessageOnce(ctx context.Context, staged []string) error {
	s.agent.step(fmt.Sprintf("describing %d staged file(s)", len(staged)))

	ask := func(prompt string) (*bridge.Turn, error) {
		return s.oneShot(ctx, bridge.OneShotOpts{
			Bin: s.agent.bin,
			// Not the conversation's model: describing a diff you wrote is the
			// same size of job as rewriting a note, and this inherited Opus.
			Model:        outgoing.FastModel,
			Dir:          s.space().diffRoot(),
			Prompt:       prompt,
			AllowedTools: commitmsg.Tools(),
			// It reads the diff with git and needs nothing else. Starting the
			// MCP servers was most of the wait.
			NoMCP:   true,
			OnEvent: s.onRunEvent(kindCommitMessage),
		})
	}

	turn, err := ask(commitmsg.Prompt())
	if err != nil {
		return err
	}
	// ade does not ship /ctb, so some machines do not have it.
	if commitmsg.MissingSkill(turn.Text) {
		s.agent.step("that machine has no /ctb skill; describing the diff directly")
		if turn, err = ask(commitmsg.Fallback()); err != nil {
			return err
		}
	}

	msg, parseErr := commitmsg.Parse(turn.Text)
	if parseErr != nil {
		// Keep the reply so the modal can show what the agent actually said.
		s.setDraft(&CommitDraft{Staged: staged, Raw: turn.Text})
		return parseErr
	}

	s.setDraft(&CommitDraft{Title: msg.Title, Body: msg.Body, Staged: staged})
	s.agent.step("commit message ready")
	return nil
}

// handleCommit commits the index with the message from the modal. The message is
// taken from the request, not the stored draft, so edits are honoured.
func (s *Server) handleCommit(w http.ResponseWriter, r *http.Request) {
	ws := s.space()
	var req struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		// Amend folds this into the last commit instead of making a new one.
		// With no title it keeps the old message.
		Amend bool `json:"amend"`
	}
	if !decode(w, r, &req) {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" && !req.Amend {
		writeError(w, http.StatusBadRequest, "a commit needs a title")
		return
	}
	if req.Amend && !gitdiff.HasCommits(r.Context(), ws.diffRoot()) {
		writeError(w, http.StatusBadRequest, "there is no commit to amend")
		return
	}

	staged, err := gitdiff.StagedFiles(r.Context(), ws.diffRoot())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Amending only the message is a real thing to want, so an empty index is
	// only a problem for a new commit.
	if len(staged) == 0 && !req.Amend {
		writeError(w, http.StatusBadRequest, "nothing is staged to commit")
		return
	}
	if len(staged) == 0 && title == "" {
		writeError(w, http.StatusBadRequest, "nothing is staged, and no new message, so there is nothing to amend")
		return
	}

	text := ""
	if title != "" {
		text = message(title, req.Body)
	}

	// An amend rewrites this sha, and the ranges stamped with it move onto the
	// new one. Read before the commit, because after it there is no way back.
	replacing := ""
	if req.Amend {
		replacing, _ = gitdiff.HeadSHA(r.Context(), ws.diffRoot())
	}

	var sha string
	if req.Amend {
		sha, err = gitdiff.Amend(r.Context(), ws.diffRoot(), text)
	} else {
		sha, err = gitdiff.Commit(r.Context(), ws.diffRoot(), text)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// An amend rewrites the last commit, so a branch already on the remote can
	// only be pushed over. The browser asks; this says whether it needs to.
	pushed := 0
	if req.Amend {
		if status, err := gitdiff.WorktreeStatus(r.Context(), ws.diffRoot()); err == nil && status.Upstream != "" {
			pushed = 1
		}
	}

	if full, shaErr := gitdiff.HeadSHA(r.Context(), ws.diffRoot()); shaErr == nil {
		s.stampCommit(r.Context(), full, replacing)
	}

	s.setDraft(nil)
	if refreshErr := s.refresh(s.ctx); refreshErr != nil {
		slog.Debug("refresh after commit", "err", refreshErr)
	}
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"sha": sha, "files": len(staged), "amended": req.Amend, "needsForcePush": pushed == 1,
	})
}

// message joins the title and body, dropping any line that credits an AI as a
// co-author. Attribution to a person is left alone.
func message(title, body string) string {
	var kept []string
	for _, line := range strings.Split(body, "\n") {
		if isAIAttribution(line) {
			continue
		}
		kept = append(kept, line)
	}
	body = strings.TrimSpace(strings.Join(kept, "\n"))
	if body == "" {
		return title + "\n"
	}
	return title + "\n\n" + body + "\n"
}

func isAIAttribution(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if strings.HasPrefix(lower, "co-authored-by:") {
		return strings.Contains(lower, "claude") || strings.Contains(lower, "anthropic") ||
			strings.Contains(lower, "noreply@anthropic.com")
	}
	return strings.HasPrefix(lower, "generated with") && strings.Contains(lower, "claude")
}

func (s *Server) setDraft(draft *CommitDraft) {
	s.mu.Lock()
	s.draft = draft
	s.mu.Unlock()
}

func (s *Server) currentDraft() *CommitDraft {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.draft
}

/* Worktree */

// refreshWorktree records what is staged and what is not, so the commit modal
// can warn about work that would be left behind. It reports a real change.
func (s *Server) refreshWorktree(ctx context.Context) bool {
	if s.space().snapshot {
		return false
	}
	status, err := gitdiff.WorktreeStatus(ctx, s.space().diffRoot())
	if err != nil {
		slog.Debug("worktree status", "err", err)
		return false
	}
	status.Signing = gitdiff.SigningOf(ctx, s.space().diffRoot())
	// Resolved, not only the one set at start: an empty base left Behind at 0,
	// so Rebase read "up to date" however far behind you were.
	base := s.space().opts.Base
	if base == "" {
		base = gitdiff.BaseBranch(ctx, s.space().diffRoot())
	}
	if base != "" {
		status.Behind, _ = gitdiff.CommitsBehind(ctx, s.space().diffRoot(), base)
	}
	fingerprint := fmt.Sprintf("%v|%v|%s|%d|%d|%v", status.Staged, status.Unstaged,
		status.Upstream, status.Ahead, status.Behind, status.Signing)

	s.mu.Lock()
	defer s.mu.Unlock()
	changed := fingerprint != s.worktreeHash
	s.worktree, s.worktreeHash, s.baseRef = &status, fingerprint, base
	return changed
}

func (s *Server) currentWorktree() *gitdiff.Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.worktree
}

// currentBase is the branch a rebase would land on, named so the button can
// say it. FetchBase resolves an empty one the same way, so the two agree.
func (s *Server) currentBase() string {
	if base := s.space().opts.Base; base != "" {
		return base
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.baseRef
}

// indexSide is one direction across the index: which paths a request may name,
// how to move them, and the words for the reply and for a path that cannot go.
type indexSide struct {
	verb     string
	past     string
	needs    string
	eligible func(gitdiff.Status) map[string]bool
	apply    func(context.Context, string, []string) error
}

var stageSide = indexSide{
	verb: "stage", past: "staged", needs: "unstaged", apply: gitdiff.Stage,
	eligible: func(status gitdiff.Status) map[string]bool {
		paths := make(map[string]bool, len(status.Unstaged))
		for _, change := range status.Unstaged {
			paths[change.Path] = true
		}
		return paths
	},
}

var unstageSide = indexSide{
	verb: "unstage", past: "unstaged", needs: "staged", apply: gitdiff.Unstage,
	eligible: func(status gitdiff.Status) map[string]bool {
		paths := make(map[string]bool, len(status.Staged))
		for _, path := range status.Staged {
			paths[path] = true
		}
		return paths
	},
}

func (s *Server) handleStage(w http.ResponseWriter, r *http.Request) {
	s.reindex(w, r, stageSide)
}

func (s *Server) handleUnstage(w http.ResponseWriter, r *http.Request) {
	s.reindex(w, r, unstageSide)
}

// reindex moves the given paths across the index. Only paths git reports on
// the far side are accepted, so a request cannot reach outside the diff.
func (s *Server) reindex(w http.ResponseWriter, r *http.Request, side indexSide) {
	ws := s.space()
	var req struct {
		Paths []string `json:"paths"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.Paths) == 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("no files selected to %s", side.verb))
		return
	}

	status, err := gitdiff.WorktreeStatus(r.Context(), ws.diffRoot())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	eligible := side.eligible(status)
	for _, path := range req.Paths {
		if !eligible[path] {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%s has no %s changes", path, side.needs))
			return
		}
	}

	if err := side.apply(r.Context(), ws.diffRoot(), req.Paths); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if refreshErr := s.refresh(s.ctx); refreshErr != nil {
		slog.Debug("refresh after "+side.verb, "err", refreshErr)
	}
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{side.past: len(req.Paths)})
}

// handlePush publishes the branch, setting the upstream on a first push.
func (s *Server) handlePush(w http.ResponseWriter, r *http.Request) {
	if s.space().snapshot {
		writeError(w, http.StatusBadRequest, "this is a fetched snapshot, so there is nothing local to push")
		return
	}
	var req struct {
		// Force replaces what is on the remote, which an amended commit needs.
		Force bool `json:"force"`
	}
	if r.Body != nil && r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}

	pushTo := gitdiff.Push
	if req.Force {
		pushTo = gitdiff.PushForce
	}
	upstream, err := pushTo(r.Context(), s.space().diffRoot())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.refreshWorktree(s.ctx)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{"upstream": upstream})
}

// handleBranch starts a branch here and moves onto it, carrying whatever is
// not committed. The base branch is left where it was, to be merged onto.
func (s *Server) handleBranch(w http.ResponseWriter, r *http.Request) {
	ws := s.space()
	if ws.snapshot {
		writeError(w, http.StatusBadRequest, "this is a fetched snapshot, so there is no branch to start")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name the branch")
		return
	}
	if err := gitdiff.NewBranch(r.Context(), ws.diffRoot(), name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Without this the comments store stays bound to the branch left behind
	// until the next poll, and the Diff tab describes somewhere you are not.
	s.followBranch(s.ctx)
	if err := s.refresh(s.ctx); err != nil {
		slog.Debug("refresh after branch", "err", err)
	}
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{"branch": name})
}

// handleHeadMessage is the last commit's message, so amending with an edit
// starts from what is there rather than from nothing.
func (s *Server) handleHeadMessage(w http.ResponseWriter, r *http.Request) {
	ws := s.space()
	if !gitdiff.HasCommits(r.Context(), ws.diffRoot()) {
		writeError(w, http.StatusBadRequest, "there is no commit to amend")
		return
	}
	text, err := gitdiff.HeadMessage(r.Context(), ws.diffRoot())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	title, body, _ := strings.Cut(text, "\n")
	writeJSON(w, http.StatusOK, map[string]any{
		"title": strings.TrimSpace(title),
		"body":  strings.TrimSpace(body),
	})
}
