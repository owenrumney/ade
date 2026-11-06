package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/sessions"
)

func TestANamedSessionIsCreatedAsAWorkspace(t *testing.T) {
	m := newFixture(t)
	repo := filepath.Join(m.home, "elsewhere", "thing")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o700))

	s := newLauncherOn(t, m)
	s.open = func(context.Context, string, string, string, string) (string, error) {
		t.Fatal("a named session must not start in the checkout it was asked from")
		return "", nil
	}
	var in, named string
	s.create = func(_ context.Context, dir string, work sessions.Work) (string, string, error) {
		in, named = dir, work.Name
		return "http://127.0.0.1:27431/?t=tok", repo + "-cancel-button", nil
	}

	rec := do(t, s, http.MethodPost, "/api/open", `{"dir":"`+repo+`","fresh":true,"name":" cancel-button "}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, repo, in)
	assert.Equal(t, "cancel-button", named)

	var got struct{ URL, Path string }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, repo+"-cancel-button", got.Path)

	// The name does not widen where a process may be started.
	refused := do(t, s, http.MethodPost, "/api/open", `{"dir":"/etc","name":"cancel-button"}`)
	assert.Equal(t, http.StatusBadRequest, refused.Code)
}

func TestATicketIsCreatedAsAWorkspaceAndCheckedBeforeAnythingStarts(t *testing.T) {
	m := newFixture(t)
	repo := filepath.Join(m.home, "elsewhere", "thing")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o700))

	s := newLauncherOn(t, m)
	var work sessions.Work
	s.create = func(_ context.Context, _ string, w sessions.Work) (string, string, error) {
		work = w
		return "http://127.0.0.1:27431/?t=tok", repo + "-fix-623", nil
	}

	rec := do(t, s, http.MethodPost, "/api/open", `{"dir":"`+repo+`","fresh":true,"ticket":" FIX-623 "}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "FIX-623", work.Ticket)
	assert.Empty(t, work.Name, "the flag names the document, so there is no positional name")

	// A bad reference is a bad request, not an empty Design tab in a worktree.
	work = sessions.Work{}
	refused := do(t, s, http.MethodPost, "/api/open", `{"dir":"`+repo+`","fresh":true,"ticket":"not a ticket"}`)
	assert.Equal(t, http.StatusBadRequest, refused.Code)
	assert.Empty(t, work.Ticket)
}

func TestASessionAcrossRepositoriesPassesTheOthersToCreate(t *testing.T) {
	m := newFixture(t)
	home := filepath.Join(m.home, "elsewhere", "home")
	other := filepath.Join(m.home, "elsewhere", "other")
	for _, dir := range []string{home, other} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o700))
	}

	s := newLauncherOn(t, m)
	var work sessions.Work
	s.create = func(_ context.Context, _ string, w sessions.Work) (string, string, error) {
		work = w
		return "http://127.0.0.1:27431/?t=tok", home + "-cancel-button", nil
	}

	rec := do(t, s, http.MethodPost, "/api/open",
		`{"dir":"`+home+`","fresh":true,"name":"cancel-button","also":["`+other+`"]}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{other}, work.Also)
}

func TestASecondRepositoryThatIsNotACheckoutIsRefused(t *testing.T) {
	m := newFixture(t)
	home := filepath.Join(m.home, "elsewhere", "home")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".git"), 0o700))

	s := newLauncherOn(t, m)
	s.create = func(context.Context, string, sessions.Work) (string, string, error) {
		t.Fatal("nothing is started when one of the repositories cannot be found")
		return "", "", nil
	}

	rec := do(t, s, http.MethodPost, "/api/open",
		`{"dir":"`+home+`","fresh":true,"name":"cancel-button","also":["/etc"]}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "not a checkout")
}

func TestMoreThanOneRepositoryNeedsAName(t *testing.T) {
	m := newFixture(t)
	home := filepath.Join(m.home, "elsewhere", "home")
	other := filepath.Join(m.home, "elsewhere", "other")
	for _, dir := range []string{home, other} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o700))
	}

	s := newLauncherOn(t, m)
	s.open = func(context.Context, string, string, string, string) (string, error) {
		t.Fatal("a session with no branch has nothing to check the others out on")
		return "", nil
	}

	rec := do(t, s, http.MethodPost, "/api/open", `{"dir":"`+home+`","fresh":true,"also":["`+other+`"]}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "needs a name")
}
