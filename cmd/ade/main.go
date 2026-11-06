package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/owenrumney/ade/internal/runlog"
	"github.com/owenrumney/ade/internal/window"

	"github.com/owenrumney/ade/internal/agents"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/fdlimit"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/launcher"
	"github.com/owenrumney/ade/internal/logs"
	"github.com/owenrumney/ade/internal/port"
	"github.com/owenrumney/ade/internal/scratch"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
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
		committed   bool
		base        string
		contextLine int
		noOpen      bool
		sessionID   string
		prNumber    int
		agent       string
		provider    string
		model       string
		foreground  bool
		detachFlag  bool
		idle        time.Duration
		noLauncher  bool
	)

	cmd := &cobra.Command{
		Use:   "ade [revspec]",
		Short: "Host an agent's terminal beside a review of what it did",
		Long: `ade hosts an agent in its own terminal, one session per checkout, and reviews
what it did.

With no arguments it opens the launcher: every checkout ade knows, and what is
waiting on you across all of them. "ade ." opens this checkout instead, on the
diff — the merge base of your default branch against the working tree, so
uncommitted edits and new files are included.

  ade                                pick a checkout, from anywhere
  ade start                          bring the launcher up, without a browser
  ade .                              review the branch in this checkout
  ade --agent codex .                work in this checkout with Codex
  ade create --linear FIX-623        a worktree and a session for a ticket
  ade create --github 412            the same, from a GitHub issue of this repository
  ade done                           stop the server and remove the worktree
  ade agents install                 the default @agents, into ~/.claude/agents`,
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
				return runLaunch(cmd.Context(), noOpen, foreground, idle, 0)
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
			if prNumber > 0 {
				if opts.Rev != "" {
					return fmt.Errorf("--pr and a revspec select different diffs; pass one")
				}
				if base != "" {
					return fmt.Errorf("--pr already sets the base from the pull request; drop --base")
				}
			}
			return run(cmd.Context(), settings{
				diff:             opts,
				sessionID:        sessionID,
				provider:         selectedProvider,
				providerExplicit: cmd.Flags().Changed("agent") || cmd.Flags().Changed("provider"),
				model:            model,
				prNumber:         prNumber,
				openBrowser:      !noOpen,
				foreground:       foreground,
				idle:             idle,
				noLauncher:       noLauncher,
			})
		},
	}

	// Flags every mode needs are persistent so subcommands inherit them; the
	// rest select a diff, which only the root command does.
	shared := cmd.PersistentFlags()
	shared.BoolVar(&noOpen, "no-open", false, "print the URL without opening a browser")
	shared.BoolVar(&noLauncher, "no-launcher", false, "open this checkout on its own port, without the launcher or the rail of other sessions")
	shared.StringVar(&agent, "agent", "claude", "main agent: claude (default) or codex")
	shared.StringVar(&provider, "provider", "", "deprecated agent provider: claude or codex")
	_ = shared.MarkDeprecated("provider", "use --agent claude or --agent codex instead")
	shared.StringVar(&model, "model", defaultModel, "model for the agents, e.g. haiku or sonnet, or a \"[1m]\" variant for the long window")
	// Not defaulted from $CLAUDE_CODE_SESSION_ID. Inheriting the caller's
	// conversation sounded like a free head start and was the opposite: the
	// transcript only grows, a long one replays into the agent's much smaller
	// window and compacts every few minutes, and two processes end up appending
	// to one session file. The design document is the handoff, and it is a
	// better one — versioned, readable, and only what you meant to say.
	shared.StringVar(&sessionID, "session", "", `Agent session to resume.
Defaults to the one this checkout was last in, so a restart lands back in the
conversation. Pass an id for a specific one, or "none" to start fresh.`)

	flags := cmd.Flags()
	flags.BoolVarP(&committed, "committed", "c", false, "start on the committed view; the browser can switch between branch, committed and uncommitted")
	flags.StringVarP(&base, "base", "b", "", "base branch to diff against (default: origin/main, then main)")
	flags.IntVarP(&contextLine, "context", "U", 3, "lines of context around each change")
	flags.IntVar(&prNumber, "pr", 0, "review a GitHub pull request by number, importing its review comments")
	cmd.Version = version

	shared.BoolVarP(&foreground, "foreground", "F", false, "run in this terminal instead of the background")
	// ade backgrounds itself now, so this says nothing. Kept because the
	// fingers that learned it should not get an error for it.
	shared.BoolVarP(&detachFlag, "detach", "d", false, "")
	_ = shared.MarkHidden("detach")
	shared.DurationVar(&idle, "idle", 30*time.Minute, "exit after this long with no browser attached (0 to stay up)")

	cmd.AddCommand(newCreateCommand(&sessionID, &agent, &provider, &model, &noOpen, &foreground, &idle))
	cmd.AddCommand(newLaunchCommand(&noOpen, &foreground, &idle))
	cmd.AddCommand(newStartCommand(&foreground, &idle))
	cmd.AddCommand(newHookCommand())
	cmd.AddCommand(newBadgeCommand())
	cmd.AddCommand(newShowCommand())
	cmd.AddCommand(newScratchCommand(&noOpen))
	cmd.AddCommand(newPortCommand(&noOpen))
	cmd.AddCommand(newStopCommand())
	cmd.AddCommand(newCancelCommand())
	cmd.AddCommand(newDoneCommand())
	cmd.AddCommand(newPsCommand())
	cmd.AddCommand(newAgentsCommand())
	cmd.AddCommand(newBrowserCommand())
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
			return runLaunch(cmd.Context(), true, *foreground, stay, 0)
		},
	}
	return cmd
}

