package launcher

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plantTranscript writes a Claude transcript for a session recorded in cwd.
func plantTranscript(t *testing.T, home, cwd, id string) {
	t.Helper()
	slug := "-" + filepath.Base(filepath.Dir(cwd)) + "-" + filepath.Base(cwd)
	project := filepath.Join(home, ".claude", "projects", slug)
	require.NoError(t, os.MkdirAll(project, 0o750))
	at := time.Now().UTC().Format(time.RFC3339)
	body := fmt.Sprintf(
		`{"type":"user","timestamp":%q,"cwd":%q,"message":{"role":"user","content":"ship the thing"}}`+"\n",
		at, cwd)
	require.NoError(t, os.WriteFile(filepath.Join(project, id+".jsonl"), []byte(body), 0o600))
}

func TestImportRefusesAnIdItCannotUse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	s := newTestLauncher(t)

	for _, tc := range []struct {
		body string
		want int
		why  string
	}{
		{`{"repo":"infracost/coast","id":""}`, http.StatusBadRequest, "nothing named"},
		{`{"repo":"infracost/coast","id":"not-a-uuid"}`, http.StatusBadRequest, "not a session id"},
		{`{"repo":"infracost/coast","id":"../../etc/passwd"}`, http.StatusBadRequest, "not a session id"},
		{
			`{"repo":"infracost/coast","id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"}`,
			http.StatusNotFound, "no transcript with that id",
		},
		{
			`{"repo":"infracost/coast","id":"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb","into":"/etc"}`,
			http.StatusBadRequest, "a directory the scan never offered",
		},
	} {
		rec := do(t, s, http.MethodPost, "/api/sessions/import", tc.body)
		assert.Equalf(t, tc.want, rec.Code, "%s: %s", tc.why, rec.Body.String())
	}
}

func TestImportRefusesAConversationThatIsNotOneToReturnTo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	work := t.TempDir()
	s := newTestLauncher(t)

	id := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	slug := "-" + filepath.Base(filepath.Dir(work)) + "-" + filepath.Base(work)
	project := filepath.Join(home, ".claude", "projects", slug)
	require.NoError(t, os.MkdirAll(project, 0o750))
	at := time.Now().UTC().Format(time.RFC3339)
	require.NoError(t, os.WriteFile(filepath.Join(project, id+".jsonl"), []byte(
		`{"type":"agent-setting","agentSetting":"staff-reviewer"}`+"\n"+fmt.Sprintf(
			`{"type":"user","timestamp":%q,"cwd":%q,"message":{"role":"user","content":"You are @staff-reviewer"}}`+"\n",
			at, work)), 0o600))

	rec := do(t, s, http.MethodPost, "/api/sessions/import",
		fmt.Sprintf(`{"repo":"infracost/coast","id":%q}`, id))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

func TestImportRefusesADirectoryThatIsGone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	gone := t.TempDir()
	s := newTestLauncher(t)

	id := "dddddddd-dddd-dddd-dddd-dddddddddddd"
	plantTranscript(t, home, gone, id)
	require.NoError(t, os.RemoveAll(gone))

	rec := do(t, s, http.MethodPost, "/api/sessions/import",
		fmt.Sprintf(`{"repo":"infracost/coast","id":%q}`, id))
	assert.Equal(t, http.StatusGone, rec.Code, rec.Body.String())
}
