// Package ghpr reads pull requests through the gh CLI, reusing its auth.
package ghpr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Review comment sides, normalised from GitHub's uppercase form.
const (
	SideRight = "right"
	SideLeft  = "left"
)

// PR is the metadata ade needs to show and guard a pull request.
type PR struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Author  string `json:"author"`
	BaseRef string `json:"baseRef"`
	HeadRef string `json:"headRef"`
	HeadSHA string `json:"headSha"`
	URL     string `json:"url"`
	State   string `json:"state"`
	Body    string `json:"body,omitempty"`
	Draft   bool   `json:"draft,omitempty"`

	// Mergeable and MergeState are GitHub's own verdict on whether this can be
	// merged. Working it out locally would disagree with what the merge attempt
	// actually does.
	Mergeable  string `json:"mergeable,omitempty"`
	MergeState string `json:"mergeState,omitempty"`

	// ReviewDecision is GitHub's summary: APPROVED, CHANGES_REQUESTED,
	// REVIEW_REQUIRED, or empty where the repository requires no review.
	ReviewDecision string `json:"reviewDecision,omitempty"`

	// Reviews is the latest review from each reviewer, so the tab can say who
	// has approved and whether you are one of them.
	Reviews []Review `json:"reviews,omitempty"`

	// MergeCommit is the sha the merge produced, empty until it is merged. For
	// a squash merge it is the one commit standing for the whole branch.
	MergeCommit string `json:"mergeCommit,omitempty"`
}

// Review is one reviewer's latest verdict.
type Review struct {
	Author string `json:"author"`
	State  string `json:"state"`
}

// Issue is a GitHub issue, as a design is started from one.
type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"url"`
	State  string `json:"state"`
}

// ReviewBy is that person's latest verdict, or "" if they have not reviewed.
func (p *PR) ReviewBy(login string) string {
	if p == nil || login == "" {
		return ""
	}
	for _, review := range p.Reviews {
		if strings.EqualFold(review.Author, login) {
			return review.State
		}
	}
	return ""
}

// ApproveBlocked explains why this pull request cannot be approved by you, or
// "" when it can be. viewer is your login; an empty one means we could not work
// out who you are, which is not a reason to refuse.
func (p *PR) ApproveBlocked(viewer string) string {
	switch {
	case p == nil:
		return "there is no pull request"
	case !strings.EqualFold(p.State, "OPEN"):
		return "this pull request is " + strings.ToLower(p.State)
	case viewer != "" && strings.EqualFold(p.Author, viewer):
		return "GitHub does not let you approve your own pull request"
	case strings.EqualFold(p.ReviewBy(viewer), "APPROVED"):
		return "you have already approved this"
	}
	return ""
}

// RequestChangesBlocked explains why you cannot request changes on this pull
// request, or "" when you can. viewer is your login, as in ApproveBlocked.
func (p *PR) RequestChangesBlocked(viewer string) string {
	switch {
	case p == nil:
		return "there is no pull request"
	case !strings.EqualFold(p.State, "OPEN"):
		return "this pull request is " + strings.ToLower(p.State)
	case viewer != "" && strings.EqualFold(p.Author, viewer):
		return "GitHub does not let you review your own pull request"
	case strings.EqualFold(p.ReviewBy(viewer), "CHANGES_REQUESTED"):
		return "you have already requested changes"
	}
	return ""
}

// Merge methods ade offers. Squash is deliberately absent: these two are what
// this repository merges with.
const (
	MergeRebase = "rebase"
	MergeCommit = "merge"
)

// MergeBlocked explains why this pull request cannot be merged, or "" when it
// can be.
func (p *PR) MergeBlocked() string {
	switch {
	case p == nil:
		return "there is no pull request"
	case !strings.EqualFold(p.State, "OPEN"):
		return "this pull request is " + strings.ToLower(p.State)
	case p.Draft:
		return "this pull request is still a draft"
	}
	switch strings.ToUpper(p.MergeState) {
	case "BLOCKED":
		return "its branch protection rules are not satisfied yet"
	case "BEHIND":
		return "the branch is behind " + p.BaseRef
	case "DIRTY":
		return "it conflicts with " + p.BaseRef
	}
	if strings.EqualFold(p.Mergeable, "CONFLICTING") {
		return "it conflicts with " + p.BaseRef
	}
	return ""
}

// ReviewComment is one inline comment on a pull request.
type ReviewComment struct {
	ID        int64
	ReplyTo   int64
	Path      string
	Line      int
	Side      string
	Body      string
	Author    string
	CreatedAt time.Time
	Outdated  bool
}

// Thread is a root review comment with its replies, in the order posted.
type Thread struct {
	Root    ReviewComment
	Replies []ReviewComment
}

// Fetch reads a pull request's metadata.
func Fetch(ctx context.Context, dir string, number int) (*PR, error) {
	// The same fields ForBranch asks for. Diverging left a pull request opened by
	// number with no description and never marked draft.
	out, err := run(ctx, dir, "pr", "view", strconv.Itoa(number), "--json", prFields)
	if err != nil {
		return nil, err
	}

	return parsePR(out)
}

func parsePR(out string) (*PR, error) {
	var raw struct {
		Number         int    `json:"number"`
		Title          string `json:"title"`
		URL            string `json:"url"`
		State          string `json:"state"`
		Body           string `json:"body"`
		Draft          bool   `json:"isDraft"`
		BaseRef        string `json:"baseRefName"`
		HeadRef        string `json:"headRefName"`
		HeadSHA        string `json:"headRefOid"`
		Mergeable      string `json:"mergeable"`
		MergeState     string `json:"mergeStateStatus"`
		ReviewDecision string `json:"reviewDecision"`
		MergeCommit    struct {
			OID string `json:"oid"`
		} `json:"mergeCommit"`
		LatestReviews []struct {
			State  string `json:"state"`
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
		} `json:"latestReviews"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("parse pr view: %w", err)
	}
	if raw.Number == 0 {
		return nil, nil
	}

	var reviews []Review
	for _, r := range raw.LatestReviews {
		reviews = append(reviews, Review{Author: r.Author.Login, State: r.State})
	}
	return &PR{
		Number:  raw.Number,
		Title:   raw.Title,
		Author:  raw.Author.Login,
		BaseRef: raw.BaseRef,
		HeadRef: raw.HeadRef,
		HeadSHA: raw.HeadSHA,
		URL:     raw.URL,
		State:   raw.State,
		Body:    raw.Body,
		Draft:   raw.Draft,

		Mergeable:      raw.Mergeable,
		MergeState:     raw.MergeState,
		ReviewDecision: raw.ReviewDecision,
		Reviews:        reviews,
		MergeCommit:    raw.MergeCommit.OID,
	}, nil
}

