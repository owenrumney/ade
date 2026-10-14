package sharpen

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheGateFiresOnAPromptWorthALook(t *testing.T) {
	tests := []struct {
		prompt string
		want   bool
	}{
		{"fix the nil check at internal/web/web.go:412", false},
		{"rename `handleAside` to handleBtw", false},
		{"why does TestTheGate fail in sharpen_test.go?", false},
		{"bump the timeout in oneShot() to ten seconds", false},
		{"set max_options to four", false},
		{"/btw why is this slow?", false},
		{"/compact", false},
		{"", false},
		{"   ", false},

		{"make the login faster", true},
		{"yes", true},
		{"e.g. the sidebar should remember its width", true},
		{"fix all the tests in internal/web/chat_test.go", true},
		{"move everything out of web.go", true},
		{"handle errors properly in commit.go", true},
		{"make web.go better", true},
		{"clean up internal/sharpen/sharpen.go", true},
		{"Clean Up internal/sharpen/sharpen.go", true},
		{"refactor handleAside", true},
		{"start refactoring `coldClaude`", true},
		{"make it work in agent.go", true},
		{"the retry in web.go:40 " + strings.Repeat("and so on ", 20), true},

		{"call the handler in web.go", false},
		{"install the wallpaper from assets/wall.png", false},
	}
	for _, tt := range tests {
		t.Run(tt.prompt, func(t *testing.T) {
			assert.Equal(t, tt.want, Wanted(tt.prompt))
		})
	}
}

func TestAReplyIsReadAsChoicesAndNothingElse(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []Question
	}{
		{"a clear prompt", "[]", nil},
		{"silence", "", nil},
		{"prose", "The prompt looks clear to me.", nil},
		{"broken", `[{"ask": "Which?"`, nil},
		{
			"fenced",
			"```json\n[{\"ask\": \"Which tests?\", \"options\": [\"Unit\", \"All\"]}]\n```",
			[]Question{{Ask: "Which tests?", Options: []string{"Unit", "All"}}},
		},
		{
			"a question with no choice is dropped",
			`[{"ask": "What do you mean?", "options": []}, {"ask": "One?", "options": ["only"]}, {"ask": "Which?", "options": ["a", " ", "b"]}]`,
			[]Question{{Ask: "Which?", Options: []string{"a", "b"}}},
		},
		{
			"three questions and three options at most",
			`[{"ask":"1","options":["a","b","c","d"]},{"ask":"2","options":["a","b"]},{"ask":"3","options":["a","b"]},{"ask":"4","options":["a","b"]}]`,
			[]Question{
				{Ask: "1", Options: []string{"a", "b", "c"}},
				{Ask: "2", Options: []string{"a", "b"}},
				{Ask: "3", Options: []string{"a", "b"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Parse(tt.text))
		})
	}
}

func TestAskSendsThePromptAndTheOpenQuestionsUnderTheRole(t *testing.T) {
	var system, sent string
	run := func(_ context.Context, sys, prompt string) (string, error) {
		system, sent = sys, prompt
		return `[{"ask": "Which cache?", "options": ["Disk", "Memory"]}]`, nil
	}
	got, err := Ask(context.Background(), run, "make the cache faster", "- Is the disk cache staying?")
	require.NoError(t, err)
	assert.Equal(t, []Question{{Ask: "Which cache?", Options: []string{"Disk", "Memory"}}}, got)
	assert.Equal(t, Role(), system)
	assert.Contains(t, system, "A question with no options is not allowed.")
	assert.Contains(t, sent, "make the cache faster")
	assert.Contains(t, sent, "Is the disk cache staying?")

	_, err = Ask(context.Background(), run, "make the cache faster", "")
	require.NoError(t, err)
	assert.NotContains(t, sent, "open-questions")
}

func TestAFailedOrLateAnswerIsAnErrorAndNoQuestions(t *testing.T) {
	boom := errors.New("no claude")
	got, err := Ask(context.Background(), func(context.Context, string, string) (string, error) {
		return "", boom
	}, "make it faster", "")
	require.ErrorIs(t, err, boom)
	assert.Empty(t, got)

	var deadline time.Duration
	got, err = Ask(context.Background(), func(ctx context.Context, _, _ string) (string, error) {
		at, _ := ctx.Deadline()
		deadline = time.Until(at)
		return "", context.DeadlineExceeded
	}, "make it faster", "")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Empty(t, got)
	assert.LessOrEqual(t, deadline, Timeout)
	assert.Greater(t, deadline, Timeout-time.Second)
}

func TestOnlyTheOpenQuestionsSectionIsRead(t *testing.T) {
	doc := "# Thing\n\nIntro.\n\n## Risks\n\n- one\n\n## Open questions\n\n- Should it block?\n\n### Later\n\n- Maybe.\n\n```\n## not a heading\n```\n\n## Done\n\nno\n"
	assert.Equal(t, "- Should it block?\n\n### Later\n\n- Maybe.\n\n```\n## not a heading\n```", OpenQuestions(doc))
	assert.Empty(t, OpenQuestions("# Thing\n\n## Risks\n\n- one\n"))
	assert.Empty(t, OpenQuestions(""))
}
