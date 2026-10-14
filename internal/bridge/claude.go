package bridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/owenrumney/ade/internal/gotmp"
	"github.com/owenrumney/ade/internal/permissions"
	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/owenrumney/ade/internal/spawncap"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Claude is a long-lived `claude -p --input-format stream-json` process. Keeping
// it alive preserves turn context across messages and avoids per-message startup.
type Claude struct {
	opts   Opts
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	events chan Event
	done   chan error
	// gone closes once the process has been reaped, which is when it has let
	// go of its working directory.
	gone     chan struct{}
	mu       sync.Mutex
	writeMu  sync.Mutex
	stderrMu sync.Mutex
	asksMu   sync.Mutex
	asks     map[string]held
	stderr   bytes.Buffer

	// busy is whether a Send is in flight, which is when Inject makes sense.
	// dead is set once the process has gone, so the owner knows to start
	// another rather than keep talking to this one.
	busy atomic.Bool
	dead atomic.Bool

	// owed is whether a send is waiting for a result. Without one, events are a
	// turn Claude Code started itself, and go straight to OnEvent.
	routeMu sync.Mutex
	owed    bool
}

// Opts configures a Claude subprocess. Bin falls back to "claude" on PATH.
type Opts struct {
	Bin          string
	Dir          string
	Resume       string
	SystemPrompt string
	Model        string

	// PermissionMode maps to --permission-mode. Use "acceptEdits" for a run that
	// must edit files unattended while still gating shell commands.
	PermissionMode string

	// AutoCompact is the window Claude Code compacts the conversation at:
	// "auto", or a token count. Empty leaves Claude Code's own default.
	AutoCompact string

	// Sandbox runs claude in a container on Runtime rather than on the host.
	// The spec's Argv is filled in here; nil runs on the host as ever.
	Sandbox *sandbox.Spec
	Runtime sandbox.Runtime

	// Env is extra environment for the subprocess, as "K=V". In a container it
	// goes in through the spec, so the guest sees the same names.
	Env []string

	// MCPConfig is a --mcp-config ADE wrote for this run. A sandboxed run
	// takes ADE's servers through the spec instead, so this is for host runs.
	MCPConfig string

	// OnEvent receives every non-result event of a turn as it arrives, and
	// the result of a turn nobody sent: the only word that one ends on.
	OnEvent func(Event)

	// OnAsk fires when an ask is held or answered; neither emits an event.
	OnAsk func()

	// Rules gates the run's tool calls. Nil asks about everything the hook
	// reaches, which is what a run with no rule file would do anyway.
	Rules Rules

	// Hook is where the gate POSTs and HookToken authorizes it; HookBin is the
	// ade that carries it. Any of them empty leaves the run ungated.
	Hook      string
	HookBin   string
	HookToken string

	// DenyRules reach Claude Code as permissions.deny, which holds when ade
	// cannot be asked and in a sandbox, where nothing is gated.
	DenyRules []string
}

// Turn is the outcome of one exchange with the agent.
type Turn struct {
	Text    string
	Events  []Event
	CostUSD float64

	// SessionID is the session the turn ran in, from the first event that
	// named one. A one-shot's is a session nobody will want to come back to.
	SessionID string
}

// Image is a picture sent with a message, base64 encoded, as the API takes it.
type Image struct {
	MediaType string
	Data      string
}

// NewClaude starts the subprocess. Close must be called to reap it.
func NewClaude(ctx context.Context, opts Opts) (*Claude, error) {
	cmd, err := command(ctx, opts, claudeArgs(opts))
	if err != nil {
		return nil, err
	}
	return start(cmd, opts)
}

func command(ctx context.Context, opts Opts, args []string) (*exec.Cmd, error) {
	return claudeCommand(ctx, box{opts.Sandbox, opts.Runtime, opts.Bin, opts.Dir, opts.Env}, args)
}

type box struct {
	spec    *sandbox.Spec
	runtime sandbox.Runtime
	bin     string
	dir     string
	env     []string
}

