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
	b.RefreshOnly(ctx, "")
}

// RefreshOnly reads one of the three. A place asks for the service behind
// it, so refreshing Issues does not also spend a GitHub round trip.
func (b *Board) RefreshOnly(ctx context.Context, only string) {
	by := map[string]func(context.Context){
		readReviews:       b.RefreshReviews,
		readNotifications: b.RefreshNotifications,
		readTickets:       b.RefreshTickets,
	}
	var wg sync.WaitGroup
	for _, name := range ReadsFor(only) {
		read := by[name]
		wg.Add(1)
		go func() {
			defer wg.Done()
			read(ctx)
		}()
	}
	wg.Wait()
}

// The three services behind the board, as a refresh names them.
const (
	readReviews       = "reviews"
	readNotifications = "notifications"
	readTickets       = "tickets"
)

// ReadsFor is which services a refresh asks. A name nobody recognises asks
// all three: a stale list is worse than a wasted round trip.
func ReadsFor(only string) []string {
	all := []string{readReviews, readNotifications, readTickets}
	for _, name := range all {
		if only == name {
			return []string{name}
		}
	}
	return all
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
