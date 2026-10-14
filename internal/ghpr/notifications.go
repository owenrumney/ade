package ghpr

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Notification is one thread in your GitHub inbox.
//
// What is here is what the notifications API gives, and no more. It does not
// give the author: that would be one call per notification to
// subject.latest_comment_url, which is why nothing here filters by one. Reason
// is the lever instead, and a better one — "no more comment threads" says what
// you mean more often than a list of names does.
type Notification struct {
	// ID is GitHub's thread id. It is reused when new activity arrives on the
	// same thread, which is why a dismissal is remembered with a time.
	ID string `json:"id"`
	// Repo is owner/repo.
	Repo string `json:"repo"`
	// Reason is why this reached you: review_requested, comment, mention,
	// ci_activity, author, state_change, subscribed, manual.
	Reason string `json:"reason"`
	// Type is what the thread is about: PullRequest, Issue, Release, Commit.
	Type  string `json:"type"`
	Title string `json:"title"`
	// Number is the pull request or issue number, or 0 for a thread that has
	// no number — a release, a commit, a discussion.
	Number int `json:"number,omitempty"`
	// URL is where to go and read it, on github.com rather than the API.
	URL       string `json:"url"`
	UpdatedAt string `json:"updatedAt"`
	Unread    bool   `json:"unread"`
}

// notificationLimit is one page, which has always covered it: an inbox runs to
// tens, not thousands, and anything older than the first hundred is not
// something a dashboard should be reminding you about.
const notificationLimit = 100

// Notifications is your GitHub inbox, newest first as GitHub returns it.
func Notifications(ctx context.Context, dir string) ([]Notification, error) {
	out, err := run(ctx, dir, "api", fmt.Sprintf("notifications?per_page=%d", notificationLimit))
	if err != nil {
		return nil, err
	}
	return parseNotifications(out)
}

func parseNotifications(out string) ([]Notification, error) {
	var raw []struct {
		ID         string `json:"id"`
		Reason     string `json:"reason"`
		Unread     bool   `json:"unread"`
		UpdatedAt  string `json:"updated_at"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Subject struct {
			Title string `json:"title"`
			Type  string `json:"type"`
			URL   string `json:"url"`
		} `json:"subject"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("parse notifications: %w", err)
	}

	inbox := make([]Notification, 0, len(raw))
	for _, n := range raw {
		url, number := subjectPlace(n.Repository.FullName, n.Subject.URL)
		inbox = append(inbox, Notification{
			ID: n.ID, Repo: n.Repository.FullName, Reason: n.Reason,
			Type: n.Subject.Type, Title: n.Subject.Title, Number: number,
			URL: url, UpdatedAt: n.UpdatedAt, Unread: n.Unread,
		})
	}
	return inbox, nil
}

// subjectPlace turns the API url a notification carries into one you can open,
// and the number off its tail.
//
// The api gives ".../repos/owner/repo/pulls/1034"; a browser wants
// "https://github.com/owner/repo/pull/1034". A subject with no number of its
// own — a release, a commit, a thread shape not seen before — falls back to the
// repository, which is still somewhere useful to land.
func subjectPlace(repo, apiURL string) (string, int) {
	home := "https://github.com/" + repo
	parts := strings.Split(strings.TrimSuffix(apiURL, "/"), "/")
	if len(parts) < 2 {
		return home, 0
	}
	number, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil || number <= 0 {
		return home, 0
	}
	var kind string
	switch parts[len(parts)-2] {
	case "pulls":
		kind = "pull"
	case "issues":
		kind = "issues"
	default:
		return home, 0
	}
	if repo == "" {
		return home, number
	}
	return fmt.Sprintf("%s/%s/%d", home, kind, number), number
}

// PRRef names one pull request, across repositories.
type PRRef struct {
	Repo   string // owner/repo
	Number int
}

// Key is how a pull request is named on both lists ade keeps.
func (r PRRef) Key() string { return fmt.Sprintf("%s#%d", r.Repo, r.Number) }

// safeRepo is GitHub's own charset for an owner and a repository name, and
// nothing else. These names arrive from GitHub rather than from a person, but a
// query is built from them below, and a query built from unchecked text is a
// query someone else can write.
var safeRepo = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// stateBatch is how many pull requests are asked about at once. Aliases are
// cheap, but a query is easier to reason about when it is bounded.
const stateBatch = 50

// PRStates is whether each of these pull requests is still open: OPEN, CLOSED
// or MERGED, keyed by PRRef.Key.
//
// One call for all of them, by aliasing a repository lookup per pull request.
// The notifications API says nothing about whether the thing it is telling you
// about is still live, and asking per pull request would be a call each.
//
// A pull request missing from the answer is one this could not read. It is left
// out rather than guessed at, so a caller filtering on state keeps it.
func PRStates(ctx context.Context, dir string, refs []PRRef) (map[string]string, error) {
	states := map[string]string{}
	wanted := make([]PRRef, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		if ref.Number <= 0 || !safeRepo.MatchString(ref.Repo) || seen[ref.Key()] {
			continue
		}
		seen[ref.Key()] = true
		wanted = append(wanted, ref)
	}

	for start := 0; start < len(wanted); start += stateBatch {
		batch := wanted[start:min(start+stateBatch, len(wanted))]
		out, err := graphqlRead(ctx, dir, stateQuery(batch))
		if err != nil {
			return states, err
		}
		if err := readStates(out, batch, states); err != nil {
			return states, err
		}
	}
	return states, nil
}

// stateQuery aliases one lookup per pull request: a0, a1, and so on, in the
// order of batch, which is how the answer is read back.
func stateQuery(batch []PRRef) string {
	var b strings.Builder
	b.WriteString("{")
	for i, ref := range batch {
		owner, name, _ := strings.Cut(ref.Repo, "/")
		fmt.Fprintf(&b, " a%d: repository(owner:%q,name:%q){ pullRequest(number:%d){ state } }", i, owner, name, ref.Number)
	}
	b.WriteString(" }")
	return b.String()
}

func readStates(out string, batch []PRRef, into map[string]string) error {
	var raw struct {
		Data map[string]*struct {
			PullRequest *struct {
				State string `json:"state"`
			} `json:"pullRequest"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return fmt.Errorf("parse pull request states: %w", err)
	}
	for i, ref := range batch {
		repo, ok := raw.Data[fmt.Sprintf("a%d", i)]
		if !ok || repo == nil || repo.PullRequest == nil {
			continue
		}
		into[ref.Key()] = strings.ToUpper(repo.PullRequest.State)
	}
	return nil
}