// claudeCommand builds the process; in the container the binary is the image's.
func claudeCommand(ctx context.Context, in box, args []string) (*exec.Cmd, error) {
	if in.spec != nil {
		if in.runtime == nil {
			return nil, errors.New("a sandbox needs a runtime")
		}
		spec := *in.spec
		spec.Env = capped(withEnv(spec.Env, in.env))
		spec.Argv = claudeArgv(spec)
		spec.Args = withMCP(args, in.spec.Args)
		cmd := sandbox.Command(ctx, in.runtime, spec)
		cmd.Env = withoutAnthropicKeys(os.Environ())
		return cmd, nil
	}
	bin, err := resolveBin(in.bin)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204 -- bin resolved from PATH/env by user request
	ownGroup(cmd)
	cmd.Env = spawncap.Apply(gotmp.Apply(append(withoutAnthropicKeys(os.Environ()), in.env...)))
	cmd.Dir = in.dir
	return cmd, nil
}

// claudeArgv is the container's command. The prologue empties the image's
// config, unless a mount would land that write on the host's ~/.claude.json.
func claudeArgv(spec sandbox.Spec) []string {
	home := spec.Env["HOME"]
	if home == "" || mounted(spec.Mounts, filepath.Join(home, ".claude.json")) {
		return []string{"claude"}
	}
	return []string{"sh", "-c", `printf '%s\n' '{}' > "$HOME/.claude.json"; exec "$@"`, "ade", "claude"}
}

// mounted is whether path is inside something the container mounts, so a
// write to it is a write on the host.
func mounted(mounts []sandbox.Mount, path string) bool {
	for _, m := range mounts {
		if m.Guest == path || strings.HasPrefix(path, strings.TrimSuffix(m.Guest, "/")+"/") {
			return true
		}
	}
	return false
}

// withMCP is the run's arguments, then the box's MCP config unless the run
// named one: two --mcp-config flags merge, so an empty one guarantees nothing.
func withMCP(args, fromSpec []string) []string {
	out := append([]string{}, args...)
	if !slices.Contains(args, "--mcp-config") {
		out = append(out, fromSpec...)
	}
	if !slices.Contains(out, "--strict-mcp-config") {
		out = append(out, "--strict-mcp-config")
	}
	return out
}

// withEnv folds "K=V" pairs into a spec's environment, which the container
// takes as a map.
func withEnv(into map[string]string, pairs []string) map[string]string {
	if len(pairs) == 0 {
		return into
	}
	out := map[string]string{}
	for k, v := range into {
		out[k] = v
	}
	for _, pair := range pairs {
		if k, v, ok := strings.Cut(pair, "="); ok {
			out[k] = v
		}
	}
	return out
}

// WikiTool is the wiki's publish tool as Claude names it. acceptEdits does not
// cover MCP tools, and an unattended run has nobody to answer the ask.
const WikiTool = "mcp__ade_wiki__wiki_submit"

// claudeArgs is the command line for a long-lived session.
func claudeArgs(opts Opts) []string {
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose", // required by Claude Code when --print uses stream-json output
		"--permission-prompt-tool", "stdio",
	}
	// Always, not only when the caller asked: the main agent can edit code on
	// any turn, so the house rules apply to all of them.
	args = append(args, "--append-system-prompt", systemPrompt(opts.SystemPrompt))
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.PermissionMode != "" {
		args = append(args, "--permission-mode", opts.PermissionMode)
	}
	if opts.AutoCompact != "" {
		args = append(args, "--autocompact", opts.AutoCompact)
	}
	if opts.Resume != "" {
		args = append(args, "--resume", opts.Resume)
	}
	if opts.MCPConfig != "" && opts.Sandbox == nil {
		args = append(args, "--mcp-config", opts.MCPConfig, "--allowedTools", WikiTool)
	}
	if settings := runSettings(opts); settings != "" {
		args = append(args, "--settings", settings)
	}
	return args
}

// GatedTools is what the hook is registered for. Not everything: it is
// consulted per tool call, and deciding about a Read is a round trip wasted.
const GatedTools = "Bash|Write|Edit|MultiEdit|NotebookEdit|WebFetch|Task|AskUserQuestion|mcp__.*"

