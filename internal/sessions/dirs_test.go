package sessions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repo makes a checkout at dir, on branch, and returns the path.
func repo(t *testing.T, dir, branch string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "HEAD"),
		[]byte("ref: refs/heads/"+branch+"\n"), 0o600))
	return dir
}

func TestDirsFindsCheckoutsAtEitherDepth(t *testing.T) {
	root := t.TempDir()
	repo(t, filepath.Join(root, "loose"), "main")
	repo(t, filepath.Join(root, "owner", "nested"), "fix-1")
	// Deep enough to be a vendored copy rather than work.
	repo(t, filepath.Join(root, "a", "b", "c", "d"), "main")
	// Not a checkout at all.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o750))

	got := map[string]Dir{}
	for _, dir := range Dirs([]string{root}, nil) {
		got[dir.Name] = dir
	}

	require.Contains(t, got, "loose")
	require.Contains(t, got, "nested")
	assert.NotContains(t, got, "d", "went deeper than a repository lives")
	assert.NotContains(t, got, "notes")

	assert.Equal(t, "main", got["loose"].Branch)
	// A repository directly in a scan root has no owner worth naming.
	assert.Empty(t, got["loose"].Owner)
	assert.Equal(t, "fix-1", got["nested"].Branch)
	assert.Equal(t, "owner", got["nested"].Owner)
}

func TestDirsStopsAtARepository(t *testing.T) {
	root := t.TempDir()
	outer := repo(t, filepath.Join(root, "outer"), "main")
	// What is inside a repository is that repository's business.
	repo(t, filepath.Join(outer, "vendor", "inner"), "main")

	var names []string
	for _, dir := range Dirs([]string{root}, nil) {
		names = append(names, dir.Name)
	}
	assert.Equal(t, []string{"outer"}, names)
}

func TestDirsRespectsIgnored(t *testing.T) {
	root := t.TempDir()
	repo(t, filepath.Join(root, "keep"), "main")
	repo(t, filepath.Join(root, "drop"), "main")
	repo(t, filepath.Join(root, "owner", "drop"), "main")

	ignored := func(dir string) bool { return filepath.Base(dir) == "drop" }
	for _, dir := range Dirs([]string{root}, ignored) {
		assert.NotEqual(t, "drop", dir.Name, dir.Path)
	}
}

func TestDirsPutsRecentWorkFirstThenAlphabetical(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"zulu", "alpha", "mike"} {
		repo(t, filepath.Join(root, name), "main")
	}

	// None of these have sessions, so the only order that means anything is
	// alphabetical.
	var names []string
	for _, dir := range Dirs([]string{root}, nil) {
		names = append(names, dir.Name)
	}
	assert.Equal(t, []string{"alpha", "mike", "zulu"}, names)
}

func TestDirsIgnoresWorkDoneOutsideTheRoots(t *testing.T) {
	// A conversation somewhere the roots do not cover attaches to nothing, and
	// listing it would offer a checkout the settings deliberately exclude.
	for _, dir := range Dirs([]string{t.TempDir()}, nil) {
		assert.Empty(t, dir.Sessions, dir.Path)
	}
}

func TestFindDirOnlyMatchesWhatWasOffered(t *testing.T) {
	list := []Dir{{Path: "/repo/one"}, {Path: "/repo/two"}}

	found, ok := FindDir(list, "/repo/two")
	require.True(t, ok)
	assert.Equal(t, "/repo/two", found.Path)

	for _, path := range []string{"/etc", "/", "/repo", "/repo/one/sub", ""} {
		_, ok := FindDir(list, path)
		assert.Falsef(t, ok, "accepted %q", path)
	}
}

func TestBranchOfReadsAWorktreePointer(t *testing.T) {
	root := t.TempDir()
	main := repo(t, filepath.Join(root, "main"), "trunk")

	// A worktree's .git is a file naming the real directory.
	wt := filepath.Join(root, "wt")
	gitDir := filepath.Join(main, ".git", "worktrees", "wt")
	require.NoError(t, os.MkdirAll(gitDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "HEAD"),
		[]byte("ref: refs/heads/fix-9\n"), 0o600))
	require.NoError(t, os.MkdirAll(wt, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(wt, ".git"),
		[]byte("gitdir: "+gitDir+"\n"), 0o600))

	assert.Equal(t, "fix-9", branchOf(wt))
	assert.Equal(t, "trunk", branchOf(main))
	assert.Empty(t, branchOf(root), "not a checkout")
}

func TestBranchOfLeavesADetachedHeadBlank(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "HEAD"),
		[]byte(strings.Repeat("a", 40)+"\n"), 0o600))

	assert.Empty(t, branchOf(dir))
}

