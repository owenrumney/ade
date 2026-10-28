package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/owenrumney/ade/internal/render"
	"github.com/owenrumney/ade/internal/window"

	"github.com/owenrumney/ade/internal/agents"
	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/docs"
	"github.com/owenrumney/ade/internal/docsave"
	"github.com/owenrumney/ade/internal/drift"
	"github.com/owenrumney/ade/internal/fdlimit"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/jobs"
	"github.com/owenrumney/ade/internal/launcher"
	"github.com/owenrumney/ade/internal/linear"
	"github.com/owenrumney/ade/internal/logs"
	"github.com/owenrumney/ade/internal/port"
	"github.com/owenrumney/ade/internal/provision"
	"github.com/owenrumney/ade/internal/roles"
	"github.com/owenrumney/ade/internal/said"
	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/owenrumney/ade/internal/scratch"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
	adesettings "github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/skills"
	"github.com/owenrumney/ade/internal/statedir"
	"github.com/owenrumney/ade/internal/web"
	"github.com/owenrumney/ade/internal/worktrees"
)

// version is stamped by the Makefile with `git describe`.
var version = "dev"

// A binary built without the Makefile still knows something: the go tool
// records the module version for `go install pkg@version`. Better than "dev",
// which is what an unstamped local build is left with.
func init() {
	if version != "dev" {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return
	}
	version = info.Main.Version
}

// defaultModel is the long-context variant, and it is named rather than left
// to Claude Code's default because of what fills a window before any work
// starts: tool schemas and CLAUDE.md come to 130-150k tokens. Against the
// 200k aliases that is three quarters of the window gone on turn one, and a
// session that compacts every few messages, each compaction landing a ~9k
// summary that eats the room it just freed. The same baseline is 15% of a 1M
// window.
//
// The suffix is what asks for it: internal/web.contextWindow reads "[1m]" to
// know the window is 1M, and Claude Code reads it to serve that variant.
const defaultModel = "claude-opus-5[1m]"

func providerFor(agent, legacy string, agentSet bool) (string, error) {
	selected := strings.ToLower(strings.TrimSpace(agent))
	var provider string
	switch selected {
	case "claude":
		provider = "claude"
	case "codex":
		provider = "codex"
	default:
		return "", fmt.Errorf("--agent must be claude or codex, got %q", agent)
	}

	legacy = strings.ToLower(strings.TrimSpace(legacy))
	if legacy == "" {
		return provider, nil
	}
	// "pi" is what --provider spelled Codex before the driver was Codex. A
	// script written against it keeps working.
	if legacy == "pi" {
		legacy = "codex"
	}
	if legacy != "claude" && legacy != "codex" {
		return "", fmt.Errorf("--provider must be claude or codex, got %q", legacy)
	}
	if agentSet && legacy != provider {
		return "", fmt.Errorf("--agent %s conflicts with --provider %s", selected, legacy)
	}
	return legacy, nil
}

func main() {
	installLogRing()
	raiseFileLimit()
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "ade:", err)
		os.Exit(1)
	}
}

// installLogRing makes every slog line reachable from the browser. The ring
// keeps debug; stderr keeps the level it had, or ADE_LOG's.
func installLogRing() {
	level := slog.LevelInfo
	if err := level.UnmarshalText([]byte(os.Getenv("ADE_LOG"))); err != nil && os.Getenv("ADE_LOG") != "" {
		fmt.Fprintf(os.Stderr, "ade: ADE_LOG=%q is not a level\n", os.Getenv("ADE_LOG"))
	}
	logs.Installed = logs.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(slog.New(logs.Installed))
}

// raiseFileLimit lifts the 256 a macOS shell hands down.
func raiseFileLimit() {
	from, to, err := fdlimit.Raise()
	if err != nil {
		slog.Warn("could not raise the open-file limit", "limit", from, "err", err)
		return
	}
	if to > from {
		slog.Info("raised the open-file limit", "from", from, "to", to)
	}
}

func rootCmd() *cobra.Command {
	var (
		committed      bool
		base           string
		contextLine    int
		noOpen         bool
		sessionID      string
		prNumber       int
		agent          string
		provider       string
		model          string
		autoCompact    string
		docsDir        string
		docFile        string
		host           string
		draftFrom      string
		draftFromIssue int
		sandboxed      bool
		foreground     bool
		detachFlag     bool
		idle           time.Duration
		noLauncher     bool
		skillName      string
		investigate    string
		role           string
	)

	cmd := &cobra.Command{
		Use:   "ade [revspec]",
		Short: "Design a change, have an agent build it, and review what it did",
		Long: `ade is a statement of work: you write down what a change should do, an agent
builds it, and you review what it did — in your browser, on your Claude
subscription.

With no arguments it opens the launcher: every checkout ade knows, and what is
waiting on you across all of them. "ade ." opens this checkout instead, on the
diff — the merge base of your default branch against the working tree, so
uncommitted edits and new files are included.

  ade                                pick a checkout, from anywhere
  ade start                          bring the launcher up, without a browser
  ade .                              review the branch in this checkout
  ade --agent codex .                work in this checkout with Codex, sandboxed by default
  ade --host pc ade                  open a checkout on another machine, over ssh
  ade create --linear FIX-623        a design document, and a worktree to build it in
  ade create --github 412            the same, from a GitHub issue of this repository
  ade investigate --linear FIX-623   read-only: what is going on, not a change to make
  ade done                           stop the server and remove the worktree
  ade prune                          remove the pull request checkouts ade made
  ade doctor                         what every ade on this machine is doing
  ade agents install                 the default @agents, into ~/.claude/agents
  ade roles install                  the default roles, into ~/.ade/roles
  ade jobs ls                        the scheduled jobs, and what came of them`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			selectedProvider, err := providerFor(agent, provider, cmd.Flags().Changed("agent"))
			if err != nil {
				return err
			}
			// Nothing named and nothing asked for: the launcher is where you
			// land, and "ade ." is how you say this checkout.
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return runLaunch(cmd.Context(), "", noOpen, foreground, idle, 0)
			}
			opts := gitdiff.Options{
				Base:    base,
				Scope:   gitdiff.ScopeBranch,
				Context: contextLine,
			}
			if committed {
				opts.Scope = gitdiff.ScopeCommitted
			}
			// "." is this checkout, not a revspec: git has no such rev, and it
			// is the shortest way to say the thing bare ade used to mean.
			if len(args) == 1 && args[0] != "." {
				opts.Rev = args[0]
			}
			if host != "" {
				// With --host the argument names a checkout over there, not a
				// revspec here: a diff needs a checkout to be of first.
				return openOnHost(cmd.Context(), host, opts.Rev, !noOpen, idle)
			}
			if prNumber > 0 {
				if opts.Rev != "" {
					return fmt.Errorf("--pr and a revspec select different diffs; pass one")
				}
				if base != "" {
					return fmt.Errorf("--pr already sets the base from the pull request; drop --base")
				}
			}
			if draftFrom != "" && draftFromIssue > 0 {
				return fmt.Errorf("--draft-from and --draft-from-issue are two starting points; pick one")
			}
			return run(cmd.Context(), settings{
				diff:             opts,
				sessionID:        sessionID,
				provider:         selectedProvider,
				providerExplicit: cmd.Flags().Changed("agent") || cmd.Flags().Changed("provider"),
				model:            model,
				autoCompact:      autoCompact,
				prNumber:         prNumber,
				openBrowser:      !noOpen,
				sandboxed:        sandboxed,
				sandboxExplicit:  cmd.Flags().Changed("sandbox"),
				foreground:       foreground,
				idle:             idle,
				docsDir:          docsDir,
				docFile:          docFile,
				linearTicket:     draftFrom,
				seedIssue:        draftFromIssue,
				skillName:        skillName,
				investigate:      investigate,
				noLauncher:       noLauncher,
				role:             role,
			})
		},
	}

	// Flags every mode needs are persistent so subcommands inherit them; the
	// rest select a diff, which only the root command does.
	shared := cmd.PersistentFlags()
	shared.BoolVar(&noOpen, "no-open", false, "print the URL without opening a browser")
	shared.BoolVar(&noLauncher, "no-launcher", false, "open this checkout on its own port, without the launcher or the rail of other sessions")
	cmd.Flags().StringVar(&host, "host", "", "open a checkout on another machine over ssh; the argument names it there, not a revspec")
	shared.StringVar(&agent, "agent", "claude", "main agent: claude (default) or codex")
	shared.StringVar(&provider, "provider", "", "deprecated agent provider: claude or codex")
	_ = shared.MarkDeprecated("provider", "use --agent claude or --agent codex instead")
	shared.StringVar(&model, "model", defaultModel, "model for the agents, e.g. haiku or sonnet, or a \"[1m]\" variant for the long window")
	shared.StringVar(&autoCompact, "autocompact", "", "window claude compacts the conversation at: auto, or a token count (default: claude's own)")
	// Not defaulted from $CLAUDE_CODE_SESSION_ID. Inheriting the caller's
	// conversation sounded like a free head start and was the opposite: the
	// transcript only grows, a long one replays into the agent's much smaller
	// window and compacts every few minutes, and two processes end up appending
	// to one session file. The design document is the handoff, and it is a
	// better one — versioned, readable, and only what you meant to say.
	shared.StringVar(&sessionID, "session", "", `Agent session to resume.
Defaults to the one this checkout was last in, so a restart lands back in the
conversation. Pass an id for a specific one, or "none" to start fresh.`)
	shared.StringVar(&docsDir, "docs", "", "directory holding design documents (default: .scratch, else inside the git dir)")
	shared.StringVar(&role, "role", "", "role from ~/.ade/roles this session starts under (default: the one Settings selects)")
	shared.BoolVar(&sandboxed, "sandbox", true,
		`run the agent in a container (Apple's container or Docker) with the checkout mounted.
The image is pulled on first use, or built by `+"`make sandbox-image`"+`.
--sandbox=false runs the agent straight on the host, with your files and your network.`)

	flags := cmd.Flags()
	flags.BoolVarP(&committed, "committed", "c", false, "start on the committed view; the browser can switch between branch, committed and uncommitted")
	flags.StringVarP(&base, "base", "b", "", "base branch to diff against (default: origin/main, then main)")
	flags.IntVarP(&contextLine, "context", "U", 3, "lines of context around each change")
	flags.IntVar(&prNumber, "pr", 0, "review a GitHub pull request by number, importing its review comments")
	flags.StringVar(&docFile, "doc", "", "design document to open on")
	flags.StringVar(&draftFrom, "draft-from", "", "draft the open document from this ticket")
	_ = flags.MarkHidden("draft-from")
	flags.IntVar(&draftFromIssue, "draft-from-issue", 0, "draft the open document from this GitHub issue")
	_ = flags.MarkHidden("draft-from-issue")
	// How the investigate subcommand reaches its detached child.
	flags.StringVar(&skillName, "skill", "", "skill whose SKILL.md is this session's instructions")
	_ = flags.MarkHidden("skill")
	flags.StringVar(&investigate, "investigate", "", "ticket to open an investigation on")
	_ = flags.MarkHidden("investigate")
	cmd.Version = version

	shared.BoolVarP(&foreground, "foreground", "F", false, "run in this terminal instead of the background")
	// ade backgrounds itself now, so this says nothing. Kept because the
	// fingers that learned it should not get an error for it.
	shared.BoolVarP(&detachFlag, "detach", "d", false, "")
	_ = shared.MarkHidden("detach")
	shared.DurationVar(&idle, "idle", 30*time.Minute, "exit after this long with no browser attached (0 to stay up)")

	cmd.AddCommand(newCreateCommand(&sessionID, &agent, &provider, &model, &autoCompact, &role, &noOpen, &sandboxed, &foreground, &docsDir, &idle))
	cmd.AddCommand(newInvestigateCommand(&sessionID, &agent, &provider, &model, &autoCompact, &noOpen, &sandboxed, &foreground, &idle))
	cmd.AddCommand(newLaunchCommand(&noOpen, &foreground, &idle))
	cmd.AddCommand(newStartCommand(&foreground, &idle))
	cmd.AddCommand(newHookCommand())
	cmd.AddCommand(newScratchCommand(&noOpen))
	cmd.AddCommand(newPortCommand(&noOpen))
	cmd.AddCommand(newStopCommand())
	cmd.AddCommand(newCancelCommand())
	cmd.AddCommand(newDocCommand(&docsDir))
	cmd.AddCommand(newDesignsCommand())
	cmd.AddCommand(newDoneCommand())
	cmd.AddCommand(newPsCommand())
	cmd.AddCommand(newDoctorCommand())
	cmd.AddCommand(newDashboardCommand())
	cmd.AddCommand(newPruneCommand())
	cmd.AddCommand(newSyncCommand())
	cmd.AddCommand(newAgentsCommand())
	cmd.AddCommand(newRolesCommand())
	cmd.AddCommand(newSaidCommand())
	cmd.AddCommand(newDriftCommand())
	cmd.AddCommand(newJobsCommand())
	cmd.AddCommand(newBrowserCommand())
	cmd.AddCommand(newWikiCommand())
	cmd.AddCommand(newWhyCommand())
	cmd.AddCommand(newFixturesCommand())
	return cmd
}