// prFields is what a pull request is read with, in one place so the two ways of
// asking for one cannot describe it differently.
const prFields = "number,title,author,baseRefName,headRefName,headRefOid,url,state,body,isDraft," +
	"mergeable,mergeStateStatus,reviewDecision,latestReviews,mergeCommit"

// Check is one CI check on a pull request.
type Check struct {
	Name        string `json:"name"`
	Workflow    string `json:"workflow,omitempty"`
	State       string `json:"state"`
	Bucket      string `json:"bucket"`
	Description string `json:"description,omitempty"`
	Link        string `json:"link,omitempty"`
}

// Failed reports whether this check is one worth acting on.
func (c Check) Failed() bool { return c.Bucket == "fail" }

// ForBranch finds the pull request for a branch, if there is one. A branch with
// no pull request is an ordinary state, so it returns nil rather than an error.
func ForBranch(ctx context.Context, dir, branch string) (*PR, error) {
	out, err := run(ctx, dir, "pr", "view", branch, "--json", prFields)
	if err != nil {
		// gh exits non-zero when the branch has no pull request, which is not a
		// failure worth surfacing.
		return nil, nil //nolint:nilerr // absence is a valid answer here
	}
	return parsePR(out)
}

// Head is one pull request's branch and what became of it, which GitHub still
// answers once that branch is deleted.
type Head struct {
	Number int    `json:"number"`
	Branch string `json:"branch"`
	State  string `json:"state"`
	URL    string `json:"url"`
}

// Merged reports whether this pull request landed.
func (h Head) Merged() bool { return strings.EqualFold(h.State, "MERGED") }

// Open reports whether this pull request is still in flight.
func (h Head) Open() bool { return strings.EqualFold(h.State, "OPEN") }

