package fixtures

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/rewind"
	"github.com/owenrumney/ade/internal/verify"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), identity...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, root, path, body string) {
	t.Helper()
	full := filepath.Join(root, path)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o600))
}

// repo is a checkout with one commit, an uncommitted edit on top, and a
// snapshot of that tree as a turn would take it.
func repo(t *testing.T) (root, dir, snapshot string) {
	t.Helper()
	root = t.TempDir()
	gitIn(t, root, "init", "-q", "-b", "main")
	write(t, root, "a.go", "package main\n")
	write(t, root, "b.go", "package main\n")
	write(t, root, "CLAUDE.md", "old rules\n")
	gitIn(t, root, "add", ".")
	gitIn(t, root, "commit", "-qm", "base")
	write(t, root, "a.go", "package main\n\nvar x = 1\n")
	write(t, root, "new.txt", "untracked\n")
	snapshot, err := rewind.Save(context.Background(), root)
	require.NoError(t, err)
	return root, Dir(filepath.Join(root, ".git")), snapshot
}

func TestCaptureRecordsThePromptTheBaseAndTheUncommittedTree(t *testing.T) {
	root, dir, snapshot := repo(t)
	require.NoError(t, Capture(context.Background(), root, dir, "narrow-comment", "Narrow the comment\nplease", snapshot))

	f, err := Load(dir, "narrow-comment")
	require.NoError(t, err)
	assert.Equal(t, "Narrow the comment\nplease", f.Prompt)
	assert.Equal(t, gitIn(t, root, "rev-parse", "HEAD"), f.Base)
	assert.Contains(t, string(f.Patch), "+var x = 1")
	assert.Contains(t, string(f.Patch), "+untracked")
	assert.WithinDuration(t, time.Now(), f.Captured, time.Minute)
	assert.Equal(t, "pass", f.Expect.Verify, "the template asserts the checks, so a fresh fixture is not green by accident")

	assert.ErrorContains(t, Capture(context.Background(), root, dir, "narrow-comment", "x", snapshot), "already exists")
	assert.ErrorIs(t, Capture(context.Background(), root, dir, "other", "x", "deadbeef"), ErrNoSnapshot)
	assert.ErrorContains(t, Capture(context.Background(), root, dir, "Bad Name", "x", snapshot), "not a fixture name")
}

func TestNameComesFromThePromptAndDoesNotCollide(t *testing.T) {
	dir := t.TempDir()
	assert.Equal(t, "narrow-the-comment-in", Name(dir, "Narrow the comment in verify.go\nmore"))
	assert.Equal(t, "turn", Name(dir, "???"))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "fix-it"), 0o750))
	assert.Equal(t, "fix-it-2", Name(dir, "Fix it"))
}

func TestParseExpectRejectsAMisspeltKeyAndABadVerify(t *testing.T) {
	_, err := ParseExpect([]byte("touch: [a.go]\n"))
	assert.ErrorContains(t, err, "touch")
	_, err = ParseExpect([]byte("verify: maybe\n"))
	assert.ErrorContains(t, err, `verify must be "pass"`)
	e, err := ParseExpect([]byte("# nothing\n"))
	require.NoError(t, err)
	assert.True(t, e.Empty())
	e, err = ParseExpect([]byte("asks: false\ncomments: { maxLines: 2 }\n"))
	require.NoError(t, err)
	assert.Equal(t, []string{"comments", "asks"}, e.checked())
}

