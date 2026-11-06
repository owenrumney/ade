package oneshot

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

// after returns the value following flag, or "" if the flag is absent.
func after(args []string, flag string) string {
	at := slices.Index(args, flag)
	if at < 0 || at+1 >= len(args) {
		return ""
	}
	return args[at+1]
}

// pin replaces the standing instruction for one test.
func pin(t *testing.T, text string) {
	t.Helper()
	was := standing
	standing = func() string { return text }
	t.Cleanup(func() { standing = was })
}

func TestTheHouseRulesReachEveryRunThatCanEditCode(t *testing.T) {
	// Without this the machine's own settings would leak into these.
	pin(t, "")

	t.Run("a run that can write gets them", func(t *testing.T) {
		args := argv(Opts{Prompt: "apply the findings", Tools: []string{"Read", "Edit"}})
		assert.Contains(t, after(args, "--append-system-prompt"), "Comments in code you write or edit")
	})

	t.Run("a read-only reviewer does not", func(t *testing.T) {
		// Its "comments" are review comments.
		args := argv(Opts{Prompt: "review this", Tools: []string{"Read", "Grep", "Glob"}})
		assert.NotContains(t, args, "--append-system-prompt")
	})

	t.Run("a read-only run keeps a system prompt it was given", func(t *testing.T) {
		args := argv(Opts{Prompt: "review this", Tools: []string{"Read"}, SystemPrompt: "be adversarial"})
		assert.Equal(t, "be adversarial", after(args, "--append-system-prompt"))
	})

	t.Run("a run with Claude Code's default tools does not", func(t *testing.T) {
		args := argv(Opts{Prompt: "rewrite this comment"})
		assert.NotContains(t, args, "--append-system-prompt")
	})
}

func TestEditsCode(t *testing.T) {
	for _, tools := range [][]string{
		{"Write"}, {"Edit"}, {"MultiEdit"}, {"NotebookEdit"}, {"Read", "Grep", "Edit"},
	} {
		assert.Truef(t, editsCode(tools), "%v can write", tools)
	}
	for _, tools := range [][]string{
		nil, {}, {"Read"}, {"Read", "Grep", "Glob", "Bash"},
	} {
		assert.Falsef(t, editsCode(tools), "%v cannot write", tools)
	}
}

func TestTheWritingSearchAndToolRulesReachEveryNamedRun(t *testing.T) {
	pin(t, "")

	reviewer := systemPrompt(Opts{Agent: "staff-reviewer", Tools: []string{"Read", "Grep", "Glob"}})
	editor := systemPrompt(Opts{Agent: "fixer", Tools: []string{"Read", "Edit"}})
	for _, rules := range []string{writingRules, searchRules, toolRules} {
		assert.Contains(t, reviewer, rules)
		assert.Contains(t, editor, rules)
	}
	assert.NotContains(t, reviewer, houseRules)
	assert.Contains(t, editor, houseRules)

	assert.Contains(t, writingRules, "A reply is 150 words or fewer")
	assert.Contains(t, writingRules, "at the size it was asked")
	assert.Contains(t, searchRules, "this\n  wins")
	assert.Contains(t, toolRules, "mise use -g")
	assert.Contains(t, toolRules, "which <tool>")
}

// The prose once said three while the test enforced two.
func TestTheCommentRuleQuotesTheCapTheTestEnforces(t *testing.T) {
	assert.Contains(t, houseRules, "2 lines is the limit")
	assert.Contains(t, houseRules, "the 2-line limit")
}

func TestABareRewriteIsLeftAlone(t *testing.T) {
	pin(t, "")
	assert.Equal(t, "rewrite this comment", systemPrompt(Opts{SystemPrompt: "rewrite this comment"}))
}

func TestAReadOnlyRunStillGetsTheStandingInstruction(t *testing.T) {
	pin(t, "only ever drive the local dev environment")

	got := systemPrompt(Opts{Agent: "architect", Tools: []string{"Read"}})

	assert.Contains(t, got, "only ever drive the local dev environment")
	assert.Contains(t, got, userInstruction)
}