// Heads lists every pull request's head branch, whatever its state, newest
// first: how a branch the repository no longer has is matched to its fate.
func Heads(ctx context.Context, dir string, limit int) ([]Head, error) {
	if limit <= 0 {
		limit = 100
	}
	out, err := run(ctx, dir, "pr", "list", "--state", "all", "--limit", strconv.Itoa(limit),
		"--json", "number,headRefName,state,url")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Number  int    `json:"number"`
		HeadRef string `json:"headRefName"`
		State   string `json:"state"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("parse pr list: %w", err)
	}
	heads := make([]Head, 0, len(raw))
	for _, r := range raw {
		heads = append(heads, Head{Number: r.Number, Branch: r.HeadRef, State: r.State, URL: r.URL})
	}
	return heads, nil
}

// Listed is a pull request in the picker: enough to choose one, no more.
type Listed struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Author  string `json:"author"`
	HeadRef string `json:"headRef"`
	Draft   bool   `json:"draft"`
	Mine    bool   `json:"mine"`
	URL     string `json:"url"`

	// ReviewRequested is a pull request waiting on the viewer's review, which
	// is the one worth surfacing without being asked.
	ReviewRequested bool `json:"reviewRequested"`

	// UpdatedAt is GitHub's last-touched time, ReviewDecision its summary of
	// the reviews (APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or empty),
	// and Checks the CI in one word — pass, fail, pending, or empty where
	// nothing has reported — so the list says where each one stands.
	UpdatedAt      string `json:"updatedAt,omitempty"`
	ReviewDecision string `json:"reviewDecision,omitempty"`
	Checks         string `json:"checks,omitempty"`
}

// List returns the open pull requests on this repository, so one can be picked
// to review without knowing its number.
func List(ctx context.Context, dir string, limit int) ([]Listed, error) {
	if limit <= 0 {
		limit = 30
	}
	out, err := run(ctx, dir, "pr", "list", "--state", "open", "--limit", strconv.Itoa(limit),
		"--json", "number,title,author,headRefName,isDraft,url,reviewRequests,updatedAt,reviewDecision,statusCheckRollup")
	if err != nil {
		return nil, err
	}
	return parseListed(out, viewer(ctx, dir))
}

// parseListed reads gh's list, marking the pull requests that are the
// viewer's own and the ones whose review the viewer has been asked for. A
// request to a team the viewer is on cannot be told from one to a team they
// are not, so only a request by login counts.
func parseListed(out, me string) ([]Listed, error) {
	var raw []struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		HeadRef string `json:"headRefName"`
		Draft   bool   `json:"isDraft"`
		URL     string `json:"url"`
		Author  struct {
			Login string `json:"login"`
		} `json:"author"`
		ReviewRequests []struct {
			Login string `json:"login"`
		} `json:"reviewRequests"`
		UpdatedAt      string   `json:"updatedAt"`
		ReviewDecision string   `json:"reviewDecision"`
		Rollup         []rollup `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("parse pr list: %w", err)
	}

	listed := make([]Listed, 0, len(raw))
	for _, r := range raw {
		asked := false
		for _, req := range r.ReviewRequests {
			if me != "" && req.Login == me {
				asked = true
			}
		}
		listed = append(listed, Listed{
			Number: r.Number, Title: r.Title, Author: r.Author.Login,
			HeadRef: r.HeadRef, Draft: r.Draft, URL: r.URL,
			Mine:            me != "" && r.Author.Login == me,
			ReviewRequested: asked,
			UpdatedAt:       r.UpdatedAt,
			ReviewDecision:  r.ReviewDecision,
			Checks:          summarise(r.Rollup),
		})
	}
	return listed, nil
}

// rollup is one entry of GitHub's status check rollup: a check run, with a
// status and a conclusion, or an older commit status, with only a state.
type rollup struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	State      string `json:"state"`
}

// summarise folds the rollup into one word: fail if anything failed,
// pending if anything is still to report, pass if everything that counts
// passed, and empty where nothing reported. Neutral and skipped runs do
// not count either way, as on GitHub's own tick.
func summarise(runs []rollup) string {
	out := ""
	for _, r := range runs {
		verdict := r.Conclusion
		if verdict == "" {
			verdict = r.State
		}
		switch strings.ToUpper(verdict) {
		case "FAILURE", "ERROR", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE":
			return "fail"
		case "SUCCESS":
			if out == "" {
				out = "pass"
			}
		case "NEUTRAL", "SKIPPED", "CANCELLED", "STALE":
		default:
			// PENDING, EXPECTED, QUEUED, IN_PROGRESS, or no conclusion yet.
			out = "pending"
		}
	}
	return out
}

