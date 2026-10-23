// Package dash is what is waiting on you across every repository: reviews
// asked of you, your GitHub inbox, and your Linear tickets. See the design doc.
package dash

import (
	"context"
	"sync"

	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/sessions"
)

// Deps is what a board needs from whoever is serving it.
type Deps struct {
	// Dir is where gh runs. Empty is enough for the launcher: these are
	// account-wide searches, not questions about one repository.
	Dir string

	// Repos lists the checkouts ade knows, for finding the one a review
	// belongs in.
	Repos func() ([]sessions.Repo, error)

	// Open starts or reuses a ade in a checkout and answers its link, and
	// Create makes a ticket's worktree and document in one.
	Open   func(ctx context.Context, dir, sessionID string) (string, error)
	Create func(ctx context.Context, dir, ticket string) (string, error)

	// Changed is called whenever a list here changes, for the page to be told.
	Changed func()

	// CheckOut puts a pull request in a worktree and Clone fetches a repository
	// this machine lacks. nil is the real one; a test stands them in.
	CheckOut func(ctx context.Context, main string, number int) (string, error)
	Clone    func(ctx context.Context, nameWithOwner, dest string) error

	// States reads whether each pull request is still open. nil is the real one.
	States func(ctx context.Context, dir string, refs []ghpr.PRRef) (map[string]string, error)
}

// Board is the three lists, as last read.
type Board struct {
	deps Deps

	mu            sync.RWMutex
	reviews       []ghpr.Request
	reviewTrouble ReviewTrouble
	notifications []ghpr.Notification
	linear        *linearCache

	// running is the cold reviews this launcher started, by review key, so a
	// row can say so while nothing is attached to the server doing it.
	running map[string]bool
}

// New is a board that has not read anything yet.
func New(deps Deps) *Board {
	return &Board{deps: deps, linear: newLinearCache()}
}

// Refresh reads all three, in parallel: they are three different services and
// none of them needs the others.
func (b *Board) Refresh(ctx context.Context) {
	var wg sync.WaitGroup
	for _, read := range []func(context.Context){
		b.RefreshReviews, b.RefreshNotifications, b.RefreshTickets,
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			read(ctx)
		}()
	}
	wg.Wait()
}

func (b *Board) changed() {
	if b.deps.Changed != nil {
		b.deps.Changed()
	}
}

func (b *Board) repos() ([]sessions.Repo, error) {
	if b.deps.Repos == nil {
		return nil, nil
	}
	return b.deps.Repos()
}
