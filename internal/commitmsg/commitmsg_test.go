package commitmsg

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrompt(t *testing.T) {
	p := Prompt()

	assert.Contains(t, p, "/ctb")
	assert.Contains(t, p, fence)
	assert.Contains(t, p, "Do not run git commit")
	assert.Contains(t, p, "do not edit any files")
}

func TestToolsAllowGitButNotEditing(t *testing.T) {
	tools := Tools()

	assert.Contains(t, tools, "Bash", "ctb reads the staged diff, so it needs git")
	assert.NotContains(t, tools, "Edit")
	assert.NotContains(t, tools, "Write")
}

func TestParseFencedBlock(t *testing.T) {
	reply := "Here is the message.\n\n```ade-commit\n" +
		"feat(web): add the commit message modal [FIX-520]\n" +
		"\n" +
		"The review loop had no way to get a message out.\n" +
		"\n" +
		"Adds a modal that shows the generated title and body.\n" +
		"```\n"

	msg, err := Parse(reply)
	require.NoError(t, err)
	assert.Equal(t, "feat(web): add the commit message modal [FIX-520]", msg.Title)
	assert.Equal(t, "The review loop had no way to get a message out.\n\nAdds a modal that shows the generated title and body.", msg.Body)
}

func TestParseEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		reply string
		title string
		body  string
		isErr bool
	}{
		{
			name:  "title only",
			reply: "```ade-commit\nchore: bump deps\n```",
			title: "chore: bump deps",
		},
		{
			name:  "leading blank lines in the block",
			reply: "```ade-commit\n\n\nfix: stop the panic\n\nbody here\n```",
			title: "fix: stop the panic",
			body:  "body here",
		},
		{
			name:  "prefers the last block",
			reply: "```ade-commit\nfix: first\n```\nactually:\n```ade-commit\nfix: second\n```",
			title: "fix: second",
		},
		{
			name:  "body containing a code fence survives",
			reply: "```ade-commit\nfix: quoting\n\nUse this:\n\n```sh\necho hi\n```\n```",
			title: "fix: quoting",
		},
		{
			name:  "falls back to a conventional subject in prose",
			reply: "Here is the commit message:\n\nfeat(api): add pagination [ABC-1]\n\nThe list endpoint returned everything.",
			title: "feat(api): add pagination [ABC-1]",
			body:  "The list endpoint returned everything.",
		},
		{
			name:  "fallback skips the preamble line",
			reply: "I looked at the staged changes.\nfix(auth): reject short tokens\n\nWhy it matters.",
			title: "fix(auth): reject short tokens",
			body:  "Why it matters.",
		},
		{
			name:  "fallback ignores a git command block",
			reply: "feat: add thing\n\nbody\n\n```sh\ngit commit -m \"feat: add thing\"\n```",
			title: "feat: add thing",
			body:  "body",
		},
		{
			name:  "prose with no conventional subject is rejected",
			reply: "I could not find any staged changes, so there is no message to write.",
			isErr: true,
		},
		{
			name:  "refuses to invent a title from a preamble",
			reply: "Here is your commit message, ready to use:\n\nIt updates the parser.",
			isErr: true,
		},
		{
			name:  "empty reply",
			reply: "",
			isErr: true,
		},
		{
			name:  "empty fenced block falls through and fails",
			reply: "```ade-commit\n\n```",
			isErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := Parse(tt.reply)
			if tt.isErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.title, msg.Title)
			if tt.body != "" {
				assert.Equal(t, tt.body, msg.Body)
			}
		})
	}
}

func TestParseAcceptsAllConventionalTypes(t *testing.T) {
	for _, kind := range []string{"feat", "fix", "refactor", "docs", "test", "chore", "perf", "ci", "build", "style", "revert"} {
		t.Run(kind, func(t *testing.T) {
			msg, err := Parse("preamble\n" + kind + ": do a thing")
			require.NoError(t, err)
			assert.Equal(t, kind+": do a thing", msg.Title)
		})
	}

	t.Run("breaking change marker", func(t *testing.T) {
		msg, err := Parse("preamble\nfeat(api)!: drop v1")
		require.NoError(t, err)
		assert.Equal(t, "feat(api)!: drop v1", msg.Title)
	})

	t.Run("an unknown type is not a title", func(t *testing.T) {
		_, err := Parse("preamble\nwibble: do a thing")
		require.Error(t, err)
	})
}

func TestTheCliRefusingTheSkillIsRecognised(t *testing.T) {
	// /ctb is one person's skill and ade does not ship it, so a machine
	// without it answers this rather than writing a message.
	for _, reply := range []string{
		"Unknown command: /ctb",
		"unknown command: /ctb\n",
		"Error: Unknown command: /ctb (did you mean /commit?)",
	} {
		assert.Truef(t, MissingSkill(reply), "%q is the skill being missing", reply)
	}
}

func TestAnOrdinaryBadReplyIsNotTheSkill(t *testing.T) {
	// Falling back on every unparseable reply would run the model twice for
	// an agent that simply wandered off the contract.
	for _, reply := range []string{
		"I could not read the staged diff.",
		"feat: something",
		"",
	} {
		assert.Falsef(t, MissingSkill(reply), "%q is not the skill being missing", reply)
	}
}

func TestTheFallbackCarriesTheSameContract(t *testing.T) {
	// It has to be parseable by the same Parse, or the retry is pointless.
	body := Fallback()
	assert.NotContains(t, body, "/ctb", "the fallback is the one that does not need the skill")
	assert.Contains(t, body, "ade-commit")
	assert.Contains(t, body, "git diff --cached", "it has to read the staged diff itself")

	msg, err := Parse("```ade-commit\nfix(auth): stop dropping the token\n\nThe handler ignored it.\n```")
	require.NoError(t, err)
	assert.Equal(t, "fix(auth): stop dropping the token", msg.Title)
}