// runSettings is the --settings JSON: the gate and the deny list. Inline, so
// nothing is written to a settings file on disk.
func runSettings(opts Opts) string {
	settings := map[string]any{}
	if opts.Hook != "" && opts.HookBin != "" {
		// A command, not Claude Code's http hook. An http hook ade does not answer
		// lets the call proceed; `ade hook` answers even when ade is unreachable.
		hook := map[string]any{
			"type":    "command",
			"command": opts.HookBin + " hook",
			"timeout": int(hookGrace.Seconds()),
		}
		settings["hooks"] = map[string]any{
			preToolUse: []any{map[string]any{
				"matcher": GatedTools,
				"hooks":   []any{hook},
			}},
		}
	}
	if len(opts.DenyRules) > 0 {
		settings["permissions"] = map[string]any{"deny": opts.DenyRules}
	}
	if len(settings) == 0 {
		return ""
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// start runs the process and wires it up. It is what NewClaude does once the
// command is settled, and what a test uses to stand something else in.
func start(cmd *exec.Cmd, opts Opts) (*Claude, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	// Our pipe, not StdoutPipe's: Wait closes that one the moment the process
	// goes, and a result printed on the way out goes with it.
	stdout, toChild, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = toChild
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	c := &Claude{
		opts:   opts,
		cmd:    cmd,
		stdin:  stdin,
		events: make(chan Event, 64),
		done:   make(chan error, 1),
		gone:   make(chan struct{}),
	}
	started := time.Now()
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = toChild.Close()
		slog.Error("could not start the agent", "bin", cmd.Path, "dir", opts.Dir, "err", err)
		return nil, fmt.Errorf("start claude: %w", err)
	}
	// The child holds the only writer now, so the read side ends at its exit.
	_ = toChild.Close()
	slog.Info("agent started", "bin", cmd.Path, "dir", opts.Dir, "pid", cmd.Process.Pid,
		"model", opts.Model, "sandboxed", opts.Sandbox != nil, "resume", opts.Resume != "")

	go c.readStderr(stderr)
	go func() {
		defer func() { _ = stdout.Close() }()
		c.readStdout(stdout)
	}()
	go func() {
		err := cmd.Wait()
		c.dead.Store(true)
		close(c.gone)
		slog.Log(context.Background(), exitLevel(err), "agent exited",
			"pid", cmd.Process.Pid, "alive", time.Since(started).Round(time.Second).String(), "err", err)
		c.done <- err
	}()
	return c, nil
}

// exitLevel keeps a clean exit out of the Logs panel's default view, which
// opens at WARN.
func exitLevel(err error) slog.Level {
	if err != nil {
		return slog.LevelWarn
	}
	return slog.LevelInfo
}

// Send delivers one user message and blocks until that turn completes. A
// message injected while it waits is folded into the turn by Claude Code —
// delivered to the model at its next tool boundary, not answered as a turn of
// its own — so the one result is still the end of it. Images go with the
// message, ahead of the text.
func (c *Claude) Send(ctx context.Context, message string, images ...Image) (*Turn, error) {
	return c.send(ctx, message, images, false)
}

// SendCommand delivers a slash command. Claude Code runs a built-in like
// /usage or /compact locally, emits nothing for it, and holds its output for
// the next prompt — so on its own the turn would never end. If nothing has
// been said commandGrace after sending, a nudge asks for the output, which
// makes a turn of it. A command that is a skill expands into a turn of its
// own; a nudge that lands inside one is folded in like any injection.
func (c *Claude) SendCommand(ctx context.Context, command string) (*Turn, error) {
	return c.send(ctx, command, nil, true)
}

// commandGrace is how long a slash command gets to say something before it is
// taken to have run locally.
var commandGrace = 3 * time.Second

// commandNudge is what asks for a local command's output.
const commandNudge = "Show me what that command returned, verbatim."

// exitDrain is how long the events already on the wire get once the process
// has gone. Bounded: a grandchild holding stdout open would never close it.
var exitDrain = 2 * time.Second

func exitedError(err error, stderr string) error {
	if err != nil {
		return fmt.Errorf("claude exited: %w%s", err, stderr)
	}
	return fmt.Errorf("claude exited%s", stderr)
}

func (c *Claude) send(ctx context.Context, message string, images []Image, command bool) (turn *Turn, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.busy.Store(true)
	defer c.busy.Store(false)

	started := time.Now()
	slog.Info("turn started", "chars", len(message), "images", len(images), "command", command)
	defer func() {
		args := []any{"ms", time.Since(started).Milliseconds()}
		if turn != nil {
			args = append(args, "events", len(turn.Events), "cost", turn.CostUSD, "chars", len(turn.Text))
		}
		if err != nil {
			slog.Warn("turn failed", append(args, "err", err)...)
			return
		}
		slog.Info("turn finished", args...)
	}()

	c.drainStale()
	c.setOwed(true)
	defer c.setOwed(false)
	if err := c.write(message, images); err != nil {
		return nil, err
	}

	var grace <-chan time.Time
	if command {
		timer := time.NewTimer(commandGrace)
		defer timer.Stop()
		grace = timer.C
	}

	turn = &Turn{}
	// The exit is read once, then waited out: see the drain below.
	done, exit := c.done, error(nil)
	var drained <-chan time.Time
	// Turns are serialized by c.mu, so the next result event is this turn's.
	for {
		select {
		case <-ctx.Done():
			c.abortAndDrain()
			return nil, ctx.Err()
		case <-grace:
			grace = nil
			if err := c.write(commandNudge, nil); err != nil {
				return nil, err
			}
		case err := <-done:
			// A process that prints its result and exits at once makes both cases
			// ready, and select picks at random: read the rest before giving up.
			exit = err
			done = nil
			timer := time.NewTimer(exitDrain)
			defer timer.Stop()
			drained = timer.C
		case <-drained:
			return nil, exitedError(exit, c.stderrTail())
		case ev, ok := <-c.events:
			if !ok {
				// events closes before the waiter reports the exit, so the status
				// is a moment behind the close rather than absent.
				if drained == nil {
					timer := time.NewTimer(exitDrain)
					defer timer.Stop()
					select {
					case exit = <-done:
					case <-timer.C:
						return nil, fmt.Errorf("claude closed%s", c.stderrTail())
					}
				}
				return nil, exitedError(exit, c.stderrTail())
			}
			turn.Events = append(turn.Events, ev)
			if ev.Type == "assistant" {
				// The command turned out to be a turn after all.
				grace = nil
			}
			if ev.Type != "result" {
				if c.opts.OnEvent != nil {
					c.opts.OnEvent(ev)
				}
				continue
			}
			turn.CostUSD = ev.CostUSD
			if ev.IsError {
				return turn, TurnError(ev)
			}
			turn.Text = strings.TrimSpace(ev.Result)
			if turn.Text == "" {
				return turn, errors.New("claude completed without assistant text")
			}
			return turn, nil
		}
	}
}

// Inject slips a user message into the turn in flight, so what you typed
// while the agent was working reaches it without waiting. Claude Code hands it
// to the model at its next tool boundary, inside the same turn. It only makes
// sense while Busy; otherwise the message would sit unanswered until the next
// Send read its result as its own.
func (c *Claude) Inject(message string, images ...Image) error {
	if !c.busy.Load() {
		return errors.New("nothing is running to inject into")
	}
	return c.write(message, images)
}

// Busy reports whether a turn is in flight.
func (c *Claude) Busy() bool { return c.busy.Load() }

// Dead reports whether the process has exited. A dead one cannot be talked to
// and has to be replaced.
func (c *Claude) Dead() bool { return c.dead.Load() }

// closeGrace bounds the wait for a killed process to be reaped, so a wedged
// one cannot hold up the close forever.
const closeGrace = 5 * time.Second

// Close kills the subprocess and waits for it to go: until Wait has reaped it
// the files are still open, and a caller deleting the directory races it.
func (c *Claude) Close() error {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd == nil || c.cmd.Process == nil {
		return nil
	}
	killGroup(c.cmd)
	select {
	case <-c.gone:
	case <-time.After(closeGrace):
	}
	return nil
}

func (c *Claude) write(message string, images []Image) error {
	payload, err := json.Marshal(userMessage(message, images))
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.stdin.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("send prompt to claude: %w%s", err, c.stderrTail())
	}
	return nil
}

