package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/design"
	"github.com/owenrumney/ade/internal/docs"
	"github.com/owenrumney/ade/internal/drift"
	"github.com/owenrumney/ade/internal/review"
	"github.com/owenrumney/ade/internal/settings"
)

// In internal/drift: the agent reads the same shape back over the delegate.
type (
	DriftState = drift.State
	DriftItem  = drift.Item
)

// handleDelegateDrift is the agent's only way to the checklist.
func (s *Server) handleDelegateDrift(w http.ResponseWriter, r *http.Request) {
	if !s.delegateAuthorized(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	s.handleDrift(w, r)
}

// driftCategory files contradictions in their own group. The diff sidebar
// already groups threads by category, so they land apart from review findings
// with no work on that side.
const driftCategory = "drift"

// driftName is who the scoring runs as, beside the named agents.
const driftName = "drift"

// handleDrift answers the checklist. It is rebuilt from the document on every
// read, so editing the design changes the checklist without a pass.
func (s *Server) handleDrift(w http.ResponseWriter, r *http.Request) {
	file := s.currentDoc()
	if file == "" {
		writeJSON(w, http.StatusOK, DriftState{Items: []DriftItem{}})
		return
	}
	items, err := s.driftChecklist(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read "+file)
		return
	}
	state := DriftState{File: file, Items: items}
	s.mu.RLock()
	last := s.drift
	s.mu.RUnlock()
	if last != nil && last.File == file {
		state.At = last.At
	}
	writeJSON(w, http.StatusOK, state)
}

// handleDriftRun scores the diff against the design.
func (s *Server) handleDriftRun(w http.ResponseWriter, r *http.Request) {
	file := s.currentDoc()
	if file == "" {
		writeError(w, http.StatusBadRequest, "there is no design document to score against")
		return
	}
	if s.space().snapshot {
		writeError(w, http.StatusBadRequest, "this is a fetched snapshot, so there is nothing local to score")
		return
	}
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()
	if diff == nil || len(diff.Files) == 0 {
		writeError(w, http.StatusBadRequest, "nothing to score: the diff is empty")
		return
	}
	text, err := docs.Read(s.docsDir, file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read "+file)
		return
	}
	reqs := design.Requirements(text)
	if len(reqs) == 0 {
		writeError(w, http.StatusBadRequest, file+
			" has nothing to score: put what the change should do under ## "+
			strings.Join(design.ApproachHeadings, ", ## "))
		return
	}

	// Beside the conversation, not in it: scoring reads the diff and the
	// document and writes nothing, so a turn in flight is no reason to refuse.
	started := s.startJobs([]sideJob{{name: driftName, cold: true, run: func(ctx context.Context) {
		s.runDrift(ctx, file, reqs)
	}}}, 0)
	if !started {
		writeError(w, http.StatusConflict, "too many agents are already running")
		return
	}
	s.hub.Broadcast("agent", nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"running": true})
}

// handleDriftDismiss waves a requirement off, or brings it back. The wording is
// stored with it, so a reworded requirement is scored again.
func (s *Server) handleDriftDismiss(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID        string `json:"id"`
		Dismissed bool   `json:"dismissed"`
	}
	if !decode(w, r, &req) {
		return
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		writeError(w, http.StatusBadRequest, "a requirement id is needed")
		return
	}
	file := s.currentDoc()
	if file == "" {
		writeError(w, http.StatusBadRequest, "there is no design document open")
		return
	}
	items, err := s.driftChecklist(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read "+file)
		return
	}
	hash := ""
	for _, item := range items {
		if item.ID == id {
			hash = item.Hash
			break
		}
	}
	if hash == "" {
		writeError(w, http.StatusNotFound, file+" has no requirement "+id)
		return
	}
	if !req.Dismissed {
		hash = ""
	}
	if err := settings.SetDismissedRequirement(s.startRoot, file, id, hash); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save that: "+err.Error())
		return
	}
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{"dismissed": req.Dismissed})
}