func TestRunReplaysTheTreeAndAssertsOnWhatTheRunChanged(t *testing.T) {
	root, dir, snapshot := repo(t)
	require.NoError(t, Capture(context.Background(), root, dir, "fix", "Fix a.go", snapshot))
	write(t, dir, "fix/expect.yaml", "touches: [a.go]\nuntouched: [b.go]\nverify: pass\ncomments: { maxLines: 2 }\nasks: false\n")
	write(t, root, "CLAUDE.md", "new rules\n")
	require.NoError(t, verify.Save(verify.Path(filepath.Join(root, ".git")), verify.Config{Checks: []verify.Check{{Name: "ok", Run: "test -f ok.txt"}}}))

	var seen Request
	runner := func(_ context.Context, req Request) (Response, error) {
		seen = req
		rules, err := os.ReadFile(filepath.Join(req.Dir, "CLAUDE.md"))
		require.NoError(t, err)
		assert.Equal(t, "new rules\n", string(rules), "the rules under test are the checkout's, not the base commit's")
		body, err := os.ReadFile(filepath.Join(req.Dir, "a.go"))
		require.NoError(t, err)
		assert.Equal(t, "package main\n\nvar x = 1\n", string(body), "the uncommitted edit is laid over the base")
		assert.FileExists(t, filepath.Join(req.Dir, "new.txt"))
		write(t, req.Dir, "a.go", "package main\n\n// one\n// two\n// three\nvar x = 2\n")
		write(t, req.Dir, "ok.txt", "")
		return Response{Text: "Done.", CostUSD: 0.01}, nil
	}
	results, err := Run(context.Background(), root, dir, runner, nil, Options{Runs: 2})
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "haiku", seen.Model)
	assert.NoDirExists(t, seen.Dir, "the worktree is gone once the run is measured")
	assert.Equal(t, []string{"comments: a.go:3 — 3 lines"}, results[0].Failed)
	assert.Equal(t, 2, results[1].Run)
	assert.Equal(t, "untracked\n", read(t, root, "new.txt"), "the checkout itself is never touched")
	assert.Equal(t, "new rules\n", read(t, root, "CLAUDE.md"))

	t.Run("a question, a missed file and a failed check each fail by name", func(t *testing.T) {
		runner := func(_ context.Context, req Request) (Response, error) {
			write(t, req.Dir, "b.go", "package main\n\nvar y = 1\n")
			return Response{Text: "Should I also change b.go?"}, nil
		}
		results, err := Run(context.Background(), root, dir, runner, []string{"fix"}, Options{Model: "opus"})
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, []string{
			"touches: a.go was not changed",
			"untouched: b.go was changed",
			"verify: ok failed",
			"asks: the turn ended in a question",
		}, results[0].Failed)
		assert.Equal(t, "opus", results[0].Model)
	})

	t.Run("a run that errors counts against the fixture", func(t *testing.T) {
		runner := func(context.Context, Request) (Response, error) {
			return Response{}, errors.New("claude failed: exit 1")
		}
		results, err := Run(context.Background(), root, dir, runner, nil, Options{})
		require.NoError(t, err)
		assert.False(t, results[0].Passed)
		assert.Equal(t, "claude failed: exit 1", results[0].Error)
	})

	t.Run("a fixture that asserts nothing fails rather than passing quietly", func(t *testing.T) {
		write(t, dir, "fix/expect.yaml", "# nothing\n")
		results, err := Run(context.Background(), root, dir, func(context.Context, Request) (Response, error) {
			t.Fatal("nothing to measure, so nothing should run")
			return Response{}, nil
		}, nil, Options{})
		require.NoError(t, err)
		assert.Equal(t, []string{"expect.yaml asserts nothing"}, results[0].Failed)
	})

	_, err = Run(context.Background(), root, dir, nil, []string{"nope"}, Options{})
	assert.ErrorContains(t, err, `no fixture called "nope"`)
}

func TestListIsEmptyUntilACaptureAndReportsStaleOnes(t *testing.T) {
	root, dir, snapshot := repo(t)
	_, err := List(dir)
	assert.ErrorIs(t, err, ErrNoFixtures)
	require.NoError(t, Capture(context.Background(), root, dir, "old", "x", snapshot))
	write(t, dir, "old/captured", time.Now().Add(-100*24*time.Hour).Format(time.RFC3339)+"\n")
	all, err := List(dir)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.True(t, all[0].Stale(time.Now()))
}

func TestCompareCarriesTheDenominatorAndTheDelta(t *testing.T) {
	before := []Result{
		{Name: "a", Run: 1, Model: "haiku", Checked: []string{"touches", "untouched"}, Failed: []string{"untouched: x was changed"}},
		{Name: "a", Run: 2, Model: "haiku", Checked: []string{"touches", "untouched"}, Passed: true, CostUSD: 0.5},
		{Name: "b", Run: 1, Model: "haiku", Checked: []string{"verify"}, Error: "boom", Stale: true},
	}
	after := []Result{
		{Name: "a", Run: 1, Model: "haiku", Checked: []string{"touches", "untouched"}, Passed: true},
		{Name: "a", Run: 2, Model: "haiku", Checked: []string{"touches", "untouched"}, Passed: true},
		{Name: "b", Run: 1, Model: "haiku", Checked: []string{"verify"}, Passed: true},
	}
	report := Compare(before, after)
	text := report.String()
	assert.Contains(t, text, "touches          2/2      2/2\n")
	assert.Contains(t, text, "untouched        1/2      2/2  +1\n")
	assert.Contains(t, text, "verify           0/1      1/1  +1\n")
	assert.Contains(t, text, "model haiku")
	assert.Contains(t, text, "cost $0.50")
	assert.Contains(t, text, "stale, captured over 90 days ago: b")
	assert.Contains(t, text, "error: b run 1: boom")

	one := Compare(nil, after).String()
	assert.NotContains(t, one, "before")
	assert.Contains(t, one, "touches          2/2\n")
}

func read(t *testing.T, root, path string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, path))
	require.NoError(t, err)
	return string(body)
}
