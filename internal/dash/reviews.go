package dash

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
)

// Review is a pull request waiting on you. Muting is ade's own state, kept out
// of ghpr.Request, which is what GitHub said.
type Review struct {
	ghpr.Request
	Muted bool `json:"muted,omitempty"`

	// Review is what your own review of it has reached, read off the
	// worktree it was checked out into. Absent until one was started.
	Review *ReviewState `json:"review,omitempty"`
}

// RefreshReviews asks GitHub for the pull requests waiting on your review,
// anywhere.
func (b *Board) RefreshReviews(ctx context.Context) {
	asked, err := ghpr.ReviewRequests(ctx, b.deps.Dir)
	if err != nil {
		slog.Warn("list review requests", "err", err)
		b.setReviewsTrouble(err.Error(), 0)
		return
	}
	kept, hidden := wanted(asked)
	b.setReviewsTrouble("", hidden)
	b.SetReviews(kept)
}

// wanted drops what the standing rules say never to show, and counts what it
// dropped: an empty list has to be able to say why it is empty.
func wanted(asked []ghpr.Request) ([]ghpr.Request, int) {
	cfg, err := settings.Load()
	if err != nil {
		return asked, 0
	}
	out := make([]ghpr.Request, 0, len(asked))
	for _, pr := range asked {
		if cfg.Ignore.SkipReview(pr.Repo, pr.Author, pr.UpdatedAt) {
			continue
		}
		out = append(out, pr)
	}
	return out, len(asked) - len(out)
}

// ReviewTrouble is why the list of review requests may not be what you expect:
// the search failed, or your own rules hid some of what it found.
type ReviewTrouble struct {
	// Error is the last failed search. The list is left as it was, so without
	// this an unreachable GitHub reads as nothing waiting on you.
	Error  string `json:"error,omitempty"`
	Hidden int    `json:"hidden,omitempty"`
}

func (b *Board) setReviewsTrouble(failed string, hidden int) {
	b.mu.Lock()
	was := b.reviewTrouble
	b.reviewTrouble = ReviewTrouble{Error: failed, Hidden: hidden}
	changed := was != b.reviewTrouble
	b.mu.Unlock()
	if changed {
		b.changed()
	}
}

func (b *Board) ReviewTroubleNow() ReviewTrouble {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.reviewTrouble
}

// SetReviews replaces the list, and tells the page only if it is different.
func (b *Board) SetReviews(asked []ghpr.Request) {
	b.mu.Lock()
	changed := len(asked) != len(b.reviews)
	for i := range asked {
		if !changed && asked[i] != b.reviews[i] {
			changed = true
		}
	}
	b.reviews = asked
	b.mu.Unlock()
	if changed {
		b.changed()
	}
}

// ReviewKey names a pull request across every repository, the only scope that
// means anything for a search.
func ReviewKey(repo string, number int) string {
	return fmt.Sprintf("%s#%d", repo, number)
}

// Reviews is the requests as last read. Muted ones are sent, not dropped, or
// there would be no way to see what you put aside.
func (b *Board) Reviews() []Review {
	b.mu.RLock()
	asked := append([]ghpr.Request(nil), b.reviews...)
	b.mu.RUnlock()

	muted := settings.MutedReviews()
	repos, _ := b.repos()
	out := make([]Review, 0, len(asked))
	for _, pr := range asked {
		row := Review{Request: pr, Muted: muted[ReviewKey(pr.Repo, pr.Number)]}
		if main, ok := checkoutFor(repos, pr.Repo); ok {
			state := reviewStateIn(main, pr.Number, b.reviewing(pr.Repo, pr.Number))
			row.Review = &state
		}
		out = append(out, row)
	}
	return out
}

// reviewing is a cold review this launcher started and has not seen finish.
func (b *Board) reviewing(repo string, number int) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.running[ReviewKey(repo, number)]
}

