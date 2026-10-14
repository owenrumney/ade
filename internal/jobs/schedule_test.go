package jobs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func at(text string) time.Time {
	t, err := time.Parse(time.RFC3339, text)
	if err != nil {
		panic(err)
	}
	return t
}

func TestACronExpressionFiresWhenItSays(t *testing.T) {
	cases := []struct {
		name, spec, after, want string
	}{
		{"weekday morning", "0 8 * * 1-5", "2026-09-10T07:30:00Z", "2026-09-10T08:00:00Z"},
		{"skips the weekend", "0 8 * * 1-5", "2026-09-11T09:00:00Z", "2026-09-14T08:00:00Z"},
		{"every quarter hour", "*/15 * * * *", "2026-09-10T08:01:00Z", "2026-09-10T08:15:00Z"},
		{"a list of hours", "30 9,17 * * *", "2026-09-10T10:00:00Z", "2026-09-10T17:30:00Z"},
		{"a named weekday", "0 6 * * mon", "2026-09-10T00:00:00Z", "2026-09-14T06:00:00Z"},
		{"sunday as seven", "0 6 * * 7", "2026-09-10T00:00:00Z", "2026-09-13T06:00:00Z"},
		{"the first of the month", "0 0 1 * *", "2026-09-10T00:00:00Z", "2026-10-01T00:00:00Z"},
		{"every minute", "* * * * *", "2026-09-10T08:00:30Z", "2026-09-10T08:01:00Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sched, err := ParseSchedule(tc.spec)
			require.NoError(t, err)
			assert.Equal(t, at(tc.want), sched.Next(at(tc.after)))
		})
	}
}

func TestBothDayFieldsRestrictedIsAnOr(t *testing.T) {
	// Standard cron: "the 13th, or any Friday", not "Friday the 13th".
	sched, err := ParseSchedule("0 0 13 * 5")
	require.NoError(t, err)

	// 2026-09-11 is a Friday; the 13th is the Sunday after it.
	assert.Equal(t, at("2026-09-11T00:00:00Z"), sched.Next(at("2026-09-10T12:00:00Z")))
	assert.Equal(t, at("2026-09-13T00:00:00Z"), sched.Next(at("2026-09-11T12:00:00Z")))
}

func TestNextIsAlwaysStrictlyAfter(t *testing.T) {
	// Otherwise a job fired at 08:00 is due again at 08:00 and fires forever.
	sched, err := ParseSchedule("0 8 * * *")
	require.NoError(t, err)
	assert.Equal(t, at("2026-09-11T08:00:00Z"), sched.Next(at("2026-09-10T08:00:00Z")))
}

func TestAnIntervalIsWrittenAsADuration(t *testing.T) {
	for _, spec := range []string{"@every 2h", "every 2h", "2h"} {
		sched, err := ParseSchedule(spec)
		require.NoError(t, err, spec)
		every, ok := sched.Interval()
		assert.True(t, ok, spec)
		assert.Equal(t, 2*time.Hour, every)
		assert.Equal(t, at("2026-09-10T10:00:00Z"), sched.Next(at("2026-09-10T08:00:00Z")))
	}
}

func TestABadScheduleIsRefusedRatherThanNeverFiring(t *testing.T) {
	cases := map[string]string{
		"empty":           "",
		"four fields":     "0 8 * *",
		"six fields":      "0 0 8 * * *",
		"hour past 23":    "0 24 * * *",
		"month zero":      "0 8 * 0 *",
		"backwards":       "0 8 * * 5-1",
		"zero step":       "*/0 * * * *",
		"not a number":    "0 eight * * *",
		"under the floor": "@every 30s",
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseSchedule(spec)
			assert.Error(t, err)
		})
	}
}

func TestAScheduleThatMatchesNothingAnswersZeroRatherThanLooping(t *testing.T) {
	sched, err := ParseSchedule("0 0 30 2 *")
	require.NoError(t, err)
	assert.True(t, sched.Next(at("2026-09-10T00:00:00Z")).IsZero())
}

func TestAManualScheduleHasNoClockBehindIt(t *testing.T) {
	for _, spec := range []string{"manual", "Manual", "adhoc", "ad hoc", "on demand"} {
		sched, err := ParseSchedule(spec)
		require.NoError(t, err, spec)
		assert.True(t, sched.Manual(), spec)
		assert.Equal(t, Manual, sched.Spec(), "however it was written, it reads back the one way")
		assert.True(t, sched.Next(at("2026-09-10T08:00:00Z")).IsZero(), "nothing is due")
		_, interval := sched.Interval()
		assert.False(t, interval, "not a duration, whatever the reader would make of the word")
	}
}

func TestAManualJobIsNeverDueOfItsOwnAccord(t *testing.T) {
	sched, err := ParseSchedule(Manual)
	require.NoError(t, err)
	job := Job{Name: "prod-triage", Every: sched}
	// A never-fired scheduled job is due on the next tick; this one is not,
	// or "run it yourself" would mean "run it once, now".
	assert.False(t, Due(job, State{}, at("2026-09-10T08:00:00Z")))
	assert.False(t, Due(job, State{LastFired: at("2026-09-10T07:00:00Z")}, at("2026-09-10T08:00:00Z")))
}

func TestAManualJobIsToldItWasRunByHand(t *testing.T) {
	sched, err := ParseSchedule(Manual)
	require.NoError(t, err)
	text := Prompt(Job{Name: "prod-triage", Every: sched, Continuity: Fresh, Prompt: "/prod-triage"}, State{})
	assert.Contains(t, text, "run by hand")
	assert.NotContains(t, text, "ade's clock on the schedule")
}
