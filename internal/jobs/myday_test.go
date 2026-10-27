package jobs

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/linear"
	"github.com/owenrumney/ade/internal/myday"
)

func issue(id string, updated time.Time) linear.Issue {
	return linear.Issue{Identifier: id, Title: id + " title", State: "Todo", UpdatedAt: updated}
}

func estimate(id string, low, high float64) string {
	return fmt.Sprintf("```%s\n{\"identifier\":%q,\"lowHours\":%v,\"highHours\":%v}\n```\n",
		myday.EstimateFence, id, low, high)
}

func TestTheSecondMorningAsksTheModelNothingWhenNothingMoved(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	tickets := []linear.Issue{issue("ENG-1", now.Add(-24*time.Hour))}

	asked := 0
	model := func(context.Context, string) (string, float64, error) {
		asked++
		return estimate("ENG-1", 2, 4), 0.02, nil
	}

	first := FireMyDay(context.Background(), myday.Input{Now: now, Tickets: tickets}, model, "")
	require.Equal(t, 1, asked)
	assert.Contains(t, first.Report, "**2–4h**")
	assert.InDelta(t, 0.02, first.CostUSD, 0.0001)

	second := FireMyDay(context.Background(), myday.Input{Now: now.Add(24 * time.Hour), Tickets: tickets}, model, "")
	assert.Equal(t, 1, asked, "a ticket Linear has not touched is not sized again")
	assert.Contains(t, second.Report, "**2–4h**", "and the cached estimate is still what prints")
	assert.Zero(t, second.CostUSD)
}

func TestATicketTouchedOvernightIsSizedAgain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)

	answers := []string{estimate("ENG-1", 2, 4), estimate("ENG-1", 5, 8)}
	model := func(context.Context, string) (string, float64, error) {
		out := answers[0]
		answers = answers[1:]
		return out, 0, nil
	}

	FireMyDay(context.Background(), myday.Input{Now: now, Tickets: []linear.Issue{issue("ENG-1", now)}}, model, "")
	again := FireMyDay(context.Background(),
		myday.Input{Now: now.Add(24 * time.Hour), Tickets: []linear.Issue{issue("ENG-1", now.Add(12*time.Hour))}}, model, "")

	assert.Contains(t, again.Report, "**5–8h**")
	assert.Empty(t, answers, "both answers were used, so the second morning did ask")
}

func TestAHeldWindowStillGetsAReportAndSaysWhyItIsShort(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)

	model := func(context.Context, string) (string, float64, error) {
		t.Fatal("a held window must not reach the model")
		return "", 0, nil
	}
	out := FireMyDay(context.Background(),
		myday.Input{Now: now, Tickets: []linear.Issue{issue("ENG-1", now)}}, model, "the daily window is spent")

	assert.Contains(t, out.Report, "ENG-1")
	assert.Contains(t, out.Report, "not sized")
	assert.Contains(t, out.Report, "the daily window is spent")
	assert.Zero(t, out.CostUSD)
}

func TestAModelThatFailedIsAShortReportAndNotASilentOne(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)

	model := func(context.Context, string) (string, float64, error) {
		return "", 0, errors.New("oauth token expired")
	}
	out := FireMyDay(context.Background(),
		myday.Input{Now: now, Tickets: []linear.Issue{issue("ENG-1", now)}}, model, "")

	assert.Contains(t, out.Error, "oauth token expired")
	assert.Contains(t, out.Report, "ENG-1", "the tickets are still listed; only the size is missing")
}

func TestAFailedLinearReadNeverForgetsTheEstimatesAlreadyHeld(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC)
	tickets := []linear.Issue{issue("ENG-1", now)}

	model := func(context.Context, string) (string, float64, error) { return estimate("ENG-1", 2, 4), 0, nil }
	FireMyDay(context.Background(), myday.Input{Now: now, Tickets: tickets}, model, "")

	// Linear is down: no tickets came back, and the cache must survive it.
	FireMyDay(context.Background(), myday.Input{Now: now.Add(time.Hour), TicketError: "Linear answered 503"}, model, "")

	back := FireMyDay(context.Background(), myday.Input{Now: now.Add(2 * time.Hour), Tickets: tickets}, model, "")
	assert.Contains(t, back.Report, "**2–4h**")
}

func TestTheBuiltInMyDayJobFiresOnWeekdayMornings(t *testing.T) {
	job := MyDayJob()
	assert.Equal(t, KindMyDay, job.Kind)
	assert.Equal(t, Reads, job.Allow)

	// Friday 08:00 to Monday 08:00: the weekend windows are not fired.
	friday := time.Date(2026, 10, 30, 8, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC), job.Every.Next(friday))
}
