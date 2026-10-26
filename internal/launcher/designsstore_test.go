package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/designs"
	"github.com/owenrumney/ade/internal/sessions"
)

func repoNamed(owner, name, path string, checkouts ...sessions.Dir) sessions.Repo {
	return sessions.Repo{
		Owner:     owner,
		Name:      name,
		Checkouts: append([]sessions.Dir{{Path: path, Name: name}}, checkouts...),
	}
}

func TestOwnerAndNameResolveToTheMainCheckoutHere(t *testing.T) {
	repos := []sessions.Repo{
		repoNamed("owenrumney", "ade", "/code/ade"),
		repoNamed("infracost", "cli", "/code/cli"),
	}

	got, ok := checkoutNamed(repos, "infracost/cli")

	assert.True(t, ok)
	assert.Equal(t, "/code/cli", got)
}

func TestTheOwnerDecidesBetweenTwoOfTheSameName(t *testing.T) {
	repos := []sessions.Repo{
		repoNamed("someone", "cli", "/code/theirs"),
		repoNamed("infracost", "cli", "/code/ours"),
	}

	got, ok := checkoutNamed(repos, "infracost/cli")

	assert.True(t, ok)
	assert.Equal(t, "/code/ours", got)
}

func TestARepositoryThisMachineLacksDoesNotResolve(t *testing.T) {
	_, ok := checkoutNamed([]sessions.Repo{repoNamed("o", "ade", "/code/ade")}, "o/nope")
	assert.False(t, ok)
}

func TestADesignSaysWhereItCanBeWorkedOnAndWhatIsMissing(t *testing.T) {
	design := designs.Design{Name: "retry", Meta: designs.Meta{
		Repos: []string{"infracost/cli", "infracost/infra"},
	}}
	repos := []sessions.Repo{repoNamed("infracost", "cli", "/code/cli")}

	row := storeRowFor(design, repos)

	assert.Equal(t, "infracost/cli", row.Repo, "the first repository is home")
	assert.Equal(t, []string{"/code/cli"}, row.Here)
	assert.Equal(t, []string{"infracost/infra"}, row.Missing)
}

func TestADesignWithASessionOnItsBranchIsMarkedLive(t *testing.T) {
	design := designs.Design{Name: "retry", Meta: designs.Meta{Repos: []string{"infracost/cli"}}}
	repos := []sessions.Repo{repoNamed("infracost", "cli", "/code/cli", sessions.Dir{
		Path: "/code/cli-retry", Branch: "feat/retry", Worktree: true,
		Sessions: []sessions.Session{{ID: "s1", Live: true, LastUsed: time.Now()}},
	})}

	row := storeRowFor(design, repos)

	assert.Equal(t, "s1", row.Session)
	assert.Equal(t, "feat/retry", row.Branch)
	assert.True(t, row.Live)
}

func TestAnArchivedSessionDoesNotCountAsWorkInFlight(t *testing.T) {
	design := designs.Design{Name: "retry", Meta: designs.Meta{Repos: []string{"infracost/cli"}}}
	repos := []sessions.Repo{repoNamed("infracost", "cli", "/code/cli", sessions.Dir{
		Path: "/code/cli-retry", Branch: "retry", Worktree: true,
		Sessions: []sessions.Session{{ID: "s1", Archived: true}},
	})}

	assert.Empty(t, storeRowFor(design, repos).Session)
}

func TestStartingNeedsEveryRepositoryTheDesignNames(t *testing.T) {
	design := designs.Design{Name: "retry", Meta: designs.Meta{
		Repos: []string{"infracost/cli", "infracost/infra"},
	}}
	repos := []sessions.Repo{repoNamed("infracost", "cli", "/code/cli")}

	_, _, err := placesFor(design, repos)

	require.ErrorContains(t, err, "infracost/infra",
		"starting in fewer repositories than asked for says nothing")
}

func TestTheFirstRepositoryIsHomeAndTheRestFollow(t *testing.T) {
	design := designs.Design{Name: "retry", Meta: designs.Meta{
		Repos: []string{"infracost/cli", "infracost/infra"},
	}}
	repos := []sessions.Repo{
		repoNamed("infracost", "cli", "/code/cli"),
		repoNamed("infracost", "infra", "/code/infra"),
	}

	home, also, err := placesFor(design, repos)

	require.NoError(t, err)
	assert.Equal(t, "/code/cli", home)
	assert.Equal(t, []string{"/code/infra"}, also)
}

func TestADesignNamingNoRepositoriesCannotBeStarted(t *testing.T) {
	_, _, err := placesFor(designs.Design{Name: "x"}, nil)
	require.ErrorContains(t, err, "names no repositories")
}

