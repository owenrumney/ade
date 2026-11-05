package web

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ReviewRun is a review by named agents as the Diff shows it: each reviewer,
// then the stage the review has reached.
type ReviewRun struct {
	Reviewers []ReviewerRun `json:"reviewers"`
	// Stage is what it is doing now. Summary is what came of it.
	Stage     string    `json:"stage"`
	Summary   string    `json:"summary"`
	Running   bool      `json:"running"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
}

// ReviewerRun is one reviewer in it.
type ReviewerRun struct {
	Name string `json:"name"`
	// State is running, done, failed or stopped.
	State string `json:"state"`
	// Doing is its last step while it runs. Why is what went wrong.
	Doing   string    `json:"doing"`
	Why     string    `json:"why"`
	Found   int       `json:"found"`
	EndedAt time.Time `json:"endedAt"`
}

const (
	reviewerRunning = "running"
	reviewerDone    = "done"
	reviewerFailed  = "failed"
	reviewerStopped = "stopped"
)

// reviewBegan starts the record a new review is followed by. What it
// returns puts the last one back, for a review refused before it ran.
func (s *Server) reviewBegan(batch string, names []string) (undo func()) {
	run := &ReviewRun{Running: true, StartedAt: time.Now()}
	for _, name := range names {
		run.Reviewers = append(run.Reviewers, ReviewerRun{Name: name, State: reviewerRunning})
	}
	run.Stage = reviewersLeft(run)
	s.mu.Lock()
	was, wasBatch := s.reviewRun, s.reviewRunBatch
	s.reviewRun, s.reviewRunBatch = run, batch
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.reviewRunBatch == batch {
			s.reviewRun, s.reviewRunBatch = was, wasBatch
		}
	}
}

// reviewerDoing is a reviewer's latest step. The step's own broadcast carries it.
func (s *Server) reviewerDoing(name, step string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if one := s.reviewerIn(s.reviewRunBatch, name); one != nil && one.State == reviewerRunning {
		one.Doing = step
	}
}

// reviewerEnded is a reviewer's run over: what it found, or why it did not finish.
func (s *Server) reviewerEnded(ctx context.Context, batch, name string, found int, err error) {
	s.mu.Lock()
	one := s.reviewerIn(batch, name)
	if one == nil || one.State != reviewerRunning {
		s.mu.Unlock()
		return
	}
	one.Doing, one.Found, one.EndedAt = "", found, time.Now()
	switch {
	case err == nil:
		one.State = reviewerDone
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		one.State, one.Why = reviewerStopped, "ran out of time"
	case ctx.Err() != nil:
		one.State = reviewerStopped
	default:
		one.State, one.Why = reviewerFailed, err.Error()
	}
	s.reviewRun.Stage = reviewersLeft(s.reviewRun)
	s.mu.Unlock()
	s.hub.Broadcast("state", nil)
}

// reviewStage is the review moving on to what follows the reviewers.
func (s *Server) reviewStage(batch, stage string) {
	s.mu.Lock()
	if s.reviewRun == nil || s.reviewRunBatch != batch || !s.reviewRun.Running {
		s.mu.Unlock()
		return
	}
	s.reviewRun.Stage = stage
	s.mu.Unlock()
	s.hub.Broadcast("state", nil)
}

// reviewFiled is the comments on the diff. A rewrite still running keeps the review open.
func (s *Server) reviewFiled(batch, summary string, rewriting int) {
	s.mu.Lock()
	if s.reviewRun != nil && s.reviewRunBatch == batch {
		s.reviewRun.Summary = summary
	}
	s.mu.Unlock()
	if rewriting == 0 {
		s.reviewEnded(batch)
		return
	}
	s.reviewStage(batch, fmt.Sprintf("Putting %d comment(s) in your words", rewriting))
}

// reviewEnded closes the record. It stays to be read until the next review.
func (s *Server) reviewEnded(batch string) {
	s.mu.Lock()
	if s.reviewRun == nil || s.reviewRunBatch != batch || !s.reviewRun.Running {
		s.mu.Unlock()
		return
	}
	s.reviewRun.Running, s.reviewRun.Stage, s.reviewRun.EndedAt = false, "", time.Now()
	s.mu.Unlock()
	s.hub.Broadcast("state", nil)
}

// currentReviewRun is a copy for the page, or nil when no review has run here.
func (s *Server) currentReviewRun() *ReviewRun {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.reviewRun == nil {
		return nil
	}
	run := *s.reviewRun
	run.Reviewers = append([]ReviewerRun(nil), s.reviewRun.Reviewers...)
	return &run
}

// reviewerIn finds a reviewer of the review this batch is. Callers hold s.mu.
func (s *Server) reviewerIn(batch, name string) *ReviewerRun {
	if s.reviewRun == nil || batch == "" || s.reviewRunBatch != batch {
		return nil
	}
	for i := range s.reviewRun.Reviewers {
		if s.reviewRun.Reviewers[i].Name == name {
			return &s.reviewRun.Reviewers[i]
		}
	}
	return nil
}

// reviewersLeft is the first stage in words: how many reviewers are through.
func reviewersLeft(run *ReviewRun) string {
	through := 0
	for _, one := range run.Reviewers {
		if one.State != reviewerRunning {
			through++
		}
	}
	return fmt.Sprintf("%d of %d reviewers finished", through, len(run.Reviewers))
}
