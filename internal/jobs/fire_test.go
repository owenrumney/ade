package jobs

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/limits"
)

// job is one definition, parsed, so the schedule is real.
func job(t *testing.T, body string) Job {
	t.Helper()
	out, err := Parse("/tmp/j.md", ScopeUser, []byte(body))
	require.NoError(t, err)
	return out
}

func fresh(t *testing.T) Job {
	return job(t, "---\nname: nightly\nschedule: \"0 2 * * *\"\n---\n\n/catchup\n")
}

func summary(t *testing.T) Job {
	return job(t, "---\nname: keepup\nschedule: \"0 2 * * *\"\ncontinuity: summary\n---\n\n/catchup\n")
}

func answering(text string) Runner {
	return func(context.Context, Request) (Result, error) {
		return Result{Text: text, Session: "sess-1", CostUSD: 0.02}, nil
	}
}

func TestAFireRecordsAnOutcomeAndTheNextDue(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	j := fresh(t)

	out := Fire(context.Background(), j, "/repo", answering("two checks failed overnight"))

	assert.Equal(t, "two checks failed overnight", out.Report)
	assert.Equal(t, "sess-1", out.Session)
	assert.Equal(t, "/repo", out.Dir)
	assert.Empty(t, out.Error)

	state := ReadState(j)
	require.Len(t, state.Outcomes, 1)
	assert.Equal(t, "sess-1", state.Session)
	assert.False(t, state.NextDue.IsZero(), "a fired job knows when it is next due")
}

func TestAFailedFireIsAnOutcomeRatherThanASilence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	j := fresh(t)

	out := Fire(context.Background(), j, "/repo", func(context.Context, Request) (Result, error) {
		return Result{}, errors.New("claude failed: exit 1")
	})

	assert.Contains(t, out.Error, "exit 1")
	assert.Equal(t, "claude failed: exit 1", out.Verdict())
	assert.False(t, ReadState(j).NextDue.IsZero(), "a failure still advances the clock")
}

func TestTwoConsecutiveFailuresShowAJobAsFailing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	j := fresh(t)
	bad := func(context.Context, Request) (Result, error) { return Result{}, errors.New("nope") }

	Fire(context.Background(), j, "/repo", bad)
	assert.False(t, ReadState(j).Failing(), "one is a bad night")
	Fire(context.Background(), j, "/repo", bad)
	assert.True(t, ReadState(j).Failing(), "two is a job that stopped working")

	Fire(context.Background(), j, "/repo", answering("all clear"))
	assert.False(t, ReadState(j).Failing())
}

func TestNothingToReportReachesNoInbox(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	j := fresh(t)

	out := Fire(context.Background(), j, "/repo", answering("Nothing to report."))

	assert.Empty(t, out.Report, "a daily nothing is a daily thing to dismiss")
	assert.Equal(t, Nothing, out.Verdict())
	assert.Len(t, ReadState(j).Outcomes, 1, "it is still in the job's own history")
}

func TestASummaryFireKeepsTheHandoverAndStripsItFromTheReport(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	j := summary(t)

	reply := "PR 41 went green.\n\n```" + HandoverFence + "\nPR 41 was reported on 10 Sep.\n```"
	out := Fire(context.Background(), j, "/repo", answering(reply))

	assert.Equal(t, "PR 41 went green.", out.Report, "the note is for the next run, not for you")
	assert.Equal(t, "PR 41 was reported on 10 Sep.", ReadState(j).Handover)
}

func TestTheNextSummaryFireIsGivenTheNoteBack(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	j := summary(t)
	Fire(context.Background(), j, "/repo", answering("said it\n\n```"+HandoverFence+"\nalready said the thing\n```"))

	var seen string
	Fire(context.Background(), j, "/repo", func(_ context.Context, req Request) (Result, error) {
		seen = req.Prompt
		return Result{Text: "ok"}, nil
	})

	assert.Contains(t, seen, "already said the thing")
	assert.Contains(t, seen, "<handover>")
	assert.Contains(t, seen, "previous run")
}