// StartReview checks the pull request out and asks its server for a cold
// review, with no window opened. The row is what says it is going.
func (b *Board) StartReview(ctx context.Context, repo string, number int) (string, error) {
	url, path, err := b.OpenReview(ctx, repo, number)
	if err != nil {
		return "", err
	}
	key := ReviewKey(repo, number)
	b.mu.Lock()
	if b.running == nil {
		b.running = map[string]bool{}
	}
	if b.running[key] {
		b.mu.Unlock()
		return path, nil
	}
	b.running[key] = true
	b.mu.Unlock()
	b.changed()

	go func() {
		err := askForReview(context.WithoutCancel(ctx), url)
		b.mu.Lock()
		delete(b.running, key)
		b.mu.Unlock()
		if err != nil {
			slog.Warn("cold review", "repo", repo, "pr", number, "err", err)
		}
		b.changed()
	}()
	return path, nil
}

// askForReview is the session server's own review endpoint, which files the
// findings as threads. No agents named is the cold reviewer.
func askForReview(ctx context.Context, url string) error {
	if cut := strings.Index(url, "#"); cut >= 0 {
		url = url[:cut]
	}
	_, err := sessions.AskFor(ctx, url, http.MethodPost, "/api/review", "{}", reviewWait)
	return err
}

// reviewWait is how long the ask may take. The run itself is longer; this is
// the server accepting it.
const reviewWait = 30 * time.Second

// Mute puts a pull request aside, or brings it back. Only one the search
// returned: this writes to ade's own state.
func (b *Board) Mute(repo string, number int, muted bool) error {
	b.mu.RLock()
	known := false
	for _, pr := range b.reviews {
		if pr.Repo == repo && pr.Number == number {
			known = true
			break
		}
	}
	b.mu.RUnlock()
	if !known {
		return ErrNotOffered
	}
	if err := settings.SetMutedReview(ReviewKey(repo, number), muted); err != nil {
		return err
	}
	b.changed()
	return nil
}

// OpenReview checks the pull request out in a worktree of its repository and
// answers the link to the ade now serving it, on the Review tab.
func (b *Board) OpenReview(ctx context.Context, repo string, number int) (url, path string, err error) {
	var found *ghpr.Request
	for _, asked := range b.Reviews() {
		if asked.Number == number && strings.EqualFold(asked.Repo, repo) {
			found = &asked.Request
			break
		}
	}
	if found == nil {
		return "", "", ErrGone
	}
	repos, err := b.repos()
	if err != nil {
		return "", "", err
	}
	return b.startReview(ctx, repos, *found)
}

// startReview finds the repository, puts the pull request in a worktree of it,
// and starts or reuses that worktree's ade.
func (b *Board) startReview(ctx context.Context, repos []sessions.Repo, asked ghpr.Request) (string, string, error) {
	main, ok := checkoutFor(repos, asked.Repo)
	if !ok {
		// Not on the machine: clone it into the first scan directory, as
		// <owner>/<name>, which is where the scan will find it from now on.
		var err error
		if main, err = b.cloneForReview(ctx, asked.Repo); err != nil {
			return "", "", err
		}
	}
	checkOut := b.deps.CheckOut
	if checkOut == nil {
		checkOut = ReviewWorktree
	}
	path, err := checkOut(ctx, main, asked.Number)
	if err != nil {
		return "", "", err
	}
	if b.deps.Open == nil {
		return "", "", fmt.Errorf("#%d is checked out at %s but this server cannot start a ade in it", asked.Number, path)
	}
	url, err := b.deps.Open(ctx, path, "")
	if err != nil {
		return "", "", fmt.Errorf("#%d is checked out at %s but its ade did not start: %w", asked.Number, path, err)
	}
	if cut := strings.Index(url, "#"); cut >= 0 {
		url = url[:cut]
	}
	return url + "#review", path, nil
}

