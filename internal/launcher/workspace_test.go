package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/provision"
	"github.com/owenrumney/ade/internal/sessions"
)

func stubProvision(t *testing.T, branchErr error) *[]string {
	t.Helper()
	clone, branch := provisionClone, provisionBranch
	t.Cleanup(func() { provisionClone, provisionBranch = clone, branch })
	var branched []string
	provisionClone = func(context.Context, string, string, provision.Runner) (string, error) {
		return "/srv/ade/0123456789abcdef/ade", nil
	}
	provisionBranch = func(_ context.Context, dir, name string, _ provision.Runner) error {
		branched = append(branched, dir+"@"+name)
		return branchErr
	}
	return &branched
}

func TestProvisionBranchesTheCloneFromTheDocument(t *testing.T) {
	branched := stubProvision(t, nil)
	s := newTestLauncher(t)

	rec := do(t, s, http.MethodPost, "/api/provision", `{"repository":"owenrumney/ade","token":"t","branch":"FIX-1234 Cancel.md"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"/srv/ade/0123456789abcdef/ade@fix-1234-cancel"}, *branched)
	assert.JSONEq(t, `{"path":"/srv/ade/0123456789abcdef/ade","branch":"fix-1234-cancel"}`, rec.Body.String())
}

func TestProvisionWithoutADocumentStaysOnTheDefaultBranch(t *testing.T) {
	branched := stubProvision(t, nil)
	s := newTestLauncher(t)

	rec := do(t, s, http.MethodPost, "/api/provision", `{"repository":"owenrumney/ade","token":"t"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Empty(t, *branched)
	assert.JSONEq(t, `{"path":"/srv/ade/0123456789abcdef/ade"}`, rec.Body.String())
}

func TestABranchThatFailsStillHandsBackTheClone(t *testing.T) {
	stubProvision(t, errors.New("exit status 128"))
	s := newTestLauncher(t)

	rec := do(t, s, http.MethodPost, "/api/provision", `{"repository":"owenrumney/ade","token":"t","branch":"fix-1234.md"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"path":"/srv/ade/0123456789abcdef/ade"}`, rec.Body.String())
}

func TestABuildOnAMachineAsksForABranchAndPassesItBack(t *testing.T) {
	token := readGitHubToken
	t.Cleanup(func() { readGitHubToken = token })
	readGitHubToken = func(context.Context) (string, error) { return "t", nil }

	var asked struct{ Repository, Branch string }
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/provision":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&asked))
			_ = json.NewEncoder(w).Encode(map[string]string{"path": "/srv/ade/0123456789abcdef/ade", "branch": "fix-1234"})
		case "/api/open":
			_ = json.NewEncoder(w).Encode(map[string]string{"url": farURL(r)})
		default:
			http.NotFound(w, r)
		}
	}))
	defer far.Close()

	s := newTestLauncher(t)
	s.hosts["linux"] = &conn{url: far.URL + "/?t=faraway"}
	rec := do(t, s, http.MethodPost, "/api/open",
		`{"machine":"linux","dir":"owenrumney/ade","fresh":true,"brief":{"name":"fix-1234.md","body":"# Fix","build":true}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "owenrumney/ade", asked.Repository)
	assert.Equal(t, "fix-1234.md", asked.Branch)

	var got struct{ Path, Branch string }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "/srv/ade/0123456789abcdef/ade", got.Path)
	assert.Equal(t, "fix-1234", got.Branch)
}

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
		`{"dir":"`+home+`","fresh":true,"name":"cancel-button","also":["`+other+`"],"role":"oss-dev"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{other}, work.Also)
	assert.Equal(t, "oss-dev", work.Role, "a role is only chosen as the session starts")
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

func TestASessionOnAnotherMachineIsRefusedASecondRepository(t *testing.T) {
	m := newFixture(t)
	home := filepath.Join(m.home, "elsewhere", "home")
	other := filepath.Join(m.home, "elsewhere", "other")
	for _, dir := range []string{home, other} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o700))
	}

	s := newLauncherOn(t, m)
	rec := do(t, s, http.MethodPost, "/api/open",
		`{"machine":"pc","dir":"`+home+`","fresh":true,"name":"cancel-button","also":["`+other+`"]}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "one repository")
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

// A named session is how every branching row starts, and the brief was
// dropped on that path: a document could be picked here and never arrive.
func TestANamedSessionIsHandedItsDocumentBeforeTheFirstMessage(t *testing.T) {
	m := newFixture(t)
	repo := filepath.Join(m.home, "elsewhere", "coast")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o700))

	s := newLauncherOn(t, m)
	s.create = func(context.Context, string, sessions.Work) (string, string, error) {
		return "http://127.0.0.1:27431/?t=tok", repo + "-cart", nil
	}
	was, wasFirst := askSession, sendFirst
	var calls []string
	var pushed string
	askSession = func(_ context.Context, _, path, body string) error {
		calls = append(calls, path)
		pushed = body
		return nil
	}
	sendFirst = func(context.Context, string, string) error {
		calls = append(calls, "/api/chat")
		return nil
	}
	t.Cleanup(func() { askSession, sendFirst = was, wasFirst })

	rec := do(t, s, http.MethodPost, "/api/open",
		`{"dir":"`+repo+`","fresh":true,"name":"cart.md","prompt":"build it",`+
			`"brief":{"name":"cart.md","body":"# Cart\n\nCache it.\n"}}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"/api/design/push", "/api/chat"}, calls,
		"the document is what the session is for, so it lands before the message")
	assert.Contains(t, pushed, "Cache it.")
}

func TestANamedSessionWhoseDocumentNeverLandedIsNotToldToStart(t *testing.T) {
	m := newFixture(t)
	repo := filepath.Join(m.home, "elsewhere", "coast")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o700))

	s := newLauncherOn(t, m)
	s.create = func(context.Context, string, sessions.Work) (string, string, error) {
		return "http://127.0.0.1:27431/?t=tok", repo + "-cart", nil
	}
	was, wasFirst := askSession, sendFirst
	askSession = func(context.Context, string, string, string) error {
		return errors.New("the session refused it")
	}
	sendFirst = func(context.Context, string, string) error {
		t.Fatal("a message answered without its document is answered against a skeleton")
		return nil
	}
	t.Cleanup(func() { askSession, sendFirst = was, wasFirst })

	rec := do(t, s, http.MethodPost, "/api/open",
		`{"dir":"`+repo+`","fresh":true,"name":"cart.md","prompt":"build it",`+
			`"brief":{"name":"cart.md","body":"# Cart\n"}}`)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), repo+"-cart", "the session is open either way, so the failure says where")
}
