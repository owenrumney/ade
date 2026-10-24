package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/ghpr"
)

// draftOnGitHub stands in for the pull request's pending review, recording
// what ade asked GitHub to do with it.
type draftOnGitHub struct {
	review   string
	added    []ghpr.InlineComment
	removed  []string
	event    string
	body     string
	submits  int
	addFails error
}

func stubDraft(t *testing.T, on *draftOnGitHub) {
	t.Helper()
	was := []any{pendingReview, pullRequestID, startDraft, addToDraft, removeFromDraft, submitDraft}
	wasDiff := prDiffText
	t.Cleanup(func() { prDiffText = wasDiff })
	// Every line of a.go is one GitHub would take, unless a test says otherwise.
	prDiffText = func(context.Context, string, int) (string, error) { return "", errors.New("no gh here") }
	t.Cleanup(func() {
		pendingReview = was[0].(func(context.Context, string, string, string, int) (ghpr.Draft, error))
		pullRequestID = was[1].(func(context.Context, string, string, string, int) (string, error))
		startDraft = was[2].(func(context.Context, string, string, ghpr.InlineComment) (string, string, error))
		addToDraft = was[3].(func(context.Context, string, string, ghpr.InlineComment) (string, error))
		removeFromDraft = was[4].(func(context.Context, string, string) error)
		submitDraft = was[5].(func(context.Context, string, string, string, string) error)
	})

	wasRepo := repoOf
	repoOf = func(context.Context, string) (string, string, error) { return "owenrumney", "ade", nil }
	t.Cleanup(func() { repoOf = wasRepo })

	pendingReview = func(context.Context, string, string, string, int) (ghpr.Draft, error) {
		return ghpr.Draft{ID: on.review, Comments: len(on.added)}, nil
	}
	pullRequestID = func(context.Context, string, string, string, int) (string, error) {
		return "PR_NODE", nil
	}
	startDraft = func(_ context.Context, _, _ string, c ghpr.InlineComment) (string, string, error) {
		if on.addFails != nil {
			return "", "", on.addFails
		}
		on.review = "REVIEW1"
		on.added = append(on.added, c)
		return on.review, "COMMENT1", nil
	}
	addToDraft = func(_ context.Context, _, _ string, c ghpr.InlineComment) (string, error) {
		if on.addFails != nil {
			return "", on.addFails
		}
		on.added = append(on.added, c)
		return "COMMENT2", nil
	}
	removeFromDraft = func(_ context.Context, _, id string) error {
		on.removed = append(on.removed, id)
		return nil
	}
	submitDraft = func(_ context.Context, _, review, event, body string) error {
		on.submits++
		on.event, on.body = event, body
		if review != on.review {
			return errors.New("finished the wrong review")
		}
		return nil
	}
}

func converted(t *testing.T, s *Server, line int, words string) comments.Thread {
	t.Helper()
	thread, err := s.space().store.Add(comments.Thread{
		File: "a.go", Side: "right", Line: line,
		Comments: []comments.Comment{{Author: "security", Body: "the finding, in full"}},
	})
	require.NoError(t, err)
	_, err = s.space().store.SetOutgoing(thread.ID, words)
	require.NoError(t, err)
	return thread
}

func draftState(t *testing.T, s *Server) DraftState {
	t.Helper()
	rec := do(t, s, http.MethodGet, "/api/review/draft", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got DraftState
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	return got
}

// The first comment opens the pending review; the rest join the same one.
func TestCommentsJoinOneDraftReview(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})
	on := &draftOnGitHub{}
	stubDraft(t, on)

	converted(t, s, 3, "this reads oddly")
	converted(t, s, 30, "and so does this")

	rec := do(t, s, http.MethodPost, "/api/review/draft", `{"confirm":7}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Len(t, on.added, 2)
	assert.Equal(t,
		"![Medium](https://img.shields.io/badge/Medium-orange)\n\nthis reads oddly",
		on.added[0].Body, "a thread with no severity set goes out as medium")
	assert.Equal(t, 2, draftState(t, s).InDraft)
	assert.Equal(t, 0, draftState(t, s).Ready, "both are waiting on GitHub now")
}

// A pending review is visible only to its author, so nothing in it counts as
// sent until the verdict. `ade done` leans on this.
func TestNothingIsSentUntilTheReviewIsFinished(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})
	on := &draftOnGitHub{}
	stubDraft(t, on)
	one := converted(t, s, 3, "this reads oddly")

	require.Equal(t, http.StatusOK,
		do(t, s, http.MethodPost, "/api/review/draft", `{"confirm":7}`).Code)

	got, err := s.space().store.Get(one.ID)
	require.NoError(t, err)
	assert.True(t, got.Drafted())
	assert.False(t, got.Sent())

	rec := do(t, s, http.MethodPost, "/api/review/finish",
		`{"confirm":7,"event":"approve","body":"Approved with 1 comment"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, ghpr.EventApprove, on.event)
	assert.Equal(t, "Approved with 1 comment", on.body)

	got, err = s.space().store.Get(one.ID)
	require.NoError(t, err)
	assert.True(t, got.Sent(), "the author can see it now")
}

