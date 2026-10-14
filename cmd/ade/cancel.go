package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/launcher"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
)

// cancelWait covers a hop over ssh to a worker, not only loopback.
const cancelWait = 30 * time.Second

// newCancelCommand is the Stop button, for when the window will not answer.
func newCancelCommand() *cobra.Command {
	var all bool
	var host string

	cmd := &cobra.Command{
		Use:   "cancel",
		Short: "Cancel the turn the agent in this repository is running",
		Long: `cancel is the Stop button without the editor: it cancels the running turn,
drops the queued messages and stops the side runs. The server, the agent and
the conversation stay up.

When the window answers, use Stop there. When cancel does not end the turn,
'ade stop' is the next step: the server goes and the transcript stays.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if all || host != "" {
				record, err := launcher.RecordPath()
				if err != nil {
					return err
				}
				return cancelEvery(ctx, cmd.OutOrStdout(), record, host)
			}

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			root, err := gitdiff.RepoRoot(ctx, cwd)
			if err != nil {
				return err
			}
			gitDir, err := gitdiff.GitDir(ctx, root)
			if err != nil {
				return err
			}
			return cancelHere(ctx, cmd.OutOrStdout(), session.Path(gitDir))
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "cancel every session this machine is running")
	cmd.Flags().StringVar(&host, "host", "", "cancel every session on a connected machine instead")
	return cmd
}

// cancelHere cancels the turn of the server one record names.
func cancelHere(ctx context.Context, out io.Writer, path string) error {
	rec, ok := session.Read(path)
	if !ok {
		return errors.New("no ade is running for this repository")
	}
	// Read first and not required: the route answers no counts, and a server
	// too busy to answer a probe is the one that most needs the cancel.
	health, _ := session.Probe(ctx, rec)

	err := sessions.Ask(ctx, rec.URL, http.MethodPost, "/api/cancel", "")
	if err == nil || nothingRunning(err) {
		_, _ = fmt.Fprintln(out, cancelOutcome(rec.Root, health.Agent, err))
		return nil
	}
	if !session.Running(rec.PID) {
		_ = session.Remove(path)
		return fmt.Errorf("the ade for %s (pid %d) has gone, and its record is cleared: %w", rec.Root, rec.PID, err)
	}
	return fmt.Errorf("cancel the turn in %s (pid %d): %w; `ade stop` is the next step", rec.Root, rec.PID, err)
}

// cancelEvery cancels every live session the launcher lists, on this machine
// or on the connected host named.
func cancelEvery(ctx context.Context, out io.Writer, record, host string) error {
	rec, ok := session.Read(record)
	if !ok {
		return errors.New("the ade launcher is not running, so nothing lists the sessions; run `ade cancel` in the checkout")
	}
	list := "/api/graph"
	if host != "" {
		list += "?machine=" + url.QueryEscape(host)
	}
	answer, err := sessions.AskFor(ctx, rec.URL, http.MethodGet, list, "", cancelWait)
	if err != nil {
		return fmt.Errorf("ask the launcher what is running: %w", err)
	}
	var graph struct {
		Repos []sessions.GraphRepo `json:"repos"`
	}
	if err := json.Unmarshal(answer, &graph); err != nil {
		return fmt.Errorf("read the launcher's answer: %w", err)
	}

	found, failed := 0, 0
	for _, repo := range graph.Repos {
		for _, checkout := range repo.Checkouts {
			if checkout.Server == nil {
				continue
			}
			found++
			body, err := json.Marshal(map[string]string{"machine": host, "dir": checkout.Path})
			if err != nil {
				return err
			}
			_, err = sessions.AskFor(ctx, rec.URL, http.MethodPost, "/api/graph/cancel", string(body), cancelWait)
			if err != nil && !nothingRunning(err) {
				failed++
			}
			_, _ = fmt.Fprintln(out, cancelOutcome(checkout.Path, checkout.Server.Agent, err))
		}
	}
	if found == 0 {
		_, _ = fmt.Fprintln(out, "no ade is running there")
		return nil
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d sessions were not cancelled", failed, found)
	}
	return nil
}

// nothingRunning is the route's refusal of an idle server, which is not a
// failure of the command. A launcher relaying it wraps the same words.
func nothingRunning(err error) bool {
	return strings.Contains(err.Error(), "nothing is running")
}

// cancelOutcome is one line for one session. It names the checkout, so the
// reader knows which window to look at.
func cancelOutcome(where string, was *session.AgentHealth, err error) string {
	if err != nil {
		if nothingRunning(err) {
			return where + ": nothing is running"
		}
		return fmt.Sprintf("%s: not cancelled: %v", where, err)
	}
	var did []string
	if was != nil {
		if was.Running {
			did = append(did, "the running turn")
		}
		if was.Queued > 0 {
			did = append(did, fmt.Sprintf("%d queued message(s) dropped", was.Queued))
		}
		if len(was.SideRuns) > 0 {
			did = append(did, fmt.Sprintf("%d side run(s) stopped", len(was.SideRuns)))
		}
	}
	if len(did) == 0 {
		return where + ": cancelled"
	}
	return where + ": cancelled " + strings.Join(did, ", ")
}