// newScratchCommand starts a session with nowhere to put it yet.
func newScratchCommand(noOpen *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scratch [what it is about]",
		Short: "Open ade in a throwaway checkout, for work with no home yet",
		Long: `scratch makes a git repository under ~/.ade/scratch and opens ade in it.

Like launch, it does not need a repository to run in: it is for the work that
has no obvious project yet. The checkout starts on a branch above an empty
main, so what you end up with is a range of commits — cherry-pick them, or
` + "`git format-patch main`" + ` and apply the result, wherever the work belongs.`,
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := scratch.New(cmd.Context(), strings.Join(args, " "))
			if err != nil {
				return err
			}
			url, err := sessions.OpenIn(cmd.Context(), dir, "")
			if err != nil {
				return err
			}
			fmt.Println(dir)
			fmt.Println(url)
			if !*noOpen {
				openUnwatched(cmd.Context(), url, dir)
			}
			return nil
		},
	}
	return cmd
}

// newPortCommand moves a scratch's work into the repository it turned out to
// belong in. The other half of scratch.
func newPortCommand(noOpen *bool) *cobra.Command {
	var branch string
	var remove bool

	cmd := &cobra.Command{
		Use:   "port <repository>",
		Short: "Move this scratch's work into a repository, in a worktree",
		Long: `port carries the work out of a scratch checkout and into a real repository.

Run it in the scratch, naming where the work belongs. Anything uncommitted is
committed first, the commits above the scratch's empty base are cherry-picked
into a new worktree beside that repository, and ade opens there. The target's
own checkout is never touched, so it does not matter what is going on in it.

A conflict stops the cherry-pick and leaves the worktree in place to finish by
hand. --rm deletes the scratch once the work is safely across.`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			target, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}

			moved, err := port.Move(cmd.Context(), cwd, target, branch)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if moved.Committed {
				fmt.Fprintln(out, "committed what was still outstanding in the scratch")
			}
			fmt.Fprintf(out, "%d commit(s) onto %s in %s\n", moved.Commits, moved.Branch, moved.Path)

			// Only once the work is definitely across, and only when asked.
			if remove {
				if err := port.Remove(cmd.Context(), cwd); err != nil {
					return err
				}
				fmt.Fprintln(out, "the scratch is gone")
			}

			url, err := sessions.OpenIn(cmd.Context(), moved.Path, "")
			if err != nil {
				return err
			}
			fmt.Fprintln(out, url)
			if !*noOpen {
				openUnwatched(cmd.Context(), url, moved.Path)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&branch, "branch", "", "branch to make in the repository (default: the scratch's)")
	cmd.Flags().BoolVar(&remove, "rm", false, "delete the scratch once its work is across")
	return cmd
}

// newLaunchCommand opens the session list on its own, for when you have not
// decided what to work on yet. It needs no repository: everything it offers
// comes from the agent sessions on this machine.
// newStartCommand brings the launcher up and leaves it there. Unlike `ade
// launch` it never opens a browser.
func newStartCommand(foreground *bool, idle *time.Duration) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the launcher in the background, without opening a browser",
		Long: `start brings up the launcher and prints its URL.

It is ` + "`ade launch --no-open`" + `: the same one launcher per machine, reused if it is
already running. Nothing is opened, so this is what to run at login, or before
a ade page offers its way back to the picker.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// No browser attaches, so the idle timer would close it unasked.
			stay := *idle
			if !cmd.Flags().Changed("idle") {
				stay = 0
			}
			return runLaunch(cmd.Context(), "", true, *foreground, stay, 0)
		},
	}
	return cmd
}

func newLaunchCommand(noOpen, foreground *bool, idle *time.Duration) *cobra.Command {
	var host string
	var port int

	cmd := &cobra.Command{
		Use:   "launch",
		Short: "Open the session picker, with no repository",
		Long: `launch serves the list of recent agent sessions and nothing else.

Pick one and ade opens in that checkout, the same as running ade there. Run it
from anywhere; unlike every other command it does not need a git repository.

With --host it opens another machine's picker instead, over ssh. The host is a
name from your settings, and the launcher here stays up to hold the tunnel.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLaunch(cmd.Context(), host, *noOpen, *foreground, *idle, port)
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "open the picker on another machine, over ssh")
	// For a second launcher beside the running one, over its own HOME: what
	// `make shots` needs and nothing a normal run should name.
	cmd.Flags().IntVar(&port, "port", 0, "serve on this port instead of ade's own")
	return cmd
}