func newLaunchCommand(noOpen, foreground *bool, idle *time.Duration) *cobra.Command {
	var port int

	cmd := &cobra.Command{
		Use:   "launch",
		Short: "Open the session picker, with no repository",
		Long: `launch serves the list of recent agent sessions and nothing else.

Pick one and ade opens in that checkout, the same as running ade there. Run it
from anywhere; unlike every other command it does not need a git repository.
`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLaunch(cmd.Context(), *noOpen, *foreground, *idle, port)
		},
	}
	// For a second launcher beside the running one, over its own HOME: what
	// `make shots` needs and nothing a normal run should name.
	cmd.Flags().IntVar(&port, "port", 0, "serve on this port instead of ade's own")
	return cmd
}

// runLaunch serves the picker, and is what both `ade launch` and a bare `ade`
// run: the launcher is where you land when you have not named a checkout.
func runLaunch(ctx context.Context, noOpen, foreground bool, idle time.Duration, port int) error {
	home, err := launcher.Home()
	if err != nil {
		return err
	}
	record, err := launcher.RecordPath()
	if err != nil {
		return err
	}
	// One launcher per machine: hand back the running one's link rather than
	// starting a rival on another port. A rival needs its own HOME, not --port.
	if reuse(ctx, record, false) {
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

// newCreateCommand makes a worktree for a piece of work and a session in it.
func newCreateCommand(sessionID, agent, provider, model *string, noOpen, foreground *bool, idle *time.Duration) *cobra.Command {
	var linear, base string
	var also []string
	var issue int

	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Check the repository out on a new branch and open a session there",
		Long: `create checks the repository out beside itself, on a branch named after the
work, and opens a session in that worktree.

Given --linear or --github, the branch is named after the ticket and the
session records it.`,
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
					return fmt.Errorf("--linear already names the work; drop the positional argument")
				}
				name = ticket
			}
			if issue > 0 {
				if linear != "" {
					return fmt.Errorf("--github and --linear are two starting points; pick one")
				}
				if name != "" {
					return fmt.Errorf("--github already names the work; drop the positional argument")
				}
			}
			if issue <= 0 && strings.TrimSpace(name) == "" {
				return fmt.Errorf("give the work a name, e.g. ade create cart-redesign")
			}
			return run(cmd.Context(), settings{
				diff:             gitdiff.Options{Scope: gitdiff.ScopeBranch, Context: 3},
				sessionID:        *sessionID,
				provider:         selectedProvider,
				providerExplicit: cmd.Flags().Changed("agent") || cmd.Flags().Changed("provider"),
				model:            *model,
				openBrowser:      !*noOpen,
				foreground:       *foreground,
				idle:             *idle,
				createName:       name,
				createBase:       base,
				createAlso:       also,
				ticket:           strings.ToUpper(strings.TrimSpace(linear)),
				createIssue:      issue,
			})
		},
	}
	cmd.Flags().StringVar(&linear, "linear", "",
		"start work on a ticket: names the branch after it and checks the repo out beside itself on its own branch")
	cmd.Flags().IntVar(&issue, "github", 0,
		"start work on a GitHub issue of this repository, by number: reads it, names the branch after it, and checks the repo out beside itself on its own branch")
	cmd.Flags().StringVar(&base, "base", "",
		"branch the new worktree from this ref rather than a freshly fetched main; HEAD is this checkout as it stands")
	cmd.Flags().StringArrayVar(&also, "also", nil,
		"another checkout to work in, given a worktree on the same branch; repeatable. The first is home: the agent runs there")
	return cmd
}

