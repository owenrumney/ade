package dash

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
)

func TestAReviewRequestBecomesASessionInAWorktreeOfTheRepository(t *testing.T) {
	asked := ghpr.Request{Repo: "acme/other", Number: 9, URL: "https://github.com/acme/other/pull/9"}

	var checkedOutIn, openedDir, cloned, clonedTo string
	var cloneErr error
	b := New(Deps{
		CheckOut: func(_ context.Context, main string, number int) (string, error) {
			checkedOutIn = main
			return main + "-pr-" + strconv.Itoa(number), nil
		},
		Clone: func(_ context.Context, nameWithOwner, dest string) error {
			cloned, clonedTo = nameWithOwner, dest
			return cloneErr
		},
		Open: func(_ context.Context, dir, _ string) (string, error) {
			openedDir = dir
			return "http://127.0.0.1:1/?token=x#chat", nil
		},
	})

	repos := []sessions.Repo{{Path: "/srv/other", Name: "other", Owner: "acme"}}
	url, path, err := b.startReview(context.Background(), repos, asked)
	require.NoError(t, err)
	assert.Equal(t, "/srv/other", checkedOutIn)
	assert.Equal(t, "/srv/other-pr-9", path)
	assert.Equal(t, path, openedDir)
	assert.Equal(t, "http://127.0.0.1:1/?token=x#review", url)

	// Not on the machine: cloned into the first scan directory as
	// owner/name, then reviewed there like any other.
	home := t.TempDir()
	t.Setenv("HOME", home)
	_, path, err = b.startReview(context.Background(), nil, asked)
	require.NoError(t, err)
	assert.Equal(t, "acme/other", cloned)
	assert.Equal(t, filepath.Join(home, "code", "acme", "other"), clonedTo)
	assert.Equal(t, clonedTo, checkedOutIn)
	assert.Equal(t, clonedTo+"-pr-9", path)

	// A clone that fails says where it tried.
	cloneErr = errors.New("gh: not logged in")
	_, _, err = b.startReview(context.Background(), nil, asked)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "acme/other is not on this machine")
	assert.Contains(t, err.Error(), "gh: not logged in")
}

func TestCheckoutForMatchesNameThenOwner(t *testing.T) {
	repos := []sessions.Repo{
		{Path: "/a/fork/ade", Name: "ade", Owner: "fork"},
		{Path: "/a/owenrumney/ade", Name: "ade", Owner: "owenrumney"},
		{Path: "/a/x/other", Name: "other", Owner: "x"},
	}
	path, ok := checkoutFor(repos, "OwenRumney/ade")
	require.True(t, ok)
	assert.Equal(t, "/a/owenrumney/ade", path)

	path, ok = checkoutFor(repos, "someone/ade")
	require.True(t, ok)
	assert.Equal(t, "/a/fork/ade", path, "name alone is enough")

	_, ok = checkoutFor(repos, "nobody/missing")
	assert.False(t, ok)
	_, ok = checkoutFor(repos, "bare")
	assert.False(t, ok)
}

func TestTheIgnoreRulesSayHowManyTheyHid(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, settings.Save(settings.Settings{
		ScanDirs: []string{"/tmp/code"},
		Ignore:   settings.Ignore{Authors: []string{"dependabot"}, OlderThanDays: 90},
	}))

	now := time.Now().UTC().Format(time.RFC3339)
	old := time.Now().UTC().AddDate(0, 0, -400).Format(time.RFC3339)
	kept, hidden := wanted([]ghpr.Request{
		{Repo: "acme/app", Author: "dependabot", UpdatedAt: now},
		{Repo: "acme/app", Author: "sam", UpdatedAt: old},
		{Repo: "acme/app", Author: "sam", UpdatedAt: now},
	})

	require.Len(t, kept, 1)
	assert.Equal(t, "sam", kept[0].Author)
	assert.Equal(t, 2, hidden, "the picker has to be able to say why the list is empty")
}

func TestAFailedSearchIsNotAnEmptyList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := New(Deps{})
	b.SetReviews([]ghpr.Request{{Repo: "acme/app", Number: 1}})
	b.setReviewsTrouble("gh: not logged in", 0)

	// The list is left as it was. Clearing it would report an unreachable
	// GitHub as nothing waiting on you.
	assert.Len(t, b.Reviews(), 1)
	assert.Equal(t, "gh: not logged in", b.ReviewTroubleNow().Error)

	b.setReviewsTrouble("", 3)
	assert.Empty(t, b.ReviewTroubleNow().Error)
	assert.Equal(t, 3, b.ReviewTroubleNow().Hidden)
}

// The branch of a merged pull request is deleted, so the checkout fails with
// git's words. What the person needs to hear is that it has gone.
func TestAReviewOfAMergedPullRequestSaysSoRatherThanRepeatingGit(t *testing.T) {
	gitSaid := errors.New("gh pr checkout 78: fatal: couldn't find remote ref refs/heads/fix/x")
	state := "MERGED"
	var askedFor []ghpr.PRRef
	b := New(Deps{
		Repos: func() ([]sessions.Repo, error) {
			return []sessions.Repo{{Path: "/srv/lsp", Name: "lsp", Owner: "infracost"}}, nil
		},
		CheckOut: func(context.Context, string, int) (string, error) { return "", gitSaid },
		States: func(_ context.Context, _ string, refs []ghpr.PRRef) (map[string]string, error) {
			askedFor = refs
			return map[string]string{"infracost/lsp#78": state}, nil
		},
	})

	_, _, err := b.OpenPullRequest(context.Background(), "infracost/lsp", 78)
	var gone *GoneError
	require.ErrorAs(t, err, &gone)
	assert.Equal(t, "infracost/lsp#78 is already merged", gone.Error())
	assert.Equal(t, []ghpr.PRRef{{Repo: "infracost/lsp", Number: 78}}, askedFor)

	// Still open: whatever went wrong was something else, and git's words are
	// the only account of it there is.
	state = "OPEN"
	_, _, err = b.OpenPullRequest(context.Background(), "infracost/lsp", 78)
	require.ErrorIs(t, err, gitSaid)
}

// Git refuses a second checkout of a branch, so a pull request whose head is
// already out — your own branch, most often — opens where it is rather than
// failing in a worktree gh cannot check it out into.
func TestAHeadAlreadyCheckedOutIsReviewedWhereItIs(t *testing.T) {
	main := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = main
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	run("init", "-q", ".")
	run("commit", "-qm", "init", "--allow-empty")
	run("checkout", "-qb", "fix/x")

	was := headBranch
	t.Cleanup(func() { headBranch = was })
	headBranch = func(context.Context, string, int) string { return "fix/x" }

	path, err := ReviewWorktree(context.Background(), main, 21)
	require.NoError(t, err)
	assert.Equal(t, main, path)
	assert.NoDirExists(t, gitdiff.WorktreePath(main, "pr-21"))
}