// The window's editor sends the whole document back, front matter and
// all. The store parses the matter off again rather than being told it
// twice, and a name it does not have is a write with nowhere to go.
func TestWritingADesignBackKeepsItsFrontMatter(t *testing.T) {
	s := newTestLauncher(t)
	root := t.TempDir()
	store := designs.New(root, "")
	s.designs = store
	_, err := store.Write("one-window", designs.Meta{Repos: []string{"owenrumney/ade"}}, []byte("# One window\n"))
	require.NoError(t, err)

	body := "---\nrepos:\n  - owenrumney/ade\n  - owenrumney/go-lsp\n---\n\n# One window\n\nRewritten.\n"
	rec := do(t, s, http.MethodPut, "/api/designs/write", `{"name":"one-window","body":`+strconv.Quote(body)+`}`)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	back, err := store.Read("one-window")
	require.NoError(t, err)
	assert.Equal(t, []string{"owenrumney/ade", "owenrumney/go-lsp"}, back.Meta.Repos)
	read, err := os.ReadFile(back.File)
	require.NoError(t, err)
	assert.Contains(t, string(read), "Rewritten.")
}

func TestWritingADesignTheStoreDoesNotHaveIsRefused(t *testing.T) {
	s := newTestLauncher(t)
	s.designs = designs.New(t.TempDir(), "")

	rec := do(t, s, http.MethodPut, "/api/designs/write", `{"name":"nope","body":"# nope"}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// A start elsewhere is forwarded. The design travels by git, so the push goes
// first and the far side is told to pull before it reads.
func TestStartingADesignOnAnotherMachineForwardsItAndAsksForASync(t *testing.T) {
	var got struct {
		Name, Agent, Model, Machine string
		Sync                        bool
	}
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/designs/start", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_ = json.NewEncoder(w).Encode(map[string]any{"url": "http://far/?t=x", "path": "/home/ade/code/ade"})
	}))
	defer far.Close()

	s := newTestLauncher(t)
	s.hosts["linux"] = &conn{url: far.URL + "/?t=faraway"}
	bare := bareRepo(t)
	gitIdentity(t)
	s.designs = designs.New(t.TempDir(), bare)
	require.NoError(t, s.designs.Ensure(context.Background()))
	_, err := s.designs.Write("retry", designs.Meta{Repos: []string{"owenrumney/ade"}}, []byte("# Retry\n"))
	require.NoError(t, err)

	rec := do(t, s, http.MethodPost, "/api/designs/start",
		`{"name":"retry","agent":"codex","machine":"linux"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Contains(t, gitOut(t, bare, "ls-tree", "-r", "--name-only", "main"), "retry/design.md",
		"the design is pushed before the far side is asked to pull")
	assert.Equal(t, "retry", got.Name)
	assert.Equal(t, "codex", got.Agent)
	assert.True(t, got.Sync, "the far launcher is told to pull before it reads")
	assert.Empty(t, got.Machine, "the machine is stripped, or it would forward for ever")

	var back map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &back))
	assert.Equal(t, "linux", back["machine"], "the caller is told where the path is")
	assert.Equal(t, "/home/ade/code/ade", back["path"])
}

func TestStartingSomewhereWithNoSharedStoreSaysSoRatherThanStarting(t *testing.T) {
	s := newTestLauncher(t)
	s.hosts["linux"] = &conn{url: "http://unused/?t=faraway"}
	s.designs = designs.New(t.TempDir(), "")

	rec := do(t, s, http.MethodPost, "/api/designs/start", `{"name":"retry","machine":"linux"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "designs_remote")
}

func TestStartingOnAMachineThatIsNotConnectedIsRefused(t *testing.T) {
	s := newTestLauncher(t)
	s.designs = designs.New(t.TempDir(), "")

	rec := do(t, s, http.MethodPost, "/api/designs/start", `{"name":"retry","machine":"linux"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "not connected to linux")
}

// bareRepo is a remote the store really pushes to: faking git tests the fake.
func bareRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "designs.git")
	gitIn(t, "", "init", "--bare", "-b", "main", dir)
	return dir
}

// gitOut is gitIn for a command read rather than run for its effect.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return string(out)
}

// gitIdentity names the committer: the test HOME has no gitconfig, and a
// commit without one fails.
func gitIdentity(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"GIT_AUTHOR_NAME": "ade", "GIT_AUTHOR_EMAIL": "ade@example.com",
		"GIT_COMMITTER_NAME": "ade", "GIT_COMMITTER_EMAIL": "ade@example.com",
	} {
		t.Setenv(key, value)
	}
}