// runLaunch serves the picker, and is what both `ade launch` and a bare `ade`
// run: the launcher is where you land when you have not named a checkout.
func runLaunch(ctx context.Context, host string, noOpen, foreground bool, idle time.Duration, port int) error {
	home, err := launcher.Home()
	if err != nil {
		return err
	}
	record, err := launcher.RecordPath()
	if err != nil {
		return err
	}
	if host != "" {
		return launchOnHost(ctx, home, record, host, foreground, idle)
	}
	// One launcher per machine: hand back the running one's link rather than
	// starting a rival on another port. A rival needs its own HOME, not --port.
	if reuse(ctx, record, false, "", "", "", false, true) {
		return nil
	}
	if !foreground {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		args := []string{"launch", "--foreground", "--no-open", "--idle", idle.String()}
		if port != 0 {
			args = append(args, "--port", strconv.Itoa(port))
		}
		return detachTo(cwd, home, record, args, false)
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	server, err := launcher.New(idle, version)
	if err != nil {
		return err
	}
	server.ServeOn(port)
	return server.Run(ctx)
}

// launchOnHost opens another machine's launcher. The tunnel belongs to the
// launcher on this one, so that has to be running before anything else.
func launchOnHost(ctx context.Context, home, record, host string, foreground bool, idle time.Duration) error {
	if foreground {
		if rec, ok := session.Read(record); ok && session.Live(ctx, rec) {
			return fmt.Errorf("a launcher is already running here; drop --foreground and it will be asked to connect")
		}
		server, err := launcher.New(idle, version)
		if err != nil {
			return err
		}
		server.ConnectTo(host)
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		return server.Run(ctx)
	}

	url, err := hostLauncher(ctx, home, record, host, idle)
	if err != nil {
		return err
	}
	fmt.Println(url)
	return nil
}

// openOnHost opens one checkout on another machine. The argument names it over
// there, not here: this shell would have expanded a path against this home.
// The local launcher holds the tunnel, so it is always the detached one.
func openOnHost(ctx context.Context, host, want string, openBrowser bool, idle time.Duration) error {
	if strings.TrimSpace(want) == "" {
		return fmt.Errorf("name a checkout on %s, or run `ade launch --host %s` to pick one", host, host)
	}
	home, err := launcher.Home()
	if err != nil {
		return err
	}
	record, err := launcher.RecordPath()
	if err != nil {
		return err
	}

	remote, err := hostLauncher(ctx, home, record, host, idle)
	if err != nil {
		return err
	}

	dir, err := checkoutOn(ctx, remote, host, want)
	if err != nil {
		return err
	}
	answer, err := sessions.AskFor(ctx, remote, http.MethodPost, "/api/open",
		fmt.Sprintf(`{"dir":%q,"id":""}`, dir), connectWait)
	if err != nil {
		return fmt.Errorf("open %s on %s: %w", dir, host, err)
	}
	var res struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(answer, &res); err != nil || res.URL == "" {
		return fmt.Errorf("open %s on %s: no url came back", dir, host)
	}
	fmt.Println(res.URL)
	if openBrowser {
		openUnwatched(ctx, res.URL, "")
	}
	return nil
}

// hostLauncher brings a host up and returns its launcher's URL, starting the
// local launcher that holds the tunnel if one is not already running.
func hostLauncher(ctx context.Context, home, record, host string, idle time.Duration) (string, error) {
	rec, ok := session.Read(record)
	if !ok || !session.Live(ctx, rec) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		rec, err = startDetached(cwd, home, record,
			[]string{"launch", "--foreground", "--no-open", "--idle", idle.String()})
		if err != nil {
			return "", err
		}
	}
	answer, err := sessions.AskFor(ctx, rec.URL, http.MethodPost, "/api/connect",
		fmt.Sprintf(`{"host":%q}`, host), connectWait)
	if err != nil {
		return "", fmt.Errorf("connect to %s: %w", host, err)
	}
	var res struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(answer, &res); err != nil || res.URL == "" {
		return "", fmt.Errorf("connect to %s: the launcher did not say where it got to", host)
	}
	return res.URL, nil
}

// checkoutOn resolves what was typed against the checkouts that machine offers.
// An exact path wins; otherwise the name has to pick out exactly one, since
// opening the wrong repository is worse than being asked to be specific.
func checkoutOn(ctx context.Context, remoteURL, host, want string) (string, error) {
	answer, err := sessions.AskFor(ctx, remoteURL, http.MethodGet, "/api/repos", "", connectWait)
	if err != nil {
		return "", fmt.Errorf("list the checkouts on %s: %w", host, err)
	}
	var body struct {
		Repos []sessions.Repo `json:"repos"`
	}
	if err := json.Unmarshal(answer, &body); err != nil {
		return "", fmt.Errorf("list the checkouts on %s: %w", host, err)
	}

	var matched []string
	for _, repo := range body.Repos {
		for _, dir := range repo.Checkouts {
			if dir.Path == want {
				return dir.Path, nil
			}
			if dir.Name == want || strings.HasSuffix(dir.Path, "/"+want) {
				matched = append(matched, dir.Path)
			}
		}
	}
	switch len(matched) {
	case 1:
		return matched[0], nil
	case 0:
		return "", fmt.Errorf("%s has no checkout called %s", host, want)
	default:
		return "", fmt.Errorf("%s has %d checkouts called %s: %s", host, len(matched), want, strings.Join(matched, ", "))
	}
}

// newCreateCommand starts a new design document and opens on it, which is the
// front of the design, build, review loop.
func newCreateCommand(sessionID, agent, provider, model, autoCompact, role *string, noOpen, sandboxed, foreground *bool, docsDir *string, idle *time.Duration) *cobra.Command {
	var title, linear, base string
	var also []string
	var issue int

	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Start a design document and open it in the browser",
		Long: `create scaffolds a markdown design document and opens ade on the Design tab.

Talk the design through with the agent, then press Go to have it build what the
document describes, and switch to the Diff tab to review what it did.

The repository is checked out beside itself, on a branch named after the
document, to build it in. Given --linear or --github, the document is named
after the ticket and drafted from it.`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			selectedProvider, err := providerFor(*agent, *provider, cmd.Flags().Changed("agent"))
			if err != nil {
				return err
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			if ticket := strings.TrimSpace(linear); ticket != "" {
				if name != "" {
					return fmt.Errorf("--linear already names the document; drop the positional argument")
				}
				name = ticket
			}
			if issue > 0 {
				if linear != "" {
					return fmt.Errorf("--github and --linear are two starting points; pick one")
				}
				if name != "" {
					return fmt.Errorf("--github already names the document; drop the positional argument")
				}
			}
			if issue <= 0 && strings.TrimSpace(name) == "" && strings.TrimSpace(title) == "" {
				return fmt.Errorf("give the document a name, e.g. ade create cart-redesign")
			}
			return run(cmd.Context(), settings{
				diff:             gitdiff.Options{Scope: gitdiff.ScopeBranch, Context: 3},
				sessionID:        *sessionID,
				provider:         selectedProvider,
				providerExplicit: cmd.Flags().Changed("agent") || cmd.Flags().Changed("provider"),
				model:            *model,
				autoCompact:      *autoCompact,
				openBrowser:      !*noOpen,
				sandboxed:        *sandboxed,
				sandboxExplicit:  cmd.Flags().Changed("sandbox"),
				foreground:       *foreground,
				idle:             *idle,
				docsDir:          *docsDir,
				createName:       name,
				createTitle:      title,
				createBase:       base,
				createAlso:       also,
				role:             *role,
				linearTicket:     strings.TrimSpace(linear),
				createIssue:      issue,
			})
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "document title (default: the file name)")
	cmd.Flags().StringVar(&linear, "linear", "",
		"start work on a ticket: names the document after it and checks the repo out beside itself on its own branch")
	cmd.Flags().IntVar(&issue, "github", 0,
		"start work on a GitHub issue of this repository, by number: reads it, names the document after it, and checks the repo out beside itself on its own branch")
	cmd.Flags().StringVar(&base, "base", "",
		"branch the new worktree from this ref rather than a freshly fetched main; HEAD is this checkout as it stands")
	cmd.Flags().StringArrayVar(&also, "also", nil,
		"another checkout to work in, given a worktree on the same branch; repeatable. The first is home: the agent runs there and the document goes there")
	return cmd
}