// Request is a pull request, anywhere on GitHub, waiting on the viewer's
// review.
type Request struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	Author string `json:"author"`
	Draft  bool   `json:"draft"`
	URL    string `json:"url"`
	// UpdatedAt is GitHub's last-touched time.
	UpdatedAt string `json:"updatedAt,omitempty"`
	// Checks is where the head commit's checks stand, as one of the buckets
	// gh itself uses: pass, fail, pending, or empty for nothing reported.
	Checks string `json:"checks,omitempty"`
	// ReviewDecision is APPROVED, CHANGES_REQUESTED or empty.
	ReviewDecision string `json:"reviewDecision,omitempty"`
}

// requestSearch asks for the open pull requests waiting on the viewer, with
// where each one's checks stand. `gh search prs` cannot answer the second part
// — its --json has no field for the checks — so this is one GraphQL read
// instead of a search followed by a call per pull request.
const requestSearch = `{
  search(query: "is:open is:pr review-requested:@me archived:false", type: ISSUE, first: 30) {
    nodes {
      ... on PullRequest {
        number title url isDraft updatedAt reviewDecision
        author { login }
        repository { nameWithOwner }
        commits(last: 1) { nodes { commit { statusCheckRollup { state } } } }
      }
    }
  }
}`

// ReviewRequests is every open pull request on GitHub asking for the viewer's
// review, in any repository.
func ReviewRequests(ctx context.Context, dir string) ([]Request, error) {
	out, err := graphqlRead(ctx, dir, requestSearch)
	if err != nil {
		return nil, err
	}
	return parseRequests(out)
}

