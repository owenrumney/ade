package launcher

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/todo"
)

// todoLauncher is a launcher with a home of its own, so one test's captures
// are not another's list.
func todoLauncher(t *testing.T) *Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return newTestLauncher(t)
}

func todoCheckout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "--initial-branch", "main")
	return dir
}

func todoList(t *testing.T, s *Server, query string) []todo.Item {
	t.Helper()
	rec := do(t, s, http.MethodGet, "/api/todos"+query, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out struct {
		Items []todo.Item `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out.Items
}

func TestACaptureIsOneSentenceAndNothingElse(t *testing.T) {
	s := todoLauncher(t)

	rec := do(t, s, http.MethodPost, "/api/todos", `{"text":"ring the bell"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	list := todoList(t, s, "")
	require.Len(t, list, 1)
	assert.Equal(t, "ring the bell", list[0].Text)
	assert.Equal(t, todo.FromYou, list[0].From)
}

func TestACheckoutsTodosAreWrittenBesideItsOwnState(t *testing.T) {
	s := todoLauncher(t)
	dir := todoCheckout(t)

	rec := do(t, s, http.MethodPost, "/api/todos", `{"dir":`+quote(dir)+`,"text":"fix the proxy"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.FileExists(t, filepath.Join(dir, ".git", "ade", todo.File))
	global, err := todo.GlobalPath()
	require.NoError(t, err)
	assert.NoFileExists(t, global, "a repository's todo is not the machine's")
}

func TestTheListIsTheMachinesUntilACheckoutIsNamed(t *testing.T) {
	s := todoLauncher(t)
	dir := todoCheckout(t)
	require.Equal(t, http.StatusOK, do(t, s, http.MethodPost, "/api/todos", `{"text":"mine"}`).Code)
	require.Equal(t, http.StatusOK,
		do(t, s, http.MethodPost, "/api/todos", `{"dir":`+quote(dir)+`,"text":"theirs"}`).Code)

	assert.Len(t, todoList(t, s, ""), 1)

	both := todoList(t, s, "?dir="+dir)
	require.Len(t, both, 2)
	assert.Equal(t, filepath.Base(dir), byText(both, "theirs").Repo)
	assert.Empty(t, byText(both, "mine").Repo, "the machine's carry no checkout")
}

// Keyed by the directory, a worktree had a list of its own: the todos filed
// against coast were invisible from coast-trk-1301.
func TestAWorktreeReadsTheRepositorysTodos(t *testing.T) {
	s := todoLauncher(t)
	dir := todoCheckout(t)
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "test")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "first")
	tree := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-trk-1301")
	gitRun(t, dir, "worktree", "add", "-b", "trk-1301", tree)

	rec := do(t, s, http.MethodPost, "/api/todos", `{"dir":`+quote(dir)+`,"text":"fix the proxy"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	from := todoList(t, s, "?dir="+tree)
	require.Len(t, from, 1, "the worktree reads the repository's list")
	assert.Equal(t, "fix the proxy", from[0].Text)
	assert.Equal(t, filepath.Base(dir), from[0].Repo, "named for the repository, not the worktree")
}

// Shared todos travel between machines, where the same repository sits under
// a different path. The remote names it; the folder does not.
func TestARepositoryIsNamedByItsRemote(t *testing.T) {
	s := todoLauncher(t)
	dir := todoCheckout(t)
	gitRun(t, dir, "remote", "add", "origin", "git@github.com:infracost/coast.git")

	scope, err := s.todoScope(t.Context(), dir)
	require.NoError(t, err)
	assert.Equal(t, "infracost/coast", scope)
}

func TestTheSoonestDueIsReadFirst(t *testing.T) {
	s := todoLauncher(t)
	later := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	soon := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	for _, body := range []string{
		`{"text":"undated"}`,
		`{"text":"later","due":"` + later + `"}`,
		`{"text":"soon","due":"` + soon + `"}`,
	} {
		require.Equal(t, http.StatusOK, do(t, s, http.MethodPost, "/api/todos", body).Code)
	}

	var order []string
	for _, item := range todoList(t, s, "") {
		order = append(order, item.Text)
	}

	assert.Equal(t, []string{"soon", "later", "undated"}, order)
}

func TestADateIsSetAndTakenOffAgainOverTheWire(t *testing.T) {
	s := todoLauncher(t)
	id := added(t, s, `{"text":"set a date"}`).ID
	when := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)

	rec := do(t, s, http.MethodPatch, "/api/todos/"+id, `{"due":"`+when+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotNil(t, todoList(t, s, "")[0].Due)

	rec = do(t, s, http.MethodPatch, "/api/todos/"+id, `{"due":""}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Nil(t, todoList(t, s, "")[0].Due)
}

func TestAnUnreadableDateIsRefusedRatherThanIgnored(t *testing.T) {
	s := todoLauncher(t)

	rec := do(t, s, http.MethodPost, "/api/todos", `{"text":"soon-ish","due":"next tuesday"}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, todoList(t, s, ""), "and nothing is filed without the date you asked for")
}

func TestDoneAndStartedAreRecorded(t *testing.T) {
	s := todoLauncher(t)
	id := added(t, s, `{"text":"start the work"}`).ID

	rec := do(t, s, http.MethodPatch, "/api/todos/"+id, `{"done":true,"started":"s9"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	list := todoList(t, s, "")
	require.Len(t, list, 1)
	assert.True(t, list[0].Done())
	assert.Equal(t, "s9", list[0].Started, "the session it became is one click away")
}

func TestDeletingTheLastOneLeavesNoFile(t *testing.T) {
	s := todoLauncher(t)
	id := added(t, s, `{"text":"never mind"}`).ID

	rec := do(t, s, http.MethodDelete, "/api/todos/"+id, "")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, todoList(t, s, ""))
	path, err := todo.GlobalPath()
	require.NoError(t, err)
	assert.NoFileExists(t, path)
}

func TestAnIdThatIsNotThereSaysSo(t *testing.T) {
	s := todoLauncher(t)

	assert.Equal(t, http.StatusNotFound, do(t, s, http.MethodPatch, "/api/todos/nope", `{"done":true}`).Code)
	assert.Equal(t, http.StatusNotFound, do(t, s, http.MethodDelete, "/api/todos/nope", "").Code)
}

func TestAPathThatIsNoCheckoutIsRefused(t *testing.T) {
	s := todoLauncher(t)
	plain := t.TempDir()

	// The file lands in a checkout's git dir, so a path that is not one would
	// scatter ade's state wherever it was asked to.
	rec := do(t, s, http.MethodPost, "/api/todos", `{"dir":`+quote(plain)+`,"text":"nowhere"}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	entries, err := os.ReadDir(plain)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestTheTodoRoutesNeedTheToken(t *testing.T) {
	s := todoLauncher(t)

	for _, route := range [][2]string{
		{http.MethodGet, "/api/todos"},
		{http.MethodPost, "/api/todos"},
		{http.MethodPatch, "/api/todos/x"},
		{http.MethodDelete, "/api/todos/x"},
	} {
		req := httptest.NewRequest(route[0], route[1], strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		s.routes().ServeHTTP(rec, req)
		assert.Equalf(t, http.StatusForbidden, rec.Code, "%s %s was open", route[0], route[1])
	}
}

func added(t *testing.T, s *Server, body string) todo.Item {
	t.Helper()
	rec := do(t, s, http.MethodPost, "/api/todos", body)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var item todo.Item
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &item))
	return item
}

func byText(items []todo.Item, text string) todo.Item {
	for _, item := range items {
		if item.Text == text {
			return item
		}
	}
	return todo.Item{}
}

func quote(s string) string {
	out, _ := json.Marshal(s)
	return string(out)
}
