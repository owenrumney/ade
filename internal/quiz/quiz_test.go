package quiz

import (
	"math/rand"
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

func block(body string) string {
	return "```" + Fence + "\n" + body + "\n```\n"
}

const good = `{"ref":"Q1","file":"a.go","line":12,"about":"b.go",` +
	`"question":"What does the caller get now?","options":["Nothing","An error","A zero"],` +
	`"answer":"B","why":"The early return skips the assignment."}`

func TestParseKeepsAGoodBlock(t *testing.T) {
	got := Parse(block(good), inDiff("a.go"))

	require.Len(t, got, 1)
	assert.Equal(t, "Q1", got[0].Ref)
	assert.Equal(t, "a.go", got[0].File)
	assert.Equal(t, 12, got[0].Line)
	assert.Equal(t, "b.go", got[0].About)
	assert.Equal(t, "B", got[0].Answer)
	assert.Len(t, got[0].Options, 3)
}

func TestParseDropsWhatCannotBeAskedOrMarked(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"two options", `{"ref":"Q1","file":"a.go","line":1,"question":"Why?","options":["One","Two"],"answer":"A","why":"x"}`},
		{"duplicate options", `{"ref":"Q1","file":"a.go","line":1,"question":"Why?","options":["One","One","Two"],"answer":"A","why":"x"}`},
		{"a letter outside the options", `{"ref":"Q1","file":"a.go","line":1,"question":"Why?","options":["One","Two","Three"],"answer":"D","why":"x"}`},
		{"an index rather than a letter", `{"ref":"Q1","file":"a.go","line":1,"question":"Why?","options":["One","Two","Three"],"answer":"0","why":"x"}`},
		{"no ref", `{"ref":" ","file":"a.go","line":1,"question":"Why?","options":["One","Two","Three"],"answer":"A","why":"x"}`},
		{"no question", `{"ref":"Q1","file":"a.go","line":1,"question":"  ","options":["One","Two","Three"],"answer":"A","why":"x"}`},
		{"a file outside the diff", `{"ref":"Q1","file":"elsewhere.go","line":1,"question":"Why?","options":["One","Two","Three"],"answer":"A","why":"x"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Empty(t, Parse(block(tc.body), inDiff("a.go")))
		})
	}
}

func TestParseDropsARepeatedRef(t *testing.T) {
	second := strings.Replace(good, `"question":"What does the caller get now?"`,
		`"question":"A second question under the same ref"`, 1)
	got := Parse(block(good)+block(second), inDiff("a.go"))

	require.Len(t, got, 1)
	assert.Equal(t, "What does the caller get now?", got[0].Question)
}

// One unreadable question must not take the set with it.
func TestAMalformedBlockLeavesTheGoodOneStanding(t *testing.T) {
	got := Parse(block(good)+block(`{"ref":"Q2", not json`), inDiff("a.go"))

	require.Len(t, got, 1)
	assert.Equal(t, "Q1", got[0].Ref)
}

// A bad second link is not fatal: a sound question should not die over it.
func TestABadAboutIsKept(t *testing.T) {
	body := strings.Replace(good, `"about":"b.go"`, `"about":"  "`, 1)
	got := Parse(block(body), inDiff("a.go"))

	require.Len(t, got, 1)
	assert.Empty(t, got[0].About)
}

func TestParseStopsAtFiveQuestions(t *testing.T) {
	var text string
	for _, ref := range []string{"Q1", "Q2", "Q3", "Q4", "Q5", "Q6", "Q7"} {
		text += block(strings.Replace(good, `"ref":"Q1"`, `"ref":"`+ref+`"`, 1))
	}
	assert.Len(t, Parse(text, inDiff("a.go")), MaxQuestions)
}

func TestShuffleMovesTheLetterWithTheAnswer(t *testing.T) {
	all := Parse(block(good), inDiff("a.go"))
	require.Len(t, all, 1)
	right := all[0].Options[1]

	// Several seeds: one that happens not to move the answer proves nothing.
	moved := false
	for seed := int64(0); seed < 20; seed++ {
		got := Shuffle(all, rand.New(rand.NewSource(seed))) // #nosec G404 -- a shuffle, not a secret
		require.Len(t, got, 1)
		assert.ElementsMatch(t, all[0].Options, got[0].Options)
		assert.Equal(t, right, got[0].Options[index(got[0].Answer)],
			"the letter must still name the right option")
		if got[0].Answer != all[0].Answer {
			moved = true
		}
	}
	assert.True(t, moved, "20 shuffles that never move the answer is not a shuffle")
}

func TestThePromptCarriesTheDiffAndTheContract(t *testing.T) {
	out := Prompt("--- a/a.go\n+++ b/a.go\n")

	assert.Contains(t, out, Fence, "the output contract names the fence")
	assert.Contains(t, out, "<diff>")
	assert.Contains(t, out, "+++ b/a.go")
	assert.NotContains(t, Tools(), "Bash", "the diff is handed over rendered")
}
