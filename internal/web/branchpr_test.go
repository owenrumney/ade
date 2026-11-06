package web

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/owenrumney/ade/internal/ghpr"
)

func TestCountFailing(t *testing.T) {
	assert.Zero(t, countFailing(nil))
	assert.Equal(t, 2, countFailing([]ghpr.Check{
		{Bucket: "fail"}, {Bucket: "pass"}, {Bucket: "fail"}, {Bucket: "pending"},
	}))
}

func TestBranchPRChangedIgnoresTheTimestamp(t *testing.T) {
	base := &BranchPR{Number: 7, State: "OPEN", Checks: []ghpr.Check{{Name: "test", Bucket: "pass"}}}

	tests := []struct {
		name  string
		after *BranchPR
		want  bool
	}{
		{name: "identical but re-checked", after: &BranchPR{Number: 7, State: "OPEN",
			Checks: []ghpr.Check{{Name: "test", Bucket: "pass"}}, CheckedAt: time.Now()}},
		{name: "a check started failing", want: true, after: &BranchPR{Number: 7, State: "OPEN",
			Checks: []ghpr.Check{{Name: "test", Bucket: "fail"}}}},
		{name: "a check appeared", want: true, after: &BranchPR{Number: 7, State: "OPEN",
			Checks: []ghpr.Check{{Name: "test", Bucket: "pass"}, {Name: "lint", Bucket: "pass"}}}},
		{name: "merged", want: true, after: &BranchPR{Number: 7, State: "MERGED",
			Checks: []ghpr.Check{{Name: "test", Bucket: "pass"}}}},
		{name: "the pull request went away", want: true, after: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A poll that finds nothing new must not make every browser refetch.
			assert.Equal(t, tt.want, branchPRChanged(base, tt.after))
		})
	}
}

func TestStateCarriesTheBranchPR(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 7, Title: "Cart", Failing: 1,
		Checks: []ghpr.Check{{Name: "test", Bucket: "fail", Link: "https://ci/1"}}})

	rec := do(t, s, "GET", "/api/state", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"branchPr"`)
	assert.Contains(t, rec.Body.String(), "https://ci/1")
}
