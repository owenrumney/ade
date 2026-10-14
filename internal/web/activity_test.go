package web

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// onBucket is a time on a bucket boundary, so a test's "one second later" is
// not sometimes the next bucket.
func onBucket() time.Time {
	return time.Now().Truncate(activityBucket)
}

func TestActivityCountsEventsIntoTheBucketTheyLandIn(t *testing.T) {
	var a activity
	now := onBucket()
	a.note(now)
	a.note(now.Add(activityBucket / 2))
	a.note(now.Add(activityBucket))

	got := a.series(now.Add(activityBucket))
	require.Len(t, got, activityBuckets)
	assert.Equal(t, 1, got[len(got)-1], "the newest bucket is last")
	assert.Equal(t, 2, got[len(got)-2])
}

func TestSilenceReadsAsSilenceRatherThanAsALapOld(t *testing.T) {
	// The ring is indexed by wall time, so without clearing what it passes,
	// a quiet run would show the counts from one lap ago and look busy.
	var a activity
	start := onBucket()
	for i := range activityBuckets {
		a.note(start.Add(time.Duration(i) * activityBucket))
	}
	assert.NotZero(t, sum(a.series(start.Add(time.Duration(activityBuckets-1)*activityBucket))))

	quiet := start.Add(time.Duration(activityBuckets*2) * activityBucket)
	assert.Zero(t, sum(a.series(quiet)), "a run that has said nothing for a lap is flat")
}

func TestAGapInsideTheWindowIsZeroesNotAShift(t *testing.T) {
	// A hang is a flat tail with the earlier work still visible behind it;
	// that is the shape the badge is drawn to show.
	var a activity
	start := onBucket()
	a.note(start)
	a.note(start.Add(activityBucket))

	got := a.series(start.Add(5 * activityBucket))
	require.Len(t, got, activityBuckets)
	for i := len(got) - 4; i < len(got); i++ {
		assert.Zerof(t, got[i], "bucket %d should be silent", i)
	}
	assert.Equal(t, 2, sum(got), "the work before the gap is still there")
}

func TestAFinishedRunReportsNoActivity(t *testing.T) {
	// A run that ended has a flat tail by definition, and drawing it would
	// say "hung" about something that simply finished.
	s := newTestServer(t)
	s.agent.touch()
	assert.Empty(t, s.agent.State().Activity)
}

func sum(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}

func TestTheBadgeGetsTheSeriesWhileTheRunIsStillGoing(t *testing.T) {
	// Without an event of its own the badge kept the series the state carried
	// when the run began — an empty ring — so every run drew a flat line.
	s := newTestServer(t)
	_, ok := s.agent.begin(s.ctx, kindChat)
	require.True(t, ok)
	defer s.endRun(nil)

	_, running := s.agent.beat()
	require.True(t, running)
	s.agent.touch()

	series, running := s.agent.beat()
	require.True(t, running)
	assert.NotZero(t, sum(series), "the work is in the series the event carries")
}

func TestNothingIsBeatenOutWhenNoRunIsGoing(t *testing.T) {
	s := newTestServer(t)
	s.agent.touch()
	_, running := s.agent.beat()
	assert.False(t, running, "a finished run has a flat tail by definition")
}

func TestTheLastEventOutlivesTheRing(t *testing.T) {
	s := newTestServer(t)
	spoke := time.Now().Add(-11 * time.Minute)
	s.agent.lastEvent = spoke

	// Long past the ring's 120s: the series is flat, and the moment is still
	// there, which is what turns "silent" into "silent for eleven minutes".
	assert.Zero(t, sum(s.agent.beats.series(time.Now())))
	assert.True(t, spoke.Equal(s.agent.State().LastEventAt))
}

func TestAnAgentThatNeverSpokeHasNoLastEvent(t *testing.T) {
	s := newTestServer(t)
	assert.True(t, s.agent.State().LastEventAt.IsZero())
}

func TestANewRunIsNotTheLastRunsSilence(t *testing.T) {
	// The watchdog's clock is reset when a run begins; the one doctor reads
	// has to be the same clock, or a long first tool call reads as a hang.
	s := newTestServer(t)
	s.agent.lastEvent = time.Now().Add(-3 * time.Hour)

	_, ok := s.agent.begin(s.ctx, kindChat)
	require.True(t, ok)
	defer s.endRun(nil)

	assert.WithinDuration(t, time.Now(), s.agent.State().LastEventAt, time.Second)
}