// settings is everything one run of the server needs. It is a struct rather
// than a parameter list because the list had outgrown being readable.
type settings struct {
	diff             gitdiff.Options
	sessionID        string
	provider         string
	providerExplicit bool
	model            string
	prNumber         int
	openBrowser      bool

	// ticket is what the work started from, kept on the session's record.
	ticket string

	// createIssue is a GitHub issue to start from; it names the branch.
	createIssue int

	// foreground keeps the server in this terminal. Otherwise ade re-runs
	// itself in the background and returns the prompt.
	foreground bool

	// idle is how long a server with no browser attached waits before exiting.
	idle time.Duration

	// noLauncher opens this checkout on its own port, with no rail.
	noLauncher bool

	// createName, when set, checks the repository out beside itself on a
	// branch of that name, to work in.
	createName string
	// createBase is the ref a new worktree branches from; empty fetches main.
	createBase string
	// createAlso are other checkouts that get a worktree on the same branch,
	// so one session can read and edit more than one repository.
	createAlso []string
}

// recordTicket keeps the ticket the work started from on the session's record.
func recordTicket(gitDir, ticket string) error {
	if ticket == "" {
		return nil
	}
	return runlog.Update(runlog.Path(gitDir), func(r *runlog.Record) { r.Ticket = ticket })
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
	// error rather than a worktree named after nothing.
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
		cfg.ticket = fmt.Sprintf("#%d", issue.Number)
	}

	// Codex sees what Claude sees; the links are refreshed on every start.
	if home, err := os.UserHomeDir(); err == nil {
		if err := skills.ShareWithCodex(home); err != nil {
			slog.Warn("could not share skills with codex", "err", err)
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
	if cfg.createName == "" && reuse(ctx, session.Path(gitDir), cfg.openBrowser) {
		return nil
	}
	if cfg.createName != "" {
		// Before the server starts, so a failure is a plain CLI error.
		var added bool
		root, gitDir, branch, added, err = intoWorktree(ctx, root, cfg.createName, cfg.createBase)
		if err != nil {
			return err
		}
		cfg.diff.Repo = root
		fmt.Println(labelled(root, added))

		// The worktree may already have its own server from last time.
		if reuse(ctx, session.Path(gitDir), cfg.openBrowser) {
			return nil
		}

		// After reuse: a session already serving is handed back even when a
		// repository it spans has moved since.
		if err := alongside(ctx, gitDir, branch, cfg.createAlso); err != nil {
			return err
		}
		if err := recordTicket(gitDir, cfg.ticket); err != nil {
			return err
		}
	}

	// Detached after any worktree switch, so the child serves the checkout the
	// work is actually in and records itself under that checkout's git dir,
	// which is where the parent then looks for it.
	if !cfg.foreground {
		return detach(root, gitDir, childArgs(cfg), cfg.openBrowser)
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
		Root:         root,
		Branch:       branch,
		GitDir:       gitDir,
		CommonGitDir: commonGitDir,
		Session:      cfg.sessionID,
		ClaudeBin:    os.Getenv("ADE_CLAUDE_BIN"),
		Provider:     cfg.provider,
		Model:        cfg.model,
		Diff:         cfg.diff,
		Idle:         cfg.idle,
		Version:      version,
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

// intoWorktree checks the repository out beside itself on a branch named for
// the work. An existing one is reused, so coming back lands where you left it.
func intoWorktree(ctx context.Context, root, name, base string) (string, string, string, bool, error) {
	slug := strings.ToLower(slugOf(name))
	if slug == "" {
		return "", "", "", false, fmt.Errorf("%q does not give a usable branch name", name)
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

// unsafeSlug is anything that has no business in a branch name or a directory.
var unsafeSlug = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func slugOf(name string) string {
	return strings.Trim(unsafeSlug.ReplaceAllString(filepath.Base(name), "-"), "-.")
}

// alongside gives every other repository a worktree on the same branch and
// records them on the session, so its diff can look in each.
func alongside(ctx context.Context, gitDir, branch string, also []string) error {
	if len(also) == 0 {
		return nil
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

	paths := make([]string, 0, len(repos))
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
		paths = append(paths, path)
	}
	if err := runlog.Update(runlog.Path(gitDir), func(r *runlog.Record) { r.Worktrees = paths }); err != nil {
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
	return gitdiff.EnsureWorktreeForBranch(ctx, repo, branch, func() string {
		return freshBase(ctx, repo)
	})
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

// reuse hands back a server already running for this checkout.
func reuse(ctx context.Context, record string, openBrowser bool) bool {
	rec, ok := session.Read(record)
	if !ok || !session.Live(ctx, rec) {
		return false
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
func childArgs(cfg settings) []string {
	// --foreground is not optional: the child is already the background process,
	// and without it every server would fork another forever.
	args := []string{"--foreground", "--no-open"}
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

// newDoneCommand finishes with a ticket's worktree, since create --linear makes
// one per ticket and nothing else removes them.
func newDoneCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "done",
		Short: "Finish with this workspace: stop its ade and remove the checkout",
		Long: `done is run inside a worktree. It stops the ade serving it and removes the
checkout.

The branch is kept: it is your local record of the work.

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
			if !gitdiff.IsWorktree(ctx, root) {
				return fmt.Errorf("%s is the main checkout, not a worktree; done removes worktrees", root)
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
