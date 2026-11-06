package web

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/review"
	"github.com/owenrumney/ade/internal/statedir"
)

// changeFile is the rendered diff, written so the agent reads one file
// rather than re-deriving it with git.
const changeFile = "change.diff"

// handleExplainChange types one line at the agent asking for a shareable
// artifact: the change in plain words, with a flow diagram of how it runs.
func (s *Server) handleExplainChange(w http.ResponseWriter, r *http.Request) {
	ws := s.space()
	if ws.snapshot {
		writeError(w, http.StatusBadRequest, "this is a fetched snapshot; the agent has no checkout to read it from")
		return
	}
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()
	if diff == nil || len(diff.Files) == 0 {
		writeError(w, http.StatusConflict, "there is no change to explain")
		return
	}
	path := filepath.Join(statedir.Git(ws.gitDir), changeFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("write the change: %v", err))
		return
	}
	if err := os.WriteFile(path, []byte(review.RenderDiff(diff)), 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("write the change: %v", err))
		return
	}
	var url string
	if ws.pr != nil {
		url = ws.pr.URL
	}
	ask := explainAsk(path, ws.opts.Scope, s.currentBase(), url)
	if _, err := s.agentPane(r.Context(), ask); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errMidTurn) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"file": path, "files": len(diff.Files)})
}

// explainAsk names the file and keeps the page small: a Mermaid flowchart of
// the changed path, not a designed SVG, is what took the minutes off.
func explainAsk(path, scope, base, pr string) string {
	what := "this branch against " + base
	if scope == gitdiff.ScopeUncommitted {
		what = "the uncommitted change"
	} else if base == "" {
		what = "this branch against its base"
	}
	var b strings.Builder
	b.WriteString("Explain this change as a Claude artifact. The diff of " + what + " is already in " + path + ": read that,")
	b.WriteString(" and only the functions it touches, rather than re-running git.")
	b.WriteString(" Publish one short page, shareable by link: what changed and why in plain words,")
	b.WriteString(" and a Mermaid flowchart of the changed path only. Keep it to the default style, no bespoke SVG or design exploration.")
	if pr != "" {
		b.WriteString(" The pull request is " + pr + ".")
	}
	b.WriteString(" Paste the link here when it is up.")
	return b.String()
}
