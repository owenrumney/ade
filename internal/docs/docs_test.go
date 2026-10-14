package docs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func repo(t *testing.T) (root, gitDir string) {
	t.Helper()
	root = t.TempDir()
	gitDir = filepath.Join(root, ".git")
	require.NoError(t, os.MkdirAll(gitDir, 0o750))
	return root, gitDir
}

// Put creates what it is given, so a dir the caller never resolved would land
// documents in whatever directory the process happens to be in.
func TestPutRefusesADirectoryItCannotResolve(t *testing.T) {
	dir := t.TempDir()
	file, err := Put(dir, "cart", "# Cart\n")
	require.NoError(t, err)
	assert.Equal(t, "cart.md", file)

	_, err = Put("", "cart.md", "# Cart\n")
	require.Error(t, err)
	_, err = Put("docs", "a/b/deep.md", "x\n")
	require.Error(t, err)
	assert.NoDirExists(t, "docs")
}

func TestDirPrefersScratch(t *testing.T) {
	root, gitDir := repo(t)
	scratch := filepath.Join(root, scratchLink)
	require.NoError(t, os.MkdirAll(scratch, 0o750))

	dir, err := Dir(root, gitDir, "")
	require.NoError(t, err)
	assert.Equal(t, scratch, dir)
}

func TestDirFallsBackToTheGitDir(t *testing.T) {
	root, gitDir := repo(t)

	dir, err := Dir(root, gitDir, "")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(gitDir, "ade", "docs"), dir)
	assert.DirExists(t, dir, "the directory is created so a first write succeeds")
}

func TestDirHonoursAnOverride(t *testing.T) {
	root, gitDir := repo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, scratchLink), 0o750))
	want := filepath.Join(t.TempDir(), "elsewhere")

	dir, err := Dir(root, gitDir, want)
	require.NoError(t, err)
	assert.Equal(t, want, dir, "an explicit --docs beats .scratch")
}

func TestDirFollowsAScratchSymlink(t *testing.T) {
	root, gitDir := repo(t)
	target := t.TempDir()
	require.NoError(t, os.Symlink(target, filepath.Join(root, scratchLink)))

	dir, err := Dir(root, gitDir, "")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, scratchLink), dir)
}

func TestCreate(t *testing.T) {
	dir := t.TempDir()

	file, err := Create(dir, "design-notes", "Design notes")
	require.NoError(t, err)
	assert.Equal(t, "design-notes.md", file, "a bare name gains the extension")

	body, err := Read(dir, file)
	require.NoError(t, err)
	assert.Contains(t, body, "# Design notes")
}

func TestCreateTitlesFromTheFilenameWhenNoneIsGiven(t *testing.T) {
	dir := t.TempDir()
	file, err := Create(dir, "sub/plan.md", "")
	require.NoError(t, err)

	body, err := Read(dir, file)
	require.NoError(t, err)
	assert.Contains(t, body, "# plan")
	assert.FileExists(t, filepath.Join(dir, "sub", "plan.md"), "nested names create their directory")
}

func TestCreateRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	_, err := Create(dir, "plan.md", "")
	require.NoError(t, err)

	_, err = Create(dir, "plan.md", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestResolveRejectsAnythingOutsideTheDirectory(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name string
		file string
	}{
		{name: "parent traversal", file: "../secrets.md"},
		{name: "buried traversal", file: "a/../../secrets.md"},
		{name: "absolute path", file: "/etc/passwd.md"},
		{name: "windows separators", file: `..\..\secrets.md`},
		{name: "empty", file: ""},
		{name: "whitespace", file: "   "},
		{name: "bare dot", file: "."},
		{name: "not markdown", file: "notes.txt"},
		{name: "no extension", file: "notes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Resolve(dir, tt.file)
			require.Error(t, err)
		})
	}
}

func TestResolveAcceptsDocumentsInside(t *testing.T) {
	dir := t.TempDir()
	for _, file := range []string{"plan.md", "sub/plan.markdown", "./plan.md"} {
		got, err := Resolve(dir, file)
		require.NoError(t, err, file)
		assert.True(t, filepath.IsAbs(got) || got != "", file)
	}
}

