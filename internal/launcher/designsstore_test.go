package launcher

import (
	"net/http"
	"os"
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
