package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/review"
	"github.com/owenrumney/ade/internal/walkthrough"
)

// Walkthrough is the change read back as ordered steps, with the shape they
// move through. The diff says what each hunk is; this says what it is for.
type Walkthrough struct {
	Steps []walkthrough.Step `json:"steps"`
	Flow  *walkthrough.Flow  `json:"flow,omitempty"`

	// Files, Base and Scope are what the panel header names, so a walkthrough
	// of a third of the change is visible rather than silent.
	Files int    `json:"files"`
	Base  string `json:"base"`
	Scope string `json:"scope"`

	// Stale is set on the way out, by comparing hash with the diff now.
	Stale bool `json:"stale"`

	// hash is the diff version this was written for. The panel is told
	// Stale instead.
	hash string
}

const (
	// excerptContext is the lines either side of a step's own range.
	excerptContext = 3
	// maxExcerpt is the most lines a step shows. Past it nobody reads the
	// code, and the step was pointed at the wrong range anyway.
	maxExcerpt = 40
	// maxExcerptFile is the most of a file read to cut those lines out of.
	// Past it the step is anchored to generated output, not code.
	maxExcerptFile = 2 << 20
)

// handleWalkthrough asks for the steps through the diff. One already written
// for it is handed back; Regenerate asks again.
func (s *Server) handleWalkthrough(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Regenerate bool `json:"regenerate"`
	}
	if r.Body != nil && r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()
	if diff == nil || len(diff.Files) == 0 {
		writeError(w, http.StatusBadRequest, "nothing to explain: the diff is empty")
		return
	}
	if !req.Regenerate {
		if held := s.currentWalkthrough(); held != nil && !held.Stale {
			writeJSON(w, http.StatusOK, map[string]any{"walkthrough": held})
			return
		}
	}

	runCtx, ok := s.agent.begin(s.ctx, kindWalkthrough)
	if !ok {
		// You ask what a change does while the agent is still changing it,
		// the same as you stage and describe while it works.
		s.setWalkthrough(nil)
		waiting := s.stack(queued{kind: kindWalkthrough})
		s.hub.Broadcast("agent", nil)
		s.hub.Broadcast("state", nil)
		writeJSON(w, http.StatusAccepted, map[string]any{"running": true, "queued": waiting})
		return
	}

	s.setWalkthrough(nil)
	s.spawn(func() { s.runWalkthrough(runCtx, diff) })
	s.hub.Broadcast("agent", nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"running": true, "files": len(diff.Files)})
}

func (s *Server) runWalkthrough(ctx context.Context, diff *gitdiff.Diff) {
	defer s.recoverRun(kindWalkthrough)

	err := s.walkthroughOnce(ctx, diff)
	s.agent.finish(err)
	if err != nil {
		slog.Warn("walkthrough failed", "err", err)
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
}

func (s *Server) walkthroughOnce(ctx context.Context, diff *gitdiff.Diff) error {
	s.agent.step(fmt.Sprintf("reading %d file(s)", len(diff.Files)))

	// Read before the run, not after it: a diff that moves while the agent is
	// working leaves a walkthrough of the old one, and it must say so.
	hash := s.currentHash()
	paths := map[string]bool{}
	binary := map[string]bool{}
	for _, file := range diff.Files {
		paths[file.Path] = true
		binary[file.Path] = file.Binary
	}
	rendered := review.RenderDiff(diff)

	ask := func() ([]walkthrough.Step, *walkthrough.Flow, error) {
		turn, err := s.oneShot(ctx, bridge.OneShotOpts{
			Bin: s.agent.bin,
			// Not the fast model the quiz runs on: putting the steps in the
			// order that makes the change readable is the whole job here.
			Model:        s.agent.model,
			Dir:          s.space().diffRoot(),
			Prompt:       walkthrough.Prompt(rendered),
			AllowedTools: walkthrough.Tools(),
			NoMCP:        true,
			OnEvent:      s.onRunEvent(kindWalkthrough),
		})
		if err != nil {
			return nil, nil, err
		}
		steps, flow := walkthrough.Parse(turn.Text, func(path string) bool { return paths[path] })
		return steps, flow, nil
	}

	steps, flow, err := ask()
	if err != nil {
		return err
	}
	// Dropping can leave too few. Ask once more and keep the larger set.
	if len(steps) < walkthrough.MinSteps {
		s.agent.step("too few steps came back; asking once more")
		again, againFlow, againErr := ask()
		if againErr != nil {
			return againErr
		}
		if len(again) > len(steps) {
			steps, flow = again, againFlow
		}
	}
	if len(steps) < walkthrough.MinSteps {
		return fmt.Errorf("only %d step(s) came back readable; fewer than %d is not a walk through anything",
			len(steps), walkthrough.MinSteps)
	}

	ws := s.space()
	for i := range steps {
		if binary[steps[i].File] {
			continue
		}
		steps[i].Excerpt = excerptOf(ctx, ws, steps[i])
	}
	s.setWalkthrough(&Walkthrough{
		Steps: steps,
		Flow:  flow,
		Files: len(diff.Files),
		Base:  diff.Base,
		Scope: ws.opts.Scope,
		hash:  hash,
	})
	s.agent.step(fmt.Sprintf("%d step(s) ready", len(steps)))
	return nil
}

// excerptOf cuts a step's lines out of the right side of the diff, which in a
// committed scope is HEAD and not the tree. Empty when the file will not read.
func excerptOf(ctx context.Context, ws *workspace, step walkthrough.Step) []walkthrough.Line {
	body, err := gitdiff.Blob(ctx, ws.diffRoot(), ws.opts, step.File, gitdiff.SideRight)
	if err != nil || len(body) == 0 || len(body) > maxExcerptFile {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(string(body), "\n"), "\n")
	first := max(step.Line-excerptContext, 1)
	last := min(step.EndLine+excerptContext, len(lines))
	if first > len(lines) {
		return nil
	}
	last = min(last, first+maxExcerpt-1)
	out := make([]walkthrough.Line, 0, last-first+1)
	for n := first; n <= last; n++ {
		out = append(out, walkthrough.Line{
			N:    n,
			Text: lines[n-1],
			In:   n >= step.Line && n <= step.EndLine,
		})
	}
	return out
}

func (s *Server) setWalkthrough(walk *Walkthrough) {
	s.mu.Lock()
	s.walkthrough = walk
	s.mu.Unlock()
}

// currentWalkthrough is the held walkthrough, marked stale when the diff has
// moved under it. In memory only: it describes code that no longer exists.
func (s *Server) currentWalkthrough() *Walkthrough {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.walkthrough == nil {
		return nil
	}
	held := *s.walkthrough
	held.Stale = held.hash != s.diffHash
	return &held
}
