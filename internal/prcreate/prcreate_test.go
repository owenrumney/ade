package prcreate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolsCanPushAndCallGH(t *testing.T) {
	// Bash for git and gh; Write because the skill passes the body with
	// --body-file rather than inline on the command line.
	for _, tool := range []string{"Bash", "Write", "Read"} {
		assert.Contains(t, Tools(), tool)
	}
}

func TestPromptRunsTheSkill(t *testing.T) {
	prompt := Prompt("")

	assert.Contains(t, prompt, "/pr-create")
	assert.Contains(t, prompt, "Follow the skill as written")
	assert.Contains(t, prompt, fence)
	assert.NotContains(t, prompt, "<template>", "no template, nothing to say about one")
}

func TestPromptCarriesTheTemplate(t *testing.T) {
	// A template that is a checklist only: the shape that got dropped when the
	// skill was left to find the template itself.
	template := "### Review bot :robot:\n- [ ] TBC: `/review` risk score"
	prompt := Prompt(template)

	assert.Contains(t, prompt, "<template>")
	assert.Contains(t, prompt, "### Review bot :robot:")
	assert.Contains(t, prompt, "- [ ] TBC:")
	assert.Contains(t, prompt, "put your prose first and keep every line")
	assert.Contains(t, prompt, "never tick a")
}

func TestTemplate(t *testing.T) {
	t.Run("no template", func(t *testing.T) {
		_, ok := Template(t.TempDir())
		assert.False(t, ok)
	})

	t.Run("every known location", func(t *testing.T) {
		for _, name := range templatePaths {
			root := t.TempDir()
			full := filepath.Join(root, filepath.FromSlash(name))
			require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
			require.NoError(t, os.WriteFile(full, []byte("### Checklist\n- [ ] thing\n"), 0o600))

			body, ok := Template(root)
			require.Truef(t, ok, "did not find %s", name)
			assert.Contains(t, body, "### Checklist")
		}
	})

	t.Run("an empty file is not a template", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".github"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, ".github", "pull_request_template.md"), []byte("  \n"), 0o600))
		_, ok := Template(root)
		assert.False(t, ok)
	})
}

func TestKept(t *testing.T) {
	template := "### Review bot :robot:\n- [ ] TBC: `/review` risk score"

	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "template preserved below the prose",
			body: "This PR fixes a thing.\n\n### Review bot :robot:\n- [ ] TBC: `/review` risk score",
			want: true,
		},
		{
			// What the skill did when it went looking for the template itself.
			name: "prose only, template dropped",
			body: "This PR fixes orphaned check executions.\n\n## Problem\n\nStuff.",
		},
		{
			name: "heading kept but the checklist dropped",
			body: "Prose.\n\n### Review bot :robot:",
		},
		{
			name: "box ticked on the author's behalf",
			body: "Prose.\n\n### Review bot :robot:\n- [x] TBC: `/review` risk score",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Kept(template, tt.body))
		})
	}
}

func TestKeptIgnoresProseGuidance(t *testing.T) {
	// Prose in a template is guidance the skill is meant to replace, so its
	// absence is not a dropped template.
	template := "## Description\n\n> DESCRIPTION GOES HERE\n\n- [ ] tested"
	assert.True(t, Kept(template, "## Description\n\nWhat I did.\n\n- [ ] tested"))
}

func TestParseURL(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		want  string
		isErr bool
	}{
		{
			name: "from the fenced block",
			text: "Opened it.\n```" + fence + "\nhttps://github.com/o/r/pull/42\n```",
			want: "https://github.com/o/r/pull/42",
		},
		{
			name: "fenced block wins over an earlier mention",
			text: "See https://github.com/o/r/pull/1\n```" + fence + "\nhttps://github.com/o/r/pull/42\n```",
			want: "https://github.com/o/r/pull/42",
		},
		{
			name: "bare prose falls back to the last URL",
			text: "A PR already exists: https://github.com/o/r/pull/7",
			want: "https://github.com/o/r/pull/7",
		},
		{
			name: "enterprise host",
			text: "```" + fence + "\nhttps://git.corp.example.com/o/r/pull/9\n```",
			want: "https://git.corp.example.com/o/r/pull/9",
		},
		{
			name: "an empty block falls back to prose",
			text: "https://github.com/o/r/pull/3\n```" + fence + "\n\n```",
			want: "https://github.com/o/r/pull/3",
		},
		{
			name:  "an issue link is not a pull request",
			text:  "See https://github.com/o/r/issues/5",
			isErr: true,
		},
		{
			name:  "no URL at all",
			text:  "I could not create the PR.",
			isErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, err := ParseURL(tt.text)
			if tt.isErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, url)
		})
	}
}

func TestNumber(t *testing.T) {
	assert.Equal(t, "42", Number("https://github.com/o/r/pull/42"))
	assert.Empty(t, Number(""))
}

// A template that leaves a `- [ ] TBC: … risk score` placeholder is asking for
// one; anything else is not.
func TestARiskScoreIsAskedForOnlyWhenTheTemplateWantsOne(t *testing.T) {
	withScore := "## Checklist\n- [ ] Technical docs updated\n\n### Review bot :robot:\n- [ ] TBC: `/review` risk score"
	assert.True(t, NeedsRiskScore(withScore))
	assert.False(t, NeedsRiskScore("## Checklist\n- [ ] Technical docs updated"))
	assert.False(t, NeedsRiskScore(""))

	// The guidance rides with the template, and only then.
	assert.Contains(t, Prompt(withScore), "risk score of N/10")
	assert.Contains(t, Prompt(withScore), "9 — schema migration")
	assert.NotContains(t, Prompt("## Checklist\n- [ ] Technical docs updated"), "risk score of N/10")
}

// The placeholder is meant to be written over. Counting it as dropped put a
// warning on every PR in a repository that asks for a score.
func TestWritingTheScoreIsNotTheTemplateBeingDropped(t *testing.T) {
	template := "## Checklist\n- [ ] Technical docs updated\n\n### Review bot :robot:\n- [ ] TBC: `/review` risk score"
	body := "Does the thing.\n\n## Checklist\n- [ ] Technical docs updated\n\n### Review bot :robot:\n" +
		"- [x] `/review` risk score of 4/10, estimated when this PR was opened. Adds a tested endpoint."
	assert.True(t, Kept(template, body), "the score line replaces the placeholder")

	// Everything else is still checked.
	assert.False(t, Kept(template, "Does the thing.\n\n- [x] `/review` risk score of 4/10, estimated when this PR was opened."),
		"the rest of the template really is missing")
}
