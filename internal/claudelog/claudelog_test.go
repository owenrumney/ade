package claudelog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	convOld = "11111111-1111-4111-8111-111111111111"
	convNew = "22222222-2222-4222-8222-222222222222"
	convFar = "44444444-4444-4444-8444-444444444444"
)

func TestTheMachinesConversationsAreListedNewestFirstByName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := "/work/some.repo"
	old := transcript(t, dir, convOld,
		`{"type":"user","isMeta":true,"message":{"content":"written by the CLI"}}`,
		`{"type":"user","message":{"content":"<command-name>/clear</command-name>"}}`,
		`{"type":"user","message":{"content":[{"type":"text","text":"why is the entry hidden\nsecond line"}]}}`)
	transcript(t, dir, convNew,
		`{"type":"user","entrypoint":"cli","message":{"content":"first words"}}`,
		`{"type":"ai-title","aiTitle":"Lean terminal host"}`,
		`{"type":"custom-title","customTitle":"my name for it"}`,
		`{"type":"ai-title","aiTitle":"Lean terminal host, later"}`)
	transcript(t, dir, "agent-abc123", `{"type":"user","message":{"content":"a subagent"}}`)
	transcript(t, dir, "33333333-3333-4333-8333-333333333333",
		`{"type":"user","entrypoint":"sdk-cli","message":{"content":"Name this conversation in six words"}}`)
	far := transcript(t, "/else/where", convFar, `{"type":"user","message":{"content":"another folder"}}`)
	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(old, past, past))
	require.NoError(t, os.Chtimes(far, past.Add(-time.Hour), past.Add(-time.Hour)))

	got := Recent("")
	require.Len(t, got, 3, "a subagent's file and a program's headless run are left out")
	assert.Equal(t, []string{convNew, convOld, convFar}, []string{got[0].ID, got[1].ID, got[2].ID})
	assert.Equal(t, "my name for it", got[0].Title, "a name typed by hand beats claude's")
	assert.Equal(t, "why is the entry hidden", got[1].Title, "with no name, how it began")
	assert.Equal(t, dir, got[1].Dir)
	assert.Equal(t, "/else/where", got[2].Dir)

	assert.Equal(t, convFar, Recent("/else/where")[0].ID, "the checkout in front leads, whatever its age")
}

func TestAConversationALivingClaudeHasOpenSaysSo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	transcript(t, "/work/repo", convNew, `{"type":"user","message":{"content":"hello"}}`)
	assert.False(t, Recent("")[0].Running)

	path := filepath.Join(os.Getenv("HOME"), ".claude", "sessions", "1.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	body := fmt.Sprintf(`{"pid":%d,"sessionId":%q}`, os.Getpid(), convNew)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	assert.True(t, Recent("")[0].Running)
	found, ok := Find(convNew)
	require.True(t, ok)
	assert.True(t, found.Running)
}

func TestAConversationIsFoundByItsIdWithTheFolderItResumesFrom(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	transcript(t, "/work/repo", convNew, `{"type":"user","message":{"content":"hello"}}`)

	found, ok := Find(convNew)
	require.True(t, ok)
	assert.Equal(t, "/work/repo", found.Dir)
	assert.Equal(t, "hello", found.Title)
	assert.True(t, Held("/work/repo", convNew))
	assert.False(t, Held("/work/other", convNew), "claude resumes it from the folder it began in")

	_, ok = Find(convOld)
	assert.False(t, ok, "claude holds nothing by that id")
	_, ok = Find("../../etc/passwd")
	assert.False(t, ok)
}

func TestATranscriptFiledUnderAnotherFolderIsNotOffered(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(os.Getenv("HOME"), ".claude", "projects", "-work-repo", convNew+".jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	line := `{"type":"user","cwd":"/somewhere/else","message":{"content":"hello"}}` + "\n"
	require.NoError(t, os.WriteFile(path, []byte(line), 0o600))

	assert.Empty(t, Recent(""), "its own folder does not agree with where it says it ran")
	_, ok := Find(convNew)
	assert.False(t, ok)
}

// transcript writes what claude would have kept for a conversation had in dir.
func transcript(t *testing.T, dir, id string, lines ...string) string {
	t.Helper()
	path := filepath.Join(projectsOf(dir)[0], id+".jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	first := fmt.Sprintf(`{"type":"summary","cwd":%q}`, dir)
	body := strings.Join(append([]string{first}, lines...), "\n") + "\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}
