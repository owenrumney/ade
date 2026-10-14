package bridge

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/sandbox"
)

// OneShotOpts configures a single cold `claude -p` run with no session history.
type OneShotOpts struct {
	Bin    string
	Dir    string
	Model  string
	Prompt string

	// Agent names one of Claude Code's own agent definitions to run as, so
	// the definition's prompt, model and tools apply without being repeated
	// here.
	Agent string

	// Tools is the set of built-in tools the run has at all; nil leaves
	// Claude Code's default set. AllowedTools is which of those may run
	// without asking, e.g. read-only tools for a reviewer.
	Tools        []string
	AllowedTools []string

	// PermissionMode is what a run may do without asking. A one-shot has no
	// stdin left open to answer a prompt, so AllowedTools is what gates it.
	PermissionMode string

	// AddDirs are directories outside Dir the run may read; without one, a read there is a prompt nobody answers.
	AddDirs []string

	// SystemPrompt is appended to Claude Code's own. ade's house rules are
	// added to it for a run that can edit code.
	SystemPrompt string

	// Sandbox runs claude in a container on Runtime, as a session does.
	Sandbox *sandbox.Spec
	Runtime sandbox.Runtime

	// AutoCompact is the window Claude Code compacts at. A one-shot cannot be
	// resumed and retried, so the run has to survive its own reading.
	AutoCompact string

	// NoMCP starts claude without the configured MCP servers. Connecting to
	// them is most of the startup cost, and a turn that only transforms the
	// text it was given has nothing to ask them.
	NoMCP bool

	// NoSessionPersistence writes no transcript, for a run whose own record
	// would otherwise be read back as a conversation.
	NoSessionPersistence bool

	// Fork is a session to start from a copy of. Never a plain resume: that
	// would write into a conversation another process is still in.
	Fork string

	OnEvent func(Event)
}

// OneShot runs the prompt to completion in a fresh process and returns the turn.
// emptyMCP is a configuration with no servers in it.
const emptyMCP = `{"mcpServers":{}}`

// oneShotArgs is the command line for a single turn. The prompt goes on stdin:
// Linux caps one argument at 128 KiB and a review embeds a diff of up to 300 KiB.
func oneShotArgs(opts OneShotOpts) []string {
	args := []string{"-p", "--input-format", "text", "--output-format", "stream-json", "--verbose"}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Agent != "" {
		args = append(args, "--agent", opts.Agent)
	}
	if opts.Fork != "" {
		args = append(args, "--resume", opts.Fork, "--fork-session")
	}
	if opts.AutoCompact != "" {
		args = append(args, "--autocompact", opts.AutoCompact)
	}
	if opts.PermissionMode != "" {
		args = append(args, "--permission-mode", opts.PermissionMode)
	}
	// Only a run that can write: a read-only reviewer's "comments" are review
	// comments, and telling it to keep those down is the opposite of its job.
	if prompt := oneShotSystemPrompt(opts); prompt != "" {
		args = append(args, "--append-system-prompt", prompt)
	}
	if opts.Tools != nil {
		// Variadic like --allowedTools below. An empty list is passed as ""
		// which Claude Code reads as "no tools".
		args = append(args, "--tools")
		if len(opts.Tools) == 0 {
			args = append(args, "")
		} else {
			args = append(args, opts.Tools...)
		}
	}
	if len(opts.AllowedTools) > 0 {
		// --allowedTools is variadic, so each tool must be its own argument; one
		// space-joined string is read as a single bogus tool name and allows nothing.
		args = append(args, "--allowedTools")
		args = append(args, opts.AllowedTools...)
	}
	for _, dir := range opts.AddDirs {
		args = append(args, "--add-dir", dir)
	}
	if opts.NoMCP {
		// Both flags: the empty config replaces what is configured, and strict
		// stops the usual files being merged back in on top of it.
		args = append(args, "--strict-mcp-config", "--mcp-config", emptyMCP)
	}
	if opts.NoSessionPersistence {
		args = append(args, "--no-session-persistence")
	}
	return args
}