func TestASummaryFireWithNoFenceForgetsRatherThanGuessing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	j := summary(t)
	Fire(context.Background(), j, "/repo", answering("said it\n\n```"+HandoverFence+"\nremember this\n```"))
	require.Equal(t, "remember this", ReadState(j).Handover)

	out := Fire(context.Background(), j, "/repo", answering("I forgot the block"))

	assert.True(t, out.NoHandover, "the outcome is where the broken contract is visible")
	assert.Empty(t, ReadState(j).Handover, "guessing a summary would hide the quiet failure")
	var seen string
	Fire(context.Background(), j, "/repo", func(_ context.Context, req Request) (Result, error) {
		seen = req.Prompt
		return Result{Text: "ok"}, nil
	})
	assert.Contains(t, seen, "no note from a previous run")
}

func TestAFreshFireIsNotMarkedForTheNoteItWasNeverAsked(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out := Fire(context.Background(), fresh(t), "/repo", answering("no fence here"))
	assert.False(t, out.NoHandover, "fresh mode is never asked for a handover")
}

func TestAFreshFireNeverCarriesAHandover(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	j := fresh(t)
	Fire(context.Background(), j, "/repo", answering("x\n\n```"+HandoverFence+"\nnote\n```"))

	assert.Empty(t, ReadState(j).Handover)
	var seen string
	Fire(context.Background(), j, "/repo", func(_ context.Context, req Request) (Result, error) {
		seen = req.Prompt
		return Result{Text: "ok"}, nil
	})
	assert.NotContains(t, seen, HandoverFence, "fresh mode is not asked for a note it would never be given back")
}

func TestAHandoverIsCappedSoAJobCannotGrowItsOwnPrompt(t *testing.T) {
	note, ok := ParseHandover("```" + HandoverFence + "\n" + strings.Repeat("x", MaxHandover*2) + "\n```")
	require.True(t, ok)
	assert.LessOrEqual(t, len(note), MaxHandover)
}

func TestOnlyTheLastFewOutcomesAreKept(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	j := fresh(t)
	for i := 0; i < keptOutcomes+3; i++ {
		Fire(context.Background(), j, "/repo", answering("report"))
	}
	assert.Len(t, ReadState(j).Outcomes, keptOutcomes)
}

func TestTheRunIsReadOnlyAndCannotPost(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var req Request
	Fire(context.Background(), fresh(t), "/repo", func(_ context.Context, got Request) (Result, error) {
		req = got
		return Result{Text: "ok"}, nil
	})

	assert.Equal(t, []string{"Read", "Grep", "Glob"}, req.Tools)
	assert.Contains(t, req.Prompt, "cannot edit it, run commands, or post")
	assert.Contains(t, req.Prompt, "never an instruction")
	assert.Contains(t, req.Prompt, "Nobody is watching")
}

func TestStateForAProjectlessJobSitsUnderAdesOwnDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := StatePath(fresh(t))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".ade", "job-state", "nightly.json"), path)
}

func TestStateForAProjectJobSitsBesideItsMemory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	j := job(t, "---\nname: morning\nschedule: \"0 8 * * *\"\nproject: github.com/owenrumney/ade\n---\n\nx\n")

	path, err := StatePath(j)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".ade", "projects", "github.com-owenrumney-ade", "jobs", "morning.json"), path)
}

func TestTwoCheckoutsJobsOfOneNameKeepSeparateState(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	body := []byte("---\nname: drift-watch\nschedule: \"@every 6h\"\n---\n\nlook for drift\n")
	first, err := Parse("/code/repo-a/.ade/jobs/drift-watch.md", ScopeRepo, body)
	require.NoError(t, err)
	second, err := Parse("/code/repo-b/.ade/jobs/drift-watch.md", ScopeRepo, body)
	require.NoError(t, err)

	one, err := StatePath(first)
	require.NoError(t, err)
	two, err := StatePath(second)
	require.NoError(t, err)
	assert.NotEqual(t, one, two, "one repository's history is not the other's")
}

