package outgoing

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/comments"
)

func note(id, file string, line int, body string) comments.Thread {
	return comments.Thread{
		ID: id, File: file, Line: line,
		Comments: []comments.Comment{{Author: "agent", Body: body, At: time.Now()}},
	}
}

func TestThePromptCarriesEveryNoteAndItsPlace(t *testing.T) {
	prompt := Prompt([]comments.Thread{
		note("t3", "internal/web/agent.go", 42, "retry loop never resets the counter"),
		note("t7", "cmd/ade/main.go", 8, "this flag is undocumented"),
	})

	// One call for all of them: they are short, they share a voice, and the
	// round trip is most of the cost.
	for _, want := range []string{
		`id="t3"`, `file="internal/web/agent.go"`, "line=42",
		"retry loop never resets the counter",
		`id="t7"`, "this flag is undocumented",
	} {
		assert.Containsf(t, prompt, want, "the prompt is missing %s", want)
	}

	// It is a rephrase, not a review: nothing to read, nothing to run.
	assert.Contains(t, prompt, "Do not read any file")
	assert.Empty(t, Tools())
}

func TestThePromptSaysToKeepTheSubstance(t *testing.T) {
	prompt := Prompt([]comments.Thread{note("t1", "a.go", 1, "nil deref")})

	// The danger in rewriting someone's findings is losing the finding. A
	// softened bug report is worse than a blunt one.
	assert.Contains(t, prompt, "Keep the technical content exactly")
	assert.Contains(t, prompt, "do not add")
	assert.Contains(t, prompt, "First person")
}

func TestParseReadsTheFencedObject(t *testing.T) {
	got, err := Parse("Here you go:\n\n```ade-outgoing\n" +
		`{"t3": "The retry loop never resets the counter.", "t7": "Could we document this flag?"}` +
		"\n```\n\nHope that helps.")
	require.NoError(t, err)

	assert.Equal(t, "The retry loop never resets the counter.", got["t3"])
	assert.Equal(t, "Could we document this flag?", got["t7"])
	assert.Len(t, got, 2)
}

func TestParseCopesWithTheUsualVariations(t *testing.T) {
	// A json fence, and bare JSON, are both still an answer.
	for _, reply := range []string{
		"```json\n{\"t1\":\"text\"}\n```",
		`{"t1":"text"}`,
		"\n\n" + `{"t1":"text"}` + "\n",
	} {
		got, err := Parse(reply)
		require.NoErrorf(t, err, "reply: %q", reply)
		assert.Equal(t, "text", got["t1"])
	}
}

func TestParseDropsEmptyRewrites(t *testing.T) {
	// An empty comment would post as an empty comment.
	got, err := Parse("```ade-outgoing\n" + `{"t1":"real", "t2":"", "t3":"   "}` + "\n```")
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Contains(t, got, "t1")
}

func TestParseRefusesWhatItCannotRead(t *testing.T) {
	for _, reply := range []string{"", "   ", "I could not do that.", "```ade-outgoing\n{oops\n```"} {
		_, err := Parse(reply)
		assert.Errorf(t, err, "accepted %q", reply)
	}
}

func TestThePromptCapsTheLength(t *testing.T) {
	// The rewrite drifts long without a number to hold it to: "short" and
	// "concise" are read as suggestions, a word count is not. It also carries
	// a wordy example marked as wrong, which holds better than a rule.
	prompt := Prompt([]comments.Thread{note("t1", "a.go", 1, "nil deref")})

	assert.Contains(t, prompt, "One to three sentences")
	assert.Contains(t, prompt, "Under 40 words")
	assert.Contains(t, prompt, "must not become")
	assert.Contains(t, prompt, "Send the short one.")

	// The escape hatch that let it run to a second paragraph is gone.
	assert.NotContains(t, prompt, "Two only if")
}

func TestOnlyTheFindingIsRewritten(t *testing.T) {
	thread := note("t1", "a.go", 1, "the finding")
	thread.Comments = append(thread.Comments,
		comments.Comment{Author: comments.AuthorYou, Body: "note to self: check this later"})

	// Replies on a thread are your working notes. They are not for the author.
	prompt := Prompt([]comments.Thread{thread})
	assert.Contains(t, prompt, "the finding")
	assert.NotContains(t, prompt, "note to self")
}

// A review hands one line to several agents, and each leaves its own comment.
// Rewriting the first and dropping the rest loses most of the review.
func TestEveryReviewersFindingIsRewritten(t *testing.T) {
	thread := note("t1", "a.go", 1, "the fallback skips the hardening")
	thread.Comments[0].Author = "staff-reviewer"
	thread.Comments = append(thread.Comments,
		comments.Comment{Author: "security", Body: "the branch name is not escaped"},
		comments.Comment{Author: comments.AuthorYou, Body: "note to self: check this later"},
		comments.Comment{Author: "architect", Body: "and there is no timeout"})

	prompt := Prompt([]comments.Thread{thread})
	assert.Contains(t, prompt, "the fallback skips the hardening")
	assert.Contains(t, prompt, "the branch name is not escaped")
	assert.Contains(t, prompt, "and there is no timeout")
	assert.Contains(t, prompt, "security found:", "whose finding it was")
	assert.NotContains(t, prompt, "note to self", "your own replies are still yours")
}

// One comment goes to the author, however many reviewers found something.
func TestThePromptAsksForOneCommentCoveringThemAll(t *testing.T) {
	prompt := Prompt([]comments.Thread{note("t1", "a.go", 1, "x")})
	assert.Contains(t, prompt, "One comment, covering every one of them")
	assert.Contains(t, prompt, "Attribute nothing")
}

func TestTheFastModelIsTheDefault(t *testing.T) {
	// A rephrase of text you already wrote, with no code to read: not the big
	// model, and not the smallest either, which adds fixes the note did not
	// make. The prompt does not name it, so changing one is not changing both.
	assert.Equal(t, "sonnet", FastModel)
	assert.False(t, strings.Contains(Prompt(nil), "sonnet"))
}
