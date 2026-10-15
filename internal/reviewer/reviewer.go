// Package reviewer turns the person a review was asked of on the bus into the
// reviewer on the pull request. See .git/ade/docs/bus-pr-reviewer.md.
package reviewer

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/owenrumney/ade/internal/agentbus"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
)

// Outcome is what happened, in words a person can be shown. Set says whether
// the pull request now names a reviewer; Why says why not when it does not.
type Outcome struct {
	Login string
	Set   bool
	Why   string
}

// Resolver finds handles and sets reviewers. The calls are fields so a test
// can answer for git and GitHub; build one with New.
type Resolver struct {
	commitBy func(ctx context.Context, dir, email string) (string, error)
	loginOf  func(ctx context.Context, dir, repo, sha string) (string, error)
	accepts  func(ctx context.Context, dir, repo string) ([]string, error)
	add      func(ctx context.Context, dir, pr, login string) error

	// known is the resolved handles, keyed by repo and address. The answer
	// does not change, and the lookup is two round trips.
	mu    sync.Mutex
	known map[string]string
}

func New() *Resolver {
	return &Resolver{
		commitBy: gitdiff.LastCommitBy,
		loginOf:  ghpr.LoginOfCommit,
		accepts:  ghpr.Assignable,
		add:      ghpr.AddReviewer,
		known:    map[string]string{},
	}
}

// Email is the one address a target names, empty when it names no one person.
// A group and anyone are not failures; there is just nobody single to name.
func Email(to agentbus.Target) string {
	switch {
	case to.User != "":
		return to.User
	case to.Instance != "":
		// An instance is user@host/id, and the user part is the person.
		if user, _, ok := strings.Cut(to.Instance, "/"); ok {
			return user
		}
		return to.Instance
	}
	return ""
}

// Set puts the person asked onto the pull request. Every refusal is an Outcome
// and not an error: the bus ask has already gone out.
func (r *Resolver) Set(ctx context.Context, dir, repo, pr string, to agentbus.Target) Outcome {
	email := Email(to)
	if email == "" {
		return Outcome{Why: fmt.Sprintf("%s is not one person, so no reviewer was set", to.String())}
	}
	login, err := r.login(ctx, dir, repo, email)
	if err != nil {
		return Outcome{Why: err.Error()}
	}
	if login == "" {
		return Outcome{Why: fmt.Sprintf("no GitHub handle for %s in %s; add one to the roster", email, repo)}
	}

	ok, err := r.welcome(ctx, dir, repo, login)
	if err != nil {
		return Outcome{Login: login, Why: err.Error()}
	}
	if !ok {
		return Outcome{Login: login, Why: fmt.Sprintf("%s cannot review in %s", login, repo)}
	}
	if err := r.add(ctx, dir, pr, login); err != nil {
		return Outcome{Login: login, Why: fmt.Sprintf("asking %s to review: %v", login, err)}
	}
	return Outcome{Login: login, Set: true}
}

// login is the handle for an address in this repository, from the cache when
// it has been asked before.
func (r *Resolver) login(ctx context.Context, dir, repo, email string) (string, error) {
	key := repo + "\x00" + strings.ToLower(email)
	r.mu.Lock()
	held, ok := r.known[key]
	r.mu.Unlock()
	if ok {
		return held, nil
	}

	sha, err := r.commitBy(ctx, dir, email)
	if err != nil {
		return "", err
	}
	login := ""
	if sha != "" {
		if login, err = r.loginOf(ctx, dir, repo, sha); err != nil {
			return "", err
		}
	}
	// A miss is cached too: the answer is the same until they commit, and the
	// two calls are not worth paying for on every ask.
	r.mu.Lock()
	r.known[key] = login
	r.mu.Unlock()
	return login, nil
}

func (r *Resolver) welcome(ctx context.Context, dir, repo, login string) (bool, error) {
	logins, err := r.accepts(ctx, dir, repo)
	if err != nil {
		return false, err
	}
	// An empty list is GitHub telling us nothing useful rather than telling us
	// no, so the request goes out and GitHub gets the last word.
	if len(logins) == 0 {
		return true, nil
	}
	for _, one := range logins {
		if strings.EqualFold(one, login) {
			return true, nil
		}
	}
	return false, nil
}
