package wiki

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitAndSync(t *testing.T) {
	remote := newRemote(t)
	state := t.TempDir()
	store := New(state, remote)
	require.NoError(t, store.Init(context.Background(), remote))

	st := store.Status(context.Background())
	assert.Equal(t, remote, st.Remote)
	assert.NotEmpty(t, st.Revision)
	assert.Empty(t, st.Repair)

	pushPage(t, remote, "general/second.md", "second")
	require.NoError(t, store.Sync(context.Background()))
	assert.FileExists(t, filepath.Join(store.Root(), "general", "second.md"))
	assert.NotZero(t, store.Status(context.Background()).LastFetch)
}

func TestSyncClonesConfiguredMissingRoot(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	require.NoError(t, store.Sync(context.Background()))
	assert.FileExists(t, filepath.Join(store.Root(), "general", "first.md"))
}

func TestStatusRefusesDirtyClone(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	require.NoError(t, store.Init(context.Background(), remote))
	require.NoError(t, os.WriteFile(filepath.Join(store.Root(), "general", "first.md"), []byte("changed"), 0o600))

	st := store.Status(context.Background())
	assert.True(t, st.Dirty)
	assert.Equal(t, "canonical clone is dirty", st.Repair)
	err := store.Sync(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs repair")
}

func TestInitIsTheOnlyOriginChange(t *testing.T) {
	first, second := newRemote(t), newRemote(t)
	store := New(t.TempDir(), first)
	require.NoError(t, store.Init(context.Background(), first))
	require.NoError(t, store.Init(context.Background(), second))
	got, err := store.git(context.Background(), "remote", "get-url", "origin")
	require.NoError(t, err)
	assert.Equal(t, second, stringTrim(got))
}

func TestSearchAndPageStayWithinCanonicalRoot(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	require.NoError(t, store.Init(context.Background(), remote))
	require.NoError(t, os.WriteFile(filepath.Join(store.Root(), "general", "notes.md"), []byte("Find Me\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(store.Root(), ".hidden.md"), []byte("Find Me\n"), 0o600))
	require.NoError(t, os.Symlink("/etc/passwd", filepath.Join(store.Root(), "general", "escape.md")))

	hits := store.Search("find me")
	require.Len(t, hits, 1)
	assert.Equal(t, "general/notes.md", hits[0].Path)
	assert.Equal(t, 1, hits[0].Line)
	page, err := store.Page("general/notes.md")
	require.NoError(t, err)
	assert.Equal(t, "Find Me\n", string(page))
	_, err = store.Page("../general/notes.md")
	require.Error(t, err)
	_, err = store.Page("general/escape.md")
	require.Error(t, err)
}

func TestDraftSubmitPublishesOnlyMarkdownInItsScope(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	repo := checkoutWithOrigin(t)
	draft, err := store.Draft(context.Background(), "session-1", repo)
	require.NoError(t, err)
	require.NoDirExists(t, filepath.Join(draft.Root, ".git"))
	require.NoError(t, os.WriteFile(filepath.Join(draft.Root, "general", "first.md"), []byte("changed\n"), 0o600))

	result, err := store.Submit(context.Background(), draft, "codex")
	require.NoError(t, err)
	assert.Equal(t, "published", result.State)
	assert.NoDirExists(t, draft.Root)

	work := t.TempDir()
	run(t, "", "git", "clone", remote, work)
	data, err := os.ReadFile(filepath.Join(work, "general", "first.md"))
	require.NoError(t, err)
	assert.Equal(t, "changed\n", string(data))
}

func TestDraftSubmitRetainsRejectedSecret(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	draft, err := store.Draft(context.Background(), "session-2", checkoutWithOrigin(t))
	require.NoError(t, err)
	file := filepath.Join(draft.Root, "general", "secret.md")
	require.NoError(t, os.WriteFile(file, []byte("token=abcdefghijklmnopqrstuvwxyz\n"), 0o600))

	result, err := store.Submit(context.Background(), draft, "codex")
	require.Error(t, err)
	assert.Equal(t, "rejected", result.State)
	assert.Contains(t, result.Error, "credential-assignment")
	assert.FileExists(t, file)
}

func TestDraftSubmitRejectsDeletes(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	draft, err := store.Draft(context.Background(), "session-3", checkoutWithOrigin(t))
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(draft.Root, "general", "first.md")))

	result, err := store.Submit(context.Background(), draft, "codex")
	require.Error(t, err)
	assert.Equal(t, "rejected", result.State)
	assert.Contains(t, result.Error, "deletes are not allowed")
}

func newRemote(t *testing.T) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "wiki.git")
	run(t, "", "git", "init", "--bare", "--initial-branch", "main", bare)
	work := t.TempDir()
	run(t, work, "git", "init", "--initial-branch", "main")
	run(t, work, "git", "config", "user.email", "test@example.com")
	run(t, work, "git", "config", "user.name", "Test")
	require.NoError(t, os.MkdirAll(filepath.Join(work, "general"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(work, "general", "first.md"), []byte("first\n"), 0o600))
	run(t, work, "git", "add", ".")
	run(t, work, "git", "commit", "-m", "first")
	run(t, work, "git", "remote", "add", "origin", bare)
	run(t, work, "git", "push", "origin", "main")
	return bare
}

func checkoutWithOrigin(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run(t, root, "git", "init", "--initial-branch", "main")
	run(t, root, "git", "remote", "add", "origin", "git@github.com:owner/repo.git")
	return root
}

func pushPage(t *testing.T, remote, name, content string) {
	t.Helper()
	work := t.TempDir()
	run(t, "", "git", "clone", remote, work)
	run(t, work, "git", "config", "user.email", "test@example.com")
	run(t, work, "git", "config", "user.name", "Test")
	file := filepath.Join(work, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o700))
	require.NoError(t, os.WriteFile(file, []byte(content+"\n"), 0o600))
	run(t, work, "git", "add", ".")
	run(t, work, "git", "commit", "-m", "page")
	run(t, work, "git", "push", "origin", "main")
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	require.NoError(t, err)
	return stringTrim(string(out))
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "%s", out)
}

