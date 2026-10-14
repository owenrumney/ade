package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/owenrumney/ade/internal/dashboard"
	adesettings "github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/transcripts"
)

// backfillWait bounds the whole push. The reporter drops rather than blocks,
// so this is how long the last tile gets to leave before the process exits.
const backfillWait = 10 * time.Second

func newDashboardCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "The kiosk screen this machine reports into",
		Long: `ade pushes this machine's subscription windows and transcript totals to a
kiosk dashboard, if one is configured under Settings. The reporter runs in the
launcher and needs no command; this is for filling in the history once.`,
	}
	cmd.AddCommand(newBackfillCommand())
	return cmd
}

// newBackfillCommand pushes the whole local history in one go, for a screen
// that has only ever seen the days since ade started reporting.
func newBackfillCommand() *cobra.Command {
	var since string

	cmd := &cobra.Command{
		Use:   "backfill",
		Short: "Push this machine's whole transcript history to the dashboard",
		Long: `backfill is the ordinary report with more days in it. A host's contribution
replaces its previous one, so running it twice changes nothing.

Reading every transcript on the machine takes a while; nothing else waits on
it, and it is not something to put on a timer.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			days, err := parseSince(since)
			if err != nil {
				return err
			}
			return backfill(cmd.Context(), cmd.OutOrStdout(), days)
		},
	}
	cmd.Flags().StringVar(&since, "since", "30d", "how far back the daily series goes, e.g. 30d or 180d")
	return cmd
}

func backfill(ctx context.Context, out io.Writer, days int) error {
	config, err := adesettings.Load()
	if err != nil {
		return err
	}
	if !config.Dashboard.On() {
		return errors.New("no dashboard is configured; set its url and token under Settings")
	}

	_, _ = fmt.Fprintln(out, "reading every transcript on this machine…")
	stats := transcripts.Statistics(transcripts.Scan())

	ctx, cancel := context.WithTimeout(ctx, backfillWait)
	defer cancel()
	to := dashboard.New(ctx, config.Dashboard.URL, config.Dashboard.Token, config.Dashboard.Machine())
	if to == nil {
		return errors.New("no dashboard is configured; set its url and token under Settings")
	}
	host := to.Host()
	tiles := dashboard.Usage(stats, host, time.Now(), days)
	to.Send(tiles...)

	// The reporter is fire and forget, so the push is in flight rather than
	// done. Drained before the process exits, or the last tile never leaves.
	if err := to.Drain(ctx); err != nil {
		return fmt.Errorf("push to the dashboard: %w", err)
	}
	_, _ = fmt.Fprintf(out, "sent %d tiles as %s, %d sessions over %d days\n",
		len(tiles), host, stats.Sessions, days)
	return nil
}

// parseSince reads "30d". Days only: the series is daily, so an hour is not a
// thing it could mean.
func parseSince(text string) (int, error) {
	var days int
	if _, err := fmt.Sscanf(text, "%dd", &days); err != nil || days < 1 {
		return 0, fmt.Errorf("--since %q should be a number of days, like 30d", text)
	}
	return days, nil
}
