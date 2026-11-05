package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/owenrumney/ade/internal/myday"
	"github.com/owenrumney/ade/internal/oneshot"
)

// KindMyDay is the built-in job that says what the day looks like: the pull
// requests waiting on you, your own, and your tickets. See the my-day-job doc.
const KindMyDay = "my-day"

// MyDayEstimate bounds the one model call a fire may make. Everything else is
// two GitHub searches and a Linear read.
const MyDayEstimate = 5 * time.Minute

// MyDayJob is the built-in definition. Nothing fires it on a clock: the
// window asks for it.
func MyDayJob() Job {

	return Job{
		Name:        KindMyDay,
		Kind:        KindMyDay,
		Description: "What your day looks like: reviews waiting on you, your pull requests, and your tickets sized",
		Schedule:    Manual,
		Continuity:  Fresh,
		Allow:       Reads,
		Scope:       ScopeBuiltin,
	}
}

// MyDayModel is the toolless one-shot an estimate is asked of. It runs in dir,
// outside every checkout, and leaves no transcript to be distilled in turn.
func MyDayModel(bin, dir string) myday.Model {
	return func(ctx context.Context, prompt string) (string, float64, error) {
		turn, err := oneshot.Run(ctx, oneshot.Opts{
			Bin:                  bin,
			Dir:                  dir,
			Prompt:               prompt,
			Tools:                []string{},
			NoMCP:                true,
			NoSessionPersistence: true,
		})
		if turn == nil {
			return "", 0, err
		}
		return turn.Text, turn.CostUSD, err
	}
}

// FireMyDay sizes what Linear has touched since the last fire and reuses the
// cache for the rest. Held stops the model, never the report.
func FireMyDay(ctx context.Context, in myday.Input, model myday.Model, held string) Outcome {
	started := time.Now()
	out := Outcome{FiredAt: started}
	if in.Now.IsZero() {
		in.Now = started
	}

	cache := myday.Load()
	fresh := map[string]myday.Estimate{}
	switch stale := myday.Stale(cache, in.Tickets); {
	case len(stale) == 0:
	case model == nil || held != "":
		in.Held = held
		if in.Held == "" {
			in.Held = "no model is available on this machine"
		}
	default:
		ask, done := context.WithTimeout(ctx, MyDayEstimate)
		text, cost, err := model(ask, myday.Prompt(stale))
		done()
		out.CostUSD += cost
		if err != nil {
			slog.Warn("size the tickets for my-day", "tickets", len(stale), "err", err)
			out.Error = fmt.Sprintf("%d tickets could not be sized: %s", len(stale), err)
			in.Held = "the model did not answer"
		}
		fresh = myday.ParseEstimates(text)
	}
	// Only when the tickets were read: merging against a failed read would
	// forget every estimate the cache holds.
	if in.TicketError == "" {
		cache = myday.Merge(cache, in.Tickets, fresh, in.Now)
		if err := myday.Save(cache); err != nil {
			slog.Warn("keep the my-day estimates", "err", err)
		}
	}

	in.Estimates = cache
	out.Report = myday.Render(in)
	out.Millis = time.Since(started).Milliseconds()
	return out
}