// newInvestigateCommand opens a session on a ticket in this checkout, under a
// skill. Nothing is built, so there is no document and no worktree.
func newInvestigateCommand(sessionID, agent, provider, model, autoCompact *string, noOpen, sandboxed, foreground *bool, idle *time.Duration) *cobra.Command {
	var ticket, skill string

	cmd := &cobra.Command{
		Use:   "investigate",
		Short: "Investigate a ticket in this checkout, under a Claude Code skill",
		Long: `investigate opens ade on this checkout with a ticket to look into.

The agent reads the ticket, then works out what is going on with whatever
reaches the evidence — logs, the database, the code in front of it. Its
instructions are a skill from ~/.claude/skills, ` + "`" + web.DefaultSkill + "`" + ` unless --skill
names another.

Nothing is built: no design document, no worktree, no branch.

It runs in a conversation of its own, on the host rather than in a container:
the evidence is behind this machine's MCP servers — logs, the database, the
cluster — and a sandboxed agent reaches none of them.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			selectedProvider, err := providerFor(*agent, *provider, cmd.Flags().Changed("agent"))
			if err != nil {
				return err
			}
			ticket = strings.TrimSpace(ticket)
			if !linear.ValidRef(ticket) {
				return fmt.Errorf("--linear takes a ticket identifier, e.g. FIX-623; got %q", ticket)
			}
			return run(cmd.Context(), investigateSettings(settings{
				diff:             gitdiff.Options{Scope: gitdiff.ScopeBranch, Context: 3},
				sessionID:        *sessionID,
				provider:         selectedProvider,
				providerExplicit: cmd.Flags().Changed("agent") || cmd.Flags().Changed("provider"),
				model:            *model,
				autoCompact:      *autoCompact,
				openBrowser:      !*noOpen,
				foreground:       *foreground,
				idle:             *idle,
				skillName:        strings.TrimSpace(skill),
				investigate:      ticket,
			}))
		},
	}
	cmd.Flags().StringVar(&ticket, "linear", "", "the ticket to investigate, e.g. FIX-623")
	cmd.Flags().StringVar(&skill, "skill", web.DefaultSkill,
		"skill under ~/.claude/skills whose SKILL.md is the agent's instructions")
	_ = cmd.MarkFlagRequired("linear")
	return cmd
}

// investigateSettings is what an investigation decides for itself: its own
// conversation, and the host, since the MCPs it needs are not in a container.
func investigateSettings(cfg settings) settings {
	if cfg.sessionID == "" {
		cfg.sessionID = sessions.None
	}
	cfg.sandboxed, cfg.sandboxExplicit = false, true
	return cfg
}

// settings is everything one run of the server needs. It is a struct rather
// than a parameter list because the list had outgrown being readable.
type settings struct {
	diff             gitdiff.Options
	sessionID        string
	provider         string
	providerExplicit bool
	model            string
	autoCompact      string
	prNumber         int
	openBrowser      bool

	// sandboxed runs the agent in a container, and sandboxExplicit is the flag
	// having been typed, which outranks what the checkout chose last time.
	sandboxed       bool
	sandboxExplicit bool

	// linearTicket is the ticket this run started from, drafted into the new
	// document once the server is up.
	linearTicket string

	// createIssue is a GitHub issue to start from; it names the document and
	// then becomes seedIssue, which is what the server drafts from.
	createIssue int
	seedIssue   int

	// foreground keeps the server in this terminal. Otherwise ade re-runs
	// itself in the background and returns the prompt.
	foreground bool

	// idle is how long a server with no browser attached waits before exiting.
	idle time.Duration

	// dev serves the page from ade's own source tree instead of the binary, so
	// working on its interface is a save rather than a rebuild.

	// noLauncher opens this checkout on its own port, with no rail.
	noLauncher bool

	// docsDir overrides where design documents live, and docFile is the one to
	// open on. createName, when set, scaffolds a new document first and checks
	// the repository out beside itself, on that document's branch, to work in.
	docsDir     string
	docFile     string
	createName  string
	createTitle string
	// createBase is the ref a new worktree branches from; empty fetches main.
	createBase string
	// createAlso are other checkouts that get a worktree on the same branch,
	// so one session can read and edit more than one repository.
	createAlso []string

	// role is the machine-wide prompt this session starts under, rather than
	// whichever one Settings has selected.
	role string

	// skillName is a skill to run the main chat under, investigate the ticket
	// it opens on. Both set by `ade investigate`.
	skillName   string
	investigate string
}

func run(ctx context.Context, cfg settings) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := gitdiff.RepoRoot(ctx, cwd)
	if err != nil {
		return err
	}
	cfg.diff.Repo = root

	gitDir, err := gitdiff.GitDir(ctx, root)
	if err != nil {
		return err
	}
	commonGitDir, err := gitdiff.CommonGitDir(ctx, root)
	if err != nil {
		return err
	}
	branch := gitdiff.Branch(ctx, root)

	// Before anything is created, so a number that does not resolve is a CLI
	// error rather than an empty Design tab.
	if cfg.createIssue > 0 {
		issue, err := ghpr.FetchIssue(ctx, root, cfg.createIssue)
		if err != nil {
			return err
		}
		// Said, not refused: starting from a closed issue is a fair thing to
		// want, and doing it by accident is not.
		if state := strings.ToUpper(strings.TrimSpace(issue.State)); state != "" && state != "OPEN" {
			fmt.Fprintf(os.Stderr, "issue #%d is %s\n", issue.Number, strings.ToLower(state))
		}
		cfg.createName = issueDocName(issue.Number, issue.Title)
		if strings.TrimSpace(cfg.createTitle) == "" {
			cfg.createTitle = strings.TrimSpace(issue.Title)
		}
		cfg.seedIssue = issue.Number
	}

	// Before anything composes a prompt: a role named here is this session's,
	// whatever Settings has selected machine-wide.
	bridge.UseRole(cfg.role)

	// Before the server starts, so a missing skill is a CLI error.
	var skill skills.Skill
	if cfg.skillName != "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		if skill, err = skills.Load(home, cfg.skillName); err != nil {
			return err
		}
		fmt.Println(skill.Path)
	}
	// Codex sees what Claude sees; the links are refreshed on every start.
	if home, err := os.UserHomeDir(); err == nil {
		if err := skills.ShareWithCodex(home); err != nil {
			slog.Warn("could not share skills with codex", "err", err)
		}
	}

	// A server already serving this checkout takes the investigation: a rival on
	// the same record would lose the one you are looking at.
	if cfg.investigate != "" {
		handled, err := investigateOn(ctx, gitDir, cfg.investigate, skill.Name, cfg.openBrowser)
		if err != nil {
			return err
		}
		if handled {
			return nil
		}
	}

	// One ade per checkout: hand back the running one's link rather than
	// starting a rival on another port with another token.
	//
	// Not when creating. Starting work on a ticket is a request to set that work
	// up, and a server already serving this checkout is serving something else
	// entirely — returning its link would do nothing and look like success.
	// Without this the running server's URL came back unchanged and --pr was
	// silently dropped, landing you on your own branch with no Review tab.
	if cfg.prNumber > 0 && cfg.createName == "" &&
		openedPROn(ctx, gitDir, cfg.prNumber, cfg.openBrowser) {
		return nil
	}
	if cfg.createName == "" && cfg.investigate == "" && reuse(ctx, session.Path(gitDir), cfg.openBrowser, cfg.sessionID, cfg.provider, cfg.model, cfg.providerExplicit, cfg.sandboxed) {
		return nil
	}
	docFile := cfg.docFile
	if cfg.createName != "" {
		// Scaffold before the server starts, so a failure here is a clear CLI
		// error rather than an empty Design tab.
		dir, err := docs.Dir(root, commonGitDir, cfg.docsDir)
		if err != nil {
			return err
		}
		// Running the same create again means carrying on with that design, so an
		// existing document is opened rather than refused.
		var created bool
		docFile, created, err = docs.Ensure(dir, cfg.createName, cfg.createTitle)
		if err != nil {
			return err
		}
		// Pin the server to where the document actually went. Resolving it again
		// from a worktree would pick a different directory, since a worktree has
		// no .scratch of its own.
		cfg.docsDir = dir
		fmt.Println(labelled(filepath.Join(dir, docFile), created))
		if !created {
			// Resuming: redrafting from the ticket throws away what the design
			// says. The issue stays — draftFromIssue leaves a drafted doc alone.
			cfg.linearTicket = ""
		}

		// Before the server starts, so a failure is a plain CLI error. The
		// document stays in the common git dir, which the worktree shares.
		var added bool
		root, gitDir, branch, added, err = intoWorktree(ctx, root, docFile, cfg.createBase)
		if err != nil {
			return err
		}
		cfg.diff.Repo = root
		fmt.Println(labelled(root, added))

		// The worktree may already have its own server from last time.
		if reuse(ctx, session.Path(gitDir), cfg.openBrowser, cfg.sessionID, cfg.provider, cfg.model, cfg.providerExplicit, cfg.sandboxed) {
			return nil
		}

		// After reuse: a session already serving is handed back even when a
		// repository it spans has moved since.
		if err := alongside(ctx, gitDir, branch, cfg.createAlso); err != nil {
			return err
		}
	}

	// Detached after any worktree switch, so the child serves the checkout the
	// work is actually in and records itself under that checkout's git dir,
	// which is where the parent then looks for it.
	if !cfg.foreground {
		return detach(root, gitDir, childArgs(cfg, docFile), cfg.openBrowser)
	}

	// Opened after any worktree switch, so the threads land in the checkout
	// actually being reviewed rather than the one ade was launched from.
	store, err := comments.Open(gitDir, branch)
	if err != nil {
		return err
	}

	if cfg.provider == "codex" && cfg.model == defaultModel {
		cfg.model = "gpt-5.6-terra"
	}
	webCfg := web.Config{
		Root:            root,
		Branch:          branch,
		GitDir:          gitDir,
		CommonGitDir:    commonGitDir,
		Session:         cfg.sessionID,
		ClaudeBin:       os.Getenv("ADE_CLAUDE_BIN"),
		CodexBin:        os.Getenv("ADE_CODEX_BIN"),
		Provider:        cfg.provider,
		Model:           cfg.model,
		AutoCompact:     cfg.autoCompact,
		Diff:            cfg.diff,
		Sandbox:         cfg.sandboxed,
		SandboxExplicit: cfg.sandboxExplicit,
		Idle:            cfg.idle,
		DocsDir:         cfg.docsDir,
		DocFile:         docFile,
		SeedTicket:      cfg.linearTicket,
		SeedIssue:       cfg.seedIssue,
		Version:         version,

		Skill:             skill.Prompt,
		SkillName:         skill.Name,
		SeedInvestigation: cfg.investigate,
	}
	webCfg.NoLauncher = cfg.noLauncher

	var prThreads []ghpr.Thread
	if cfg.prNumber > 0 {
		prThreads, err = configurePR(ctx, root, cfg.prNumber, &webCfg)
		if err != nil {
			return err
		}
	}

	server, err := web.New(webCfg, store)
	if err != nil {
		return err
	}

	if err := server.Run(ctx, cfg.openBrowser, prThreads); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// configurePR points the config at a pull request. When its head is checked out
// the diff comes from local git so Resolve's edits show up; otherwise it is a
// read-only snapshot fetched from GitHub.
func configurePR(ctx context.Context, root string, number int, cfg *web.Config) ([]ghpr.Thread, error) {
	pr, err := ghpr.Fetch(ctx, root, number)
	if err != nil {
		return nil, err
	}

	head, err := gitdiff.HeadSHA(ctx, root)
	if err != nil {
		return nil, err
	}

	info := &web.PRInfo{
		Number:  pr.Number,
		Title:   pr.Title,
		Author:  pr.Author,
		URL:     pr.URL,
		State:   pr.State,
		HeadRef: pr.HeadRef,
	}
	info.ResolveBlocked = web.ResolveBlockedReason(pr, head, gitdiff.Branch(ctx, root))
	cfg.PR = info

	if info.ResolveBlocked == "" {
		// The branch is checked out, so diff it locally against the PR's base.
		cfg.Diff.Base = pr.BaseRef
	} else {
		diff, err := ghpr.Diff(ctx, root, number)
		if err != nil {
			return nil, err
		}
		cfg.Snapshot = diff
	}

	comments, err := ghpr.Comments(ctx, root, number)
	if err != nil {
		return nil, err
	}
	return ghpr.GroupThreads(comments), nil
}

// detachWait bounds how long the parent waits for the child to come up before
// giving up on printing its URL.
const detachWait = 10 * time.Second

// connectWait bounds asking a launcher to bring a host up. It is an ssh
// handshake, a version check and a remote process start, not a loopback hop.
const connectWait = 2 * time.Minute

// detach re-runs this binary in the background and prints the URL it comes up
// on. The handshake is the session record rather than the child's stdout, so it
// does not depend on pipe buffering or on the child flushing.
// detach backgrounds a server for a checkout. state is where its log goes and
// record is the file it will write itself to, which the parent then watches.
func detach(dir, gitDir string, args []string, openBrowser bool) error {
	return detachTo(dir, statedir.Git(gitDir), session.Path(gitDir), args, openBrowser)
}

func detachTo(dir, state, record string, args []string, openBrowser bool) error {
	rec, err := startDetached(dir, state, record, args)
	if err != nil {
		return err
	}
	fmt.Println(rec.URL)
	// The child runs with --no-open, since it has no terminal to report a
	// failure to. Opening is the parent's job, and skipping it here was the
	// one way detaching did not behave like every other invocation.
	if openBrowser {
		openUnwatched(context.Background(), rec.URL, dir)
	}
	return nil
}

// openUnwatched shows a session in ade's window. dir empty is elsewhere.
func openUnwatched(_ context.Context, url, dir string) {
	if dir == "" {
		fmt.Println("open the session from the window's Sessions view:", url)
		return
	}
	if err := window.Open(dir); err != nil {
		fmt.Printf("%v; the session is at %s\n", err, url)
	}
}

// startDetached runs ade in the background and waits for it to record itself.
// It says nothing: a caller going on to do something else with the server —
// connect it to a host, say — has its own URL to print.
func startDetached(dir, state, record string, args []string) (*session.Record, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("find the ade binary: %w", err)
	}

	logPath := filepath.Join(state, "server.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o750); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) // #nosec G304 -- under the repo's own git dir
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", logPath, err)
	}
	defer func() { _ = logFile.Close() }()

	cmd := exec.Command(self, args...) // #nosec G204 -- this binary, with this run's own arguments
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start detached: %w", err)
	}

	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		slog.Debug("release detached process", "err", err)
	}

	deadline := time.Now().Add(detachWait)
	for time.Now().Before(deadline) {
		rec, ok := session.Read(record)
		if !ok || rec.PID != pid {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		return rec, nil
	}
	return nil, fmt.Errorf("detached ade (pid %d) did not come up; see %s", pid, logPath)
}

// newStopCommand shuts down the server running for this repository.
func newStopCommand() *cobra.Command {
	var stopLauncher bool

	cmd := &cobra.Command{
		Use:           "stop",
		Short:         "Stop the ade running for this repository",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// The launcher belongs to no repository, so it is stopped by name
			// rather than by where you happen to be standing.
			if stopLauncher {
				record, err := launcher.RecordPath()
				if err != nil {
					return err
				}
				rec, err := session.Stop(record, 5*time.Second)
				if err != nil {
					return err
				}
				fmt.Printf("stopped the ade launcher (pid %d)\n", rec.PID)
				return nil
			}

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			root, err := gitdiff.RepoRoot(cmd.Context(), cwd)
			if err != nil {
				return err
			}
			gitDir, err := gitdiff.GitDir(cmd.Context(), root)
			if err != nil {
				return err
			}

			rec, err := session.Stop(session.Path(gitDir), 5*time.Second)
			if err != nil {
				return err
			}
			fmt.Printf("stopped ade (pid %d)\n", rec.PID)
			return nil
		},
	}
	cmd.Flags().BoolVar(&stopLauncher, "launcher", false, "stop the session launcher instead")
	return cmd
}

// intoWorktree checks the repository out beside itself, on a branch named after
// the design document, and reports the checkout to serve instead. An existing
// one is reused, so coming back to a ticket lands where you left it.
//
// The document is deliberately not moved: it lives under the common git dir,
// which every worktree shares, so one design stays visible from both.
func intoWorktree(ctx context.Context, root, docFile, base string) (string, string, string, bool, error) {
	slug := strings.ToLower(web.Slug(docFile))
	if slug == "" {
		return "", "", "", false, fmt.Errorf("%q does not give a usable branch name", docFile)
	}

	path := gitdiff.WorktreePath(root, slug)
	// Resolved only if a worktree has to be made: reuse must not wait on a fetch.
	added, err := gitdiff.EnsureWorktreeFrom(ctx, root, path, slug, func() string {
		if base != "" {
			return base
		}
		return freshBase(ctx, root)
	})
	if err != nil {
		return "", "", "", false, err
	}
	if added {
		sow(ctx, root, path)
	}

	gitDir, err := gitdiff.GitDir(ctx, path)
	if err != nil {
		return "", "", "", false, err
	}
	return path, gitDir, gitdiff.Branch(ctx, path), added, nil
}

// alongside gives every other repository a worktree on the same branch and
// records them as this session's own mounts, read-write.
func alongside(ctx context.Context, gitDir, branch string, also []string) error {
	if len(also) == 0 {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	// Every path resolved before anything is made: the common failure is a
	// name that is not a checkout, and that must not cost half a session.
	repos := make([]string, 0, len(also))
	for _, dir := range also {
		repo, err := gitdiff.RepoRoot(ctx, dir)
		if err != nil {
			return fmt.Errorf("%s is not a checkout, so it cannot have a worktree: %w", dir, err)
		}
		repos = append(repos, repo)
	}

	paths := make([]sandbox.Path, 0, len(repos))
	var made []worktreeMade
	for _, repo := range repos {
		path, added, err := ensureAlso(ctx, repo, branch)
		if err != nil {
			unmake(ctx, made)
			return err
		}
		if added {
			made = append(made, worktreeMade{repo: repo, path: path})
			sow(ctx, repo, path)
		}
		fmt.Printf("also     %s\n", labelled(path, added))
		paths = append(paths, sandbox.Path{Path: path, RW: true})
	}
	if _, err = sandbox.SaveWorkspace(sandbox.SessionPath(gitDir), home, sandbox.Workspace{Paths: paths}); err != nil {
		unmake(ctx, made)
		return err
	}
	return nil
}

type worktreeMade struct{ repo, path string }

// unmake removes the worktrees this run made before a later repository failed.
// The branches stay: a branch costs nothing and losing one is not recoverable.
func unmake(ctx context.Context, made []worktreeMade) {
	for _, one := range made {
		if err := gitdiff.RemoveWorktreeForce(ctx, one.repo, one.path); err != nil {
			fmt.Fprintf(os.Stderr, "left %s behind: %v\n", one.path, err)
		}
	}
}

// ensureAlso is EnsureWorktreeFrom for a branch another repository named, and
// reports where it put it. The base ref is home's, so it is not used here.
func ensureAlso(ctx context.Context, repo, branch string) (string, bool, error) {
	// Git refuses a second checkout of a branch it already has out, so the one
	// it has out is the one this session works in.
	if at := gitdiff.WorktreeOn(ctx, repo, branch); at != "" {
		return at, false, nil
	}
	path := gitdiff.WorktreePath(repo, branch)
	if _, err := os.Stat(path); err == nil || !gitdiff.HasBranch(ctx, repo, branch) {
		added, err := gitdiff.EnsureWorktreeFrom(ctx, repo, path, branch, func() string {
			return freshBase(ctx, repo)
		})
		return path, added, err
	}
	return path, true, gitdiff.AddWorktreeOn(ctx, repo, path, branch)
}

// freshBase is gitdiff.FreshBase with the fallback said out loud, since the
// worktree it makes is not the one the command was asked for.
func freshBase(ctx context.Context, root string) string {
	ref, note := gitdiff.FreshBase(ctx, root)
	if note != "" {
		fmt.Println(note)
	}
	return ref
}

// sow puts the repository's untracked seed into a worktree just made. It is
// reported and never fatal: the checkout is there either way.
func sow(ctx context.Context, root, path string) {
	placed, err := worktrees.Sow(ctx, root, path)
	if len(placed) > 0 {
		fmt.Printf("carried  %s\n", strings.Join(placed, ", "))
	}
	if err != nil {
		fmt.Printf("not carried: %v\n", err)
	}
}

// issueDocName names the design the way GitHub names a branch from an issue:
// 412-cache-the-token. It becomes the branch, so it is ASCII or it is nothing.
func issueDocName(number int, title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	if len(slug) > 48 {
		slug = strings.Trim(slug[:48], "-")
	}
	if slug == "" {
		return fmt.Sprintf("issue-%d", number)
	}
	return fmt.Sprintf("%d-%s", number, slug)
}

// reuse hands back a server already running for this checkout. Asked for a
// particular session, it hands that to the server too: one server per
// checkout, switched to the conversation you picked, rather than a rival on
// another port for each conversation.
func reuse(ctx context.Context, record string, openBrowser bool, sessionID, provider, model string, providerExplicit, sandboxed bool) bool {
	rec, ok := session.Read(record)
	if !ok || !session.Live(ctx, rec) {
		return false
	}
	// The default model belongs to claude; codex's own default is the server's.
	if provider == "codex" && model == defaultModel {
		model = ""
	}
	// "none" included: the running server takes it as "start a conversation" on
	// the model picked with it, which is what New session has to mean here.
	if sessionID != "" {
		if err := resumeOn(ctx, rec.URL, sessionID, provider, model); err != nil {
			// The server is still the right one; it just could not switch —
			// mid-run, most likely. Say so rather than starting a second one.
			slog.Warn("the running ade could not switch session", "err", err)
		}
	} else if providerExplicit {
		var sandbox *bool
		if !sandboxed {
			sandbox = &sandboxed
		}
		if err := switchOn(ctx, rec.URL, provider, model, sandbox); err != nil {
			slog.Warn("the running ade could not switch provider", "err", err)
		}
	}
	fmt.Println(rec.URL)
	if openBrowser {
		openUnwatched(ctx, rec.URL, rec.Root)
	}
	return true
}

// labelled marks what was made now, so re-running create shows what it reused.
func labelled(path string, created bool) string {
	if created {
		return path
	}
	return path + "  (existing)"
}

// childArgs builds the arguments for a detached server.
//
// Anything the parent already did — creating the document, adding the worktree —
// is replaced by pointing the child at the result, so it serves that work rather
// than trying to do it again and failing on what already exists.
func childArgs(cfg settings, docFile string) []string {
	// --foreground is not optional: the child is already the background process,
	// and without it every server would fork another forever.
	args := []string{"--foreground", "--no-open"}
	if docFile != "" {
		args = append(args, "--doc", docFile)
	}
	if cfg.linearTicket != "" {
		args = append(args, "--draft-from", cfg.linearTicket)
	}
	if cfg.seedIssue > 0 {
		args = append(args, "--draft-from-issue", strconv.Itoa(cfg.seedIssue))
	}
	if cfg.skillName != "" {
		args = append(args, "--skill", cfg.skillName)
	}
	if cfg.investigate != "" {
		args = append(args, "--investigate", cfg.investigate)
	}
	if cfg.sessionID != "" {
		args = append(args, "--session", cfg.sessionID)
	}
	if cfg.provider != "" {
		agent := "claude"
		if cfg.provider == "codex" {
			agent = "codex"
		}
		args = append(args, "--agent", agent)
	}
	if cfg.model != "" {
		args = append(args, "--model", cfg.model)
	}
	if cfg.autoCompact != "" {
		args = append(args, "--autocompact", cfg.autoCompact)
	}
	if cfg.docsDir != "" {
		args = append(args, "--docs", cfg.docsDir)
	}
	if cfg.role != "" {
		args = append(args, "--role", cfg.role)
	}
	// Passed when typed even if it is the default, so the child knows it was
	// typed and does not take the checkout's remembered choice over it.
	if !cfg.sandboxed || cfg.sandboxExplicit {
		args = append(args, "--sandbox="+strconv.FormatBool(cfg.sandboxed))
	}
	args = append(args, "--idle", cfg.idle.String())

	if cfg.diff.Scope == gitdiff.ScopeCommitted {
		args = append(args, "--committed")
	}
	if cfg.diff.Base != "" {
		args = append(args, "--base", cfg.diff.Base)
	}
	if cfg.diff.Context > 0 {
		args = append(args, "--context", strconv.Itoa(cfg.diff.Context))
	}
	if cfg.prNumber > 0 {
		args = append(args, "--pr", strconv.Itoa(cfg.prNumber))
	}
	if cfg.diff.Rev != "" {
		args = append(args, cfg.diff.Rev)
	}
	return args
}

// newDocCommand groups the commands that act on design documents directly,
// rather than through the browser.
func newDocCommand(docsDir *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doc",
		Short: "Work with design documents",
	}
	cmd.AddCommand(newDocListCommand(docsDir))
	cmd.AddCommand(newDocArchiveCommand(docsDir))
	cmd.AddCommand(newDocRemoveCommand(docsDir))
	cmd.AddCommand(newDocWriteCommand())
	return cmd
}

func newDocRemoveCommand(docsDir *string) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "rm <document>",
		Short: "Delete a design document",
		Long: `rm deletes a design document, looking for it wherever designs live: the
repository's .scratch directory and the ade directory inside the git dir.

The name may be given with or without its .md extension.`,
		Aliases:       []string{"remove", "delete"},
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dirs, err := designDirs(cmd.Context(), *docsDir)
			if err != nil {
				return err
			}

			full, err := docs.Locate(dirs, args[0])
			if err != nil {
				return err
			}
			if !force {
				ok, err := confirm(cmd, fmt.Sprintf("Delete %s?", full))
				if err != nil {
					return err
				}
				if !ok {
					fmt.Println("left alone")
					return nil
				}
			}

			if _, err := docs.Delete(dirs, args[0]); err != nil {
				return err
			}
			fmt.Printf("deleted %s\n", full)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "delete without asking")
	return cmd
}

// designDirs resolves where this repository keeps design documents.
func designDirs(ctx context.Context, override string) ([]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	root, err := gitdiff.RepoRoot(ctx, cwd)
	if err != nil {
		return nil, err
	}
	commonGitDir, err := gitdiff.CommonGitDir(ctx, root)
	if err != nil {
		return nil, err
	}
	return docs.SearchDirs(root, commonGitDir, override), nil
}

// confirm asks before something irreversible. A non-terminal stdin answers no,
// so a script that meant to pass --force does not delete on a stray newline.
func confirm(cmd *cobra.Command, question string) (bool, error) {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N] ", question); err != nil {
		return false, err
	}

	reader := bufio.NewReader(cmd.InOrStdin())
	answer, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

// investigateOn hands the investigation to a server already serving this
// checkout. A live server that refuses is an error, not a fall-through.
func investigateOn(ctx context.Context, gitDir, ticket, skill string, openBrowser bool) (bool, error) {
	rec, ok := session.Read(session.Path(gitDir))
	if !ok || !session.Live(ctx, rec) {
		return false, nil
	}
	body, err := json.Marshal(map[string]string{"ticket": ticket, "skill": skill})
	if err != nil {
		return false, err
	}
	if err := sessions.Ask(ctx, rec.URL, http.MethodPost, "/api/investigate", string(body)); err != nil {
		return false, fmt.Errorf("the ade already serving this checkout could not take the investigation: %w", err)
	}

	fmt.Println(rec.URL)
	if openBrowser {
		openUnwatched(ctx, rec.URL, rec.Root)
	}
	return true, nil
}

// openedPROn asks a server already running for this checkout to open a pull
// request, and reports whether it took it. A refusal (usually: the agent is
// mid-run) leaves it alone and falls through to reuse.
func openedPROn(ctx context.Context, gitDir string, number int, openBrowser bool) bool {
	rec, ok := session.Read(session.Path(gitDir))
	if !ok || !session.Live(ctx, rec) {
		return false
	}
	if err := openPR(ctx, rec.URL, number); err != nil {
		slog.Warn("the running ade could not open the pull request", "pr", number, "err", err)
		return false
	}

	fmt.Println(rec.URL)
	if openBrowser {
		openUnwatched(ctx, rec.URL, rec.Root)
	}
	return true
}

func openPR(ctx context.Context, recURL string, number int) error {
	return sessions.Ask(ctx, recURL, http.MethodPost, "/api/prs/"+strconv.Itoa(number), "")
}

// switchOn asks a running server to move the conversation to another provider.
// noTurn: --agent asks for a provider, not for a turn nobody typed.
func switchOn(ctx context.Context, recURL, provider, model string, sandbox *bool) error {
	body, err := json.Marshal(map[string]any{"provider": provider, "model": model, "noTurn": true, "sandbox": sandbox})
	if err != nil {
		return err
	}
	return sessions.Ask(ctx, recURL, http.MethodPost, "/api/chat/switch", string(body))
}

func resumeOn(ctx context.Context, recURL, id, provider, model string) error {
	body, err := json.Marshal(map[string]string{"id": id, "provider": provider, "model": model})
	if err != nil {
		return err
	}
	return sessions.Ask(ctx, recURL, http.MethodPost, "/api/chat/resume", string(body))
}

// newDoneCommand finishes with a ticket's worktree, since create --linear makes
// one per ticket and nothing else removes them.
func newDoneCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "done",
		Short: "Finish with this workspace: stop its ade and remove the checkout",
		Long: `done is run inside a worktree, or inside a clone a worker made for a
session. It stops the ade serving it and removes the checkout: the worktree
here, the clone's session directory on a worker.

The branch and the design document are kept. A worker's clone takes its local
branch with it, so there the branch is kept only where it was pushed. The branch is your local record of
the work, and the design is why the change looks the way it does; neither is
expensive to keep and both are awkward to lose.

It refuses while anything is uncommitted or unpushed, because that work exists
nowhere else, and while a review comment written here has not gone to GitHub:
the thread store lives in this worktree and goes with it. --force skips those
checks and throws it away.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			root, err := gitdiff.RepoRoot(ctx, cwd)
			if err != nil {
				return err
			}
			// A worker's clone is the workspace: there is no worktree over
			// there, and the session directory around it is what goes.
			owned, cloned := provision.OwnedDir(root)
			if !cloned && !gitdiff.IsWorktree(ctx, root) {
				return fmt.Errorf("%s is the main checkout, not a worktree; done removes worktrees and a worker's clones", root)
			}

			branch := gitdiff.Branch(ctx, root)
			if !force {
				if err := safeToRemove(ctx, root); err != nil {
					return err
				}
			}

			// Stopped first: removing the directory out from under a running
			// server leaves it polling a checkout that is no longer there.
			gitDir, err := gitdiff.GitDir(ctx, root)
			if err != nil {
				return err
			}
			if rec, err := session.Stop(session.Path(gitDir), 5*time.Second); err == nil {
				fmt.Printf("stopped  ade (pid %d)\n", rec.PID)
			}

			if cloned {
				if err := os.RemoveAll(owned); err != nil {
					return fmt.Errorf("remove %s: %w", owned, err)
				}
				fmt.Printf("removed  %s\n", owned)
				fmt.Printf("kept     branch %s, wherever it was pushed\n", branch)
				return nil
			}

			// Removed from the main checkout, since the current directory is
			// about to stop existing.
			common, err := gitdiff.CommonGitDir(ctx, root)
			if err != nil {
				return err
			}
			if err := gitdiff.RemoveWorktree(ctx, filepath.Dir(common), root); err != nil {
				return err
			}

			fmt.Printf("removed  %s\n", root)
			fmt.Printf("kept     branch %s\n", branch)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "remove even with uncommitted or unpushed work")
	return cmd
}

