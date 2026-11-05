package web

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/statedir"
)

// findingsFile is what Send to agent writes under .git/ade and names to the agent.
const findingsFile = "findings.md"

// handleSendFindings writes the open comments to a file and types one line at
// the agent's prompt naming it. A busy CLI queues the line itself.
func (s *Server) handleSendFindings(w http.ResponseWriter, r *http.Request) {
	space := s.space()
	if space.gitDir == "" {
		writeError(w, http.StatusConflict, "this session has no checkout to keep the findings in")
		return
	}
	open := openFindings(space.store.Unresolved())
	if len(open) == 0 {
		writeError(w, http.StatusConflict, "there are no open comments to send")
		return
	}
	path := filepath.Join(statedir.Git(space.gitDir), findingsFile)
	if err := os.WriteFile(path, []byte(findingsText(open)), 0o600); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("write the findings: %v", err))
		return
	}
	if _, err := s.agentPane(r.Context(), findingsAsk(path, len(open))); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errMidTurn) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"file": path, "sent": len(open)})
}

// openFindings is what the Diff tab lists as open, worst first.
func openFindings(threads []comments.Thread) []comments.Thread {
	open := slices.DeleteFunc(threads, func(t comments.Thread) bool {
		return t.Sent() || t.Withdrawn || t.DuplicateOf != ""
	})
	slices.SortStableFunc(open, func(a, b comments.Thread) int {
		switch {
		case comments.Worse(a.Severity, b.Severity):
			return -1
		case comments.Worse(b.Severity, a.Severity):
			return 1
		}
		return strings.Compare(a.File, b.File)
	})
	return open
}

// findingsAsk is the one line the agent is sent. The findings stay in the file.
func findingsAsk(path string, n int) string {
	return fmt.Sprintf("Read %s and work through each of the %d review findings in it: fix it, or say why it should stand.", path, n)
}

func findingsText(open []comments.Thread) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Review findings\n\n%d open, worst first. Fix each one, or say why it should stand.\n", len(open))
	for i, t := range open {
		fmt.Fprintf(&b, "\n## %d. %s:%d\n\nSeverity: %s.", i+1, t.File, t.Line, comments.Severity(t.Severity))
		if t.Category != "" {
			fmt.Fprintf(&b, " Category: %s.", t.Category)
		}
		b.WriteString("\n")
		for j, c := range t.Comments {
			body := strings.TrimSpace(c.Body)
			if j > 0 {
				body = "Reply from " + c.Author + ": " + body
			}
			fmt.Fprintf(&b, "\n%s\n", body)
		}
	}
	return b.String()
}
