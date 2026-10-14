package reply

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
)

func thread() comments.Thread {
	return comments.Thread{
		File: "internal/cart/cart.go", Side: comments.SideRight, Line: 24,
		ExternalID: "991",
		Comments: []comments.Comment{
			{Author: "reviewer", Body: "why not use a map here?"},
		},
	}
}

func TestPromptAsksForWordsNotChanges(t *testing.T) {
	prompt := Prompt(nil, thread())

	assert.Contains(t, prompt, "internal/cart/cart.go")
	assert.Contains(t, prompt, "line 24")
	assert.Contains(t, prompt, "why not use a map here?")
	assert.Contains(t, prompt, `author="reviewer"`)

	// The whole reason this is separate from Resolve: a comment that deserves an
	// argument must not quietly become a commit.
	assert.Contains(t, prompt, "Do NOT edit any file")
	assert.Contains(t, prompt, "separate action")
}

func TestPromptAllowsDisagreement(t *testing.T) {
	prompt := Prompt(nil, thread())

	// A reply that can only agree is worthless.
	assert.Contains(t, prompt, "If they are wrong, say why")
	assert.Contains(t, prompt, "ask the question")
}

func TestPromptQuotesTheCodeUnderTheComment(t *testing.T) {
	diff := &gitdiff.Diff{Files: []gitdiff.File{{
		Path: "internal/cart/cart.go",
		Hunks: []gitdiff.Hunk{{
			Rows: []gitdiff.Row{
				{Right: &gitdiff.Cell{Line: 24, Kind: gitdiff.KindAdd, Content: "for _, i := range items {"}},
				{
					Left:  &gitdiff.Cell{Line: 25, Kind: gitdiff.KindContext, Content: "  total += i.Price"},
					Right: &gitdiff.Cell{Line: 25, Kind: gitdiff.KindContext, Content: "  total += i.Price"},
				},
			},
		}},
	}}}

	prompt := Prompt(diff, thread())
	assert.Contains(t, prompt, "<code>")
	assert.Contains(t, prompt, "+ for _, i := range items {")
	assert.Contains(t, prompt, "  total += i.Price")
}

func TestPromptWithoutMatchingCode(t *testing.T) {
	// An orphaned comment still deserves an answer; it just has no hunk to quote.
	prompt := Prompt(&gitdiff.Diff{}, thread())
	assert.NotContains(t, prompt, "<code>")
	assert.Contains(t, prompt, "why not use a map here?")
}

func TestPromptCarriesTheWholeConversation(t *testing.T) {
	full := thread()
	full.Comments = append(full.Comments,
		comments.Comment{Author: "you", Body: "the order matters here"},
		comments.Comment{Author: "reviewer", Body: "does it? say more"})

	prompt := Prompt(nil, full)
	for _, want := range []string{"why not use a map here?", "the order matters here", "does it? say more"} {
		assert.Contains(t, prompt, want)
	}
	assert.Equal(t, 3, strings.Count(prompt, "<comment author="))
}

func TestRenderTruncatesAHugeHunk(t *testing.T) {
	var rows []gitdiff.Row
	for i := range maxHunkRows * 2 {
		rows = append(rows, gitdiff.Row{Right: &gitdiff.Cell{Line: i, Kind: gitdiff.KindAdd, Content: "x"}})
	}
	out := render(gitdiff.Hunk{Rows: rows})

	require.Contains(t, out, "…")
	assert.Less(t, strings.Count(out, "\n"), maxHunkRows+3)
}

// Drafts came back long and opened by agreeing with the reviewer before
// saying anything. Both are named in the prompt now.
func TestThePromptRefusesLengthAndFlattery(t *testing.T) {
	p := Prompt(nil, comments.Thread{File: "a.go", Line: 1})
	for _, want := range []string{
		"Under 60 words", "Never open by agreeing", "You're right", "Good catch",
		"worse than that", "Courteous means plain and direct",
		"Do not restate the comment",
	} {
		assert.Contains(t, p, want)
	}
}
