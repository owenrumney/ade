package bridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/owenrumney/ade/internal/gotmp"
	"github.com/owenrumney/ade/internal/limits"
	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/owenrumney/ade/internal/spawncap"
)

// Codex is a long-lived `codex app-server`, spoken to over stdio JSON-RPC.
// The envelope has no "jsonrpc" member: the generated schema carries none.
type Codex struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	done    chan struct{}
	settled chan struct{}

	mu        sync.Mutex
	sendMu    sync.Mutex
	writeMu   sync.Mutex
	waiters   map[uint64]chan codexResponse
	stderr    bytes.Buffer
	exitErr   error
	turnErr   error
	thread    string
	turn      string
	busy      atomic.Bool
	dead      atomic.Bool
	requestID atomic.Uint64
	opts      CodexOpts
}

// CodexOpts configures a Codex process backed by the user's ChatGPT login.
type CodexOpts struct {
	Bin          string
	Dir          string
	Resume       string
	Model        string
	SystemPrompt string
	Env          []string
	Sandbox      *sandbox.Spec
	Runtime      sandbox.Runtime

	// OTLPHost is the name the host's collector answers to from inside the
	// container. Empty leaves the user's `[otel]` endpoint alone.
	OTLPHost string

	// ReadOnly gives the turn a checkout it cannot write, for a reviewer.
	ReadOnly bool

	// NoMCP starts the server with no MCP servers configured; connecting to
	// them is most of the startup cost.
	NoMCP bool

	// WikiMCP is ade's wiki tool for this session: where it is from inside the
	// run, and the draft id that is its bearer token.
	WikiMCP *WikiMCP

	OnEvent  func(Event)
	OnLimits func(limits.Limits)
}

// WikiMCP is one streamable-http MCP server, as Codex configures them.
type WikiMCP struct {
	URL   string
	Token string
}

// wikiTokenEnv carries the bearer token: Codex reads it from the environment
// rather than the config, so the token never sits in argv.
const wikiTokenEnv = "ADE_WIKI_TOKEN"

// codexClientVersion identifies ade in the app-server's user agent.
const codexClientVersion = "1"

// codexSettleGrace is how long an interrupt gets to land before Send gives up
// waiting for the turn to close.
const codexSettleGrace = 5 * time.Second

type codexResponse struct {
	Result json.RawMessage
	Error  string
}