func TestSaveRequiresAnExistingDocument(t *testing.T) {
	dir := t.TempDir()

	require.Error(t, Save(dir, "ghost.md", "hello"), "save must not create files")

	file, err := Create(dir, "plan.md", "")
	require.NoError(t, err)
	require.NoError(t, Save(dir, file, "# edited\n"))

	body, err := Read(dir, file)
	require.NoError(t, err)
	assert.Equal(t, "# edited\n", body)
}

func TestTreeListsOnlyMarkdown(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o750))
	for _, name := range []string{"b.md", "a.md", "notes.txt", "sub/c.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("# x"), 0o600))
	}

	tree, err := Tree(dir, "a.md")
	require.NoError(t, err)
	require.Len(t, tree, 3, "two files plus one directory, and no .txt")

	assert.True(t, tree[0].Dir, "directories sort first")
	assert.Equal(t, "sub", tree[0].Name)
	assert.Equal(t, "a.md", tree[1].Name)
	assert.True(t, tree[1].Selected)
	assert.Equal(t, "b.md", tree[2].Name)
	assert.False(t, tree[2].Selected)
}

func TestRenderProducesAFragment(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plan.md"), []byte("# Title\n\nSome *text*.\n"), 0o600))

	doc, err := Render(dir, "plan.md")
	require.NoError(t, err)
	assert.Contains(t, string(doc.Body), "<em>text</em>")
	assert.NotContains(t, string(doc.Body), "<html", "a fragment, not a page")
	require.NotEmpty(t, doc.TOC)
	assert.Equal(t, "Title", doc.TOC[0].Text)

	// The title comes from front matter, falling back to the filename. The
	// first heading is not consulted.
	assert.Equal(t, "plan.md", doc.Title)
}

func TestRenderTakesItsTitleFromFrontMatter(t *testing.T) {
	dir := t.TempDir()
	body := "---\ntitle: Cart redesign\n---\n\n# Something else\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plan.md"), []byte(body), 0o600))

	doc, err := Render(dir, "plan.md")
	require.NoError(t, err)
	assert.Equal(t, "Cart redesign", doc.Title)
}

func TestRenderRejectsAnEscapingPath(t *testing.T) {
	_, err := Render(t.TempDir(), "../../etc/passwd.md")
	require.Error(t, err)
}

func TestTicket(t *testing.T) {
	tests := []struct {
		name   string
		branch string
		want   string
	}{
		{name: "typed branch", branch: "feat/dev-82-add-the-lsp-to-the-plugin", want: "DEV-82"},
		{name: "fix prefix and fix ticket", branch: "fix/fix-427-handle-fix-task", want: "FIX-427"},
		{name: "chore", branch: "chore/dev-71-update-skill", want: "DEV-71"},
		{name: "bare ticket", branch: "DEV-82", want: "DEV-82"},
		{name: "already uppercase", branch: "feat/FIX-301-thing", want: "FIX-301"},
		{name: "no ticket", branch: "feat/add-claude-support"},
		{name: "no ticket, plain", branch: "main"},
		// A version number is not a ticket.
		{name: "dependabot", branch: "dependabot/npm_and_yarn/setup/undici-6.27.0"},
		{name: "single letter prefix is too short", branch: "x-12"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Ticket(tt.branch))
		})
	}
}