func parseRequests(out string) ([]Request, error) {
	var raw struct {
		Data struct {
			Search struct {
				Nodes []struct {
					Number    int    `json:"number"`
					Title     string `json:"title"`
					Draft     bool   `json:"isDraft"`
					URL       string `json:"url"`
					UpdatedAt string `json:"updatedAt"`
					Decision  string `json:"reviewDecision"`
					Author    struct {
						Login string `json:"login"`
					} `json:"author"`
					Repository struct {
						NameWithOwner string `json:"nameWithOwner"`
					} `json:"repository"`
					Commits struct {
						Nodes []struct {
							Commit struct {
								Rollup *struct {
									State string `json:"state"`
								} `json:"statusCheckRollup"`
							} `json:"commit"`
						} `json:"nodes"`
					} `json:"commits"`
				} `json:"nodes"`
			} `json:"search"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("parse pr search: %w", err)
	}

	nodes := raw.Data.Search.Nodes
	asked := make([]Request, 0, len(nodes))
	for _, r := range nodes {
		// A node with no number is one the search matched and this query could
		// not read as a pull request. There is nothing to show for it.
		if r.Number == 0 {
			continue
		}
		checks := ""
		if len(r.Commits.Nodes) > 0 {
			if rollup := r.Commits.Nodes[0].Commit.Rollup; rollup != nil {
				checks = rollupBucket(rollup.State)
			}
		}
		asked = append(asked, Request{
			Repo: r.Repository.NameWithOwner, Number: r.Number, Title: r.Title,
			Author: r.Author.Login, Draft: r.Draft, URL: r.URL, UpdatedAt: r.UpdatedAt,
			Checks: checks, ReviewDecision: r.Decision,
		})
	}
	return asked, nil
}

// rollupBucket turns GitHub's one-word rollup into the bucket the page already
// draws: the same words summarise() produces from a list of runs.
func rollupBucket(state string) string {
	switch strings.ToUpper(state) {
	case "SUCCESS":
		return "pass"
	case "FAILURE", "ERROR":
		return "fail"
	case "PENDING", "EXPECTED":
		return "pending"
	default:
		return ""
	}
}

// Checkout checks the pull request's head out in dir as a local branch named
// after it, tracking wherever it came from, so the branch's pull request is
// this one and gh can find it from the branch.
func Checkout(ctx context.Context, dir string, number int) error {
	_, err := write(ctx, dir, "pr", "checkout", strconv.Itoa(number))
	return err
}

func Viewer(ctx context.Context, dir string) string { return viewer(ctx, dir) }

func viewer(ctx context.Context, dir string) string {
	out, err := run(ctx, dir, "api", "user", "--jq", ".login")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Body returns the pull request's description.
func Body(ctx context.Context, dir string, number int) (string, error) {
	out, err := run(ctx, dir, "pr", "view", strconv.Itoa(number), "--json", "body", "--jq", ".body")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// FetchIssue reads one issue of the repository checked out at dir.
func FetchIssue(ctx context.Context, dir string, number int) (*Issue, error) {
	if number <= 0 {
		return nil, errors.New("an issue needs a number")
	}
	out, err := run(ctx, dir, "issue", "view", strconv.Itoa(number), "--json", "number,title,body,url,state")
	if err != nil {
		return nil, fmt.Errorf("read issue #%d: %w", number, err)
	}
	return parseIssue(out)
}

func parseIssue(out string) (*Issue, error) {
	var raw struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		URL    string `json:"url"`
		State  string `json:"state"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("parse the issue: %w", err)
	}
	if raw.Number == 0 {
		return nil, errors.New("that is not an issue of this repository")
	}
	// gh issue view follows a pull request number to the pull request, and a
	// design drafted from a merged PR is not what was asked for.
	if strings.Contains(raw.URL, "/pull/") {
		return nil, fmt.Errorf("#%d is a pull request, not an issue", raw.Number)
	}
	return &Issue{Number: raw.Number, Title: raw.Title, Body: raw.Body, URL: raw.URL, State: strings.ToLower(raw.State)}, nil
}

// Checks lists the CI checks on a pull request. gh exits non-zero when checks
// are failing or still pending, which is information rather than an error, so
// the output is parsed either way.
func Checks(ctx context.Context, dir string, number int) ([]Check, error) {
	out, err := run(ctx, dir, "pr", "checks", strconv.Itoa(number),
		"--json", "name,workflow,state,bucket,description,link")
	if strings.TrimSpace(out) == "" {
		if err != nil {
			return nil, err
		}
		return nil, nil
	}

	var checks []Check
	if err := json.Unmarshal([]byte(out), &checks); err != nil {
		return nil, fmt.Errorf("parse pr checks: %w", err)
	}
	sort.SliceStable(checks, func(i, j int) bool {
		// Failures first: they are the reason to look at this at all.
		return rankBucket(checks[i].Bucket) < rankBucket(checks[j].Bucket)
	})
	return checks, nil
}

func rankBucket(bucket string) int {
	switch bucket {
	case "fail":
		return 0
	case "pending":
		return 1
	case "cancel":
		return 2
	case "skipping":
		return 3
	default:
		return 4
	}
}

// Diff returns the pull request's unified diff.
func Diff(ctx context.Context, dir string, number int) (string, error) {
	return run(ctx, dir, "pr", "diff", strconv.Itoa(number))
}

// Comments returns every inline review comment on the pull request.
func Comments(ctx context.Context, dir string, number int) ([]ReviewComment, error) {
	out, err := run(ctx, dir, "api", "--paginate",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/comments", number))
	if err != nil {
		return nil, err
	}
	return ParseComments(out)
}

// ParseComments decodes the review comments API payload. Pagination can
// concatenate several JSON arrays, so each is decoded in turn.
func ParseComments(payload string) ([]ReviewComment, error) {
	decoder := json.NewDecoder(strings.NewReader(payload))
	var out []ReviewComment

	for {
		var page []struct {
			ID           int64     `json:"id"`
			InReplyToID  int64     `json:"in_reply_to_id"`
			Path         string    `json:"path"`
			Line         *int      `json:"line"`
			OriginalLine *int      `json:"original_line"`
			Side         string    `json:"side"`
			Body         string    `json:"body"`
			CreatedAt    time.Time `json:"created_at"`
			User         struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		if err := decoder.Decode(&page); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("parse review comments: %w", err)
		}
		for _, c := range page {
			line, outdated := resolveLine(c.Line, c.OriginalLine)
			if c.Path == "" || line <= 0 {
				continue
			}
			out = append(out, ReviewComment{
				ID:        c.ID,
				ReplyTo:   c.InReplyToID,
				Path:      c.Path,
				Line:      line,
				Side:      normaliseSide(c.Side),
				Body:      strings.TrimSpace(c.Body),
				Author:    c.User.Login,
				CreatedAt: c.CreatedAt,
				Outdated:  outdated,
			})
		}
	}
	return out, nil
}

// GroupThreads collects replies under their root comment, oldest first.
func GroupThreads(comments []ReviewComment) []Thread {
	byID := make(map[int64]*Thread)
	var roots []*Thread

	sorted := append([]ReviewComment(nil), comments...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].CreatedAt.Before(sorted[j].CreatedAt) })

	for _, c := range sorted {
		if c.ReplyTo == 0 {
			t := &Thread{Root: c}
			byID[c.ID] = t
			roots = append(roots, t)
		}
	}
	for _, c := range sorted {
		if c.ReplyTo == 0 {
			continue
		}
		if parent, ok := byID[c.ReplyTo]; ok {
			parent.Replies = append(parent.Replies, c)
			continue
		}
		// A reply whose root is missing still deserves to be shown.
		t := &Thread{Root: c}
		byID[c.ID] = t
		roots = append(roots, t)
	}

	out := make([]Thread, 0, len(roots))
	for _, t := range roots {
		out = append(out, *t)
	}
	return out
}

