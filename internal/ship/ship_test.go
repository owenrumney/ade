package ship

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/ghpr"
)

// repo is a checkout with a base branch and one commit on it.
func repo(t *testing.T) (string, func(args ...string)) {
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
	git("init", "-b", "main", "-q")
	write(t, dir, "one.txt", "one\n")
	git("add", ".")
	git("commit", "-qm", "base")
	return dir, git
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
}

func TestABranchOffTheBaseSaysWhatItIsAheadBy(t *testing.T) {
	dir, git := repo(t)
	git("checkout", "-qb", "work")
	write(t, dir, "two.txt", "two\n")
	git("add", ".")
	git("commit", "-qm", "the one commit")

	got := Read(context.Background(), Repo{Root: dir, Home: true}, nil)
	assert.Equal(t, "work", got.Branch)
	assert.Equal(t, "main", got.Base)
	assert.Equal(t, 0, got.Behind)
	require.Len(t, got.Unpushed, 1)
	assert.Equal(t, "the one commit", got.Unpushed[0].Subject)
	assert.Empty(t, got.Note)
}

func TestAPathBothStagedAndEditedSinceCountsOnce(t *testing.T) {
	dir, git := repo(t)
	write(t, dir, "one.txt", "staged\n")
	git("add", "one.txt")
	write(t, dir, "one.txt", "and edited again\n")

	got := Read(context.Background(), Repo{Root: dir}, nil)
	assert.Equal(t, 1, got.Files)
	assert.Equal(t, 1, got.Staged)
}

func TestABranchBehindTheBaseCountsWhatItIsMissing(t *testing.T) {
	dir, git := repo(t)
	git("checkout", "-qb", "work")
	git("checkout", "-q", "main")
	write(t, dir, "three.txt", "three\n")
	git("add", ".")
	git("commit", "-qm", "moved on")
	git("checkout", "-q", "work")

	got := Read(context.Background(), Repo{Root: dir}, nil)
	assert.Equal(t, 1, got.Behind)
	assert.Empty(t, got.Unpushed)
}

func TestARepositoryGitWillNotAnswerForComesBackNamed(t *testing.T) {
	dir := t.TempDir()
	got := Read(context.Background(), Repo{Root: dir, Name: "nowhere"}, nil)
	assert.Equal(t, "nowhere", got.Name)
	assert.NotEmpty(t, got.Note)
}

func TestThePullRequestComesFromTheCallersCacheAndNowhereElse(t *testing.T) {
	dir, _ := repo(t)
	asked := 0
	look := func(_ context.Context, repo Repo) *PR {
		asked++
		assert.Equal(t, "main", repo.Branch, "the branch is resolved before the cache is asked")
		return &PR{Number: 7}
	}

	got := Read(context.Background(), Repo{Root: dir}, look)
	assert.Equal(t, 1, asked)
	require.NotNil(t, got.PR)
	assert.Equal(t, 7, got.PR.Number)
}

func TestTheFirstFailingCheckIsWhatTheCardSays(t *testing.T) {
	checks := []ghpr.Check{
		{Name: "lint", Bucket: "pass"},
		{Name: "test", Workflow: "ci", Bucket: "fail", Description: "2 assertions failed"},
		{Name: "build", Bucket: "fail"},
	}
	count, line := Failing(checks)
	assert.Equal(t, 2, count)
	assert.Equal(t, "test (ci): 2 assertions failed", line)
}

func TestAPullRequestKeepsGitHubsOwnMergeVerdict(t *testing.T) {
	pr := &ghpr.PR{Number: 3, State: "OPEN", BaseRef: "main", MergeState: "DIRTY"}
	got := FromPR(pr, nil)
	require.NotNil(t, got)
	assert.Equal(t, "it conflicts with main", got.MergeBlocked)
	assert.Nil(t, FromPR(nil, nil))
}