func TestSearchDirsSkipsWhatIsNotThere(t *testing.T) {
	root, gitDir := repo(t)

	assert.Empty(t, SearchDirs(root, gitDir, ""), "nothing exists yet")

	scratch := filepath.Join(root, scratchLink)
	require.NoError(t, os.MkdirAll(scratch, 0o750))
	docsDir := filepath.Join(gitDir, "ade", "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o750))

	assert.Equal(t, []string{scratch, docsDir}, SearchDirs(root, gitDir, ""),
		"scratch is searched first")

	override := t.TempDir()
	assert.Equal(t, []string{override, scratch, docsDir}, SearchDirs(root, gitDir, override))
	assert.NotContains(t, SearchDirs(root, gitDir, "relative/path"), "relative/path")
}

func TestFind(t *testing.T) {
	scratch, gitDocs := t.TempDir(), t.TempDir()
	write := func(dir, name string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("# x"), 0o600))
	}
	write(scratch, "DEV-82.md")
	write(gitDocs, "cart-redesign.md")
	write(gitDocs, "fix-427-notes.md")

	dirs := []string{scratch, gitDocs}

	tests := []struct {
		name     string
		branch   string
		wantDir  string
		wantFile string
		wantOK   bool
	}{
		{name: "ticket in scratch", branch: "feat/dev-82-add-lsp", wantDir: scratch, wantFile: "DEV-82.md", wantOK: true},
		{name: "ticket embedded in a longer name", branch: "fix/fix-427-handle", wantDir: gitDocs, wantFile: "fix-427-notes.md", wantOK: true},
		{name: "exact branch name", branch: "cart-redesign", wantDir: gitDocs, wantFile: "cart-redesign.md", wantOK: true},
		{name: "last segment of a path", branch: "feat/cart-redesign", wantDir: gitDocs, wantFile: "cart-redesign.md", wantOK: true},
		{name: "no ticket and no match", branch: "feat/add-claude-support"},
		{name: "main matches nothing", branch: "main"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, file, ok := Find(dirs, "", tt.branch, "s1")
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantDir, dir)
			assert.Equal(t, tt.wantFile, file)
		})
	}
}

func TestFindPrefersTheMostSpecificMatch(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"DEV-82.md", "dev-82-notes.md", "cart-redesign.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("# x"), 0o600))
	}

	t.Run("a ticket beats a name", func(t *testing.T) {
		// The ticket is in an earlier segment and the last segment names a
		// different document, so the two rules genuinely disagree.
		_, file, ok := Find([]string{dir}, "", "dev-82/cart-redesign", "s1")
		require.True(t, ok)
		assert.Equal(t, "DEV-82.md", file)
	})

	t.Run("named for the ticket beats mentioning it", func(t *testing.T) {
		_, file, ok := Find([]string{dir}, "", "feat/dev-82-notes", "s1")
		require.True(t, ok)
		assert.Equal(t, "DEV-82.md", file)
	})

	t.Run("mentioning it is enough when nothing is named for it", func(t *testing.T) {
		other := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(other, "fix-427-notes.md"), []byte("# x"), 0o600))
		_, file, ok := Find([]string{other}, "", "fix/fix-427-handle", "s1")
		require.True(t, ok)
		assert.Equal(t, "fix-427-notes.md", file)
	})
}

func TestDirAndSearchDirsAgree(t *testing.T) {
	// Divergence between these two is what wrote a document into .scratch and
	// then went hunting for it under the git dir.
	root, gitDir := repo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, scratchLink), 0o750))

	dir, err := Dir(root, gitDir, "")
	require.NoError(t, err)

	found := SearchDirs(root, gitDir, "")
	require.NotEmpty(t, found)
	assert.Equal(t, found[0], dir, "a document is created where the next run looks first")
}

func TestSearchDirsFindsTheMainCheckoutsScratchFromAWorktree(t *testing.T) {
	// The shape ade create --linear produces: the document is written in the
	// main checkout's .scratch, and then served from a worktree that has none.
	main, commonGitDir := repo(t)
	scratch := filepath.Join(main, scratchLink)
	require.NoError(t, os.MkdirAll(scratch, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(scratch, "fix-623.md"), []byte("# Fix 623\n"), 0o600))

	worktree := t.TempDir() // no .scratch of its own

	dirs := SearchDirs(worktree, commonGitDir, "")
	assert.Contains(t, dirs, scratch, "the worktree has to see where the document was written")

	dir, file, ok := Find(dirs, "", "fix-623", "s1")
	require.True(t, ok, "the document created for this branch must be findable from its worktree")
	assert.Equal(t, scratch, dir)
	assert.Equal(t, "fix-623.md", file)
}

