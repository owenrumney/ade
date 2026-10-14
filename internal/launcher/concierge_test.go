package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/concierge"
	"github.com/owenrumney/ade/internal/todo"
)

// deskLauncher is a launcher with a home of its own whose sessions are faked:
// the desk starts a real agent, and a test must not.
func deskLauncher(t *testing.T) (*Server, *[]string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)
	var opened []string
	s.open = func(_ context.Context, dir, agent, model, _ string) (string, error) {
		opened = append(opened, dir+" "+agent+" "+model)
		return "http://127.0.0.1:1/?t=x", nil
	}
	return s, &opened
}

func TestTheDeskOpensOnTheCheapModelInItsOwnCheckout(t *testing.T) {
	s, opened := deskLauncher(t)

	rec := do(t, s, http.MethodPost, "/api/concierge", "{}")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out struct{ URL, Path string }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	home, err := concierge.Home()
	require.NoError(t, err)
	assert.Equal(t, home, out.Path)
	assert.Equal(t, []string{home + " claude haiku"}, *opened)
	assert.FileExists(t, filepath.Join(out.Path, "CLAUDE.md"))
}

func TestOpeningTheDeskTwiceResumesRatherThanForks(t *testing.T) {
	s, opened := deskLauncher(t)

	require.Equal(t, http.StatusOK, do(t, s, http.MethodPost, "/api/concierge", "{}").Code)
	require.Equal(t, http.StatusOK, do(t, s, http.MethodPost, "/api/concierge", "{}").Code)

	// No session id either time: an empty one reopens whatever that checkout
	// was last in, which is what makes the checkout the desk's identity.
	assert.Len(t, *opened, 2)
	assert.Equal(t, (*opened)[0], (*opened)[1])
}

func TestTheDeskRoutesNeedTheToken(t *testing.T) {
	s, _ := deskLauncher(t)

	for _, path := range []string{"/api/concierge", concierge.MCPPath} {
		rec := unauthenticated(t, s, http.MethodPost, path)
		assert.Equalf(t, http.StatusForbidden, rec.Code, "%s was open", path)
	}
}

// call sends one JSON-RPC request to the desk's MCP server.
func call(t *testing.T, s *Server, method, body string) map[string]any {
	t.Helper()
	rec := do(t, s, http.MethodPost, concierge.MCPPath,
		`{"jsonrpc":"2.0","id":1,"method":"`+method+`"`+body+`}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out
}

// said is the text a tool call answered with.
func said(t *testing.T, answer map[string]any) string {
	t.Helper()
	result, ok := answer["result"].(map[string]any)
	require.True(t, ok, "%v", answer)
	content, ok := result["content"].([]any)
	require.True(t, ok, "%v", result)
	first, ok := content[0].(map[string]any)
	require.True(t, ok)
	text, _ := first["text"].(string)
	return text
}

func TestTheDeskIsOfferedOnlyWhatItMayDo(t *testing.T) {
	s, _ := deskLauncher(t)

	answer := call(t, s, "tools/list", "")

	result := answer["result"].(map[string]any)
	var names []string
	for _, tool := range result["tools"].([]any) {
		names = append(names, tool.(map[string]any)["name"].(string))
	}
	assert.ElementsMatch(t, []string{
		"todo_list", "todo_add", "todo_done", "todo_drop",
		"sessions", "waiting", "say", "start",
	}, names)
}

func TestTheDeskFilesATodoUnderItsOwnName(t *testing.T) {
	s, _ := deskLauncher(t)

	answer := call(t, s, "tools/call",
		`,"params":{"name":"todo_add","arguments":{"text":"ring the bell"}}`)

	assert.Contains(t, said(t, answer), "ring the bell")
	list := todoList(t, s, "")
	require.Len(t, list, 1)
	assert.Equal(t, todo.FromConcierge, list[0].From, "a row says who filed it")
}

func TestTheDeskReadsTheListItFiledInto(t *testing.T) {
	s, _ := deskLauncher(t)
	require.Equal(t, http.StatusOK, do(t, s, http.MethodPost, "/api/todos", `{"text":"mine"}`).Code)

	assert.Contains(t, said(t, call(t, s, "tools/call",
		`,"params":{"name":"todo_list","arguments":{}}`)), "mine")
}

func TestTheDeskClosesAndDropsOne(t *testing.T) {
	s, _ := deskLauncher(t)
	id := added(t, s, `{"text":"close me"}`).ID

	done := call(t, s, "tools/call",
		`,"params":{"name":"todo_done","arguments":{"id":"`+id+`"}}`)
	assert.Contains(t, said(t, done), "doneAt")

	call(t, s, "tools/call", `,"params":{"name":"todo_drop","arguments":{"id":"`+id+`"}}`)
	assert.Empty(t, todoList(t, s, ""))
}

func TestAToolThatFailsSaysSoRatherThanLying(t *testing.T) {
	s, _ := deskLauncher(t)

	answer := call(t, s, "tools/call",
		`,"params":{"name":"todo_done","arguments":{"id":"nothing"}}`)

	result := answer["result"].(map[string]any)
	assert.Equal(t, true, result["isError"])
	assert.Contains(t, said(t, answer), "no such todo")
}

func TestAnUnknownToolIsRefused(t *testing.T) {
	s, _ := deskLauncher(t)

	answer := call(t, s, "tools/call", `,"params":{"name":"rm_rf","arguments":{}}`)

	rpcError, ok := answer["error"].(map[string]any)
	require.True(t, ok, "%v", answer)
	assert.Contains(t, rpcError["message"], "rm_rf")
}

func TestTheDeskCanSeeWhatIsRunningAndWhatIsWaiting(t *testing.T) {
	s, _ := deskLauncher(t)

	for _, name := range []string{"sessions", "waiting"} {
		answer := call(t, s, "tools/call", `,"params":{"name":"`+name+`","arguments":{}}`)
		result := answer["result"].(map[string]any)
		assert.Equalf(t, false, result["isError"], "%s failed: %s", name, said(t, answer))
	}
}

func unauthenticated(t *testing.T, s *Server, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)
	return rec
}

func TestANewDayGetsAFreshDeskAndABrief(t *testing.T) {
	s, _ := deskLauncher(t)
	var sent []string
	old := sendFirst
	sendFirst = func(_ context.Context, _, body string) error {
		sent = append(sent, body)
		return nil
	}
	t.Cleanup(func() { sendFirst = old })

	first := do(t, s, http.MethodPost, "/api/concierge", "{}")
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	assert.Contains(t, first.Body.String(), `"fresh":true`)
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0], "what is waiting on me")

	// The same day again continues the conversation rather than starting one,
	// and asks for no second brief.
	again := do(t, s, http.MethodPost, "/api/concierge", "{}")
	require.Equal(t, http.StatusOK, again.Code)
	assert.Contains(t, again.Body.String(), `"fresh":false`)
	assert.Len(t, sent, 1)
}

func TestYesterdaysDeskIsNotTodays(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now()

	assert.True(t, deskIsNewToday(now), "a machine that has never opened it")
	markDeskOpened(now)
	assert.False(t, deskIsNewToday(now))
	assert.True(t, deskIsNewToday(now.Add(24*time.Hour)))
}
