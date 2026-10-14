package design

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Requirement is one line of the design the branch is scored against.
type Requirement struct {
	// ID names where the requirement sits rather than what it says, so a
	// dismissal survives the bullet being reworded and Hash is what expires it.
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Text string `json:"text"`
	Line int    `json:"line"`
	// Hash fingerprints the wording, so a dismissal cannot outlive the words
	// it was about.
	Hash string `json:"hash"`
}

// Drift is one requirement's verdict, as the model reported it.
type Drift struct {
	ID      string `json:"id"`
	Verdict string `json:"verdict"`
	Note    string `json:"note,omitempty"`
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
}

// DriftFence tags the blocks a drift pass emits, one per requirement.
const DriftFence = "ade-drift"

// The verdicts a drift pass may return.
const (
	DriftDone         = "done"
	DriftPartial      = "partial"
	DriftAbsent       = "absent"
	DriftContradicted = "contradicted"
)

// The kinds of requirement a design document yields.
const (
	KindHeading = "heading"
	KindBullet  = "bullet"
)

// maxRequirements bounds the checklist, because it all goes in one prompt.
const maxRequirements = 200

var (
	headingRE = regexp.MustCompile(`^(#{1,6})\s+(.*\S)\s*$`)
	bulletRE  = regexp.MustCompile(`^(\s*)(?:[-*+]|\d+[.)])\s+(.*\S)\s*$`)
	nonSlugRE = regexp.MustCompile(`[^a-z0-9]+`)
	driftRE   = regexp.MustCompile("(?s)```" + DriftFence + `\s*\n(.*?)` + "```")
)

// Requirements reads a design document into the checklist a branch is scored
// against: the headings under ## Approach, and the bullets under them.
//
// Only that section. A design's problem statement and open questions are not
// things the branch is meant to have done, so scoring them reports drift on
// every document. A document with no ## Approach heading is read whole rather
// than scored as nothing.
func Requirements(doc string) []Requirement {
	lines := strings.Split(doc, "\n")
	from, to := approachSpan(lines)

	var out []Requirement
	slugs := map[string]int{}
	section, index, current := "approach", 0, -1
	fenced := false

	for i := from; i < to; i++ {
		line := lines[i]
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced, current = !fenced, -1
			continue
		}
		if fenced {
			continue
		}
		if strings.TrimSpace(line) == "" {
			current = -1
			continue
		}
		if m := headingRE.FindStringSubmatch(line); m != nil {
			section, index, current = unique(slugs, slug(m[2]), "section"), 0, -1
			out = append(out, requirement(section, KindHeading, m[2], i+1))
			continue
		}
		if m := bulletRE.FindStringSubmatch(line); m != nil {
			index++
			out = append(out, requirement(fmt.Sprintf("%s#b%d", section, index), KindBullet, m[2], i+1))
			current = len(out) - 1
			continue
		}
		// Indented prose under a bullet is that bullet wrapping, not a
		// requirement of its own.
		if current >= 0 && line != strings.TrimLeft(line, " \t") {
			out[current].Text += " " + strings.TrimSpace(line)
			out[current].Hash = hashText(out[current].Text)
			continue
		}
		current = -1
	}
	if len(out) > maxRequirements {
		out = out[:maxRequirements]
	}
	return out
}

// DriftPrompt asks for one verdict per requirement in a single pass, whatever
// the checklist's length.
func DriftPrompt(docPath string, reqs []Requirement, diffText string) string {
	var b strings.Builder
	b.WriteString("You are scoring a change against the design it is meant to implement.\n\n")
	fmt.Fprintf(&b, "The design is %s. Read it, and read the code the change touches.\n", docPath)
	b.WriteString("Its requirements are listed below, each with an id, and after them the\nchange itself with new-file line numbers.\n\n")
	b.WriteString("For each requirement emit exactly one block, using its id verbatim:\n\n")
	b.WriteString("```" + DriftFence + "\n{\"id\": \"…\", \"verdict\": \"" + DriftDone + "\"}\n```\n")
	b.WriteString("```" + DriftFence + "\n{\"id\": \"…\", \"verdict\": \"" + DriftPartial + "\", \"note\": \"what is still missing\"}\n```\n")
	b.WriteString("```" + DriftFence + "\n{\"id\": \"…\", \"verdict\": \"" + DriftAbsent + "\"}\n```\n")
	b.WriteString("```" + DriftFence + "\n{\"id\": \"…\", \"verdict\": \"" + DriftContradicted + "\", \"note\": \"what it does instead\", \"file\": \"internal/x/y.go\", \"line\": 42}\n```\n\n")
	b.WriteString("- " + DriftDone + " — the change does what the requirement says.\n")
	b.WriteString("- " + DriftPartial + " — started, not finished. The note says what is left.\n")
	b.WriteString("- " + DriftAbsent + " — nothing in the change addresses it. Prefer this to guessing.\n")
	b.WriteString("- " + DriftContradicted + " — the change does the opposite, or something the\n  requirement rules out. Give the file and the new-file line where it does.\n  The note is filed as a comment on that line, so write it for whoever reads\n  it there.\n\n")
	b.WriteString("A requirement about the design itself, or about work outside this change,\nis " + DriftAbsent + ". Do not edit anything and do not run git.\n")
	b.WriteString("Prose outside the blocks is ignored, so do not write any.\n\n")
	b.WriteString(fence("requirements", renderRequirements(reqs)))
	b.WriteString("\n\n")
	b.WriteString(fence("diff", diffText))
	return b.String()
}

