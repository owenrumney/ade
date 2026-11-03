package walkthrough

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inDiff stands in for the file list the diff shows.
func inDiff(paths ...string) func(string) bool {
	set := map[string]bool{}
	for _, path := range paths {
		set[path] = true
	}
	return func(path string) bool { return set[path] }
}

func step(body string) string {
	return "```" + StepFence + "\n" + body + "\n```\n"
}

func flowBlock(body string) string {
	return "```" + FlowFence + "\n" + body + "\n```\n"
}

const good = `{"ref":"S1","title":"The limit is read once","file":"a.go","line":12,` +
	`"endLine":20,"node":"config","body":"Every caller loses its error path."}`

const twoNodes = `{"nodes":[{"id":"config","label":"Client config","file":"a.go","line":12},` +
	`{"id":"api","label":"GitHub API"}],"edges":[{"from":"config","to":"api","label":"one request"}]}`

func TestParseKeepsAGoodStepAndItsFlow(t *testing.T) {
	steps, flow := Parse(step(good)+flowBlock(twoNodes), inDiff("a.go"))

	require.Len(t, steps, 1)
	assert.Equal(t, "S1", steps[0].Ref)
	assert.Equal(t, "a.go", steps[0].File)
	assert.Equal(t, 12, steps[0].Line)
	assert.Equal(t, 20, steps[0].EndLine)
	assert.Equal(t, "config", steps[0].Node)

	require.NotNil(t, flow)
	assert.Len(t, flow.Nodes, 2)
	require.Len(t, flow.Edges, 1)
	assert.Equal(t, "one request", flow.Edges[0].Label)
}

func TestParseKeepsTheStepsInTheOrderTheyCame(t *testing.T) {
	one := `{"ref":"S1","title":"First","file":"a.go","line":1,"body":"One."}`
	two := `{"ref":"S2","title":"Second","file":"a.go","line":4,"body":"Two."}`
	three := `{"ref":"S3","title":"Third","file":"a.go","line":9,"body":"Three."}`

	steps, _ := Parse(step(one)+step(two)+step(three), inDiff("a.go"))

	require.Len(t, steps, 3)
	assert.Equal(t, []string{"S1", "S2", "S3"},
		[]string{steps[0].Ref, steps[1].Ref, steps[2].Ref})
}

func TestParseDropsWhatCannotBeDrawn(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"no ref", `{"ref":" ","title":"T","file":"a.go","line":1,"body":"B."}`},
		{"no title", `{"ref":"S1","title":"","file":"a.go","line":1,"body":"B."}`},
		{"no body", `{"ref":"S1","title":"T","file":"a.go","line":1,"body":"  "}`},
		{"no line", `{"ref":"S1","title":"T","file":"a.go","line":0,"body":"B."}`},
		{"a file outside the diff", `{"ref":"S1","title":"T","file":"elsewhere.go","line":1,"body":"B."}`},
		{"not json", `{"ref":"S1",`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			steps, _ := Parse(step(tc.body), inDiff("a.go"))
			assert.Empty(t, steps)
		})
	}
}

func TestADuplicateRefIsTakenOnce(t *testing.T) {
	steps, _ := Parse(step(good)+step(good), inDiff("a.go"))
	assert.Len(t, steps, 1)
}

func TestAnEndLineBeforeItsStartIsTheOneLine(t *testing.T) {
	steps, _ := Parse(step(`{"ref":"S1","title":"T","file":"a.go","line":12,"endLine":3,"body":"B."}`),
		inDiff("a.go"))

	require.Len(t, steps, 1)
	assert.Equal(t, 12, steps[0].EndLine)
}

func TestNoMoreStepsThanAnyoneReads(t *testing.T) {
	var text strings.Builder
	for _, ref := range []string{"S1", "S2", "S3", "S4", "S5", "S6", "S7", "S8", "S9"} {
		text.WriteString(step(`{"ref":"` + ref + `","title":"T","file":"a.go","line":1,"body":"B."}`))
	}
	steps, _ := Parse(text.String(), inDiff("a.go"))
	assert.Len(t, steps, MaxSteps)
}

// A node nobody declared lights nothing, but the step still reads.
func TestAStepNamingAnUnknownNodeKeepsItsWords(t *testing.T) {
	body := `{"ref":"S1","title":"T","file":"a.go","line":1,"node":"ghost","body":"B."}`

	steps, _ := Parse(step(body)+flowBlock(twoNodes), inDiff("a.go"))

	require.Len(t, steps, 1)
	assert.Empty(t, steps[0].Node)
}

// The flow is left out when the change has no shape, and the panel hides the
// pane rather than drawing one box.
func TestAFlowOfOneBoxIsNoFlow(t *testing.T) {
	_, flow := Parse(flowBlock(`{"nodes":[{"id":"only","label":"The one thing"}]}`), inDiff("a.go"))
	assert.Nil(t, flow)
}

func TestNoFlowBlockIsNoFlow(t *testing.T) {
	_, flow := Parse(step(good), inDiff("a.go"))
	assert.Nil(t, flow)
}

// An arrow to a box that is not on the diagram cannot be drawn.
func TestAnEdgeToAnUndeclaredNodeIsDropped(t *testing.T) {
	body := `{"nodes":[{"id":"a","label":"A"},{"id":"b","label":"B"}],` +
		`"edges":[{"from":"a","to":"b"},{"from":"b","to":"gone"}]}`

	_, flow := Parse(flowBlock(body), inDiff("a.go"))

	require.NotNil(t, flow)
	assert.Len(t, flow.Edges, 1)
}

func TestThePromptCarriesTheDiffAndTheContract(t *testing.T) {
	got := Prompt("@@ -1 +1 @@\n-var x = 1\n+var x = 2\n")

	assert.Contains(t, got, "var x = 2")
	assert.Contains(t, got, StepFence)
	assert.Contains(t, got, FlowFence)
}

func TestTheRunReadsAndRunsNothing(t *testing.T) {
	assert.NotContains(t, Tools(), "Bash")
	assert.Contains(t, Tools(), "Read")
}
