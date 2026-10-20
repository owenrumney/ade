package launcher

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitCheckout is a real repository, since the documents live beside the git
// dir and finding them means asking git where that is.
func gitCheckout(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o750))
	git(t, dir, "init", "-b", "main", "-q")
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

func designsOf(t *testing.T, s *Server, dir string) []string {
	t.Helper()
	rec := do(t, s, http.MethodGet, "/api/designs?dir="+dir, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Designs []string `json:"designs"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	return got.Designs
}

func TestACheckoutsDesignDocumentsAreListedForTheScreen(t *testing.T) {
	m := newFixture(t)
	dir := filepath.Join(m.code, "ade")
	gitCheckout(t, dir)
	scratch := filepath.Join(dir, ".scratch")
	require.NoError(t, os.MkdirAll(scratch, 0o750))
	for _, name := range []string{"cart-redesign.md", "FIX-623-cancel.md", "notes.txt"} {
		require.NoError(t, os.WriteFile(filepath.Join(scratch, name), []byte("# x\n"), 0o600))
	}

	assert.Equal(t, []string{"cart-redesign.md", "FIX-623-cancel.md"}, designsOf(t, newLauncherOn(t, m), dir),
		"markdown only, in the order the design tree draws them")
}

// The layout every repository here actually uses: no .scratch, documents
// under the git dir, which is where `ade create` puts them.
func TestDocumentsUnderTheGitDirAreFoundToo(t *testing.T) {
	m := newFixture(t)
	dir := filepath.Join(m.code, "coast")
	gitCheckout(t, dir)
	under := filepath.Join(dir, ".git", "ade", "docs")
	require.NoError(t, os.MkdirAll(under, 0o750))
	for _, name := range []string{"TRK-1065.md", "add-iconfs-for-mcp.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(under, name), []byte("# x\n"), 0o600))
	}

	assert.Equal(t, []string{"add-iconfs-for-mcp.md", "TRK-1065.md"}, designsOf(t, newLauncherOn(t, m), dir))
}

// Both places at once: an empty .scratch left by an earlier `ade scratch` must
// not hide the documents under the git dir.
func TestEveryDirectoryDocumentsCanLiveInIsListed(t *testing.T) {
	m := newFixture(t)
	dir := filepath.Join(m.code, "coast")
	gitCheckout(t, dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".scratch"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".scratch", "scratched.md"), []byte("# x\n"), 0o600))
	under := filepath.Join(dir, ".git", "ade", "docs")
	require.NoError(t, os.MkdirAll(under, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(under, "cart.md"), []byte("# x\n"), 0o600))

	s := newLauncherOn(t, m)
	assert.Equal(t, []string{"scratched.md", "cart.md"}, designsOf(t, s, dir))

	// And read from wherever it was listed, not only the first directory.
	rec := do(t, s, http.MethodGet, "/api/design?file=cart.md&dir="+dir, "")
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// A worktree sees its repository's documents: they live in the git dir the
// two share, and the screen resolves either to the checkout they were made from.
func TestAWorktreeSeesItsRepositorysDocuments(t *testing.T) {
	m := newFixture(t)
	dir := filepath.Join(m.code, "coast")
	gitCheckout(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o600))
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "base")
	under := filepath.Join(dir, ".git", "ade", "docs")
	require.NoError(t, os.MkdirAll(under, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(under, "cart.md"), []byte("# x\n"), 0o600))

	tree := filepath.Join(m.code, "coast-cart")
	git(t, dir, "worktree", "add", "-q", tree, "-b", "cart")

	assert.Equal(t, []string{"cart.md"}, designsOf(t, newLauncherOn(t, m), tree))
}

func TestACheckoutWithNowhereToKeepDocumentsHasNoneRatherThanAnError(t *testing.T) {
	m := newFixture(t)
	dir := filepath.Join(m.code, "ade")
	gitCheckout(t, dir)

	assert.Empty(t, designsOf(t, newLauncherOn(t, m), dir))
}

func TestDocumentsAreOnlyListedForACheckoutThisMachineHas(t *testing.T) {
	s := newLauncherOn(t, newFixture(t))

	rec := do(t, s, http.MethodGet, "/api/designs?dir=/etc", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = do(t, s, http.MethodGet, "/api/designs", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOneDocumentIsReadSoItCanBeCarriedToAnotherMachine(t *testing.T) {
	m := newFixture(t)
	dir := filepath.Join(m.code, "coast")
	gitCheckout(t, dir)
	under := filepath.Join(dir, ".git", "ade", "docs")
	require.NoError(t, os.MkdirAll(under, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(under, "cart.md"), []byte("# Cart\n\nCache it.\n"), 0o600))

	s := newLauncherOn(t, m)
	rec := do(t, s, http.MethodGet, "/api/design?file=cart.md&dir="+dir, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got struct{ File, Body string }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "cart.md", got.File)
	assert.Equal(t, "# Cart\n\nCache it.\n", got.Body)

	missing := do(t, s, http.MethodGet, "/api/design?file=gone.md&dir="+dir, "")
	assert.Equal(t, http.StatusNotFound, missing.Code)
}
