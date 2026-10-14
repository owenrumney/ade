package launcher

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/owenrumney/ade/internal/wiki"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWikiSearchReturnsCachedHits(t *testing.T) {
	remote := wikiRemote(t)
	store := wiki.New(t.TempDir(), remote)
	require.NoError(t, store.Sync(context.Background()))
	s := newTestLauncher(t)
	s.wiki = store

	rec := do(t, s, "GET", "/api/wiki/search?q=needle", "")
	require.Equal(t, 200, rec.Code)
	assert.Contains(t, rec.Body.String(), `"path":"general/page.md"`)
}

func wikiRemote(t *testing.T) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "wiki.git")
	gitRun(t, "", "init", "--bare", "--initial-branch", "main", bare)
	work := t.TempDir()
	gitRun(t, work, "init", "--initial-branch", "main")
	gitRun(t, work, "config", "user.email", "test@example.com")
	gitRun(t, work, "config", "user.name", "Test")
	require.NoError(t, os.MkdirAll(filepath.Join(work, "general"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(work, "general", "page.md"), []byte("needle\n"), 0o600))
	gitRun(t, work, "add", ".")
	gitRun(t, work, "commit", "-m", "page")
	gitRun(t, work, "remote", "add", "origin", bare)
	gitRun(t, work, "push", "origin", "main")
	return bare
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "%s", out)
}

func TestWikiPageRendersWithoutExecutableHTML(t *testing.T) {
	remote := wikiRemote(t)
	store := wiki.New(t.TempDir(), remote)
	require.NoError(t, store.Sync(context.Background()))
	require.NoError(t, os.WriteFile(filepath.Join(store.Root(), "general", "page.md"),
		[]byte("# Heading\n\n<script>alert(1)</script>\n"), 0o600))
	s := newTestLauncher(t)
	s.wiki = store

	rec := do(t, s, "GET", "/api/wiki/page/general/page.md", "")
	require.Equal(t, 200, rec.Code)
	var body struct {
		HTML     string `json:"html"`
		Markdown string `json:"markdown"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Contains(t, body.HTML, "<h1")
	assert.Contains(t, body.HTML, "raw HTML omitted")
	assert.NotContains(t, body.HTML, "alert(1)", "the render is what a browser runs")
}

// The source travels beside the render, so an editor can open the Markdown
// rather than a picture of it. Raw HTML in it stays raw: it is text here.
func TestWikiPageCarriesTheMarkdownItRendered(t *testing.T) {
	remote := wikiRemote(t)
	store := wiki.New(t.TempDir(), remote)
	require.NoError(t, store.Sync(context.Background()))
	source := "# Heading\n\n<script>alert(1)</script>\n"
	require.NoError(t, os.WriteFile(filepath.Join(store.Root(), "general", "page.md"),
		[]byte(source), 0o600))
	s := newTestLauncher(t)
	s.wiki = store

	rec := do(t, s, "GET", "/api/wiki/page/general/page.md", "")
	require.Equal(t, 200, rec.Code)
	var body struct {
		Markdown string `json:"markdown"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, source, body.Markdown)
}

func TestWikiPageRefusesAPathOutsideTheClone(t *testing.T) {
	store := wiki.New(t.TempDir(), wikiRemote(t))
	require.NoError(t, store.Sync(context.Background()))
	s := newTestLauncher(t)
	s.wiki = store

	// The mux cleans a traversal into a redirect; either way no page is served.
	assert.NotEqual(t, 200, do(t, s, "GET", "/api/wiki/page/general/../../../etc/passwd", "").Code)
	assert.Equal(t, 404, do(t, s, "GET", "/api/wiki/page/general/page.txt", "").Code)
	assert.Equal(t, 404, do(t, s, "GET", "/api/wiki/page/.git/config", "").Code)
}