// oneShotSystemPrompt is what to append for one turn, or "" for nothing.
func oneShotSystemPrompt(opts OneShotOpts) string {
	if editsCode(opts.Tools) {
		return systemPromptWithoutRole(opts.SystemPrompt)
	}
	// A named agent writes a report, so the writing rules apply. A bare
	// one-shot rewrites one artifact and they would be noise on it.
	if strings.TrimSpace(opts.Agent) != "" || opts.Fork != "" {
		return readOnlyPromptWithoutRole(opts.SystemPrompt)
	}
	return strings.TrimSpace(opts.SystemPrompt)
}

// authRetryDelay is the pause before the second attempt, long enough for a
// token refresh already in flight to land.
var authRetryDelay = 2 * time.Second

// OneShot runs a single prompt, retrying once when the run died on the login
// before it streamed anything: the work never started, so a retry repeats nothing.
func OneShot(ctx context.Context, opts OneShotOpts) (*Turn, error) {
	var streamed bool
	first := opts
	if opts.OnEvent != nil {
		first.OnEvent = func(ev Event) {
			streamed = true
			opts.OnEvent(ev)
		}
	}
	turn, err := oneShot(ctx, first)
	if streamed || !IsAuthFailure(err) {
		return turn, err
	}
	slog.Warn("one-shot failed to authenticate, retrying", "agent", opts.Agent, "err", err)
	select {
	case <-ctx.Done():
		return turn, err
	case <-time.After(authRetryDelay):
	}
	return oneShot(ctx, opts)
}

// IsAuthFailure reports whether a run died on the login rather than the work.
func IsAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, sign := range []string{"oauth", "failed to authenticate", "authentication_error", "invalid api key"} {
		if strings.Contains(text, sign) {
			return true
		}
	}
	return false
}

func oneShot(ctx context.Context, opts OneShotOpts) (*Turn, error) {
	if strings.TrimSpace(opts.Prompt) == "" {
		return nil, fmt.Errorf("oneshot: empty prompt")
	}
	cmd, err := claudeCommand(ctx, box{opts.Sandbox, opts.Runtime, opts.Bin, opts.Dir, nil}, oneShotArgs(opts))
	if err != nil {
		return nil, err
	}
	cmd.Stdin = strings.NewReader(opts.Prompt)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		slog.Error("could not start a one-shot", "bin", cmd.Path, "dir", opts.Dir, "err", err)
		return nil, fmt.Errorf("start claude: %w", err)
	}
	started := time.Now()
	slog.Info("one-shot started", "dir", opts.Dir, "model", opts.Model, "sandboxed", opts.Sandbox != nil, "chars", len(opts.Prompt))

	turn := &Turn{}
	var runErr error
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		ev, ok := DecodeEvent(line)
		if !ok {
			continue
		}
		turn.Events = append(turn.Events, ev)
		if turn.SessionID == "" {
			turn.SessionID = ev.SessionID
		}
		if ev.Type != "result" {
			if opts.OnEvent != nil {
				opts.OnEvent(ev)
			}
			continue
		}
		turn.CostUSD = ev.CostUSD
		turn.Text = strings.TrimSpace(ev.Result)
		if ev.IsError {
			runErr = TurnError(ev)
		}
	}

	took := time.Since(started).Milliseconds()
	if err := cmd.Wait(); err != nil && runErr == nil {
		slog.Warn("one-shot failed", "ms", took, "err", err, "stderr", strings.TrimSpace(stderrTail(stderr.String())))
		return turn, fmt.Errorf("claude failed: %w%s", err, stderrTail(stderr.String()))
	}
	if runErr != nil {
		slog.Warn("one-shot errored", "ms", took, "events", len(turn.Events), "err", runErr)
		return turn, runErr
	}
	slog.Info("one-shot done", "ms", took, "events", len(turn.Events), "cost", turn.CostUSD, "chars", len(turn.Text))
	return turn, nil
}