// newPruneCommand removes the worktrees ade made and has finished with: the
// session worktrees beside the repository and the pull request review
// checkouts under its git dir. done is the wrong instrument for these in bulk:
// it runs inside the one worktree it removes.
func newPruneCommand() *cobra.Command {
	var force, dryRun bool

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove finished worktrees",
		Long: `prune removes the worktrees ade made for this repository and is finished with:
session worktrees beside it (<repo>-<branch>) and pull request review checkouts
under <git-dir>/ade/worktrees.

A worktree is kept if a ade is still serving it, if it holds uncommitted work, or
if it has commits that are not pushed or merged — work that exists nowhere else.
--force removes the unpushed ones too; the branch is kept, so the commits are
not lost, only the checkout. Uncommitted work is never removed by prune.
Branches are never touched.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			root, err := gitdiff.RepoRoot(ctx, cwd)
			if err != nil {
				return err
			}
			found, err := worktrees.Scan(ctx, root, force)
			if err != nil {
				return err
			}
			if len(found) == 0 {
				fmt.Println("no worktrees to prune")
				return nil
			}

			removed := 0
			for _, wt := range found {
				if !wt.Removable {
					fmt.Printf("kept     %-40s %s\n", wt.Path, keepReason(wt))
					continue
				}
				if dryRun {
					fmt.Printf("would remove %-36s %s\n", wt.Path, wt.Branch)
					continue
				}
				if err := worktrees.Remove(ctx, root, wt); err != nil {
					fmt.Printf("kept     %-40s %v\n", wt.Path, err)
					continue
				}
				fmt.Printf("removed  %s\n", wt.Path)
				removed++
			}

			if dryRun {
				return nil
			}
			// Clears the admin records git keeps for directories deleted by hand.
			common, err := gitdiff.CommonGitDir(ctx, root)
			if err != nil {
				return err
			}
			if err := gitdiff.PruneWorktrees(ctx, filepath.Dir(common)); err != nil {
				return err
			}
			if removed == 0 {
				fmt.Println("nothing to remove")
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "remove worktrees with unpushed commits too (the branch is kept)")
	cmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "say what would go without removing anything")
	return cmd
}

// newSyncCommand brings every checkout of the repository up to a freshly
// fetched base: one fetch, then a rebase in each worktree that can take one.
func newSyncCommand() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Rebase this repository's worktrees onto a fresh base",
		Long: `sync fetches the base branch once, fast-forwards the main checkout, and
rebases every session worktree onto it.

A worktree with uncommitted work is skipped, and so is a pull request review
checkout, which is detached and has no base to be behind. A rebase that
conflicts is aborted and reported: the worktree is left exactly as it was, for
its own ade to finish. Unpushed commits do not block a rebase — they are what
is being replayed — and neither does a worktree ade is serving.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			root, err := gitdiff.RepoRoot(ctx, cwd)
			if err != nil {
				return err
			}
			report, err := worktrees.Sync(ctx, root, dryRun)
			if err != nil {
				return err
			}

			if report.Note != "" {
				fmt.Printf("%s (%s)\n", report.Base, report.Note)
			} else {
				suffix := "s"
				if report.Fetched == 1 {
					suffix = ""
				}
				fmt.Printf("fetched %s (%d new commit%s)\n", report.Base, report.Fetched, suffix)
			}
			for _, res := range report.Results {
				label := res.Outcome
				if res.Main {
					label = "main " + label
				}
				fmt.Printf("%-16s %-40s %s\n", label, res.Path, res.Detail)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "say what would be rebased without rebasing anything")
	return cmd
}