func stringTrim(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func TestSyncDoesNotWaitOnAConcurrentPublish(t *testing.T) {
	// The design gives publishing its own lock so the read path never waits on
	// a slow writer. One mutex for both would fail here.
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	require.NoError(t, store.Init(context.Background(), remote))

	draft, err := store.Draft(context.Background(), "session-slow", checkoutWithOrigin(t))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(draft.Root, "general", "first.md"), []byte("changed\n"), 0o600))

	started, release := make(chan struct{}), make(chan struct{})
	real := store.run
	var once sync.Once
	store.run = func(ctx context.Context, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "clone" {
			once.Do(func() { close(started) })
			<-release
		}
		return real(ctx, args...)
	}

	published := make(chan struct{})
	go func() {
		defer close(published)
		_, _ = store.Submit(context.Background(), draft, "codex")
	}()
	<-started
	// Let the publisher finish before the temp directory is swept, or its
	// clone is still being written when cleanup runs.
	defer func() { close(release); <-published }()

	done := make(chan struct{})
	go func() {
		_ = store.Sync(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Sync waited on an in-flight publish; the read path must not block on a slow writer")
	}
}

func TestAStalledCanonicalRefreshIsNotACleanPublish(t *testing.T) {
	// The commit is on the remote, but the draft is retained and search has
	// not caught up: reporting that as success loses the draft's own state.
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	draft, err := store.Draft(context.Background(), "session-stall", checkoutWithOrigin(t))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(draft.Root, "general", "first.md"), []byte("changed\n"), 0o600))

	real := store.run
	store.run = func(ctx context.Context, args ...string) (string, error) {
		// Only the canonical clone's own fetch, not the publish clone's.
		if len(args) > 3 && args[0] == "-C" && args[1] == store.Root() && args[2] == "fetch" {
			return "", errors.New("offline")
		}
		return real(ctx, args...)
	}

	result, err := store.Submit(context.Background(), draft, "codex")
	require.Error(t, err, "a stalled refresh is not a clean publish")
	assert.Equal(t, "published", result.State)
	assert.NotEmpty(t, result.Commit)
	assert.DirExists(t, draft.Root, "the draft is retained until the clone catches up")
}

func TestAGitconfigRewritingURLsDoesNotBreakTheClone(t *testing.T) {
	// `url.<base>.insteadOf` — how a GitHub user clones https and pushes ssh —
	// rewrites what `remote get-url` reports, not what ade configured.
	bare := newRemote(t)
	const configured = "adewiki:owenrumney/ade-wiki"
	store := New(t.TempDir(), configured)
	run(t, "", "git", "clone", bare, store.Root())
	run(t, store.Root(), "git", "remote", "set-url", "origin", configured)
	run(t, store.Root(), "git", "config", "url."+bare+".insteadOf", configured)

	require.Equal(t, bare, gitOut(t, store.Root(), "remote", "get-url", "origin"), "git reports the rewrite")
	assert.Empty(t, store.Status(context.Background()).Repair, "ade compares what it configured")
	assert.NoError(t, store.Sync(context.Background()))
}

func TestAnOriginThatIsNotTheConfiguredOneSaysBoth(t *testing.T) {
	remote, other := newRemote(t), newRemote(t)
	store := New(t.TempDir(), remote)
	require.NoError(t, store.Init(context.Background(), remote))
	run(t, store.Root(), "git", "remote", "set-url", "origin", other)

	repair := store.Status(context.Background()).Repair
	assert.Contains(t, repair, other, "what it found")
	assert.Contains(t, repair, remote, "what was configured")
	assert.Contains(t, repair, "ade wiki init --remote")
}