// worktree makes a worktree of main at dir, on branch.
func worktree(t *testing.T, main, dir, branch string) string {
	t.Helper()
	gitDir := filepath.Join(main, ".git", "worktrees", filepath.Base(dir))
	require.NoError(t, os.MkdirAll(gitDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "HEAD"),
		[]byte("ref: refs/heads/"+branch+"\n"), 0o600))
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git"),
		[]byte("gitdir: "+gitDir+"\n"), 0o600))
	return dir
}

func TestReposPutWorktreesUnderTheirRepository(t *testing.T) {
	root := t.TempDir()
	owner := filepath.Join(root, "acme")
	main := repo(t, filepath.Join(owner, "widget"), "main")
	worktree(t, main, filepath.Join(owner, "widget-fix-1"), "fix-1")
	worktree(t, main, filepath.Join(owner, "widget-fix-2"), "fix-2")
	repo(t, filepath.Join(owner, "gadget"), "main")

	repos := Repos([]string{root}, nil)
	byName := map[string]Repo{}
	for _, r := range repos {
		byName[r.Name] = r
	}

	// A worktree is a place to work on a repository, not a repository of its
	// own, so the top level is two entries rather than four.
	require.Len(t, repos, 2)
	require.Contains(t, byName, "widget")
	require.Contains(t, byName, "gadget")
	assert.NotContains(t, byName, "widget-fix-1", "a worktree reached the top level")

	widget := byName["widget"]
	assert.Equal(t, "acme", widget.Owner)
	require.Len(t, widget.Checkouts, 3)
	// The main checkout leads, and is the only one not marked a worktree.
	assert.Equal(t, main, widget.Checkouts[0].Path)
	assert.False(t, widget.Checkouts[0].Worktree)
	for _, dir := range widget.Checkouts[1:] {
		assert.True(t, dir.Worktree, dir.Path)
		assert.Equal(t, main, dir.Main)
	}

	branches := []string{}
	for _, dir := range widget.Checkouts {
		branches = append(branches, dir.Branch)
	}
	assert.ElementsMatch(t, []string{"main", "fix-1", "fix-2"}, branches)

	assert.Len(t, byName["gadget"].Checkouts, 1)
}

func TestFindCheckoutReachesInsideARepository(t *testing.T) {
	root := t.TempDir()
	main := repo(t, filepath.Join(root, "widget"), "main")
	tree := worktree(t, main, filepath.Join(root, "widget-fix-1"), "fix-1")

	repos := Repos([]string{root}, nil)

	// Opening names a checkout, and a worktree is one even though it is not on
	// the top level.
	found, ok := FindCheckout(repos, tree)
	require.True(t, ok)
	assert.Equal(t, tree, found.Path)

	_, ok = FindCheckout(repos, main)
	assert.True(t, ok)

	for _, path := range []string{"/etc", root, filepath.Join(root, "nope"), ""} {
		_, ok := FindCheckout(repos, path)
		assert.Falsef(t, ok, "accepted %q", path)
	}
}

func TestMarkRepoHereFlagsBothLevels(t *testing.T) {
	root := t.TempDir()
	main := repo(t, filepath.Join(root, "widget"), "main")
	tree := worktree(t, main, filepath.Join(root, "widget-fix-1"), "fix-1")
	repo(t, filepath.Join(root, "gadget"), "main")

	// Standing in a worktree, the switcher has to say so at the repository
	// level too, or the row you are inside looks like somewhere else.
	repos := MarkRepoHere(Repos([]string{root}, nil), tree)
	for _, r := range repos {
		assert.Equal(t, r.Name == "widget", r.Here, r.Path)
		for _, dir := range r.Checkouts {
			assert.Equal(t, dir.Path == tree, dir.Here, dir.Path)
		}
	}
}

func TestAWorkerSessionDirectoryIsNotAnOwner(t *testing.T) {
	root := t.TempDir()
	// What provision.Clone makes: /srv/ade/<random hex>/<name>.
	dir := repo(t, filepath.Join(root, "80a6ccb2de7fe172", "dashboard"), "master")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "config"),
		[]byte("[core]\n\tbare = false\n[remote \"origin\"]\n\turl = git@github.com:infracost/dashboard.git\n"), 0o600))
	// One with no origin to read falls back to no owner, never to the hex.
	repo(t, filepath.Join(root, "0123456789abcdef", "orphan"), "main")

	got := map[string]Dir{}
	for _, one := range Dirs([]string{root}, nil) {
		got[one.Name] = one
	}
	require.Contains(t, got, "dashboard")
	assert.Equal(t, "infracost", got["dashboard"].Owner)
	assert.Empty(t, got["orphan"].Owner)

	repos := map[string]Repo{}
	for _, one := range Repos([]string{root}, nil) {
		repos[one.Name] = one
	}
	assert.Equal(t, "infracost", repos["dashboard"].Owner)
}