// userMessage is one stream-json input line. Text alone is sent as a string;
// with images it becomes content blocks, the pictures first so the text reads
// as a caption on them.
func userMessage(message string, images []Image) map[string]any {
	var content any = message
	if len(images) > 0 {
		blocks := make([]map[string]any, 0, len(images)+1)
		for _, img := range images {
			blocks = append(blocks, map[string]any{
				"type": "image",
				"source": map[string]any{
					"type":       "base64",
					"media_type": img.MediaType,
					"data":       img.Data,
				},
			})
		}
		blocks = append(blocks, map[string]any{"type": "text", "text": message})
		content = blocks
	}
	return map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": content,
		},
	}
}

func (c *Claude) readStdout(r io.Reader) {
	defer close(c.events)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if request, ok := decodeCanUseToolRequest(line); ok {
			if err := c.canUseTool(request); err != nil {
				slog.Warn("answer Claude permission request", "error", err)
			}
			continue
		}
		if ev, ok := DecodeEvent(line); ok {
			c.route(ev)
		}
	}
}

// route hands an event to the send waiting for it, or to OnEvent when nobody
// is: a wake on a background task's notification is a turn with no send.
func (c *Claude) route(ev Event) {
	c.routeMu.Lock()
	owed := c.owed
	if owed && ev.Type == "result" {
		c.owed = false
	}
	c.routeMu.Unlock()
	if owed {
		c.events <- ev
		return
	}
	if ev.Type == "result" {
		slog.Info("turn the agent started itself finished", "error", ev.IsError, "cost", ev.CostUSD)
	}
	if c.opts.OnEvent != nil {
		c.opts.OnEvent(ev)
	}
}