// resolveLine prefers the current line, falling back to where the comment was
// originally left when the code has moved on.
func resolveLine(line, original *int) (int, bool) {
	if line != nil && *line > 0 {
		return *line, false
	}
	if original != nil && *original > 0 {
		return *original, true
	}
	return 0, false
}

func normaliseSide(side string) string {
	if strings.EqualFold(side, "LEFT") {
		return SideLeft
	}
	return SideRight
}

// readOnly reports whether a gh invocation only reads. ade never speaks on
// your behalf: it shows you what a reviewer said and drafts what you might say
// back, and posting that is yours to do.
func readOnly(args []string) bool {
	if len(args) == 0 {
		return false
	}
	for _, arg := range args {
		// The api subcommand is a read only until it is given a method.
		if arg == "--method" || arg == "-X" || arg == "--input" || arg == "-f" || arg == "--raw-field" {
			return false
		}
	}
	switch args[0] {
	case "api", "search":
		return true
	case "pr":
		if len(args) < 2 {
			return false
		}
		switch args[1] {
		case "view", "diff", "checks", "list", "status":
			return true
		}
	case "issue":
		if len(args) < 2 {
			return false
		}
		switch args[1] {
		case "view", "list":
			return true
		}
	case "repo":
		// Repo reads the owner and name this way, and PostReview needs both
		// before it can name the path it posts to.
		return len(args) >= 2 && args[1] == "view"
	}
	return false
}

// Edit changes the pull request's title and description. This is the one part
// of a pull request ade writes: what the change says about itself.
func Edit(ctx context.Context, dir string, number int, title, body string) error {
	if strings.TrimSpace(title) == "" {
		return errors.New("a pull request needs a title")
	}
	_, err := write(ctx, dir, "pr", "edit", strconv.Itoa(number), "--title", title, "--body", body)
	return err
}

// Approve approves the pull request as you. body is optional and is your own
// words: nothing here is written by an agent.
//
// This is the one place ade speaks to a reviewer, and it speaks as you because
// you pressed the button. Nothing the agent runs can reach it.
func Approve(ctx context.Context, dir string, number int, body string) error {
	args := []string{"pr", "review", strconv.Itoa(number), "--approve"}
	if strings.TrimSpace(body) != "" {
		args = append(args, "--body", body)
	}
	_, err := write(ctx, dir, args...)
	return err
}

// RequestChanges marks the pull request as changes requested, as you. body is
// your own words and is required: GitHub refuses the verdict without one, and
// a rejection with no reason is not one worth sending.
//
// Same footing as Approve: you pressed the button, the words are yours, and
// nothing the agent runs can reach it.
func RequestChanges(ctx context.Context, dir string, number int, body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("requesting changes needs a reason")
	}
	_, err := write(ctx, dir, "pr", "review", strconv.Itoa(number), "--request-changes", "--body", body)
	return err
}

