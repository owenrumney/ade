package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func filesIn(t *testing.T, s *Server) map[string]FileGroup {
	t.Helper()
	rec := do(t, s, http.MethodGet, "/api/files", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Groups []FileGroup `json:"groups"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	out := map[string]FileGroup{}
	for _, group := range got.Groups {
		out[group.Root] = group
	}
	return out
}

func names(group FileGroup) []string {
	out := []string{}
	for _, file := range group.Files {
		out = append(out, file.Name)
	}
	return out
}

// writeIn puts a file in one of the tab's roots and returns its path.
func writeIn(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func attachDir(s *Server) string { return chat.AttachDir(s.space().gitDir) }
func outboxDir(s *Server) string { return outboxPath(s.space().gitDir) }

func TestTheFilesTabListsTheThreeRoots(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	writeDoc(t, s, "cart.md", "# Cart\n")
	writeIn(t, s.docsDir, "cart.png", "picture")
	writeIn(t, attachDir(s), "0001-error.log", "boom")
	writeIn(t, outboxDir(s), "shot.png", "picture")

	groups := filesIn(t, s)
	assert.Equal(t, []string{"cart.md", "cart.png"}, names(groups[rootDocs]),
		"sorted by name, so a document and its picture sit together")
	assert.Equal(t, []string{"0001-error.log"}, names(groups[rootAttachments]))
	assert.Equal(t, []string{"shot.png"}, names(groups[rootOutbox]))

	assert.Equal(t, "markdown", groups[rootDocs].Files[0].Kind)
	assert.Equal(t, "image", groups[rootDocs].Files[1].Kind)
	assert.Equal(t, "text", groups[rootAttachments].Files[0].Kind)
	assert.Contains(t, groups[rootDocs].Note, "every session in this repository")
}

// Nothing of the session's own state is reachable: the allow-list is three
// directories, so deleting chat.db is not a mistake anyone can make here.
func TestTheSessionsOwnStateIsNotListed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	writeIn(t, filepath.Dir(s.docsDir), "chat.db", "the conversation")

	for _, group := range filesIn(t, s) {
		assert.NotContains(t, names(group), "chat.db")
	}
	rec := do(t, s, http.MethodGet, "/api/files/raw?root=docs&name=../chat.db", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestANestedDocumentIsListedAndDeletable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	writeIn(t, s.docsDir, "a/b/deep.md", "# Deep\n")

	assert.Equal(t, []string{"a/b/deep.md"}, names(filesIn(t, s)[rootDocs]))

	rec := do(t, s, http.MethodDelete, "/api/files?root=docs&name=a/b/deep.md", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NoFileExists(t, filepath.Join(s.docsDir, "a/b/deep.md"))
}

// An agent writes the outbox, sandboxed ones included. A link planted there
// would otherwise read whatever it points at through this server.
func TestASymlinkIsNeitherListedNorServedNorDeleted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	secret := filepath.Join(t.TempDir(), "id_ed25519")
	require.NoError(t, os.WriteFile(secret, []byte("PRIVATE KEY"), 0o600))
	require.NoError(t, os.MkdirAll(outboxDir(s), 0o750))
	require.NoError(t, os.Symlink(secret, filepath.Join(outboxDir(s), "x.txt")))

	assert.Empty(t, names(filesIn(t, s)[rootOutbox]), "a link is not a file the tab shows")

	rec := do(t, s, http.MethodGet, "/api/files/raw?root=outbox&name=x.txt", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.NotContains(t, rec.Body.String(), "PRIVATE KEY")

	rec = do(t, s, http.MethodDelete, "/api/files?root=outbox&name=x.txt", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "not a file")
	require.FileExists(t, secret, "and the thing it pointed at is untouched")
}

func TestADirectoryInTheOutboxIsNotListed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	require.NoError(t, os.MkdirAll(filepath.Join(outboxDir(s), "a-folder"), 0o750))

	assert.Empty(t, names(filesIn(t, s)[rootOutbox]))
}

// Served with its own type and inline, either of these would run as a page on
// ade's origin, with the session token.
func TestScriptableFilesAreNeverServedInline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	writeIn(t, outboxDir(s), "page.html", "<script>alert(1)</script>")
	writeIn(t, outboxDir(s), "art.svg", "<svg onload=\"alert(1)\"></svg>")

	for _, name := range []string{"page.html", "art.svg"} {
		rec := do(t, s, http.MethodGet, "/api/files/raw?root=outbox&name="+name, "")
		require.Equal(t, http.StatusOK, rec.Code, name)
		assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"), name)
		assert.Contains(t, rec.Header().Get("Content-Disposition"), "attachment", name)
		assert.Equal(t, "default-src 'none'", rec.Header().Get("Content-Security-Policy"), name)
		assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"), name)
	}
}

// A link as a directory passes a Rel check and is never Lstat-ed: only the
// last part of the path was ever checked.
func TestALinkedDirectoryInADocsRootIsNotAWayOut(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "id_ed25519"), []byte("key"), 0o600))
	require.NoError(t, os.MkdirAll(s.docsDir, 0o750))
	require.NoError(t, os.Symlink(outside, filepath.Join(s.docsDir, "evil")))

	rec := do(t, s, http.MethodGet, "/api/files/raw?root=docs&name=evil/id_ed25519", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = do(t, s, http.MethodDelete, "/api/files?root=docs&name=evil/id_ed25519", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.FileExists(t, filepath.Join(outside, "id_ed25519"), "nothing outside the root may be removed")
	assert.NotContains(t, rec.Body.String(), `"deleted":["evil/id_ed25519"]`)
}

func TestAnAttachmentLinkIsNotFollowed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("hunter2"), 0o600))
	require.NoError(t, os.MkdirAll(attachDir(s), 0o750))
	require.NoError(t, os.Symlink(secret, filepath.Join(attachDir(s), "0001-note.txt")))

	rec := do(t, s, http.MethodGet, "/attachments/0001-note.txt", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.NotContains(t, rec.Body.String(), "hunter2")
}

func TestWhichDocumentsUseAPicture(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	writeDoc(t, s, "cart.md", "# Cart\n\n![the rail](cart.png)\n")
	writeIn(t, s.docsDir, "deep/nested.md", "see ../cart.png for the shape\n")
	writeDoc(t, s, "other.md", "# Other\n")
	writeIn(t, s.docsDir, "cart.png", "picture")
	writeIn(t, s.docsDir, "lonely.png", "picture")

	uses := func(name string) []string {
		rec := do(t, s, http.MethodGet, "/api/files/uses?root=docs&name="+name, "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var got struct {
			Documents []string `json:"documents"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		return got.Documents
	}

	assert.Equal(t, []string{"cart.md", "deep/nested.md"}, uses("cart.png"),
		"a document reaches a picture by any relative path, so the base name is what is looked for")
	assert.Empty(t, uses("lonely.png"), "nothing references it, and the answer is that rather than silence")
	assert.Empty(t, uses("cart.md"), "a document naming itself is not a reference to itself")
}