func (c *Claude) setOwed(owed bool) {
	c.routeMu.Lock()
	c.owed = owed
	c.routeMu.Unlock()
}

// drainStale clears what an aborted turn left behind, so its result is not
// read as the next turn's.
func (c *Claude) drainStale() {
	for {
		select {
		case ev, ok := <-c.events:
			if !ok {
				return
			}
			if ev.Type != "result" && c.opts.OnEvent != nil {
				c.opts.OnEvent(ev)
			}
		default:
			return
		}
	}
}

// askTool is the one tool whose permission request is a question for the user.
const askTool = "AskUserQuestion"

// refused is what the model reads when you deny without saying why.
const refused = "the user did not allow this tool call."

type canUseToolRequest struct {
	RequestID string `json:"request_id"`
	Request   struct {
		Subtype   string          `json:"subtype"`
		Tool      string          `json:"tool_name"`
		ToolUseID string          `json:"tool_use_id"`
		Input     json.RawMessage `json:"input"`
		// Suggestions is the rule Claude Code would write to remember this
		// answer, left as JSON so handing it back drops nothing.
		Suggestions []map[string]any `json:"permission_suggestions"`
	} `json:"request"`
}

// Ask is one thing waiting on the user. Rule is what allowing it for good
// would write, empty when there is nothing to remember.
type Ask struct {
	ToolID string `json:"toolId"`
	Tool   string `json:"tool"`
	Rule   string `json:"rule,omitempty"`

	// Target is what the ask is about — the command line, the path, the url —
	// so the card can offer its tokens as the rule to narrow.
	Target string `json:"target,omitempty"`

	// Allowed is the rules already covering the rest of a command.
	Allowed []string `json:"allowed,omitempty"`
}

// Remembering is how long an allow lasts. Empty is this call only.
const (
	RememberSession = "session"
	RememberAlways  = "always"
)

// rule is the one-line form of what remembering this answer would allow, in
// Claude Code's own wording so the button and the settings file agree.
func (r canUseToolRequest) rule() string {
	for _, suggestion := range r.Request.Suggestions {
		if suggestion["behavior"] != "allow" {
			continue
		}
		rules, _ := suggestion["rules"].([]any)
		for _, one := range rules {
			rule, _ := one.(map[string]any)
			content, _ := rule["ruleContent"].(string)
			tool, _ := rule["toolName"].(string)
			if content != "" {
				return tool + "(" + content + ")"
			}
		}
	}
	return ""
}

