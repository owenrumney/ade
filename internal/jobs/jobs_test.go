package jobs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const morning = `---
name: morning
description: what happened overnight
schedule: "0 8 * * 1-5"
project: git@github.com:owenrumney/ade.git
continuity: summary
allow: reads
---

/catchup
`

func TestADefinitionIsItsFrontmatterAndItsBody(t *testing.T) {
	job, err := Parse("/tmp/morning.md", ScopeUser, []byte(morning))
	require.NoError(t, err)

	assert.Equal(t, "morning", job.Name)
	assert.Equal(t, "what happened overnight", job.Description)
	assert.Equal(t, "0 8 * * 1-5", job.Schedule)
	assert.Equal(t, "github.com/owenrumney/ade", job.Project, "the remote is normalised, so any form git writes it in resolves")
	assert.Equal(t, Summary, job.Continuity)
	assert.Equal(t, Reads, job.Allow)
	assert.Equal(t, "/catchup", job.Prompt)
}

func TestADefinitionDefaultsToFreshAndReadOnly(t *testing.T) {
	job, err := Parse("/tmp/drift.md", ScopeRepo, []byte("---\nschedule: \"@every 4h\"\n---\n\nlook for drift\n"))
	require.NoError(t, err)

	assert.Equal(t, "drift", job.Name, "the filename names a job that does not name itself")
	assert.Equal(t, Fresh, job.Continuity)
	assert.Equal(t, Reads, job.Allow)
}

func TestWhatIsNotBuiltIsRefusedRatherThanIgnored(t *testing.T) {
	// A definition asking for a mode this build does not have must not run as
	// though it had been given a different one.
	cases := []struct{ name, body, want string }{
		{"history", "---\nschedule: \"@every 1h\"\ncontinuity: history\n---\n\nx\n", "not built yet"},
		{"nonsense", "---\nschedule: \"@every 1h\"\ncontinuity: telepathy\n---\n\nx\n", "unknown continuity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse("/tmp/x.md", ScopeUser, []byte(tc.body))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestADefinitionWithNoPromptIsNotAJob(t *testing.T) {
	_, err := Parse("/tmp/x.md", ScopeUser, []byte("---\nschedule: \"@every 1h\"\n---\n"))
	assert.ErrorContains(t, err, "no prompt")
}

func TestABadNameIsRefused(t *testing.T) {
	_, err := Parse("/tmp/x.md", ScopeUser, []byte("---\nname: ../../escape\nschedule: \"@every 1h\"\n---\n\nx\n"))
	assert.ErrorContains(t, err, "job name")
}

func TestNoFrontmatterIsNotAJob(t *testing.T) {
	_, err := Parse("/tmp/x.md", ScopeUser, []byte("/catchup\n"))
	assert.ErrorContains(t, err, "frontmatter")
}

func TestTheRepositoryShadowsTheUser(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	write(t, filepath.Join(root, ".ade", "jobs", "morning.md"), "---\nschedule: \"@every 1h\"\n---\n\nrepo one\n")
	write(t, filepath.Join(home, ".ade", "jobs", "morning.md"), "---\nschedule: \"@every 2h\"\n---\n\nuser one\n")
	write(t, filepath.Join(home, ".ade", "jobs", "nightly.md"), "---\nschedule: \"0 2 * * *\"\n---\n\nuser two\n")

	list, skipped := LoadAll(root, home)
	require.Empty(t, skipped)
	require.Len(t, list, 2)
	assert.Equal(t, "morning", list[0].Name)
	assert.Equal(t, "repo one", list[0].Prompt)
	assert.Equal(t, ScopeRepo, list[0].Scope)
	assert.Equal(t, "nightly", list[1].Name)
	assert.Equal(t, ScopeUser, list[1].Scope)
}

func TestAFileThatIsNotAJobIsNamedRatherThanDropped(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".ade", "jobs", "broken.md"), "---\nschedule: \"0 99 * * *\"\n---\n\nx\n")
	write(t, filepath.Join(home, ".ade", "jobs", "good.md"), "---\nschedule: \"0 9 * * *\"\n---\n\nx\n")

	list, skipped := LoadAll("", home)
	assert.Len(t, list, 1)
	require.Len(t, skipped, 1)
	assert.Contains(t, skipped[0].Path, "broken.md")
	assert.Contains(t, skipped[0].Error, "outside 0-23")
}

func TestNoJobsDirectoryIsNoJobs(t *testing.T) {
	list, skipped := LoadAll(t.TempDir(), t.TempDir())
	assert.Empty(t, list)
	assert.Empty(t, skipped)
}

func write(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}
