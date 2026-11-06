package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/agents"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/review"
)

func TestReviewIsRefusedWhileARunIsInFlight(t *testing.T) {
	s := newTestServer(t)

	_, ok := s.agent.begin(context.Background(), kindCommitMessage)
	require.True(t, ok)
	defer s.agent.finish(nil)

	rec := do(t, s, "POST", "/api/review", `{"level":"medium"}`)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "a run is already going here")
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

	var said []string
	added := s.fileFindings([]review.Finding{
		{File: "a.go", Line: 3, Category: "correctness", Summary: "x is magic", FailureScenario: "always"},
		{File: "a.go", Line: 5, Category: "style", Summary: "near miss"},
		{File: "a.go", Line: 900, Category: "correctness", Summary: "way off"},
		{File: "ghost.go", Line: 1, Category: "correctness", Summary: "not in the diff"},
	}, comments.AuthorAgent, func(line string) { said = append(said, line) })
	assert.Len(t, added, 2, "only what the diff touches is filed")

	// One thread, not two: the near miss snaps onto line 3, where a
	// finding already is, so it joins it rather than opening a second.
	threads := s.ws.store.List()
	require.Len(t, threads, 1)
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

	// Only the change is under review: a point on code it did not write is
	// said, so nothing is lost silently, and not filed as an orphan.
	t.Run("a distant line and a file outside the diff are said, not filed", func(t *testing.T) {
		_, filed := bySummary["way off"]
		assert.False(t, filed)
		_, filed = bySummary["not in the diff"]
		assert.False(t, filed)
		assert.Contains(t, said, "a.go:900 — outside the diff, not filed")
		assert.Contains(t, said, "ghost.go:1 — outside the diff, not filed")
	})
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
			// b.go and c.go join the diff: only what it touches is filed.
			for _, name := range []string{"b.go", "c.go"} {
				require.NoError(t, os.WriteFile(filepath.Join(s.startRoot, name), []byte("package main\n\nvar y = 1\n"), 0o600))
			}
			commitEverything(t, s.startRoot, "more files")
			require.NoError(t, s.refresh(context.Background()))
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

// The route dropped its body, so all four sweep verbs cleared the lot.
func TestResetThreadsTakesOnlyWhatTheBodyNames(t *testing.T) {
	s := newTestServer(t)
	createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"still open"}`)
	done, err := s.ws.store.Add(comments.Thread{
		File: "a.go", Side: comments.SideRight, Line: 4,
		Comments: []comments.Comment{{Body: "fixed"}},
	})
	require.NoError(t, err)
	_, err = s.ws.store.SetResolved(done.ID, true)
	require.NoError(t, err)

	rec := do(t, s, "POST", "/api/threads/reset", `{"which":"resolved"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"removed":1`)

	left := s.ws.store.List()
	require.Len(t, left, 1, "the open comment is not a resolved one")
	assert.False(t, left[0].Resolved)
}

// "Remove every comment" removed every comment but the pull request's, and
// the import poll would have refiled those a minute later anyway.
func TestResetOfEverythingTakesThePullRequestsCommentsAndKeepsThemOut(t *testing.T) {
	s := newTestServer(t)
	createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"mine"}`)
	_, err := s.ws.store.Add(comments.Thread{
		File: "a.go", Side: comments.SideRight, Line: 3, ExternalID: "41",
		Comments: []comments.Comment{{Body: "from the pull request"}},
	})
	require.NoError(t, err)

	rec := do(t, s, "POST", "/api/threads/reset", `{"which":""}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"removed":2`)
	assert.Contains(t, rec.Body.String(), `"kept":0`)
	assert.Empty(t, s.ws.store.List())

	again := []ghpr.Thread{{Root: ghpr.ReviewComment{ID: 41, Path: "a.go", Side: "right", Line: 3, Body: "from the pull request"}}}
	assert.Zero(t, s.importComments(again), "the poll does not bring it back")
	assert.Empty(t, s.ws.store.List())
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

// Reviewers are handed the diff as ade holds it, so the base is fetched
// first: a stale origin/main puts main's own commits in front of them.
func TestReviewFetchesTheBaseFirst(t *testing.T) {
	s := newTestServer(t)
	moved := originWithMovedMain(t, s.startRoot)
	rec := do(t, s, "POST", "/api/review", `{"agents":["nobody-by-that-name"]}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "the fetch comes before the agents are looked up")
	assert.Equal(t, moved, originMain(t, s.startRoot))
}

// The test repo's only change is "var x = 2" on line 3 of a.go. A point on a
// line the change did not write used to be filed as an orphan.
func TestAFindingOffTheDiffIsSaidNotFiled(t *testing.T) {
	s := newTestServer(t)
	f := s.newFiler(agents.Agent{Name: "security", Reviews: true}, "", "")
	f.push("```ade-finding\n{\"file\": \"a.go\", \"line\": 3, \"summary\": \"on the change\"}\n```\n")
	f.push("```ade-finding\n{\"file\": \"a.go\", \"line\": 400, \"summary\": \"old code\"}\n```\n")
	f.push("```ade-finding\n{\"file\": \"elsewhere.go\", \"line\": 1, \"summary\": \"not in the diff\"}\n```\n")

	require.Len(t, s.ws.store.List(), 1, "only the point on the change is a comment")
	assert.Equal(t, 3, s.ws.store.List()[0].Line)
	assert.Len(t, f.aside, 2)
}

func TestAWholeBranchReviewWritesItsFindingsToAFile(t *testing.T) {
	s := newTestServer(t)
	batch := s.newBatch([]string{"security"}, true)
	undo := s.reviewBegan(batch, []string{"security"})
	defer undo()
	f := s.newFiler(agents.Agent{Name: "security", Reviews: true}, "", batch)
	f.whole = true
	f.push("```ade-finding\n{\"file\": \"elsewhere.go\", \"line\": 9, \"severity\": \"high\", \"summary\": \"anywhere counts\"}\n```\n")
	s.batchDrained(batch)

	assert.Empty(t, s.ws.store.List(), "nothing lands on the diff")
	raw, err := os.ReadFile(filepath.Join(s.ws.gitDir, "ade", foundFile))
	require.NoError(t, err)
	var got struct {
		Scope    string `json:"scope"`
		Findings []struct {
			File string `json:"file"`
			Line int    `json:"line"`
			By   string `json:"by"`
		} `json:"findings"`
	}
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, "branch", got.Scope)
	require.Len(t, got.Findings, 1)
	assert.Equal(t, "elsewhere.go", got.Findings[0].File)
	assert.Equal(t, "security", got.Findings[0].By)
	run := s.currentReviewRun()
	require.NotNil(t, run)
	assert.False(t, run.Running)
	assert.Contains(t, run.Summary, foundFile)
}

func TestAWholeBranchReviewNamesItsReviewers(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/review", `{"scope":"branch"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "names its reviewers")
}

func commitEverything(t *testing.T, dir, message string) {
	t.Helper()
	for _, args := range [][]string{{"add", "."}, {"commit", "-qm", message}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
}
