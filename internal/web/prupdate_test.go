package web

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
)

func TestEditPRNeedsAPullRequest(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "PUT", "/api/pr", `{"title":"new","body":"b"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no pull request")
}

func TestEditPRNeedsATitle(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN", Title: "before"})

	rec := do(t, s, "PUT", "/api/pr", `{"title":"   ","body":"b"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "needs a title")
}

func TestEditPRRefusesAClosedPullRequest(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "MERGED", Title: "before"})

	rec := do(t, s, "PUT", "/api/pr", `{"title":"after","body":"b"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "merged")
}

func TestMergeNeedsAPullRequest(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/pr/merge", `{"method":"rebase"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no pull request")
}

func TestMergeRejectsAnUnknownMethod(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN"})

	// Squash is not offered, and asking for it anyway must not quietly become a
	// merge commit.
	rec := do(t, s, "POST", "/api/pr/merge", `{"method":"squash"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "unknown merge method")
}

func TestMergeRefusesWhatItAlreadyKnowsIsBlocked(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "MERGED", MergeBlocked: "this pull request is merged"})

	rec := do(t, s, "POST", "/api/pr/merge", `{"method":"rebase"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "cannot be merged")
}

func TestApproveNeedsAPullRequest(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/pr/approve", `{}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no pull request")
}

func TestApproveRefusesWhatItAlreadyKnowsIsBlocked(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{
		Number: 7, State: "OPEN", Author: "owen",
		ApproveBlocked: "GitHub does not let you approve your own pull request",
	})

	// Answered without a round trip, and without the agent ever being involved.
	rec := do(t, s, "POST", "/api/pr/approve", `{"confirm":7}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "your own pull request")
}

func TestApprovingIsReachableOnlyByPressingTheButton(t *testing.T) {
	// The one place ade speaks to a reviewer. It must stay something you do:
	// no prompt, no tool list and no agent run may name it.
	for _, dir := range []string{"../resolve", "../review", "../reply", "../checks", "../design", "../explore"} {
		matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
		require.NoError(t, err)
		for _, path := range matches {
			body, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.NotContainsf(t, string(body), "pr review",
				"%s mentions approving; that is yours to press", path)
			assert.NotContainsf(t, string(body), "--approve",
				"%s mentions approving; that is yours to press", path)
		}
	}
}

func TestApprovingHasToNameThePullRequest(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN", Author: "someone"})

	// A bare POST to this path once approved whatever happened to be open,
	// which is how a colleague's pull request got approved by a smoke test.
	for _, body := range []string{`{}`, `{"body":"nice"}`, `{"confirm":0}`, `{"confirm":8}`} {
		rec := do(t, s, "POST", "/api/pr/approve", body)
		assert.Equalf(t, http.StatusBadRequest, rec.Code, "accepted %s", body)
		assert.Contains(t, rec.Body.String(), "has to name it")
	}
}

func TestRequestChangesNeedsAPullRequest(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/pr/request-changes", `{}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no pull request")
}

func TestRequestChangesRefusesWhatItAlreadyKnowsIsBlocked(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{
		Number: 7, State: "OPEN", Author: "owen",
		RequestChangesBlocked: "GitHub does not let you review your own pull request",
	})

	rec := do(t, s, "POST", "/api/pr/request-changes", `{"confirm":7,"body":"no"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "your own pull request")
}

func TestRequestingChangesHasToNameThePullRequest(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, State: "OPEN", Author: "someone"})

	for _, body := range []string{`{}`, `{"body":"no"}`, `{"confirm":8,"body":"no"}`} {
		rec := do(t, s, "POST", "/api/pr/request-changes", body)
		assert.Equalf(t, http.StatusBadRequest, rec.Code, "accepted %s", body)
		assert.Contains(t, rec.Body.String(), "has to name it")
	}
}
