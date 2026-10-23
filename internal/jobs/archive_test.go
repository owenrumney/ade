package jobs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
)

func TestTheDesignArchiveIsScheduledByDefault(t *testing.T) {
	job, found := Find(WithBuiltins(nil, settings.Settings{}), KindDesignArchive)
	require.True(t, found, "the backlog is swept whether or not a wiki is configured")
	assert.Equal(t, Reads, job.Allow, "archiving touches untracked state and a remote, never a branch")
	assert.Equal(t, ScopeBuiltin, job.Scope)
	assert.False(t, job.Every.Manual())
}

func TestADesignArchiveWrittenByHandWins(t *testing.T) {
	mine := Job{Name: KindDesignArchive, Kind: KindDesignArchive, Scope: ScopeUser, Schedule: "0 8 * * 1"}
	job, found := Find(WithBuiltins([]Job{mine}, settings.Settings{}), KindDesignArchive)
	require.True(t, found)
	assert.Equal(t, mine, job)
}

func TestARepositoryThatIsGoneFromGitHubIsReportedAndNotFailed(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "--initial-branch", "main")
	require.NoError(t, os.WriteFile(filepath.Join(root, "a"), []byte("a"), 0o600))
	git(t, root, "add", ".")
	git(t, root, "commit", "-m", "a")
	git(t, root, "remote", "add", "origin", "https://github.com/o/gone.git")

	bin := t.TempDir()
	script := "#!/bin/sh\necho \"GraphQL: Could not resolve to a Repository with the name 'o/gone'. (repository)\" >&2\nexit 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o700))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	target := ArchiveTarget{Repo: "o/gone", Root: root, CommonGitDir: filepath.Join(root, ".git")}
	out := FireArchive(context.Background(), nil, []ArchiveTarget{target}, nil)

	assert.Empty(t, out.Error, "a remote with nothing behind it fails every fire; it must not fail the job")
	assert.Contains(t, out.Report, "o/gone: skipped")
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	body, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, body)
}