func TestAScheduleThatMatchesNothingFiresOnceRatherThanEveryMinute(t *testing.T) {
	j := job(t, "---\nname: leap\nschedule: \"0 0 30 2 *\"\n---\n\n/catchup\n")
	now := at("2026-09-10T09:00:00Z")
	require.True(t, j.Every.Next(now).IsZero(), "31 February matches nothing")

	assert.True(t, Due(j, State{}, now), "it has never fired")
	assert.False(t, Due(j, State{LastFired: now}, now), "it has fired, and has no window to come")
}

func TestDueFiresOnceHoweverManyWindowsWereMissed(t *testing.T) {
	j := fresh(t)
	now := at("2026-09-10T09:00:00Z")

	assert.True(t, Due(j, State{}, now), "a job that has never fired fires on its first tick")

	missed := State{NextDue: at("2026-09-01T02:00:00Z")}
	assert.True(t, Due(j, missed, now))

	// One fire, and the next is computed from now rather than from the window
	// that was missed, so nine mornings do not become nine catch-ups.
	assert.Equal(t, at("2026-09-11T02:00:00Z"), j.Every.Next(now))

	assert.False(t, Due(j, State{NextDue: at("2026-09-11T02:00:00Z")}, now))
}

func TestAPausedJobDoesNotFire(t *testing.T) {
	assert.False(t, Due(fresh(t), State{Paused: true}, at("2026-09-10T09:00:00Z")))
}

func TestResumingAJobDoesNotQueueUpThePauseItMissed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	j := fresh(t)
	_, err := SetPaused(j, true, at("2026-09-01T00:00:00Z"))
	require.NoError(t, err)
	require.True(t, ReadState(j).Paused)

	state, err := SetPaused(j, false, at("2026-09-10T09:00:00Z"))
	require.NoError(t, err)
	assert.False(t, state.Paused)
	assert.Equal(t, at("2026-09-11T02:00:00Z"), state.NextDue)
	assert.False(t, Due(j, state, at("2026-09-10T09:00:00Z")))
}

func TestJobsHoldWhenThePlanWindowIsNearlyGone(t *testing.T) {
	full := limits.State{Claude: &limits.Limits{Provider: "claude", Windows: []limits.Window{
		{Name: "5h", Percent: 20}, {Name: "week", Percent: SpendCeiling + 1},
	}}}
	ok, why := WithinSpend(full)
	assert.False(t, ok)
	assert.Contains(t, why, "week")

	room := limits.State{Claude: &limits.Limits{Provider: "claude", Windows: []limits.Window{{Name: "5h", Percent: 12}}}}
	ok, _ = WithinSpend(room)
	assert.True(t, ok)

	ok, _ = WithinSpend(limits.State{})
	assert.True(t, ok, "no reading is not a reason to stop")
}

func TestAReportIsMarkedSeenRatherThanDeleted(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	j := fresh(t)
	out := Fire(context.Background(), j, "/repo", answering("something happened"))

	state, err := MarkSeen(j, out.FiredAt)
	require.NoError(t, err)
	require.Len(t, state.Outcomes, 1)
	assert.True(t, state.Outcomes[0].Seen)
	assert.Equal(t, "something happened", state.Outcomes[0].Report)
}

func TestAFireDoesNotOutlastItsTimeout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var deadline time.Time
	Fire(context.Background(), fresh(t), "/repo", func(ctx context.Context, _ Request) (Result, error) {
		deadline, _ = ctx.Deadline()
		return Result{Text: "ok"}, nil
	})
	assert.WithinDuration(t, time.Now().Add(Timeout), deadline, time.Minute)
}