// ParseDrift is the verdicts in a reply. A block that cannot be read is
// dropped rather than guessed at, and the first verdict for an id wins.
func ParseDrift(text string) []Drift {
	var out []Drift
	seen := map[string]bool{}
	for _, m := range driftRE.FindAllStringSubmatch(text, -1) {
		var d Drift
		if err := json.Unmarshal([]byte(m[1]), &d); err != nil {
			continue
		}
		d.ID = strings.TrimSpace(d.ID)
		d.Verdict = strings.ToLower(strings.TrimSpace(d.Verdict))
		d.Note = strings.TrimSpace(d.Note)
		d.File = strings.TrimSpace(d.File)
		if d.ID == "" || !validDrift(d.Verdict) || seen[d.ID] {
			continue
		}
		if d.Verdict != DriftContradicted {
			d.File, d.Line = "", 0
		}
		seen[d.ID] = true
		out = append(out, d)
	}
	return out
}

// HashRequirement fingerprints a requirement's wording, for callers holding a
// dismissal against it.
func HashRequirement(text string) string { return hashText(text) }

func requirement(id, kind, text string, line int) Requirement {
	text = strings.TrimSpace(text)
	return Requirement{ID: id, Kind: kind, Text: text, Line: line, Hash: hashText(text)}
}

func renderRequirements(reqs []Requirement) string {
	var b strings.Builder
	for _, r := range reqs {
		b.WriteString(r.ID)
		b.WriteString(" (")
		b.WriteString(r.Kind)
		b.WriteString(", line ")
		b.WriteString(strconv.Itoa(r.Line))
		b.WriteString("): ")
		b.WriteString(r.Text)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ApproachHeadings say which part of a design document is the work. Every
// enhancement document here says "What this builds", so one name was not enough.
var ApproachHeadings = []string{"Approach", "What this builds", "What it builds"}

// theApproach is whether a heading opens the section that gets scored.
func theApproach(heading string) bool {
	for _, name := range ApproachHeadings {
		if strings.EqualFold(heading, name) {
			return true
		}
	}
	return false
}

// approachSpan is the lines under the approach heading, or the whole
// document when it has none.
func approachSpan(lines []string) (int, int) {
	fenced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		m := headingRE.FindStringSubmatch(line)
		if m == nil || !theApproach(m[2]) {
			continue
		}
		for j, level := i+1, len(m[1]); j < len(lines); j++ {
			if strings.HasPrefix(strings.TrimSpace(lines[j]), "```") {
				fenced = !fenced
				continue
			}
			if fenced {
				continue
			}
			if h := headingRE.FindStringSubmatch(lines[j]); h != nil && len(h[1]) <= level {
				return i + 1, j
			}
		}
		return i + 1, len(lines)
	}
	// A one-pager with no Approach is all approach. A sectioned document with
	// none has not said which part is the work, so nothing is scored.
	if sectioned(lines) {
		return 0, 0
	}
	return 0, len(lines)
}

// sectioned is whether the document is divided into sections at all.
func sectioned(lines []string) bool {
	fenced := false
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if m := headingRE.FindStringSubmatch(line); m != nil && len(m[1]) > 1 {
			return true
		}
	}
	return false
}

func validDrift(verdict string) bool {
	switch verdict {
	case DriftDone, DriftPartial, DriftAbsent, DriftContradicted:
		return true
	}
	return false
}

func hashText(text string) string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(text), " ")))
	return hex.EncodeToString(sum[:8])
}

func slug(text string) string {
	return strings.Trim(nonSlugRE.ReplaceAllString(strings.ToLower(text), "-"), "-")
}

// unique keeps ids distinct when two headings slug the same.
func unique(seen map[string]int, s, fallback string) string {
	if s == "" {
		s = fallback
	}
	seen[s]++
	if n := seen[s]; n > 1 {
		return fmt.Sprintf("%s-%d", s, n)
	}
	return s
}