// codexEnvelope is any line off the wire: a response carries id and result or
// error, a server request carries id and method, a notification only method.
type codexEnvelope struct {
	ID     *uint64         `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// NewCodex starts the app server and opens a thread on it. Close must be
// called to reap the process.
func NewCodex(ctx context.Context, opts CodexOpts) (*Codex, error) {
	cmd, err := codexCommand(ctx, opts)
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	c := &Codex{cmd: cmd, stdin: stdin, done: make(chan struct{}), settled: make(chan struct{}, 1), waiters: map[uint64]chan codexResponse{}, opts: opts}
	if err := cmd.Start(); err != nil {
		slog.Error("could not start codex", "bin", cmd.Path, "dir", opts.Dir, "err", err)
		return nil, fmt.Errorf("start codex: %w", err)
	}
	slog.Info("codex app-server started", "bin", cmd.Path, "dir", opts.Dir, "pid", cmd.Process.Pid, "model", opts.Model)

	go c.readStdout(stdout)
	go c.readStderr(stderr)
	go func() {
		err := cmd.Wait()
		slog.Log(context.Background(), exitLevel(err), "codex exited", "pid", cmd.Process.Pid, "err", err)
		c.mu.Lock()
		c.exitErr = err
		c.mu.Unlock()
		c.dead.Store(true)
		close(c.done)
	}()
	if err := c.handshake(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// CodexOneShot runs one isolated Codex turn, for a named agent that needs it.
func CodexOneShot(ctx context.Context, opts CodexOpts, message string, images ...Image) (*Turn, error) {
	var text strings.Builder
	onEvent := opts.OnEvent
	opts.OnEvent = func(event Event) {
		if event.Type == "assistant" && event.Text != "" {
			text.WriteString(event.Text)
			text.WriteString("\n\n")
		}
		if onEvent != nil {
			onEvent(event)
		}
	}
	c, err := NewCodex(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	turn, err := c.Send(ctx, message, images...)
	if turn != nil {
		turn.Text = strings.TrimSpace(text.String())
	}
	return turn, err
}

// Send delivers a prompt and waits for the turn to close.
func (c *Codex) Send(ctx context.Context, message string, images ...Image) (*Turn, error) {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	c.busy.Store(true)
	defer c.busy.Store(false)

	c.mu.Lock()
	c.turnErr = nil
	thread := c.thread
	c.mu.Unlock()
	drainSettled(c.settled)

	params := map[string]any{"threadId": thread, "input": codexInput(message, images)}
	if model := codexModel(c.opts.Model); model != "" {
		params["model"] = model
	}
	if _, err := c.request(ctx, "turn/start", params); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		c.interruptAndDrain()
		return nil, ctx.Err()
	case <-c.done:
		return nil, c.exitError()
	case <-c.settled:
	}
	if c.opts.OnEvent != nil {
		c.opts.OnEvent(Event{SessionID: thread})
	}
	c.mu.Lock()
	turnErr := c.turnErr
	c.mu.Unlock()
	if turnErr != nil {
		return &Turn{SessionID: thread}, fmt.Errorf("codex: %w", turnErr)
	}
	return &Turn{SessionID: thread}, nil
}

// SendCommand answers /compact, which the protocol has a method for. Nothing
// else is a Codex command: the rest of Claude Code's slash set has no analogue.
func (c *Codex) SendCommand(ctx context.Context, command string) (*Turn, error) {
	if strings.TrimSpace(strings.TrimPrefix(command, "/")) != "compact" {
		return nil, fmt.Errorf("codex does not support %q", command)
	}
	// Held like a Send: compaction is a turn, and the caller's next prompt
	// would be a second one on an open thread, which the server refuses.
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	c.busy.Store(true)
	defer c.busy.Store(false)

	c.mu.Lock()
	thread := c.thread
	c.mu.Unlock()
	drainSettled(c.settled)

	if _, err := c.request(ctx, "thread/compact/start", map[string]any{"threadId": thread}); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		c.interruptAndDrain()
		return nil, ctx.Err()
	case <-c.done:
		return nil, c.exitError()
	case <-c.settled:
	}
	return &Turn{SessionID: thread}, nil
}

// Inject steers the open turn. The server rejects a steer that names a turn
// other than the live one, so the id held from turn/started is the whole point.
func (c *Codex) Inject(message string, images ...Image) error {
	c.mu.Lock()
	thread, turn := c.thread, c.turn
	c.mu.Unlock()
	if !c.Busy() || turn == "" {
		return errors.New("nothing is running to inject into")
	}
	_, err := c.request(context.Background(), "turn/steer", map[string]any{
		"threadId": thread, "expectedTurnId": turn, "input": codexInput(message, images),
	})
	return err
}

func (c *Codex) Busy() bool { return c.busy.Load() }
func (c *Codex) Dead() bool { return c.dead.Load() }

// Close kills the subprocess.
func (c *Codex) Close() error {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	killGroup(c.cmd)
	return nil
}

// Thread is the id a resume needs. Empty until the handshake has run.
func (c *Codex) Thread() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.thread
}

func codexCommand(ctx context.Context, opts CodexOpts) (*exec.Cmd, error) {
	args := codexArgs(opts)
	if opts.Sandbox != nil {
		if opts.Runtime == nil {
			return nil, errors.New("a sandbox needs a runtime")
		}
		spec := *opts.Sandbox
		spec.Argv = append([]string{"codex"}, args...)
		spec.Env = capped(withEnv(spec.Env, codexEnv(opts)))
		return sandbox.Command(ctx, opts.Runtime, spec), nil
	}
	bin, err := codexBin(opts.Bin)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204 -- bin resolved from PATH/env by user request
	ownGroup(cmd)
	cmd.Dir = opts.Dir
	cmd.Env = spawncap.Apply(gotmp.Apply(append(os.Environ(), codexEnv(opts)...)))
	return cmd, nil
}

// codexArgs is the command line; everything per-thread is a thread/start
// parameter. --strict-config catches a key Codex would otherwise drop.
func codexArgs(opts CodexOpts) []string {
	args := []string{"app-server", "--strict-config"}
	if override := otelEndpointOverride(codexConfig(opts), opts.OTLPHost); override != "" {
		args = append(args, "-c", override)
	}
	// Codex's read-only sandbox blocks writes, not capability. A reviewer that
	// keeps web search and every MCP server is not the confinement pi gave.
	if opts.ReadOnly {
		args = append(args, "-c", "tools.web_search=false")
	}
	if opts.NoMCP || opts.ReadOnly {
		args = append(args, "-c", "mcp_servers={}")
	} else if opts.WikiMCP != nil && opts.WikiMCP.URL != "" {
		args = append(args, "-c", "mcp_servers.ade_wiki.url="+strconv.Quote(opts.WikiMCP.URL),
			"-c", "mcp_servers.ade_wiki.bearer_token_env_var="+strconv.Quote(wikiTokenEnv))
	}
	return args
}

// codexEnv is the run's environment with the wiki token added when there is one.
func codexEnv(opts CodexOpts) []string {
	if opts.WikiMCP == nil || opts.WikiMCP.Token == "" || opts.NoMCP || opts.ReadOnly {
		return opts.Env
	}
	return append(append([]string{}, opts.Env...), wikiTokenEnv+"="+opts.WikiMCP.Token)
}

// codexConfig is the config.toml the spawned process will read: the box's copy
// when sandboxed, the user's own otherwise.
func codexConfig(opts CodexOpts) []byte {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	path := sandbox.CodexConfig(home)
	if opts.Sandbox == nil {
		path = sandbox.CodexHostConfig(home)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the user's own Codex configuration
	if err != nil {
		return nil
	}
	return data
}

func codexBin(bin string) (string, error) {
	if bin = strings.TrimSpace(bin); bin != "" {
		return bin, nil
	}
	found, err := exec.LookPath("codex")
	if err != nil {
		return "", errors.New("codex not found in PATH")
	}
	return found, nil
}

// codexModel drops the provider prefix a stored pi model may still carry.
func codexModel(model string) string {
	return strings.TrimPrefix(strings.TrimSpace(model), "openai-codex/")
}

func codexInput(message string, images []Image) []map[string]any {
	input := make([]map[string]any, 0, len(images)+1)
	for _, image := range images {
		input = append(input, map[string]any{"type": "image", "url": "data:" + image.MediaType + ";base64," + image.Data})
	}
	return append(input, map[string]any{"type": "text", "text": message})
}

// handshake initializes the connection and lands on a thread.
func (c *Codex) handshake(ctx context.Context) error {
	if _, err := c.request(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{"name": "ade", "version": codexClientVersion},
	}); err != nil {
		return fmt.Errorf("initialize codex: %w", err)
	}
	if err := c.notify("initialized", nil); err != nil {
		return err
	}
	method, params := "thread/start", c.threadParams()
	if c.opts.Resume != "" {
		method = "thread/resume"
		params["threadId"] = c.opts.Resume
	}
	raw, err := c.request(ctx, method, params)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	var started struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &started); err != nil || started.Thread.ID == "" {
		return fmt.Errorf("codex did not name a thread: %s", truncate(string(raw), 200))
	}
	c.mu.Lock()
	c.thread = started.Thread.ID
	c.mu.Unlock()
	return nil
}

func (c *Codex) threadParams() map[string]any {
	params := map[string]any{
		"cwd": c.opts.Dir,
		// Nobody is sitting at a TTY to answer an approval, so the turn must
		// never be able to ask for one.
		"approvalPolicy": "never",
		"sandbox":        codexSandboxMode(c.opts),
	}
	if model := codexModel(c.opts.Model); model != "" {
		params["model"] = model
	}
	if c.opts.SystemPrompt != "" {
		params["developerInstructions"] = c.opts.SystemPrompt
	}
	return params
}

// codexSandboxMode decides who owns the boundary. Inside ade's container the
// container is it, and Codex's own would only block writes ade wants.
func codexSandboxMode(opts CodexOpts) string {
	if opts.ReadOnly {
		return "read-only"
	}
	if opts.Sandbox != nil {
		return "danger-full-access"
	}
	return "workspace-write"
}

func (c *Codex) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.requestID.Add(1)
	waiter := make(chan codexResponse, 1)
	c.mu.Lock()
	c.waiters[id] = waiter
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.waiters, id)
		c.mu.Unlock()
	}()
	if err := c.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, fmt.Errorf("send %s to codex: %w", method, err)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, c.exitError()
	case response := <-waiter:
		if response.Error != "" {
			return nil, errors.New(response.Error)
		}
		return response.Result, nil
	}
}

func (c *Codex) notify(method string, params any) error {
	message := map[string]any{"method": method}
	if params != nil {
		message["params"] = params
	}
	return c.write(message)
}

func (c *Codex) write(message map[string]any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.stdin.Write(append(data, '\n'))
	return err
}

// interruptAndDrain stops the open turn and waits for it to close, so the next
// Send is not answered by the last turn's events.
func (c *Codex) interruptAndDrain() {
	c.mu.Lock()
	thread, turn := c.thread, c.turn
	c.mu.Unlock()
	if turn == "" {
		return
	}
	_ = c.write(map[string]any{
		"id": c.requestID.Add(1), "method": "turn/interrupt",
		"params": map[string]any{"threadId": thread, "turnId": turn},
	})
	select {
	case <-c.settled:
	case <-c.done:
	case <-time.After(codexSettleGrace):
	}
}

func (c *Codex) readStdout(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		c.handleLine(bytes.TrimSpace(scanner.Bytes()))
	}
}

func (c *Codex) handleLine(line []byte) {
	if len(line) == 0 {
		return
	}
	var envelope codexEnvelope
	if json.Unmarshal(line, &envelope) != nil {
		return
	}
	switch {
	case envelope.ID != nil && envelope.Method == "":
		c.deliver(*envelope.ID, envelope)
	case envelope.ID != nil:
		c.declineRequest(*envelope.ID, envelope.Method)
	default:
		c.handleNotification(envelope.Method, envelope.Params)
	}
}

func (c *Codex) deliver(id uint64, envelope codexEnvelope) {
	c.mu.Lock()
	waiter := c.waiters[id]
	c.mu.Unlock()
	if waiter == nil {
		return
	}
	out := codexResponse{Result: envelope.Result}
	if envelope.Error != nil {
		out.Error = envelope.Error.Message
	}
	waiter <- out
}

// declineRequest answers a server→client request rather than leaving it
// hanging: an unanswered approval stalls the turn forever.
func (c *Codex) declineRequest(id uint64, method string) {
	// Warn, not Debug: an experimental protocol asking for something new is
	// what this driver most needs to hear about.
	slog.Warn("declining a codex server request", "method", method)
	result := map[string]any{}
	switch {
	// The v1 methods take a ReviewDecision, which has no "decline" member.
	case method == "applyPatchApproval" || method == "execCommandApproval":
		result["decision"] = map[string]any{"denied": map[string]any{"rejection": "ade runs unattended"}}
	// By shape, not by name: the method set moves, and an approval the driver
	// has not heard of is the one that stalls the turn.
	case strings.Contains(method, "equestApproval"):
		result["decision"] = "decline"
	}
	_ = c.write(map[string]any{"id": id, "result": result})
}

func (c *Codex) handleNotification(method string, params json.RawMessage) {
	switch method {
	case "turn/started":
		var started struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		if json.Unmarshal(params, &started) == nil {
			c.mu.Lock()
			c.turn = started.Turn.ID
			c.mu.Unlock()
		}
		return
	case "turn/completed":
		c.mu.Lock()
		c.turn = ""
		c.mu.Unlock()
	case "account/rateLimits/updated":
		if c.opts.OnLimits != nil {
			if found, ok := codexRateLimits(params); ok {
				c.opts.OnLimits(found)
			}
		}
		return
	}
	event, ok := decodeCodexEvent(method, params)
	if !ok {
		return
	}
	if event.IsError {
		c.mu.Lock()
		c.turnErr = errors.New(event.Result)
		c.mu.Unlock()
	} else if c.opts.OnEvent != nil {
		c.opts.OnEvent(event)
	}
	if method == "turn/completed" {
		select {
		case c.settled <- struct{}{}:
		default:
		}
	}
}

func (c *Codex) readStderr(r io.Reader) {
	data, _ := io.ReadAll(r)
	c.mu.Lock()
	_, _ = c.stderr.Write(data)
	c.mu.Unlock()
}

func (c *Codex) exitError() error {
	c.mu.Lock()
	stderr, err := c.stderr.String(), c.exitErr
	c.mu.Unlock()
	if err != nil {
		return fmt.Errorf("codex exited: %w%s", err, stderrTail(stderr))
	}
	return fmt.Errorf("codex exited%s", stderrTail(stderr))
}

func drainSettled(settled chan struct{}) {
	select {
	case <-settled:
	default:
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