// keepReason says why a worktree stays, in words.
func keepReason(wt worktrees.Worktree) string {
	switch wt.Keep {
	case worktrees.KeepServing:
		return "ade is serving it on " + wt.Serving
	case worktrees.KeepUncommitted:
		return fmt.Sprintf("%d file(s) here are uncommitted", wt.Uncommitted)
	case worktrees.KeepUnpushed:
		return fmt.Sprintf("%d commit(s) here are not pushed or merged; --force removes the checkout and keeps the branch", wt.Unpushed)
	}
	return wt.Keep
}

// safeToRemove refuses while the checkout holds work that exists nowhere else.
func safeToRemove(ctx context.Context, root string) error {
	status, err := gitdiff.WorktreeStatus(ctx, root)
	if err != nil {
		return err
	}
	if n := len(status.Staged) + len(status.Unstaged); n > 0 {
		return fmt.Errorf("%d file(s) here are uncommitted; commit them or pass --force", n)
	}
	if status.Upstream == "" {
		return fmt.Errorf("this branch has never been pushed, so removing it loses the work; push it or pass --force")
	}
	if status.Ahead > 0 {
		return fmt.Errorf("%d commit(s) here are unpushed; push them or pass --force", status.Ahead)
	}
	return unsentHere(ctx, root)
}

