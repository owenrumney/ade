package review

import (
	"encoding/json"
	"regexp"
	"strings"
)

// VerdictFence tags the blocks a rebuttal round emits, one per judgement, so
// they cannot be confused with the ade-finding blocks of a review. Exported
// because design.RebuttalPrompt asks for them and this parses them.
const VerdictFence = "ade-verdict"

var verdictRE = regexp.MustCompile("(?s)```" + VerdictFence + "\\s*\\n(.*?)```")

// The verdicts a reviewer may return about another reviewer's finding.
const (
	VerdictAgree     = "agree"
	VerdictDispute   = "dispute"
	VerdictDuplicate = "duplicate-of"
)

// Verdict is one reviewer's judgement of one finding. Ref and Of name findings
// by the short ref the prompt gave them — F1, F2 — rather than by Key, which
// is file:line:summary and too long for a model to echo back exactly.
type Verdict struct {
	Ref     string `json:"ref"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason,omitempty"`
	Of      string `json:"of,omitempty"`
}

// ParseVerdicts pulls the judgements out of a rebuttal reply. A block that
// cannot be read is dropped rather than guessed at: an unreadable verdict
// marks the wrong thread, and no verdict at all leaves the finding standing.
func ParseVerdicts(text string) []Verdict {
	var out []Verdict
	seen := map[string]bool{}
	for _, match := range verdictRE.FindAllStringSubmatch(text, -1) {
		var v Verdict
		if err := json.Unmarshal([]byte(match[1]), &v); err != nil {
			continue
		}
		v.Ref = strings.ToUpper(strings.TrimSpace(v.Ref))
		v.Of = strings.ToUpper(strings.TrimSpace(v.Of))
		v.Verdict = strings.ToLower(strings.TrimSpace(v.Verdict))
		v.Reason = strings.TrimSpace(v.Reason)
		if v.Ref == "" || seen[v.Ref] {
			continue
		}
		switch v.Verdict {
		case VerdictAgree:
		case VerdictDispute:
			// An objection with no reason is not readable next to the claim,
			// which is the only place a dispute is ever shown.
			if v.Reason == "" {
				continue
			}
		case VerdictDuplicate:
			if v.Of == "" || v.Of == v.Ref {
				continue
			}
		default:
			continue
		}
		seen[v.Ref] = true
		out = append(out, v)
	}
	return out
}