// scoped is the suggestion as given, or aimed at the session so that
// remembering it leaves nothing on disk.
func (r canUseToolRequest) scoped(remember string) []map[string]any {
	if remember != RememberSession {
		return r.Request.Suggestions
	}
	out := make([]map[string]any, 0, len(r.Request.Suggestions))
	for _, suggestion := range r.Request.Suggestions {
		copied := make(map[string]any, len(suggestion))
		for k, v := range suggestion {
			copied[k] = v
		}
		copied["destination"] = RememberSession
		out = append(out, copied)
	}
	return out
}

func decodeCanUseToolRequest(line []byte) (canUseToolRequest, bool) {
	var envelope struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(line, &envelope) != nil || envelope.Type != "control_request" {
		return canUseToolRequest{}, false
	}
	var request canUseToolRequest
	if json.Unmarshal(line, &request) != nil || request.Request.Subtype != "can_use_tool" || request.RequestID == "" || request.Request.Tool == "" {
		return canUseToolRequest{}, false
	}
	return request, true
}

// Answer replies to a pending AskUserQuestion, keyed by the tool call's id.
// answers maps each question's text to the chosen label, or the user's words.
func (c *Claude) Answer(toolUseID string, answers map[string]string) error {
	ask, ok := c.take(toolUseID)
	if !ok {
		return errors.New("no question is waiting for that answer")
	}
	return ask.settle(verdict{allow: true, answers: answers})
}

// Asking is everything waiting on the user. More than a count: the page marks
// the one card still open, and says what allowing it for good would write.
func (c *Claude) Asking() []Ask {
	c.asksMu.Lock()
	defer c.asksMu.Unlock()
	out := make([]Ask, 0, len(c.asks))
	for id, ask := range c.asks {
		out = append(out, Ask{ToolID: id, Tool: ask.tool(), Rule: ask.rule(), Target: ask.target(), Allowed: ask.allowed()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ToolID < out[j].ToolID })
	return out
}

// canUseTool holds every ask for the user. A permission request and a question
// pause the turn the same way; only the answer differs.
func (c *Claude) canUseTool(request canUseToolRequest) error {
	if request.Request.ToolUseID == "" {
		return c.writeControlResponse(request, map[string]any{"behavior": "deny", "message": refused})
	}
	if c.opts.Rules != nil && request.Request.Tool != askTool {
		// Free and the deny list only: a boxed run's allow rules are Claude
		// Code's own settings, and a call they cover never gets this far.
		decision := c.opts.Rules.Decide(request.Request.Tool, request.target())
		switch {
		case decision.Verdict == permissions.Deny:
			return c.writeControlResponse(request, map[string]any{"behavior": "deny",
				"message": "denied by your rule " + rulesText(decision.Rules)})
		case decision.Verdict == permissions.Allow && slices.Contains(decision.Rules, permissions.FreeRule):
			return c.writeControlResponse(request, map[string]any{"behavior": "allow"})
		}
	}
	c.hold(request.Request.ToolUseID, controlAsk{claude: c, request: request})
	return nil
}

// target is the one string a rule is written against, as the hook reads it.
func (r canUseToolRequest) target() string {
	var input map[string]any
	_ = json.Unmarshal(r.Request.Input, &input)
	return HookRequest{ToolInput: input}.Target()
}

// hold parks an ask under its tool call's id and tells the page there is one.
func (c *Claude) hold(toolUseID string, ask held) {
	c.asksMu.Lock()
	if c.asks == nil {
		c.asks = map[string]held{}
	}
	c.asks[toolUseID] = ask
	c.asksMu.Unlock()
	c.said()
	go c.giveUp(toolUseID)
}

// askGrace auto-denies a held ask. hookGrace is what Claude Code is told,
// above gate.Grace so ade's own answer always lands first.
var askGrace = 5 * time.Minute

const hookGrace = 6 * time.Minute

func (c *Claude) giveUp(toolUseID string) {
	timer := time.NewTimer(askGrace)
	defer timer.Stop()
	select {
	case <-c.gone:
		return
	case <-timer.C:
	}
	ask, ok := c.take(toolUseID)
	if !ok {
		return
	}
	slog.Warn("no answer to a permission ask, refusing it", "tool", ask.tool(), "after", askGrace)
	_ = ask.settle(verdict{reason: "nobody answered this in " + askGrace.String() +
		". Carry on without it, and say what you could not do."})
}

func (c *Claude) said() {
	if c.opts.OnAsk != nil {
		c.opts.OnAsk()
	}
}

// Decide answers a pending permission request. The reason reaches the model;
// remember is "", RememberSession or RememberAlways.
func (c *Claude) Decide(toolUseID string, allow bool, remember, reason string) error {
	ask, ok := c.take(toolUseID)
	if !ok {
		return errors.New("nothing is waiting on that decision")
	}
	if allow {
		return ask.settle(verdict{allow: true, remember: remember})
	}
	if strings.TrimSpace(reason) == "" {
		reason = refused
	}
	return ask.settle(verdict{reason: reason})
}

// Pending is the tool a waiting ask is for, so a caller can tell a question
// from a permission request without holding the frame.
func (c *Claude) Pending(toolUseID string) (string, bool) {
	c.asksMu.Lock()
	defer c.asksMu.Unlock()
	ask, ok := c.asks[toolUseID]
	if !ok {
		return "", false
	}
	return ask.tool(), ok
}

func (c *Claude) take(toolUseID string) (held, bool) {
	c.asksMu.Lock()
	ask, ok := c.asks[toolUseID]
	delete(c.asks, toolUseID)
	c.asksMu.Unlock()
	if ok {
		c.said()
	}
	return ask, ok
}

func (c *Claude) writeControlResponse(request canUseToolRequest, decision map[string]any) error {
	decision["toolName"] = request.Request.Tool
	response := map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": request.RequestID,
			"response":   decision,
		},
	}
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.stdin.Write(append(payload, '\n'))
	return err
}

