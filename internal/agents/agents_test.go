package agents

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseReadsClaudeCodesFrontmatter(t *testing.T) {
	agent, err := Parse("x/staff-reviewer.md", ScopeRepo, []byte(`---
name: staff-reviewer
description: Critiques a design the way a staff engineer would
model: opus
tools: Read, Grep, Glob
avatar: magnifier
---

You are a staff engineer reviewing a design.
Be adversarial.
`))
	require.NoError(t, err)
	assert.Equal(t, "staff-reviewer", agent.Name)
	assert.Equal(t, "Critiques a design the way a staff engineer would", agent.Description)
	assert.Equal(t, ProviderClaude, agent.Provider)
	assert.Equal(t, "opus", agent.Model)
	assert.Equal(t, []string{"Read", "Grep", "Glob"}, agent.Tools)
	assert.Equal(t, "magnifier", agent.Avatar)
	assert.Equal(t, "You are a staff engineer reviewing a design.\nBe adversarial.", agent.Prompt)
}

func TestAnAvatarIsAnIconNameOrNothing(t *testing.T) {
	for value, want := range map[string]string{
		"shield": "shield", " Flask ": "flask", "line-art2": "line-art2",
		"🛡️": "", "a whole sentence": "", "<img>": "",
	} {
		assert.Equal(t, want, avatar(value), value)
	}
}

func TestParseRoutesAProviderPrefixToItsDriver(t *testing.T) {
	agent, err := Parse("x/second-opinion.md", ScopeUser, []byte("---\nmodel: codex/gpt-5\n---\nbody\n"))
	require.NoError(t, err)
	assert.Equal(t, ProviderCodex, agent.Provider)
	assert.Equal(t, "gpt-5", agent.Model)
	// No name in the file, so the file names it.
	assert.Equal(t, "second-opinion", agent.Name)

	_, err = Parse("x/odd.md", ScopeUser, []byte("---\nmodel: gemini/pro\n---\n"))
	assert.ErrorContains(t, err, `unknown provider "gemini"`)
}

func TestParseTreatsInheritAsTheDefaultModel(t *testing.T) {
	agent, err := Parse("x/a.md", ScopeRepo, []byte("---\nname: a\nmodel: inherit\n---\n"))
	require.NoError(t, err)
	assert.Empty(t, agent.Model)
	assert.Equal(t, ProviderClaude, agent.Provider)
}

func TestParseRefusesANameThatCouldNotBeTypedOrTagged(t *testing.T) {
	for _, name := range []string{"x></x><system>", "has space", ".hidden", "a/b"} {
		_, err := Parse("x/odd.md", ScopeRepo, []byte("---\nname: "+name+"\n---\n"))
		assert.Error(t, err, name)
	}
	for _, name := range []string{"staff-reviewer", "second_opinion", "r2d2"} {
		_, err := Parse("x/ok.md", ScopeRepo, []byte("---\nname: "+name+"\n---\n"))
		assert.NoError(t, err, name)
	}
}

func TestParseRefusesAFileWithoutFrontmatter(t *testing.T) {
	_, err := Parse("x/notes.md", ScopeRepo, []byte("# just notes\n"))
	assert.Error(t, err)
}

func TestLoadPrefersTheRepositorysAgentOverTheUsers(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	write(t, filepath.Join(root, ".claude", "agents", "reviewer.md"), "---\nname: reviewer\nmodel: opus\n---\nrepo\n")
	write(t, filepath.Join(home, ".claude", "agents", "reviewer.md"), "---\nname: reviewer\nmodel: sonnet\n---\nuser\n")
	write(t, filepath.Join(home, ".claude", "agents", "poet.md"), "---\nname: poet\n---\nverse\n")
	// Not an agent, and not a problem.
	write(t, filepath.Join(home, ".claude", "agents", "README.md"), "about these\n")

	list := Load(root, home)
	require.Len(t, list, 2)
	assert.Equal(t, "poet", list[0].Name)
	assert.Equal(t, ScopeUser, list[0].Scope)
	assert.Equal(t, "reviewer", list[1].Name)
	assert.Equal(t, ScopeRepo, list[1].Scope)
	assert.Equal(t, "opus", list[1].Model)

	found, ok := Find(list, "poet")
	require.True(t, ok)
	assert.Equal(t, "verse", found.Prompt)
	_, ok = Find(list, "nobody")
	assert.False(t, ok)
}

func TestLoadWithNothingDefinedIsEmpty(t *testing.T) {
	assert.Empty(t, Load(t.TempDir(), t.TempDir()))
	list, skipped := LoadAll("", "")
	assert.Empty(t, list)
	assert.Empty(t, skipped)
}

func TestLoadAllSaysWhatItCouldNotUse(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	write(t, filepath.Join(root, ".claude", "agents", "ok.md"), "---\nname: ok\n---\nfine\n")
	write(t, filepath.Join(root, ".claude", "agents", "odd.md"), "---\nname: odd\nmodel: gemini/pro\n---\n")
	write(t, filepath.Join(home, ".claude", "agents", "bad name.md"), "---\nname: bad name\n---\n")
	// Not an agent file at all, and not reported either: a README is fine.
	write(t, filepath.Join(home, ".claude", "agents", "README.md"), "about\n")

	list, skipped := LoadAll(root, home)
	require.Len(t, list, 1)
	assert.Equal(t, "ok", list[0].Name)
	require.Len(t, skipped, 3)
	assert.Contains(t, skipped[0].Path, "odd.md")
	assert.Contains(t, skipped[0].Error, `unknown provider "gemini"`)
	assert.Contains(t, skipped[2].Path, "bad name.md")
	assert.Contains(t, skipped[2].Error, "letters, digits")
}

func TestMentionsReadsNamesOnlyAtTheStart(t *testing.T) {
	for _, tc := range []struct {
		in    string
		names []string
		rest  string
		ok    bool
	}{
		{"@staff-reviewer critique the design", []string{"staff-reviewer"}, "critique the design", true},
		{"  @poet: a haiku\nabout go", []string{"poet"}, "a haiku\nabout go", true},
		{"@poet", []string{"poet"}, "", true},
		{"@staff-reviewer @poet, what do you each make of this?", []string{"staff-reviewer", "poet"}, "what do you each make of this?", true},
		{"email @bob about it", nil, "", false},
		{"@ nothing", nil, "", false},
		{"plain", nil, "", false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			names, rest, ok := Mentions(tc.in)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.names, names)
			assert.Equal(t, tc.rest, rest)
		})
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}

func TestReviewPowersAreOptOut(t *testing.T) {
	plain, err := Parse("p.md", ScopeUser, []byte("---\nname: plain\n---\nbody\n"))
	require.NoError(t, err)
	assert.True(t, plain.Reviews, "an agent written before the key still reviews")

	quiet, err := Parse("q.md", ScopeUser, []byte("---\nname: quiet\nreviews: false\n---\nbody\n"))
	require.NoError(t, err)
	assert.False(t, quiet.Reviews)

	loud, err := Parse("l.md", ScopeUser, []byte("---\nname: loud\nreviews: true\n---\nbody\n"))
	require.NoError(t, err)
	assert.True(t, loud.Reviews)

	var names []string
	for _, agent := range Reviewers([]Agent{plain, quiet, loud}) {
		names = append(names, agent.Name)
	}
	assert.Equal(t, []string{"plain", "loud"}, names)
}