func (s *Server) runDrift(ctx context.Context, file string, reqs []design.Requirement) {
	defer s.recoverRun("drift")

	items, filed, err := s.driftOnce(ctx, file, reqs)
	if err != nil {
		slog.Warn("drift failed", "err", err)
		s.say(ChatMessage{Kind: chat.KindNote, Failed: true, Run: runSubagent,
			Body: fmt.Sprintf("Could not score the diff against %s: %v", file, err)})
	} else {
		s.mu.Lock()
		s.drift = &DriftState{File: file, At: time.Now(), Items: items}
		s.mu.Unlock()
		s.say(ChatMessage{Kind: chat.KindNote, Run: runSubagent, Body: driftSummary(file, items, filed)})
	}
	if refreshErr := s.refresh(s.ctx); refreshErr != nil {
		slog.Debug("refresh after drift", "err", refreshErr)
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
}

// driftOnce runs the pass and files what it says the change contradicts.
func (s *Server) driftOnce(ctx context.Context, file string, reqs []design.Requirement) ([]DriftItem, int, error) {
	w := s.space()
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()

	turn, err := s.oneShot(ctx, bridge.OneShotOpts{
		Bin:          s.agent.bin,
		Model:        s.agent.model,
		Dir:          w.homeRoot,
		Prompt:       design.DriftPrompt(s.docPath(file), reqs, review.RenderDiff(diff)),
		AllowedTools: review.ReadOnlyTools(),
		OnEvent:      s.onAgentEvent,
	})
	if err != nil {
		return nil, 0, err
	}

	scored := map[string]design.Drift{}
	for _, d := range design.ParseDrift(turn.Text) {
		scored[d.ID] = d
	}

	aside := settings.DismissedRequirements()
	items := make([]DriftItem, 0, len(reqs))
	var findings []review.Finding
	for _, r := range reqs {
		item := DriftItem{ID: r.ID, Kind: r.Kind, Text: r.Text, Line: r.Line, Hash: r.Hash,
			Dismissed: settings.IsRequirementDismissed(aside, s.startRoot, file, r.ID, r.Hash)}
		if d, ok := scored[r.ID]; ok {
			item.Verdict, item.Note, item.File, item.FileLine = d.Verdict, d.Note, d.File, d.Line
			// A dismissed requirement files nothing: waving it off is saying
			// the design was wrong, and a comment about it would be noise.
			if d.Verdict == design.DriftContradicted && d.File != "" && d.Line > 0 && !item.Dismissed {
				findings = append(findings, review.Finding{
					File: d.File, Line: d.Line, Category: driftCategory, Severity: "medium",
					Summary: contradiction(d.Note, r.Text), ShortSummary: r.Text,
				})
			}
		}
		items = append(items, item)
	}

	filed := s.fileFindings(review.Relative(findings, w.homeRoot), comments.AuthorAgent, s.agent.step)
	return items, len(filed), nil
}

// driftChecklist is the document's requirements now, carrying whatever the last
// pass said about each and whether it has been waved off.
func (s *Server) driftChecklist(file string) ([]DriftItem, error) {
	text, err := docs.Read(s.docsDir, file)
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	last := s.drift
	s.mu.RUnlock()
	scored := map[string]DriftItem{}
	if last != nil && last.File == file {
		for _, item := range last.Items {
			scored[item.ID] = item
		}
	}

	aside := settings.DismissedRequirements()
	out := []DriftItem{}
	for _, r := range design.Requirements(text) {
		item := DriftItem{ID: r.ID, Kind: r.Kind, Text: r.Text, Line: r.Line, Hash: r.Hash,
			Dismissed: settings.IsRequirementDismissed(aside, s.startRoot, file, r.ID, r.Hash)}
		// A verdict was about the wording it was given, so a reworded
		// requirement shows as unscored rather than carrying a stale score.
		if was, ok := scored[r.ID]; ok && was.Hash == r.Hash {
			item.Verdict, item.Note, item.File, item.FileLine = was.Verdict, was.Note, was.File, was.FileLine
		}
		out = append(out, item)
	}
	return out, nil
}

// contradiction is the comment body: what the pass found, and the design line it
// is answering, since the thread is read on the diff and not beside the document.
func contradiction(note, requirement string) string {
	note = strings.TrimSpace(note)
	if note == "" {
		note = "this contradicts the design"
	}
	return note + "\n\nThe design says: " + requirement
}

func driftSummary(file string, items []DriftItem, filed int) string {
	counts := map[string]int{}
	for _, item := range items {
		if item.Dismissed {
			continue
		}
		counts[item.Verdict]++
	}
	return fmt.Sprintf("Drift against %s: %d done, %d partial, %d absent, %d contradicted, %d filed.",
		file, counts[design.DriftDone], counts[design.DriftPartial],
		counts[design.DriftAbsent], counts[design.DriftContradicted], filed)
}
