package web

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAReviewIsFollowedReviewerByReviewerThenStageByStage(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	assert.Nil(t, s.currentReviewRun(), "no review has run here")

	s.reviewBegan("b1", []string{"security", "staff-reviewer"})
	run := s.currentReviewRun()
	require.NotNil(t, run)
	assert.True(t, run.Running)
	assert.Equal(t, "0 of 2 reviewers finished", run.Stage)

	s.reviewerDoing("security", "Read internal/web/web.go")
	assert.Equal(t, "Read internal/web/web.go", s.currentReviewRun().Reviewers[0].Doing)

	s.reviewerEnded(ctx, "b1", "security", 3, nil)
	s.reviewerEnded(ctx, "b1", "staff-reviewer", 0, errors.New("exit status 1"))
	run = s.currentReviewRun()
	assert.Equal(t, "2 of 2 reviewers finished", run.Stage)
	assert.Equal(t, ReviewerRun{Name: "security", State: reviewerDone, Found: 3, EndedAt: run.Reviewers[0].EndedAt}, run.Reviewers[0])
	assert.Equal(t, reviewerFailed, run.Reviewers[1].State)
	assert.Equal(t, "exit status 1", run.Reviewers[1].Why)

	// The same reviewer's cross-check ends too, and is not a second verdict.
	s.reviewerEnded(ctx, "b1", "security", 0, errors.New("late"))
	assert.Equal(t, reviewerDone, s.currentReviewRun().Reviewers[0].State)

	s.reviewStage("b1", "Grouping findings that make the same point")
	assert.Equal(t, "Grouping findings that make the same point", s.currentReviewRun().Stage)

	s.reviewFiled("b1", "3 finding(s) from 2 reviewer(s) → 2 point(s).", 2)
	run = s.currentReviewRun()
	assert.True(t, run.Running, "the rewrite is still going")
	assert.Equal(t, "Putting 2 comment(s) in your words", run.Stage)

	s.reviewEnded("b1")
	run = s.currentReviewRun()
	assert.False(t, run.Running)
	assert.Empty(t, run.Stage)
	assert.Equal(t, "3 finding(s) from 2 reviewer(s) → 2 point(s).", run.Summary)
	assert.False(t, run.EndedAt.IsZero())
}

func TestAStoppedReviewerIsNotAFailedOne(t *testing.T) {
	s := newTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s.reviewBegan("b1", []string{"security"})
	s.reviewerEnded(ctx, "b1", "security", 1, ctx.Err())

	one := s.currentReviewRun().Reviewers[0]
	assert.Equal(t, reviewerStopped, one.State)
	assert.Empty(t, one.Why)
	assert.Equal(t, 1, one.Found)
}

func TestAReviewWithNothingToRewriteEndsWhenItIsFiled(t *testing.T) {
	s := newTestServer(t)

	s.reviewBegan("b1", []string{"security"})
	s.reviewFiled("b1", "The reviewers found nothing to file.", 0)

	run := s.currentReviewRun()
	assert.False(t, run.Running)
	assert.Equal(t, "The reviewers found nothing to file.", run.Summary)
}

func TestAnOlderReviewDoesNotWriteOverTheOneThatReplacedIt(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	s.reviewBegan("b1", []string{"security"})
	s.reviewBegan("b2", []string{"tester"})

	s.reviewerEnded(ctx, "b1", "security", 4, nil)
	s.reviewStage("b1", "Filing the comments")
	s.reviewEnded("b1")

	run := s.currentReviewRun()
	assert.True(t, run.Running)
	assert.Equal(t, "tester", run.Reviewers[0].Name)
	assert.Equal(t, "0 of 1 reviewers finished", run.Stage)
}

func TestARefusedReviewPutsTheLastOneBack(t *testing.T) {
	s := newTestServer(t)

	s.reviewBegan("b1", []string{"security"})
	s.reviewFiled("b1", "1 finding(s) from 1 reviewer(s) → 1 point(s).", 0)

	undo := s.reviewBegan("b2", []string{"tester"})
	undo()

	run := s.currentReviewRun()
	assert.Equal(t, "security", run.Reviewers[0].Name)
	assert.False(t, run.Running)
}