func TestOnlyAVerdictFinishesTheReview(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})
	stubDraft(t, &draftOnGitHub{review: "REVIEW1"})

	for _, body := range []string{`{"confirm":7,"event":"dismiss"}`, `{"confirm":7}`, `{"confirm":7,"event":"merge"}`} {
		rec := do(t, s, http.MethodPost, "/api/review/finish", body)
		assert.Equalf(t, http.StatusBadRequest, rec.Code, "accepted %s", body)
	}
	rec := do(t, s, http.MethodPost, "/api/review/finish", `{"confirm":8,"event":"approve"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "has to name it")
}

func TestFinishingNeedsADraftToFinish(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})
	stubDraft(t, &draftOnGitHub{})

	rec := do(t, s, http.MethodPost, "/api/review/finish", `{"confirm":7,"event":"approve"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no draft review")
}

// Taking one back out removes it from GitHub's draft, not only from here.
func TestTakingACommentOutOfTheDraft(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})
	on := &draftOnGitHub{}
	stubDraft(t, on)
	one := converted(t, s, 3, "this reads oddly")
	require.Equal(t, http.StatusOK,
		do(t, s, http.MethodPost, "/api/review/draft", `{"confirm":7}`).Code)

	rec := do(t, s, http.MethodDelete, "/api/review/draft/"+one.ID, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"COMMENT1"}, on.removed)

	got, err := s.space().store.Get(one.ID)
	require.NoError(t, err)
	assert.False(t, got.Drafted())
	assert.Equal(t, 1, draftState(t, s).Ready, "back to waiting here")
}