func TestSearchDirsDoesNotDuplicateTheMainCheckout(t *testing.T) {
	root, gitDir := repo(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, scratchLink), 0o750))

	// Run from the main checkout, its own .scratch and "the main checkout's"
	// .scratch are the same directory.
	dirs := SearchDirs(root, gitDir, "")
	assert.Len(t, dirs, 1)
	assert.Equal(t, filepath.Join(root, scratchLink), dirs[0])
}

func TestEnsure(t *testing.T) {
	dir := t.TempDir()

	file, created, err := Ensure(dir, "fix-623", "Fix 623")
	require.NoError(t, err)
	assert.Equal(t, "fix-623.md", file)
	assert.True(t, created)

	// Write something into it, then ask again: running the same create is
	// carrying on with that design, so the work must survive.
	require.NoError(t, Save(dir, file, "# Fix 623\n\nWhat I had decided.\n"))

	again, created, err := Ensure(dir, "fix-623", "Fix 623")
	require.NoError(t, err)
	assert.Equal(t, file, again)
	assert.False(t, created, "the second call opens rather than creates")

	body, err := Read(dir, again)
	require.NoError(t, err)
	assert.Contains(t, body, "What I had decided.", "the existing document is untouched")
}

func TestEnsureStillRejectsRubbish(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"", "   ", "../escape"} {
		_, _, err := Ensure(dir, name, "")
		require.Error(t, err, name)
	}
}

func TestLocateAndDelete(t *testing.T) {
	scratch, gitDocs := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(scratch, "fix-123.md"), []byte("# a"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(gitDocs, "cart.md"), []byte("# b"), 0o600))
	dirs := []string{scratch, gitDocs}

	t.Run("finds one in either directory", func(t *testing.T) {
		got, err := Locate(dirs, "fix-123.md")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(scratch, "fix-123.md"), got)

		got, err = Locate(dirs, "cart")
		require.NoError(t, err, "the extension is optional")
		assert.Equal(t, filepath.Join(gitDocs, "cart.md"), got)
	})

	t.Run("says so when there is none", func(t *testing.T) {
		_, err := Locate(dirs, "nope.md")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no document called nope.md")
	})

	t.Run("cannot be pointed outside those directories", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "secret.md")
		require.NoError(t, os.WriteFile(outside, []byte("x"), 0o600))

		for _, name := range []string{"../secret.md", outside, "a/../../secret.md"} {
			_, err := Delete(dirs, name)
			require.Error(t, err, name)
		}
		assert.FileExists(t, outside, "nothing outside the design directories is reachable")
	})

	t.Run("deletes and reports where it was", func(t *testing.T) {
		where, err := Delete(dirs, "fix-123")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(scratch, "fix-123.md"), where)
		assert.NoFileExists(t, where)

		_, err = Delete(dirs, "fix-123")
		require.Error(t, err, "deleting it twice is an error, not a silent success")
	})
}

// An agent shares the disk, so it can plant a link in the design directory and
// then ask ade to save through it.
func TestSavingThroughALinkWritesNothingOutside(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	target := filepath.Join(outside, "zshrc.md")
	require.NoError(t, os.WriteFile(target, []byte("# yours\n"), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(dir, "plan.md")))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "away")))

	assert.Error(t, Save(dir, "plan.md", "# theirs\n"))
	_, err := Put(dir, "away/new.md", "# theirs\n")
	assert.Error(t, err)

	body, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "# yours\n", string(body))
	assert.NoFileExists(t, filepath.Join(outside, "new.md"))
}

// A link that stays inside the root passes the path check, so the write itself
// is what refuses to follow one.
func TestSavingDoesNotFollowALinkAtAll(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "real.md"), []byte("# yours\n"), 0o600))
	require.NoError(t, os.Symlink("real.md", filepath.Join(dir, "plan.md")))

	assert.Error(t, Save(dir, "plan.md", "# theirs\n"))

	body, err := os.ReadFile(filepath.Join(dir, "real.md"))
	require.NoError(t, err)
	assert.Equal(t, "# yours\n", string(body))
}
