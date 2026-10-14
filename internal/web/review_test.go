package web

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/review"
)

func TestReviewQueuesBehindAConcurrentRun(t *testing.T) {
	s := newTestServer(t)

	_, ok := s.agent.begin(context.Background(), "resolve")
	require.True(t, ok)
	defer s.agent.finish(nil)

	rec := do(t, s, "POST", "/api/review", `{"level":"medium"}`)
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), `"queued":1`)

	// The level is the ask, so a review at another depth is another entry.
	rec = do(t, s, "POST", "/api/review", `{"level":"medium"}`)
	assert.Contains(t, rec.Body.String(), `"queued":1`)
	rec = do(t, s, "POST", "/api/review", `{"level":"high"}`)
	assert.Contains(t, rec.Body.String(), `"queued":2`)
}

func TestReviewRejectsAnEmptyDiff(t *testing.T) {
	s := newTestServer(t)

	s.mu.Lock()
	s.diff.Files = nil
	s.mu.Unlock()

	rec := do(t, s, "POST", "/api/review", `{}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "the diff is empty")
}

func TestFileFindings(t *testing.T) {
	// The test repo's only change is "var x = 2" on new-side line 3.
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	added := s.fileFindings([]review.Finding{
		{File: "a.go", Line: 3, Category: "correctness", Summary: "x is magic", FailureScenario: "always"},
		{File: "a.go", Line: 5, Category: "style", Summary: "near miss"},
		{File: "a.go", Line: 900, Category: "correctness", Summary: "way off"},
		{File: "ghost.go", Line: 1, Category: "correctness", Summary: "not in the diff"},
	}, comments.AuthorAgent, s.agent.step)
	assert.Len(t, added, 4, "no finding should be silently dropped")

	// Three threads, not four: the near miss snaps onto line 3, where a
	// finding already is, so it joins it rather than opening a second.
	threads := s.ws.store.List()
	require.Len(t, threads, 3)
	bySummary := map[string]comments.Thread{}
	for _, thread := range threads {
		for _, comment := range thread.Comments {
			bySummary[comment.Body] = thread
		}
	}

	t.Run("an exact line anchors and keeps its category", func(t *testing.T) {
		got := bySummary["x is magic\n\nFails when: always"]
		assert.Equal(t, 3, got.Line)
		assert.False(t, got.Orphaned)
		assert.Equal(t, "correctness", got.Category)
		assert.Equal(t, comments.AuthorAgent, got.Comments[0].Author)
		assert.Equal(t, comments.SideRight, got.Side)
	})

	t.Run("a near miss snaps onto the diff and joins what is there", func(t *testing.T) {
		got := bySummary["near miss"]
		assert.Equal(t, 3, got.Line, "line 5 snaps to the nearest diff line, 3")
		assert.False(t, got.Orphaned)
		require.Len(t, got.Comments, 2)
		assert.Equal(t, "near miss", got.Comments[1].Body, "filed as a reply, not a second thread")
	})

	t.Run("a distant line is kept but orphaned", func(t *testing.T) {
		got := bySummary["way off"]
		assert.True(t, got.Orphaned)
		assert.Equal(t, 900, got.Line, "the reported line is preserved")
	})

	t.Run("a file outside the diff is kept but orphaned", func(t *testing.T) {
		got := bySummary["not in the diff"]
		assert.True(t, got.Orphaned)
		assert.Equal(t, "ghost.go", got.File)
	})
}

func TestFileFindingsIsResolvable(t *testing.T) {
	s := newTestServer(t)
	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	s.agent.finish(nil)

	s.fileFindings([]review.Finding{{File: "a.go", Line: 3, Summary: "fix me"}}, comments.AuthorAgent, s.agent.step)

	// A filed finding must be picked up by Resolve like any hand-written comment.
	assert.Len(t, s.threadsToResolve(nil), 1)
}

func TestReconcile(t *testing.T) {
	// Distinct files, so each finding is its own thread: what is under test
	// here is the reconciliation, not the merging of findings on one line.
	real := review.Finding{File: "a.go", Line: 3, Category: "correctness", Summary: "x is magic"}
	dropped := review.Finding{File: "b.go", Line: 3, Category: "correctness", Summary: "second thoughts"}
	late := review.Finding{File: "c.go", Line: 3, Category: "style", Summary: "only in the final list"}

	tests := []struct {
		name       string
		streamed   []review.Finding
		final      []review.Finding
		structured bool
		want       int
		wantOpen   []string
	}{
		{
			name:       "final list adds what was never streamed",
			streamed:   []review.Finding{real},
			final:      []review.Finding{real, late},
			structured: true,
			want:       2,
			wantOpen:   []string{"x is magic", "only in the final list"},
		},
		{
			name:       "a finding the reviewer dropped is withdrawn",
			streamed:   []review.Finding{real, dropped},
			final:      []review.Finding{real},
			structured: true,
			want:       1,
			wantOpen:   []string{"x is magic"},
		},
		{
			name:       "an unreadable reply keeps the streamed findings",
			streamed:   []review.Finding{real, dropped},
			structured: false,
			want:       2,
			wantOpen:   []string{"x is magic", "second thoughts"},
		},
		{
			name:       "a clean review files nothing",
			structured: true,
			want:       0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t)
			_, ok := s.agent.begin(context.Background(), "review")
			require.True(t, ok)
			defer s.agent.finish(nil)

			streamed := s.fileFindings(tt.streamed, comments.AuthorAgent, s.agent.step)
			survived := s.reconcile(tt.final, streamed, tt.structured)

			// The findings themselves come back now, so a following step can be
			// told what the review actually said.
			assert.Len(t, survived, tt.want)
			for _, f := range survived {
				assert.NotEmpty(t, f.File, "a returned finding must be usable")
			}

			var open []string
			for _, thread := range s.ws.store.List() {
				open = append(open, thread.Comments[0].Body)
			}
			for _, want := range tt.wantOpen {
				assert.Contains(t, open, want)
			}
			assert.Len(t, open, tt.want)
		})
	}
}

func TestResetThreadsClearsTheReview(t *testing.T) {
	s := newTestServer(t)
	createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"mine"}`)
	s.fileFindings([]review.Finding{{File: "a.go", Line: 3, Summary: "theirs"}}, comments.AuthorAgent, func(string) {})
	require.NotEmpty(t, s.ws.store.List())

	rec := do(t, s, "POST", "/api/threads/reset", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"removed"`)
	assert.Empty(t, s.ws.store.List())

	t.Run("resetting nothing is not an error", func(t *testing.T) {
		rec := do(t, s, "POST", "/api/threads/reset", "")
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), `"removed":0`)
	})
}

func TestResetThreadsRefusesWhileTheAgentRuns(t *testing.T) {
	s := newTestServer(t)
	createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"mine"}`)

	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	rec := do(t, s, "POST", "/api/threads/reset", "")
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Len(t, s.ws.store.List(), 1, "a run part way through filing keeps its comments")
}