// cloneForReview clones owner/name under the first scan directory: one you do
// not have is still one you were asked to review.
func (b *Board) cloneForReview(ctx context.Context, nameWithOwner string) (string, error) {
	owner, name, ok := strings.Cut(nameWithOwner, "/")
	if !ok || owner == "" || name == "" {
		return "", fmt.Errorf("%q is not owner/name", nameWithOwner)
	}
	cfg, err := settings.Load()
	if err != nil {
		return "", err
	}
	roots := cfg.Roots()
	if len(roots) == 0 {
		return "", fmt.Errorf("%s is not on this machine and there is no scan directory to clone it into; add one in settings", nameWithOwner)
	}
	dest := filepath.Join(roots[0], owner, name)
	clone := b.deps.Clone
	if clone == nil {
		clone = ghpr.Clone
	}
	if err := clone(ctx, nameWithOwner, dest); err != nil {
		return "", fmt.Errorf("%s is not on this machine and could not be cloned to %s: %w", nameWithOwner, dest, err)
	}
	return dest, nil
}

// reviewWorktreeMu serialises the check-then-add below: two browser tabs on the
// same pull request would otherwise both find the worktree missing.
var reviewWorktreeMu sync.Mutex

// ReviewWorktree is ../<repo>-pr-<n>, made detached and handed to gh. See the
// design doc.
func ReviewWorktree(ctx context.Context, main string, number int) (string, error) {
	reviewWorktreeMu.Lock()
	defer reviewWorktreeMu.Unlock()
	path := gitdiff.WorktreePath(main, fmt.Sprintf("pr-%d", number))
	made := false
	if _, err := os.Stat(path); err != nil {
		if err := gitdiff.AddDetachedWorktree(ctx, main, path); err != nil {
			return "", err
		}
		made = true
	}
	if err := ghpr.Checkout(ctx, path, number); err != nil {
		// Left behind, it is found by the next go and never checked out. Its
		// own context: a cancelled request is one way to get here.
		if made {
			if rmErr := gitdiff.RemoveWorktreeForce(context.WithoutCancel(ctx), main, path); rmErr != nil {
				slog.Warn("remove the worktree a failed checkout left", "path", path, "err", rmErr)
			}
		}
		return "", fmt.Errorf("check out #%d in %s: %w", number, path, err)
	}
	return path, nil
}

// checkoutFor is the main checkout of owner/name: the name has to match, and
// the owner breaks ties.
func checkoutFor(repos []sessions.Repo, nameWithOwner string) (string, bool) {
	owner, name, ok := strings.Cut(nameWithOwner, "/")
	if !ok {
		return "", false
	}
	path := ""
	for _, repo := range repos {
		if !strings.EqualFold(repo.Name, name) {
			continue
		}
		if strings.EqualFold(repo.Owner, owner) {
			return repo.Path, true
		}
		if path == "" {
			path = repo.Path
		}
	}
	return path, path != ""
}

// GoneError is a review request whose pull request merged or closed before
// anybody took it. Its branch is deleted, so there is nothing to check out.
type GoneError struct {
	Repo   string
	Number int
	State  string
}

func (e *GoneError) Error() string {
	return fmt.Sprintf("%s#%d is already %s", e.Repo, e.Number, strings.ToLower(e.State))
}

// OpenPullRequest is OpenReview for a pull request GitHub did not ask you
// about: an agentbus review request is the other way one arrives.
func (b *Board) OpenPullRequest(ctx context.Context, repo string, number int) (url, path string, err error) {
	repos, err := b.repos()
	if err != nil {
		return "", "", err
	}
	url, path, err = b.startReview(ctx, repos, ghpr.Request{Repo: repo, Number: number})
	if err != nil {
		if state := b.goneState(ctx, repo, number); state != "" {
			return "", "", &GoneError{Repo: repo, Number: number, State: state}
		}
	}
	return url, path, err
}

// goneState is the state of a pull request that is not open, else empty.
func (b *Board) goneState(ctx context.Context, repo string, number int) string {
	states := b.deps.States
	if states == nil {
		states = ghpr.PRStates
	}
	ref := ghpr.PRRef{Repo: repo, Number: number}
	got, err := states(ctx, b.deps.Dir, []ghpr.PRRef{ref})
	if err != nil {
		return ""
	}
	if state := got[ref.Key()]; state != "" && state != "OPEN" {
		return state
	}
	return ""
}