// unsentHere refuses while a review comment written here has not gone: the
// store is in this worktree's git dir and goes with it.
func unsentHere(ctx context.Context, root string) error {
	gitDir, err := gitdiff.GitDir(ctx, root)
	if err != nil {
		return nil //nolint:nilerr // the caller's own checks already said more
	}
	store, err := comments.Open(gitDir, gitdiff.Branch(ctx, root))
	if err != nil {
		return nil //nolint:nilerr // an unreadable store is not a reason to refuse
	}
	n := 0
	for _, thread := range store.List() {
		if thread.ExternalID == "" && !thread.Sent() && !thread.Withdrawn && !thread.Resolved {
			n++
		}
	}
	if n > 0 {
		return fmt.Errorf("%d review comment(s) here have not been sent to GitHub; send them or pass --force", n)
	}
	return nil
}

func newDocListCommand(docsDir *string) *cobra.Command {
	return &cobra.Command{
		Use:           "ls",
		Short:         "List design documents",
		Aliases:       []string{"list"},
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			dirs, err := designDirs(ctx, *docsDir)
			if err != nil {
				return err
			}

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			root, err := gitdiff.RepoRoot(ctx, cwd)
			if err != nil {
				return err
			}
			branch := gitdiff.Branch(ctx, root)
			commonGitDir, _ := gitdiff.CommonGitDir(ctx, root)
			_, current, matched := docs.Find(dirs, commonGitDir, branch, "")

			found := 0
			for _, dir := range dirs {
				tree, err := docs.Tree(dir, "")
				if err != nil {
					continue
				}
				for _, name := range flattenNames(tree) {
					mark := " "
					if matched && name == current {
						// The one this branch would open.
						mark = "*"
					}
					fmt.Printf("%s %-40s %s\n", mark, name, dir)
					found++
				}
			}
			if found == 0 {
				fmt.Println("no design documents; ade create <name> starts one")
			}
			return nil
		},
	}
}

func flattenNames(entries []render.FileEntry) []string {
	var out []string
	for _, entry := range entries {
		if entry.Dir {
			out = append(out, flattenNames(entry.Children)...)
			continue
		}
		out = append(out, entry.Path)
	}
	return out
}

// newPsCommand lists the servers running for this repository and its worktrees.
func newPsCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "ps",
		Short:         "List the ade servers running for this repository",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			running := reportLauncher(ctx)

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			root, err := gitdiff.RepoRoot(ctx, cwd)
			if err != nil {
				// The launcher belongs to no repository, so standing outside one
				// is not a reason to say nothing about it.
				if running > 0 {
					return nil
				}
				return err
			}
			paths, err := gitdiff.Worktrees(ctx, root)
			if err != nil {
				return err
			}

			for _, path := range paths {
				gitDir, err := gitdiff.GitDir(ctx, path)
				if err != nil {
					continue
				}
				rec, ok := session.Read(session.Path(gitDir))
				if !ok {
					continue
				}
				if session.Live(ctx, rec) {
					fmt.Printf("%-40s %s  pid %d\n", path, rec.URL, rec.PID)
					running++
					continue
				}
				// A record whose process has gone. Saying so is how you find one
				// worth clearing, and the next run in that checkout takes over.
				fmt.Printf("%-40s (stale record for pid %d)\n", path, rec.PID)
			}
			if running == 0 {
				fmt.Println("no ade running for this repository")
			}
			return nil
		},
	}
}

// reportLauncher prints the machine-wide launcher, if it is up, and says
// whether it counted.
func reportLauncher(ctx context.Context) int {
	record, err := launcher.RecordPath()
	if err != nil {
		return 0
	}
	rec, ok := session.Read(record)
	if !ok {
		return 0
	}
	if session.Live(ctx, rec) {
		fmt.Printf("%-40s %s  pid %d\n", "(launcher)", rec.URL, rec.PID)
		return 1
	}
	fmt.Printf("%-40s (stale record for pid %d)\n", "(launcher)", rec.PID)
	return 0
}