// InlineComment is one review comment, pinned to a line of the diff.
type InlineComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Side string `json:"side"`
	Body string `json:"body"`
}

// PostReview leaves a review with inline comments on the pull request.
//
// One review rather than one comment each: it arrives as a single pass with a
// single notification, which is what a person leaving these by hand produces.
// The words are yours — you read them before this was called.
func PostReview(ctx context.Context, dir, owner, repo string, number int, body string, inline []InlineComment) error {
	if len(inline) == 0 {
		return errors.New("nothing to send")
	}
	for i, c := range inline {
		if c.Path == "" || c.Line <= 0 || strings.TrimSpace(c.Body) == "" {
			return fmt.Errorf("comment %d has no file, line or text", i+1)
		}
	}

	payload := struct {
		Event    string          `json:"event"`
		Body     string          `json:"body,omitempty"`
		Comments []InlineComment `json:"comments"`
	}{Event: "COMMENT", Body: body, Comments: inline}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode review: %w", err)
	}

	path := fmt.Sprintf("repos/%s/%s/pulls/%d/reviews", owner, repo, number)
	_, err = writeStdin(ctx, dir, encoded, "api", "--method", "POST", path, "--input", "-")
	return err
}

// PostReply answers an existing review comment in its thread. Its own endpoint,
// not a review: a new inline comment on the same line would start a second
// thread. commentID is the thread's root, as held by Thread.ExternalID.
func PostReply(ctx context.Context, dir, owner, repo string, number int, commentID int64, body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("nothing to send")
	}
	if commentID <= 0 {
		return errors.New("no comment to reply to")
	}

	encoded, err := json.Marshal(struct {
		Body string `json:"body"`
	}{Body: body})
	if err != nil {
		return fmt.Errorf("encode reply: %w", err)
	}

	path := fmt.Sprintf("repos/%s/%s/pulls/%d/comments/%d/replies", owner, repo, number, commentID)
	_, err = writeStdin(ctx, dir, encoded, "api", "--method", "POST", path, "--input", "-")
	return err
}

// Repo is the owner and name of the repository checked out at dir.
func Repo(ctx context.Context, dir string) (string, string, error) {
	out, err := run(ctx, dir, "repo", "view", "--json", "owner,name")
	if err != nil {
		return "", "", err
	}
	var raw struct {
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return "", "", fmt.Errorf("parse repo view: %w", err)
	}
	if raw.Owner.Login == "" || raw.Name == "" {
		return "", "", errors.New("could not work out the repository")
	}
	return raw.Owner.Login, raw.Name, nil
}

// Merge merges the pull request with the given method.
func Merge(ctx context.Context, dir string, number int, method string) error {
	var flag string
	switch method {
	case MergeRebase:
		flag = "--rebase"
	case MergeCommit:
		flag = "--merge"
	default:
		return fmt.Errorf("unknown merge method %q", method)
	}
	_, err := write(ctx, dir, "pr", "merge", strconv.Itoa(number), flag)
	return err
}

// writable is the complete list of gh commands ade may run that change
// something, and it is short on purpose: edit the pull request you are working
// on, merge it, record the approval you clicked, and answer a reviewer.
//
// What is still refused is prose nobody read. A reply is sent from a button
// that shows you the words first; requesting changes has no such gate.
func writable(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == "api" {
		return apiPost(args, reviewsPath) || apiPost(args, repliesPath)
	}
	if len(args) >= 3 && args[0] == "repo" && args[1] == "clone" {
		return true
	}
	if len(args) < 3 || args[0] != "pr" {
		return false
	}
	switch args[1] {
	case "edit", "merge", "checkout":
		return true
	case "review": //nolint:goconst // the subcommand name, not a shared value
		return verdict(args[2:])
	}
	return false
}

// The two API paths ade may POST to: a review, and an answer in a reviewer's
// own thread. Anything else has no button that shows you the words first.
var (
	reviewsPath = regexp.MustCompile(`^repos/[^/]+/[^/]+/pulls/[0-9]+/reviews$`)
	repliesPath = regexp.MustCompile(`^repos/[^/]+/[^/]+/pulls/[0-9]+/comments/[0-9]+/replies$`)
)