// A finding nobody converted has never been on screen as something that would
// be posted, so it is not quietly swept into the draft.
func TestOnlyWhatYouHaveReadJoinsTheDraft(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})
	stubDraft(t, &draftOnGitHub{})
	_, err := s.space().store.Add(comments.Thread{
		File: "a.go", Side: "right", Line: 3,
		Comments: []comments.Comment{{Author: "security", Body: "terse finding"}},
	})
	require.NoError(t, err)

	rec := do(t, s, http.MethodPost, "/api/review/draft", `{"confirm":7}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "convert a finding first")
	assert.Equal(t, 1, draftState(t, s).NotGoing)
}

// gh pr review --approve starts a new review, which would strand the draft
// and post an empty one beside it.
func TestApprovingFinishesTheDraftRatherThanStartingAnother(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})
	on := &draftOnGitHub{}
	stubDraft(t, on)
	one := converted(t, s, 3, "this reads oddly")
	require.Equal(t, http.StatusOK,
		do(t, s, http.MethodPost, "/api/review/draft", `{"confirm":7}`).Code)

	err := s.giveVerdict(t.Context(), s.space().homeRoot, 7, "Approved with 1 comment", verdict{
		word:  "approved",
		event: ghpr.EventApprove,
		send: func(context.Context, string, int, string) error {
			return errors.New("started a second review")
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, on.submits)
	assert.Equal(t, ghpr.EventApprove, on.event)

	got, err := s.space().store.Get(one.ID)
	require.NoError(t, err)
	assert.True(t, got.Sent())
}

// The severity is the first thing the author sees, as a badge GitHub draws
// above the words. It is added as the comment goes, so it follows a severity
// that changed after the words were written.
func TestACommentGoesWithItsSeverityOnTop(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})
	on := &draftOnGitHub{}
	stubDraft(t, on)

	thread, err := s.space().store.Add(comments.Thread{
		File: "a.go", Side: "right", Line: 3, Severity: comments.SeverityCritical,
		Comments: []comments.Comment{{Author: "security", Body: "the finding, in full"}},
	})
	require.NoError(t, err)
	_, err = s.space().store.SetOutgoing(thread.ID, "this reads oddly")
	require.NoError(t, err)

	require.Equal(t, http.StatusOK,
		do(t, s, http.MethodPost, "/api/review/draft", `{"confirm":7}`).Code)
	require.Len(t, on.added, 1)
	assert.Equal(t,
		"![Critical](https://img.shields.io/badge/Critical-8B0000)\n\nthis reads oddly",
		on.added[0].Body)
}

// A comment on a line the diff does not touch is one GitHub takes and does
// not make. Measured on infracost/coast#1248: three of five vanished.
func TestACommentGitHubWillNotTakeIsLeftWhereItWas(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})
	on := &draftOnGitHub{}
	stubDraft(t, on)
	refuse := errors.New("GitHub took no comment on a.go:30: a review comment can only go on a line the pull request's diff touches")
	addToDraft = func(_ context.Context, _, _ string, c ghpr.InlineComment) (string, error) {
		if c.Line == 30 {
			return "", refuse
		}
		on.added = append(on.added, c)
		return "COMMENT2", nil
	}

	converted(t, s, 3, "this reads oddly")
	refused := converted(t, s, 30, "and this is nowhere near the diff")
	converted(t, s, 40, "this one is fine")

	rec := do(t, s, http.MethodPost, "/api/review/draft", `{"confirm":7}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var said struct {
		Drafted int      `json:"drafted"`
		Refused []string `json:"refused"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &said))

	// The one GitHub would not take is named, and the ones after it still go.
	assert.Equal(t, 2, said.Drafted)
	require.Len(t, said.Refused, 1)
	assert.Contains(t, said.Refused[0], "a.go:30")
	assert.Equal(t, 2, draftState(t, s).InDraft)

	// Still ready here, which is the only true state: it is not on GitHub,
	// so finishing the review must not mark it sent.
	got, err := s.space().store.Get(refused.ID)
	require.NoError(t, err)
	assert.False(t, got.Drafted())
	assert.Empty(t, got.SentAt)
	assert.Equal(t, 1, draftState(t, s).Ready)
}

// prDiff is a pull request's diff as gh prints it, touching only the lines
// given on the right side of a.go.
func prDiff(lines ...int) string {
	out := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n"
	for _, line := range lines {
		out += fmt.Sprintf("@@ -%d,1 +%d,1 @@\n+line %d\n", line, line, line)
	}
	return out
}

// The checkout a review is written in can be behind the pull request — a
// force-push, or a branch that moved while the review ran — and then the
// lines the comments name are not the lines GitHub is showing. Asking it
// anyway throws the comment away, so the diff is read first.
func TestACommentOffThePullRequestsDiffIsRefusedBeforeGitHubIsAsked(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})
	on := &draftOnGitHub{}
	stubDraft(t, on)
	prDiffText = func(context.Context, string, int) (string, error) { return prDiff(3, 40), nil }

	converted(t, s, 3, "this reads oddly")
	away := converted(t, s, 30, "and this is nowhere near the diff")
	converted(t, s, 40, "this one is fine")

	rec := do(t, s, http.MethodPost, "/api/review/draft", `{"confirm":7}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var said struct {
		Drafted int      `json:"drafted"`
		Refused []string `json:"refused"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &said))
	assert.Equal(t, 2, said.Drafted)
	require.Len(t, said.Refused, 1)
	assert.Contains(t, said.Refused[0], "a.go:30")
	assert.Len(t, on.added, 2, "GitHub was never asked about the third")

	got, err := s.space().store.Get(away.ID)
	require.NoError(t, err)
	assert.False(t, got.Drafted())
	assert.Empty(t, got.SentAt)
}

// A review checkout that is not at the pull request's head can only produce
// comments on lines GitHub is not showing, so none of them are sent.
func TestADraftFromACheckoutBehindTheHeadIsRefused(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN", HeadSHA: "677bbd0000000000000000000000000000000000"})
	on := &draftOnGitHub{}
	stubDraft(t, on)
	converted(t, s, 3, "this reads oddly")

	rec := do(t, s, http.MethodPost, "/api/review/draft", `{"confirm":7}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "gh pr checkout 7")
	assert.Empty(t, on.added)
	assert.Equal(t, 1, draftState(t, s).Ready)
}