func TestThePreviewKindsCoverWhatTheTabRenders(t *testing.T) {
	for name, want := range map[string]string{
		"notes.txt": "text", "design.md": "markdown", "shot.png": "image",
		"photo.jpg": "image", "photo.jpeg": "image", "conf.json": "text",
		"conf.yaml": "text", "conf.yml": "text",
		".env": "text", ".env.local": "text", "prod.env": "text",
		"run.bin": "other",
	} {
		assert.Equal(t, want, fileKind(name), name)
	}
}

// A yaml or dotenv file has no type on the attachment list, and served as a
// download the preview pane gets nothing to show.
func TestTextFilesAreServedAsTextRatherThanDownloaded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	for _, name := range []string{"conf.yaml", "conf.yml", ".env", ".env.local"} {
		writeIn(t, outboxDir(s), name, "port: 7777\n")
		rec := do(t, s, http.MethodGet, "/api/files/raw?root=outbox&name="+name, "")
		require.Equal(t, http.StatusOK, rec.Code, name)
		assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"), name)
		assert.Empty(t, rec.Header().Get("Content-Disposition"), name)
	}
}

func TestAFileOverThePreviewCapIsRefusedRatherThanServed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	require.NoError(t, os.MkdirAll(attachDir(s), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(attachDir(s), "huge.log"), make([]byte, previewCap+1), 0o600))

	rec := do(t, s, http.MethodGet, "/api/files/raw?root=attachments&name=huge.log", "")
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestARootOutsideTheEnumIsRefused(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)

	rec := do(t, s, http.MethodDelete, "/api/files?root=/etc&name=passwd", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = do(t, s, http.MethodGet, "/api/files/raw?root=/etc&name=passwd", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// Multiselect is the point, so one bad name must not lose the good ones.
func TestAMultiDeleteReportsEachNameSeparately(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	writeIn(t, attachDir(s), "one.txt", "a")
	writeIn(t, attachDir(s), "two.txt", "b")

	rec := do(t, s, http.MethodDelete, "/api/files?root=attachments&name=one.txt&name=gone.txt&name=two.txt", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got struct {
		Deleted []string `json:"deleted"`
		Failed  []struct {
			Name string `json:"name"`
			Why  string `json:"why"`
		} `json:"failed"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, []string{"one.txt", "two.txt"}, got.Deleted)
	require.Len(t, got.Failed, 1)
	assert.Equal(t, "gone.txt", got.Failed[0].Name)
	assert.NoFileExists(t, filepath.Join(attachDir(s), "one.txt"))
}

func TestDeletingTheOpenDocumentClosesIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	writeDoc(t, s, "cart.md", "# Cart\n")
	s.setDoc("cart.md")

	rec := do(t, s, http.MethodDelete, "/api/files?root=docs&name=cart.md", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, s.currentDoc(), "a name that is gone is not something to hold open")
}

// A build reads its brief as it runs. The Design tab refuses this already, and
// the same helper has to refuse it through the Files tab.
func TestDeletingTheDocumentTheBuildAgentIsWritingIsRefused(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	writeDoc(t, s, "cart.md", "# Cart\n")
	writeIn(t, s.docsDir, "other.md", "# Other\n")
	s.setDoc("cart.md")
	s.agent.running = true
	s.agent.state.Kind = "build"

	rec := do(t, s, http.MethodDelete, "/api/files?root=docs&name=cart.md", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "building this document")
	assert.FileExists(t, filepath.Join(s.docsDir, "cart.md"))

	rec = do(t, s, http.MethodDelete, "/api/files?root=docs&name=other.md", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NoFileExists(t, filepath.Join(s.docsDir, "other.md"),
		"only the brief being built is held, not every document")
}

// The outbox empties onto the turn's final reply, so a delete mid-turn races
// it and can drop a picture the reply refers to.
func TestTheOutboxIsReadOnlyWhileTheAgentRuns(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	writeIn(t, outboxDir(s), "shot.png", "picture")
	s.agent.running = true

	groups := filesIn(t, s)
	assert.True(t, groups[rootOutbox].ReadOnly)
	assert.Contains(t, groups[rootOutbox].Note, "the agent is running")

	rec := do(t, s, http.MethodDelete, "/api/files?root=outbox&name=shot.png", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "wait for the turn to end")
	assert.FileExists(t, filepath.Join(outboxDir(s), "shot.png"))
}