func TestAHitIsReadByItsScopeAndDateNotItsFilename(t *testing.T) {
	// Every repository's notes are called the same thing, so "log.md:11"
	// tells two hits apart no better than nothing does.
	remote := newRemote(t)
	pushPage(t, remote, "repos/owenrumney/ade/log.md", "needle in ade")
	pushPage(t, remote, "general/log.md", "needle in general")
	store := New(t.TempDir(), remote)
	require.NoError(t, store.Sync(context.Background()))

	hits := store.Search("needle")
	require.Len(t, hits, 2)
	by := map[string]Hit{}
	for _, hit := range hits {
		by[hit.Scope] = hit
	}
	require.Contains(t, by, "owenrumney/ade")
	require.Contains(t, by, "general")
	for scope, hit := range by {
		assert.NotEmpty(t, hit.Updated, scope)
		assert.Equal(t, "log.md", filepath.Base(hit.Path), "the filename really is the same")
	}
}

func TestSearchNamesADateWithoutInvokingGit(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	require.NoError(t, store.Sync(context.Background()))

	store.run = func(context.Context, ...string) (string, error) {
		t.Error("Search invoked Git; the read path must never wait on it")
		return "", errors.New("no git on the read path")
	}
	hits := store.Search("first")
	require.Len(t, hits, 1)
	assert.NotEmpty(t, hits[0].Updated)
	assert.Equal(t, "general", hits[0].Scope)
}

func TestSearchSpreadsHitsAcrossPagesRatherThanFillingFromOne(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	require.NoError(t, store.Init(context.Background(), remote))
	long := strings.Repeat("the word is here\n", 80)
	pushPage(t, remote, "general/aaa-log.md", long)
	pushPage(t, remote, "repos/owner/repo/zzz-page.md", "the word, once")
	require.NoError(t, store.Sync(context.Background()))

	hits := store.Search("the word")
	paths := map[string]int{}
	for _, hit := range hits {
		paths[hit.Path]++
	}
	assert.Equal(t, hitsPerPage, paths["general/aaa-log.md"])
	assert.Equal(t, 1, paths["repos/owner/repo/zzz-page.md"], "the page further down the walk is still found")
}

func TestAnUntouchedDraftFollowsTheWikiForward(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	repo := checkoutWithOrigin(t)
	first, err := store.Draft(context.Background(), "session-1", repo)
	require.NoError(t, err)

	pushPage(t, remote, "general/later.md", "later")
	require.NoError(t, store.Sync(context.Background()))
	second, err := store.Draft(context.Background(), "session-1", repo)
	require.NoError(t, err)
	assert.NotEqual(t, first.Base, second.Base, "the draft is re-seeded from the new revision")
	assert.FileExists(t, filepath.Join(second.Root, "general", "later.md"))
}

func TestADraftWithEditsIsNeverReseeded(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	repo := checkoutWithOrigin(t)
	first, err := store.Draft(context.Background(), "session-1", repo)
	require.NoError(t, err)
	page := filepath.Join(first.Root, "general", "mine.md")
	require.NoError(t, os.WriteFile(page, []byte("my notes\n"), 0o600))
	future := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(page, future, future))

	pushPage(t, remote, "general/later.md", "later")
	require.NoError(t, store.Sync(context.Background()))
	second, err := store.Draft(context.Background(), "session-1", repo)
	require.NoError(t, err)
	assert.Equal(t, first.Base, second.Base)
	assert.FileExists(t, page)
}

func TestAFailedGitSaysWhyBeforeWhatItWasDoing(t *testing.T) {
	// Only the first line of a job's error is shown, and a clone's first line
	// is always progress.
	cases := []struct {
		name string
		out  string
		want string
	}{
		{
			"a clone that could not log in",
			"Cloning into '/tmp/x'...\nfatal: could not read Username for 'https://github.com': terminal prompts disabled\n",
			"fatal: could not read Username for 'https://github.com': terminal prompts disabled",
		},
		{
			"a clone sent to ssh with no key",
			"Cloning into '/tmp/x'...\ngit@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.\n",
			"fatal: Could not read from remote repository.",
		},
		{
			"a repository the account cannot see",
			"Cloning into '/tmp/x'...\nERROR: Repository not found.\nfatal: Could not read from remote repository.\n",
			"ERROR: Repository not found.; fatal: Could not read from remote repository.",
		},
		{
			"progress written over carriage returns",
			"Cloning into '/tmp/x'...\rremote: Counting objects: 50%\rfatal: repository not found\n",
			"fatal: repository not found",
		},
		{"nothing said at all", "", ""},
		{"no line that says why", "Cloning into '/tmp/x'...\n", "Cloning into '/tmp/x'..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gitReason(tc.out)
			assert.Equal(t, tc.want, got)
			assert.NotContains(t, got, "\n", "a verdict keeps one line")
		})
	}
}

func TestACloneThatFailsCarriesGitsReason(t *testing.T) {
	store := New(t.TempDir(), filepath.Join(t.TempDir(), "no-such-remote"))
	err := store.Sync(context.Background())
	require.Error(t, err)
	first, _, _ := strings.Cut(err.Error(), "\n")
	assert.Contains(t, first, "does not exist", "the reason reaches a one-line verdict")
}