// apiPost reports whether an api invocation is a POST of one whole document to
// a path the allowlist names.
func apiPost(args []string, allowed *regexp.Regexp) bool {
	method, path, input := "", "", false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--method", "-X":
			if i+1 < len(args) {
				method = args[i+1]
				i++
			}
		case "--input":
			input = true
			i++
		case "-f", "-F", "--raw-field", "--field":
			// Fields build a body a line at a time, which is harder to see and
			// harder to bound. The whole payload goes in as one document.
			return false
		default:
			if !strings.HasPrefix(args[i], "-") && path == "" {
				path = args[i]
			}
		}
	}
	return method == "POST" && input && allowed.MatchString(path)
}

// verdict reports whether a review invocation is one of the two verdicts a
// button sends — approve, or request changes — and only that. Two at once, or
// --comment, is prose in your name rather than a verdict, and stays refused.
func verdict(args []string) bool {
	found := 0
	for _, arg := range args {
		switch arg {
		case "--approve", "-a", "--request-changes", "-r":
			found++
		case "--comment", "-c":
			return false
		}
	}
	return found == 1
}

// Clone checks owner/name out at dest with gh, so the host and the login are
// gh's own. dest's parent is made; dest itself must not exist yet.
func Clone(ctx context.Context, nameWithOwner, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("%s already exists", dest)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(dest), err)
	}
	if _, err := write(ctx, "", "repo", "clone", nameWithOwner, dest); err != nil {
		return fmt.Errorf("clone %s: %w", nameWithOwner, err)
	}
	return nil
}

func write(ctx context.Context, dir string, args ...string) (string, error) {
	return writeStdin(ctx, dir, nil, args...)
}

func writeStdin(ctx context.Context, dir string, stdin []byte, args ...string) (string, error) {
	if !writable(args) {
		return "", fmt.Errorf("ghpr will not run this gh command: gh %s", strings.Join(args, " "))
	}
	return exec1(ctx, dir, args, stdin)
}

func run(ctx context.Context, dir string, args ...string) (string, error) {
	if !readOnly(args) {
		return "", fmt.Errorf("ghpr will not run a gh command that writes: gh %s", strings.Join(args, " "))
	}
	return exec1(ctx, dir, args, nil)
}

// graphqlRead runs one of this package's own GraphQL queries.
//
// It does not go through run, because readOnly refuses -f: a mutation is
// handed to `gh api graphql` the same way a query is, and from the flags alone
// the two are indistinguishable. What distinguishes them here is that the
// query text is a constant in this file — nothing outside the package chooses
// it, and nothing is interpolated into it. The check below is the belt to that
// braces: whatever is passed, a mutation does not leave.
func graphqlRead(ctx context.Context, dir, query string) (string, error) {
	if strings.Contains(strings.ToLower(query), "mutation") {
		return "", errors.New("ghpr will not run a GraphQL mutation")
	}
	return exec1(ctx, dir, []string{"api", "graphql", "-f", "query=" + query}, nil)
}

// brief is the command for a log line. A GraphQL query is a whole screen of
// text, and the log viewer gives each record one row.
func brief(args []string) string {
	line := strings.Join(strings.Fields(strings.Join(args, " ")), " ")
	if len(line) > 160 {
		return line[:160] + "…"
	}
	return line
}

// noPullRequest is gh's answer when a branch simply has none open.
func noPullRequest(stderr string) bool {
	return strings.Contains(strings.ToLower(stderr), "no pull requests found")
}

func exec1(ctx context.Context, dir string, args []string, stdin []byte) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...) // #nosec G204 -- args are internally constructed
	cmd.Dir = dir
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	started := time.Now()
	err := cmd.Run()
	took := time.Since(started)

	if err != nil {
		// A branch with no pull request is the ordinary answer to a poll every
		// 30s, and warning about it buries everything else in the log.
		level := slog.LevelWarn
		if noPullRequest(stderr.String()) {
			level = slog.LevelDebug
		}
		slog.Log(ctx, level, "gh failed", "args", brief(args), "ms", took.Milliseconds(), "err", err, "stderr", strings.TrimSpace(stderr.String()))
		if errors.Is(err, exec.ErrNotFound) {
			return "", errors.New("gh not found in PATH; install the GitHub CLI to review pull requests")
		}
		return "", fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	slog.Debug("gh", "args", brief(args), "ms", took.Milliseconds(), "bytes", stdout.Len())
	return stdout.String(), nil
}
