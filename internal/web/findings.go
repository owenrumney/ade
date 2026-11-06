package web

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/owenrumney/ade/internal/review"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/statedir"
)

// findingsFile is what Send to agent writes under .git/ade and names to the agent.
const findingsFile = "findings.md"

// foundFile is where a review of the whole branch puts what it found.
const foundFile = "findings.json"

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
	b.WriteString("End each finding with one line, `Outcome: fixed` or `Outcome: stands — why`: ade reads this file back when your turn ends and resolves the comment.\n")
	for i, t := range open {
		fmt.Fprintf(&b, "\n## %d. %s:%d · id %s\n\nSeverity: %s.", i+1, t.File, t.Line, t.ID, comments.Severity(t.Severity))
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

var (
	findingHead    = regexp.MustCompile(`(?m)^## \d+\. .*· id ([0-9a-f]+|t\d+)\s*$`)
	findingOutcome = regexp.MustCompile(`(?im)^outcome:\s*(fixed|stands)\b[\s—:,-]*(.*)$`)
)

// readFindingsBack resolves what the agent's turn settled: the findings file
// says fixed or stands under each id, and the thread gets that as its reply.
func (s *Server) readFindingsBack() {
	space := s.space()
	if space.gitDir == "" {
		return
	}
	raw, err := os.ReadFile(filepath.Join(statedir.Git(space.gitDir), findingsFile)) // #nosec G304 -- under the checkout's own git dir
	if err != nil {
		return
	}
	settled := 0
	for id, said := range outcomesIn(string(raw)) {
		thread, err := space.store.Get(id)
		if err != nil || thread.Fate() != comments.FateOpen {
			continue
		}
		outcome, body := comments.OutcomeFixed, "Fixed."
		if said.stands {
			outcome, body = comments.OutcomeDeclined, cmp.Or(said.why, "Left standing.")
		} else if said.why != "" {
			body = said.why
		}
		if _, err := space.store.ReplyOutcome(id, comments.AuthorAgent, body, outcome); err != nil {
			slog.Debug("record a finding's outcome", "thread", id, "err", err)
			continue
		}
		settled++
	}
	if settled == 0 {
		return
	}
	slog.Info("read the findings back", "settled", settled)
	s.hub.Broadcast("threads", nil)
	s.hub.Broadcast("state", nil)
}

type findingSaid struct {
	stands bool
	why    string
}

// outcomesIn is each finding's last Outcome line, by thread id.
func outcomesIn(text string) map[string]findingSaid {
	out := map[string]findingSaid{}
	heads := findingHead.FindAllStringSubmatchIndex(text, -1)
	for i, head := range heads {
		end := len(text)
		if i+1 < len(heads) {
			end = heads[i+1][0]
		}
		section := text[head[1]:end]
		lines := findingOutcome.FindAllStringSubmatch(section, -1)
		if len(lines) == 0 {
			continue
		}
		last := lines[len(lines)-1]
		out[text[head[2]:head[3]]] = findingSaid{
			stands: strings.EqualFold(last[1], "stands"),
			why:    strings.TrimSpace(last[2]),
		}
	}
	return out
}

// writeFindings settles a whole-branch batch: every reviewer's findings, in
// the order found, as the report shape the cold reviewer answers in.
func (s *Server) writeFindings(b *reviewBatch) {
	type found struct {
		review.Finding
		By string `json:"by"`
	}
	all := make([]found, 0, len(b.order))
	for _, key := range b.order {
		one := b.found[key]
		all = append(all, found{Finding: one.finding, By: one.by})
	}
	path := filepath.Join(statedir.Git(s.space().gitDir), foundFile)
	raw, err := json.MarshalIndent(map[string]any{"scope": "branch", "findings": all}, "", "  ")
	if err == nil {
		err = os.MkdirAll(filepath.Dir(path), 0o750)
	}
	if err == nil {
		err = os.WriteFile(path, raw, 0o600)
	}
	if err != nil {
		s.reviewFiled(b.id, fmt.Sprintf("%d finding(s), and the file could not be written: %v", len(all), err), 0)
		return
	}
	s.tell(fmt.Sprintf("%d finding(s) from the whole branch written to %s", len(all), path))
	s.reviewFiled(b.id, fmt.Sprintf("%d finding(s) from the whole branch, in %s", len(all), path), 0)
}