// newAgentsCommand manages the named agents the chat can call on with @name.
func newSaidCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "said <agent>",
		Short: "What a named agent said in this conversation",
		Long: `ade relays a named agent's answer to the main agent once, and a review's
findings are filed as diff comments rather than relayed at all. This reads them
back out of the conversation, so an agent can ask what @security actually found
instead of only what it was told.

Only works inside a running ade session, which is where the capability is.`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			answer, err := said.Ask(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), said.Format(answer))
			return nil
		},
	}
	return cmd
}

func newDocWriteCommand() *cobra.Command {
	var from string
	cmd := &cobra.Command{
		Use:   "write <name>",
		Short: "Replace a design document's contents",
		Long: `Design documents live under .git, which the agent harness treats as sensitive
and refuses to write to. This hands the new contents to the running ade, which
writes them where the document already is.

The document must exist: this replaces what you were asked to work on, and
creates nothing. The body comes from --from, or from standard input.

Only works inside a running ade session, which is where the capability is.`,
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var body []byte
			var err error
			if from != "" {
				body, err = os.ReadFile(from) // #nosec G304 -- a path the agent names on its own command line
				if err != nil {
					return fmt.Errorf("read %s: %w", from, err)
				}
			} else if body, err = io.ReadAll(cmd.InOrStdin()); err != nil {
				return fmt.Errorf("read the document from stdin: %w", err)
			}
			answer, err := docsave.Send(cmd.Context(), args[0], string(body))
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", answer.Path)
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "read the document from this file rather than standard input")
	return cmd
}

// newDriftCommand reads the design checklist back to the agent.
func newDriftCommand() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "drift",
		Short: "The design checklist, and what the last pass said about it",
		Long: `ade scores the branch against the open design document and files what it
contradicts as diff comments. The conversation gets a count and nothing else,
so this is how an agent reads which requirements are not done.

Only works inside a running ade session, which is where the capability is.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			state, err := drift.Ask(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), drift.Format(state, all))
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "the requirements already done as well as the outstanding ones")
	return cmd
}

func newRolesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "roles",
		Short: "The machine-wide prompt for new main chats",
		Long:  "Roles are Markdown prompts in ~/.ade/roles. Pick one in Settings to tune new main chats without changing a repository's CLAUDE.md.",
	}
	cmd.AddCommand(newRolesInstallCommand())
	cmd.AddCommand(newRolesListCommand())
	return cmd
}

func newRolesInstallCommand() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:           "install",
		Short:         "Install the default roles into ~/.ade/roles",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			done, err := roles.Install(home, force)
			for _, role := range done {
				fmt.Fprintf(cmd.OutOrStdout(), "%-9s %s  %s\n", role.State, role.Name, role.Path)
			}
			if err != nil {
				return err
			}
			kept := 0
			for _, role := range done {
				if role.State == "kept" {
					kept++
				}
			}
			if kept > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "%d already there and left alone; --force replaces them\n", kept)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace roles that are already there")
	return cmd
}

func newRolesListCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "ls",
		Short:         "List the machine-wide roles",
		Aliases:       []string{"list"},
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			view, err := roles.ViewOf()
			if err != nil {
				return err
			}
			if len(view.Roles) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no roles; run `ade roles install` for the defaults")
				return nil
			}
			for _, role := range view.Roles {
				mark := " "
				if role.Name == view.Default {
					mark = "*"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %-16s %d bytes\n", mark, role.Name, len(role.Prompt))
			}
			return nil
		},
	}
}

func newAgentsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "The named agents the chat can call with @name",
		Long: `Named agents are Claude Code agent definitions — ~/.claude/agents/<name>.md,
or .claude/agents in a repository — that ade calls from the chat with @name.
A "model: codex/…" line runs one on a Codex subscription instead.`,
	}
	cmd.AddCommand(newAgentsInstallCommand())
	cmd.AddCommand(newAgentsListCommand())
	return cmd
}

func newAgentsInstallCommand() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:           "install",
		Short:         "Install the default agents into ~/.claude/agents",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			done, err := agents.Install(home, force)
			for _, d := range done {
				fmt.Fprintf(cmd.OutOrStdout(), "%-9s @%s  %s\n", d.State, d.Name, d.Path)
			}
			if err != nil {
				return err
			}
			kept := 0
			for _, d := range done {
				if d.State == "kept" {
					kept++
				}
			}
			if kept > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "%d already there and left alone; --force replaces them\n", kept)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace agents that are already there")
	return cmd
}

func newAgentsListCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "ls",
		Short:         "List the agents this checkout can call",
		Aliases:       []string{"list"},
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, _ := os.UserHomeDir()
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			root, err := gitdiff.RepoRoot(cmd.Context(), cwd)
			if err != nil {
				root = cwd
			}
			list, skipped := agents.LoadAll(root, home)
			if len(list) == 0 && len(skipped) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no agents; run `ade agents install` for the defaults")
				return nil
			}
			for _, agent := range list {
				model := agent.Provider
				if agent.Model != "" {
					model += "/" + agent.Model
				}
				fmt.Fprintf(cmd.OutOrStdout(), "@%-16s %-5s %-22s %s\n", agent.Name, agent.Scope, model, agent.Description)
			}
			for _, skip := range skipped {
				fmt.Fprintf(cmd.ErrOrStderr(), "skipped %s: %s\n", skip.Path, skip.Error)
			}
			return nil
		},
	}
}

// newJobsCommand is the scheduled work the launcher's clock fires, for looking
// at and running without a browser.
func newJobsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "The scheduled jobs the launcher's clock fires",
		Long: `A job is a prompt on a schedule: .ade/jobs/<name>.md in a repository, or
~/.ade/jobs/ for your own. The launcher fires them, since it is the only
process up whatever you are working on. A job reads and reports; it does not
edit, commit or post.`,
	}
	cmd.AddCommand(newJobsListCommand())
	cmd.AddCommand(newJobsRunCommand())
	return cmd
}

func newJobsListCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "ls",
		Short:         "List the jobs this checkout can run",
		Aliases:       []string{"list"},
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			list, skipped := loadJobs(cmd)
			if len(list) == 0 && len(skipped) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no jobs; write one at ~/.ade/jobs/<name>.md")
				return nil
			}
			for _, job := range list {
				view := jobs.Viewed(job, jobs.ReadState(job))
				fmt.Fprintf(cmd.OutOrStdout(), "%-16s %-5s %-14s %-9s %s\n",
					job.Name, job.Scope, job.Schedule, jobStatus(view), jobLast(view))
			}
			for _, skip := range skipped {
				fmt.Fprintf(cmd.ErrOrStderr(), "skipped %s: %s\n", skip.Path, skip.Error)
			}
			return nil
		},
	}
}

func newJobsRunCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "run <name>",
		Short:         "Fire a job now, whatever its schedule says",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			list, _ := loadJobs(cmd)
			job, ok := jobs.Find(list, args[0])
			if !ok {
				return fmt.Errorf("no job called %q; `ade jobs ls` lists them", args[0])
			}
			dir, err := jobRoot(cmd, job)
			if err != nil {
				return err
			}
			var out jobs.Outcome
			if job.Allow == jobs.Writes {
				out = jobs.Start(cmd.Context(), job, dir, jobs.CreateStarter())
			} else {
				out = jobs.Fire(cmd.Context(), job, dir, jobs.ColdRunner(""))
			}
			if out.Error != "" {
				return errors.New(out.Error)
			}
			if out.Report == "" {
				fmt.Fprintln(cmd.OutOrStdout(), jobs.Nothing)
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), out.Report)
			if out.URL != "" {
				fmt.Fprintln(cmd.OutOrStdout(), out.URL)
			}
			return nil
		},
	}
}

// loadJobs is the jobs visible from here: this repository's, then the user's.
func loadJobs(cmd *cobra.Command) ([]jobs.Job, []jobs.Skipped) {
	home, _ := os.UserHomeDir()
	return jobs.LoadAll(repoRootOrCwd(cmd), home)
}

// jobRoot is where a job runs: its project's checkout when it names one, and
// this one otherwise.
func jobRoot(cmd *cobra.Command, job jobs.Job) (string, error) {
	here := repoRootOrCwd(cmd)
	if job.Project == "" {
		return here, nil
	}
	cfg, err := adesettings.Load()
	if err != nil {
		return "", err
	}
	return jobs.Checkout(cmd.Context(), job, sessions.Repos(cfg.Roots(), cfg.Ignored), here)
}

func repoRootOrCwd(cmd *cobra.Command) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	root, err := gitdiff.RepoRoot(cmd.Context(), cwd)
	if err != nil {
		return cwd
	}
	return root
}

// jobStatus is the one word a list shows: what is standing in the way, if
// anything is.
func jobStatus(view jobs.View) string {
	switch {
	case view.Paused:
		return "paused"
	case view.Failing:
		return "failing"
	case view.LastFired == nil:
		return "never run"
	case view.NextDue == nil:
		return "no window"
	default:
		return "ok"
	}
}

// jobLast is when the job last fired and what came of it.
func jobLast(view jobs.View) string {
	if len(view.Outcomes) == 0 {
		return ""
	}
	last := view.Outcomes[0]
	return fmt.Sprintf("%s  %s", last.FiredAt.Format(time.RFC3339), last.Verdict())
}
