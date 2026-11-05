package oneshot

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/owenrumney/ade/internal/gotmp"
	"github.com/owenrumney/ade/internal/spawncap"
)

// Opts configures a single cold `claude -p` run with no session history.
type Opts struct {
	Bin    string
	Dir    string
	Model  string
	Prompt string

	// Agent names one of Claude Code's own agent definitions to run as.
	Agent string

	// Tools is the set the run has at all; nil leaves the default set.
	// AllowedTools is which of those may run without asking.
	Tools        []string
	AllowedTools []string

	// PermissionMode is what a run may do unasked. Nothing answers a prompt.
	PermissionMode string

	AddDirs []string

	// SystemPrompt is appended to Claude Code's own.
	SystemPrompt string

	// AutoCompact is the window Claude Code compacts at.
	AutoCompact string

	NoMCP bool

	NoSessionPersistence bool

	// Fork is a session to start from a copy of, never a plain resume.
	Fork string

	OnEvent func(Event)
}

const emptyMCP = `{"mcpServers":{}}`

// argv is the command line. The prompt goes on stdin: an argument is capped.
func argv(opts Opts) []string {
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
	if prompt := systemPrompt(opts); prompt != "" {
		args = append(args, "--append-system-prompt", prompt)
	}
	if opts.Tools != nil {
		// Variadic. An empty list is passed as "", which reads as no tools.
		args = append(args, "--tools")
		if len(opts.Tools) == 0 {
			args = append(args, "")
		} else {
			args = append(args, opts.Tools...)
		}
	}
	if len(opts.AllowedTools) > 0 {
		// Variadic: one space-joined string is read as a single bogus tool.
		args = append(args, "--allowedTools")
		args = append(args, opts.AllowedTools...)
	}
	for _, dir := range opts.AddDirs {
		args = append(args, "--add-dir", dir)
	}
	if opts.NoMCP {
		// Strict stops the usual files being merged back over the empty one.
		args = append(args, "--strict-mcp-config", "--mcp-config", emptyMCP)
	}
	if opts.NoSessionPersistence {
		args = append(args, "--no-session-persistence")
	}
	return args
}

func systemPrompt(opts Opts) string {
	if editsCode(opts.Tools) {
		return editPrompt(opts.SystemPrompt)
	}
	// A named agent writes a report; a bare run rewrites one artifact.
	if strings.TrimSpace(opts.Agent) != "" || opts.Fork != "" {
		return readOnlyPrompt(opts.SystemPrompt)
	}
	return strings.TrimSpace(opts.SystemPrompt)
}

// authRetryDelay is long enough for a token refresh in flight to land.
var authRetryDelay = 2 * time.Second

// Run runs the prompt to completion, retrying once when the run died on the
// login before it streamed anything.
func Run(ctx context.Context, opts Opts) (*Turn, error) {
	var streamed bool
	first := opts
	if opts.OnEvent != nil {
		first.OnEvent = func(ev Event) {
			streamed = true
			opts.OnEvent(ev)
		}
	}
	turn, err := run(ctx, first)
	if streamed || !IsAuthFailure(err) {
		return turn, err
	}
	slog.Warn("one-shot failed to authenticate, retrying", "agent", opts.Agent, "err", err)
	select {
	case <-ctx.Done():
		return turn, err
	case <-time.After(authRetryDelay):
	}
	return run(ctx, opts)
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

func run(ctx context.Context, opts Opts) (*Turn, error) {
	if strings.TrimSpace(opts.Prompt) == "" {
		return nil, errors.New("oneshot: empty prompt")
	}
	cmd, err := command(ctx, opts)
	if err != nil {
		return nil, err
	}
	cmd.Stdin = strings.NewReader(opts.Prompt)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("claude stdout: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		slog.Error("could not start a one-shot", "bin", cmd.Path, "dir", opts.Dir, "err", err)
		return nil, fmt.Errorf("start claude: %w", err)
	}
	started := time.Now()
	slog.Info("one-shot started", "dir", opts.Dir, "model", opts.Model, "chars", len(opts.Prompt))

	turn := &Turn{}
	var runErr error
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		ev, ok := DecodeEvent(bytes.TrimSpace(scanner.Bytes()))
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

// command is the process, in a group of its own so its tools die with it.
func command(ctx context.Context, opts Opts) (*exec.Cmd, error) {
	bin := strings.TrimSpace(opts.Bin)
	if bin == "" {
		found, err := exec.LookPath("claude")
		if err != nil {
			return nil, errors.New("claude not found in PATH")
		}
		bin = found
	}
	cmd := exec.CommandContext(ctx, bin, argv(opts)...) // #nosec G204 -- bin resolved from PATH/env by user request
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// CommandContext would signal the leader alone, and leave its `go test`.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.Env = spawncap.Apply(gotmp.Apply(withoutAnthropicKeys(os.Environ())))
	cmd.Dir = opts.Dir
	return cmd, nil
}
