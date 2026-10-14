package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/lineage"
)

func whyRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	write := func(body string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte(body), 0o600))
	}

	git("init", "-b", "main", "-q")
	write("package main\n")
	git("add", ".")
	git("commit", "-qm", "base")
	git("checkout", "-qb", "feature")
	write("package main\n\nvar y = 1\n")
	git("add", ".")
	git("commit", "-qm", "add y")
	return dir
}

// stampedStore records a turn over line 3 of a.go and stamps it with HEAD.
func stampedStore(t *testing.T, dir string) {
	t.Helper()
	ctx := context.Background()
	diff, err := gitdiff.Load(ctx, gitdiff.Options{Repo: dir, Scope: gitdiff.ScopeBranch})
	require.NoError(t, err)
	anchor, ok := comments.Locate(diff, "a.go", comments.SideRight, 3)
	require.True(t, ok)

	gitDir, err := gitdiff.GitDir(ctx, dir)
	require.NoError(t, err)
	store, err := lineage.Open(gitDir, "feature")
	require.NoError(t, err)
	require.NoError(t, store.Add(lineage.Entry{Session: "s1", Summary: "give it a y",
		Ranges: []lineage.Range{{File: "a.go", Start: 3, Lines: 1, Anchor: anchor}}}))

	head, err := gitdiff.HeadSHA(ctx, dir)
	require.NoError(t, err)
	added, err := gitdiff.AddedLines(ctx, dir, head)
	require.NoError(t, err)
	n, err := store.Stamp(diff, head, added)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func TestWhyPrintsTheCommitAndTheAskBehindALine(t *testing.T) {
	dir := whyRepo(t)
	stampedStore(t, dir)
	t.Chdir(dir)

	var out bytes.Buffer
	require.NoError(t, why(context.Background(), &out, "a.go", 3))

	assert.Contains(t, out.String(), "a.go:3")
	assert.Contains(t, out.String(), "add y")
	assert.Contains(t, out.String(), "give it a y")
}

func TestWhySaysWhenALineWasWrittenByHand(t *testing.T) {
	dir := whyRepo(t)
	t.Chdir(dir)

	var out bytes.Buffer
	require.NoError(t, why(context.Background(), &out, "a.go", 1))

	assert.Contains(t, out.String(), "written by hand")
}

func TestWhyRejectsWhatIsNotAFileAndLine(t *testing.T) {
	for _, arg := range []string{"a.go", "a.go:", "a.go:nope", "a.go:0"} {
		_, _, err := splitTarget(arg)
		assert.Errorf(t, err, "%q should not parse", arg)
	}
	file, line, err := splitTarget("internal/web/web.go:42")
	require.NoError(t, err)
	assert.Equal(t, "internal/web/web.go", file)
	assert.Equal(t, 42, line)
}

func TestWhyRefusesAPathOutsideTheRepository(t *testing.T) {
	_, err := repoRel("/repo", "/repo", "/elsewhere/a.go")
	assert.Error(t, err)

	rel, err := repoRel("/repo", "/repo/internal", "web.go")
	require.NoError(t, err)
	assert.Equal(t, "internal/web.go", rel)
}