func (c *Claude) readStderr(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			c.stderrMu.Lock()
			_, _ = c.stderr.Write(buf[:n])
			if c.stderr.Len() > 8192 {
				data := c.stderr.Bytes()
				c.stderr.Reset()
				_, _ = c.stderr.Write(data[len(data)-4096:])
			}
			c.stderrMu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// abortAndDrain interrupts the in-flight turn so the next Send starts clean.
func (c *Claude) abortAndDrain() {
	c.asksMu.Lock()
	c.asks = nil
	c.asksMu.Unlock()
	c.writeMu.Lock()
	_, _ = c.stdin.Write([]byte(`{"type":"control_request","request_id":"agentbridge-abort","request":{"subtype":"interrupt"}}` + "\n"))
	c.writeMu.Unlock()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return
		case ev, ok := <-c.events:
			if !ok || ev.Type == "result" {
				return
			}
		}
	}
}

func (c *Claude) stderrTail() string {
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()
	return stderrTail(c.stderr.String())
}

func resolveBin(bin string) (string, error) {
	if bin = strings.TrimSpace(bin); bin != "" {
		return bin, nil
	}
	found, err := exec.LookPath("claude")
	if err != nil {
		return "", errors.New("claude not found in PATH")
	}
	return found, nil
}

func TurnError(ev Event) error {
	if msg := strings.TrimSpace(ev.Result); msg != "" {
		return errors.New(msg)
	}
	if ev.Subtype != "" {
		return fmt.Errorf("claude turn failed: %s", ev.Subtype)
	}
	return errors.New("claude turn failed")
}

// BoxedTUI is the agent's interactive TUI in the box it ran in; argv starts with the binary.
func BoxedTUI(spec sandbox.Spec, argv, env []string) sandbox.Spec {
	spec.Env = withEnv(spec.Env, env)
	spec.TTY = true
	if len(argv) > 0 && argv[0] == "claude" {
		spec.Argv = claudeArgv(spec)
		spec.Args = withMCP(argv[1:], spec.Args)
		return spec
	}
	spec.Argv = argv
	return spec
}
