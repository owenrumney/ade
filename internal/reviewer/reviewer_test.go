package reviewer

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/agentbus"
)

// fake is a resolver answering for git and GitHub, and counting the calls so
// the cache can be proved.
type fake struct {
	*Resolver
	commits    map[string]string // email -> sha
	logins     map[string]string // sha -> login
	assignable []string
	added      []string
	lookups    int
	addErr     error
}

func newFake() *fake {
	f := &fake{
		commits:    map[string]string{"liam@infracost.io": "sha-liamg"},
		logins:     map[string]string{"sha-liamg": "liamg"},
		assignable: []string{"owenrumney", "liamg"},
	}
	f.Resolver = &Resolver{known: map[string]string{}}
	f.commitBy = func(_ context.Context, _, email string) (string, error) {
		f.lookups++
		return f.commits[email], nil
	}
	f.loginOf = func(_ context.Context, _, _, sha string) (string, error) {
		return f.logins[sha], nil
	}
	f.accepts = func(_ context.Context, _, _ string) ([]string, error) {
		return f.assignable, nil
	}
	f.add = func(_ context.Context, _, _, login string) error {
		if f.addErr != nil {
			return f.addErr
		}
		f.added = append(f.added, login)
		return nil
	}
	return f
}

func user(email string) agentbus.Target { return agentbus.Target{User: email} }

func TestAskingOnePersonPutsThemOnThePullRequest(t *testing.T) {
	f := newFake()

	got := f.Set(context.Background(), ".", "infracost/cli", "https://github.com/infracost/cli/pull/1", user("liam@infracost.io"))

	assert.True(t, got.Set)
	assert.Equal(t, "liamg", got.Login)
	assert.Empty(t, got.Why)
	assert.Equal(t, []string{"liamg"}, f.added)
}

// The two Liams are why a handle is never guessed from the address.
func TestTwoPeopleWithOneFirstNameGetDifferentHandles(t *testing.T) {
	f := newFake()
	f.commits["liam.cervante@infracost.io"] = "sha-cervante"
	f.logins["sha-cervante"] = "liamcervante"
	f.assignable = append(f.assignable, "liamcervante")

	for email, want := range map[string]string{
		"liam@infracost.io":          "liamg",
		"liam.cervante@infracost.io": "liamcervante",
	} {
		got := f.Set(context.Background(), ".", "infracost/cli", "pr", user(email))
		assert.Equalf(t, want, got.Login, "%s resolved to the wrong account", email)
		assert.True(t, got.Set)
	}
}

func TestATargetThatIsNotOnePersonSetsNobodyAndSaysSo(t *testing.T) {
	f := newFake()
	for _, to := range []agentbus.Target{
		{Anyone: true}, {All: true}, {Group: "backend"},
	} {
		got := f.Set(context.Background(), ".", "infracost/cli", "pr", to)
		assert.False(t, got.Set)
		assert.Contains(t, got.Why, "not one person")
		assert.Empty(t, f.added, "nothing may be put on the pull request")
	}
}

// An instance is one person at one machine, and the person is the reviewer.
func TestAnInstanceNarrowsToThePersonBehindIt(t *testing.T) {
	f := newFake()

	got := f.Set(context.Background(), ".", "infracost/cli", "pr",
		agentbus.Target{Instance: "liam@infracost.io/laptop"})

	assert.True(t, got.Set)
	assert.Equal(t, "liamg", got.Login)
}

func TestAnAddressWithNoCommitHereIsNotGuessedAt(t *testing.T) {
	f := newFake()

	got := f.Set(context.Background(), ".", "infracost/cli", "pr", user("newstarter@infracost.io"))

	assert.False(t, got.Set)
	assert.Contains(t, got.Why, "no GitHub handle for newstarter@infracost.io")
	assert.Empty(t, f.added)
}

// GitHub drops an assignee it does not recognise without saying so, so the
// list is checked first.
func TestSomeoneTheRepositoryWillNotTakeIsNotAsked(t *testing.T) {
	f := newFake()
	f.assignable = []string{"owenrumney"}

	got := f.Set(context.Background(), ".", "infracost/cli", "pr", user("liam@infracost.io"))

	assert.False(t, got.Set)
	assert.Equal(t, "liamg", got.Login, "the handle resolved; the repository refused it")
	assert.Contains(t, got.Why, "cannot review")
	assert.Empty(t, f.added)
}

// The bus ask has already gone out, so nothing below it may raise.
func TestAFailedGitHubCallIsAnOutcomeAndNotAnError(t *testing.T) {
	f := newFake()
	f.addErr = errors.New("gh: not authenticated")

	got := f.Set(context.Background(), ".", "infracost/cli", "pr", user("liam@infracost.io"))

	assert.False(t, got.Set)
	assert.Contains(t, got.Why, "not authenticated")
}

func TestTheHandleIsLookedUpOncePerRepository(t *testing.T) {
	f := newFake()
	for range 3 {
		require.True(t, f.Set(context.Background(), ".", "infracost/cli", "pr", user("liam@infracost.io")).Set)
	}
	assert.Equal(t, 1, f.lookups, "the mapping does not change")

	// A miss is cached too, and a different repository is a different answer.
	f.Set(context.Background(), ".", "infracost/cli", "pr", user("nobody@infracost.io"))
	f.Set(context.Background(), ".", "infracost/cli", "pr", user("nobody@infracost.io"))
	assert.Equal(t, 2, f.lookups)
	f.Set(context.Background(), ".", "owenrumney/ade", "pr", user("liam@infracost.io"))
	assert.Equal(t, 3, f.lookups)
}

// An empty list is GitHub telling us nothing, not telling us no.
func TestAnEmptyAssignableListStillAsks(t *testing.T) {
	f := newFake()
	f.assignable = nil

	got := f.Set(context.Background(), ".", "infracost/cli", "pr", user("liam@infracost.io"))

	assert.True(t, got.Set)
	assert.Equal(t, []string{"liamg"}, f.added)
}

func TestEmailOfATarget(t *testing.T) {
	assert.Equal(t, "liam@infracost.io", Email(user("liam@infracost.io")))
	assert.Equal(t, "liam@infracost.io", Email(agentbus.Target{Instance: "liam@infracost.io/laptop"}))
	assert.Empty(t, Email(agentbus.Target{Group: "backend"}))
	assert.Empty(t, Email(agentbus.Target{Anyone: true}))
	assert.Empty(t, Email(agentbus.Target{All: true}))
}
