// Package walkthrough asks an agent to read a change back as ordered steps,
// each pinned to a line range: the order is what the diff cannot say.
package walkthrough

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/owenrumney/ade/internal/prompt"
)

// Step is one stop on the walk.
type Step struct {
	Ref     string `json:"ref"`
	Title   string `json:"title"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	EndLine int    `json:"endLine"`
	Body    string `json:"body"`

	// Node is the flow node this step lights, empty when the flow has none
	// by that id or there is no flow.
	Node string `json:"node"`

	// Excerpt is cut by the caller, not the agent: the panel draws the code
	// beside the words without a fetch per step.
	Excerpt []Line `json:"excerpt"`
}

// Line is one line of an excerpt. In marks the step's own lines, as against
// the context either side.
type Line struct {
	N    int    `json:"n"`
	Text string `json:"text"`
	In   bool   `json:"in"`
}

// Flow is the shape the steps move through, drawn once and lit a node at a
// time. Nil when the change has no shape worth drawing.
type Flow struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// Node is one box. File and Line are optional, for the box that is a place.
type Node struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	File  string `json:"file"`
	Line  int    `json:"line"`
}

// Edge is one arrow, with the words on it when they help.
type Edge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"`
}

// The fences the prompt asks for: the steps, and the one flow they walk.
const (
	StepFence = "ade-step"
	FlowFence = "ade-flow"
)

const (
	// MinSteps is below which this is not a walkthrough of anything.
	MinSteps = 3
	// MaxSteps is what the prompt asks for; past it nobody is still reading.
	MaxSteps = 7
	// minNodes is below which a diagram says less than its own caption.
	minNodes = 2
)

//go:embed walkthrough.md
var contract string

var (
	stepRE = regexp.MustCompile("(?s)```" + StepFence + "\\s*\\n(.*?)```")
	flowRE = regexp.MustCompile("(?s)```" + FlowFence + "\\s*\\n(.*?)```")
)

// Prompt is the contract with the rendered diff under it.
func Prompt(diffText string) string {
	return strings.TrimSpace(contract) + "\n\n" + prompt.Fence("diff", diffText) + "\n"
}

// Tools let the agent read for the context a hunk does not carry — the caller,
// the test, the interface. No Bash: the diff is handed over rendered.
func Tools() []string { return []string{"Read", "Grep", "Glob"} }

// Parse keeps the steps that can be drawn and the flow they can walk, dropping
// a block that will not read. inDiff says whether a path is one the diff shows.
func Parse(text string, inDiff func(string) bool) ([]Step, *Flow) {
	flow := parseFlow(text)
	var out []Step
	seen := map[string]bool{}
	for _, match := range stepRE.FindAllStringSubmatch(text, -1) {
		var step Step
		if err := json.Unmarshal([]byte(match[1]), &step); err != nil {
			continue
		}
		if !step.tidy() || seen[step.Ref] || !inDiff(step.File) {
			continue
		}
		// A step that names a node nobody declared still reads; it just
		// lights nothing.
		if !flow.has(step.Node) {
			step.Node = ""
		}
		seen[step.Ref] = true
		out = append(out, step)
		if len(out) == MaxSteps {
			break
		}
	}
	return out, flow
}

// parseFlow takes the first block that draws. A second one is the agent
// changing its mind, and two diagrams is one too many.
func parseFlow(text string) *Flow {
	for _, match := range flowRE.FindAllStringSubmatch(text, -1) {
		var flow Flow
		if err := json.Unmarshal([]byte(match[1]), &flow); err != nil {
			continue
		}
		if flow.tidy() {
			return &flow
		}
	}
	return nil
}

func (f *Flow) has(id string) bool {
	if f == nil || id == "" {
		return false
	}
	for _, node := range f.Nodes {
		if node.ID == id {
			return true
		}
	}
	return false
}

// tidy trims the flow and reports whether what is left is worth drawing.
func (f *Flow) tidy() bool {
	nodes := make([]Node, 0, len(f.Nodes))
	known := map[string]bool{}
	for _, node := range f.Nodes {
		node.ID = strings.TrimSpace(node.ID)
		node.Label = strings.TrimSpace(node.Label)
		node.File = strings.TrimSpace(node.File)
		if node.ID == "" || node.Label == "" || known[node.ID] {
			continue
		}
		known[node.ID] = true
		nodes = append(nodes, node)
	}
	edges := make([]Edge, 0, len(f.Edges))
	for _, edge := range f.Edges {
		edge.From = strings.TrimSpace(edge.From)
		edge.To = strings.TrimSpace(edge.To)
		edge.Label = strings.TrimSpace(edge.Label)
		if !known[edge.From] || !known[edge.To] {
			continue
		}
		edges = append(edges, edge)
	}
	f.Nodes, f.Edges = nodes, edges
	return len(nodes) >= minNodes
}

// tidy trims the fields and reports whether what is left is a step that can
// be drawn and opened.
func (s *Step) tidy() bool {
	s.Ref = strings.ToUpper(strings.TrimSpace(s.Ref))
	s.Title = strings.TrimSpace(s.Title)
	s.File = strings.TrimSpace(s.File)
	s.Body = strings.TrimSpace(s.Body)
	s.Node = strings.TrimSpace(s.Node)
	if s.EndLine < s.Line {
		s.EndLine = s.Line
	}
	return s.Ref != "" && s.Title != "" && s.File != "" && s.Body != "" && s.Line > 0
}
