package agents

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheShippedAgentsParseAndOnlyTheWritersWrite(t *testing.T) {
	list, err := Defaults()
	require.NoError(t, err)
	if len(list) == 0 {
		t.Skip("the prompts live in ade-private: run make private")
	}
	var names []string
	for _, agent := range list {
		names = append(names, agent.Name)
		assert.NotEmpty(t, agent.Description, agent.Name)
		assert.NotEmpty(t, agent.Prompt, agent.Name)
		assert.NotEmpty(t, agent.Avatar, agent.Name)
		tools := []string{"Read", "Grep", "Glob"}
		if agent.Provider == ProviderClaude {
			// Bash is how a reviewer reads the diff it was called about.
			// Codex's bridge has no shell tool, so its agents get none.
			tools = append(tools, "Bash")
		}
		// The two that change files: the fixer applies findings, the
		// condenser rewrites the design doc it consolidated.
		if agent.Name == "fixer" || agent.Name == "condenser" {
			tools = append(tools, "Write", "Edit")
		}
		if agent.Name == "fixer" {
			assert.Equal(t, ProviderCodex, agent.Provider)
		}
		assert.ElementsMatch(t, tools, agent.Tools, agent.Name)
	}
	assert.ElementsMatch(t, []string{
		"architect", "staff-reviewer", "second-opinion", "security", "simplifier", "tester", "fixer", "commenter", "condenser", "eli5",
	}, names)
	second, ok := Find(list, "second-opinion")
	require.True(t, ok)
	assert.Equal(t, ProviderCodex, second.Provider)

	// Who the review picker offers. The rest do something else with a diff:
	// design it, file it, fix it, condense a document, read it out.
	var reviewers []string
	for _, agent := range Reviewers(list) {
		reviewers = append(reviewers, agent.Name)
	}
	assert.ElementsMatch(t, []string{
		"staff-reviewer", "second-opinion", "security", "simplifier", "tester",
	}, reviewers)
}

func TestInstallKeepsWhatYouEditedUnlessForced(t *testing.T) {
	stands(t, shippedTester())
	home := t.TempDir()
	shipped, err := Defaults()
	require.NoError(t, err)

	done, err := Install(home, false)
	require.NoError(t, err)
	// Counted from the shipped set rather than written down, so adding an agent
	// is a file and not also a number in here.
	require.Len(t, done, len(shipped))
	for _, d := range done {
		assert.Equal(t, "written", d.State, d.Name)
		assert.FileExists(t, d.Path)
	}
	// They load the way any user agent does.
	assert.Len(t, Load(t.TempDir(), home), len(shipped))

	// An edit is yours to keep.
	tester := filepath.Join(home, ".claude", "agents", "tester.md")
	require.NoError(t, os.WriteFile(tester, []byte("---\nname: tester\nmodel: opus\n---\nmine\n"), 0o600))
	done, err = Install(home, false)
	require.NoError(t, err)
	states := map[string]string{}
	for _, d := range done {
		states[d.Name] = d.State
	}
	assert.Equal(t, "kept", states["tester"])
	assert.Equal(t, "kept", states["security"])
	data, _ := os.ReadFile(tester)
	assert.Contains(t, string(data), "mine")

	// Unless you say so.
	done, err = Install(home, true)
	require.NoError(t, err)
	for _, d := range done {
		assert.Equal(t, "replaced", d.State, d.Name)
	}
	data, _ = os.ReadFile(tester)
	assert.NotContains(t, string(data), "mine")
}
