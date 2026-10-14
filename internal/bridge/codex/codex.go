// Package codex drives OpenAI models on a Codex subscription: the same
// ChatGPT backend the codex CLI talks to, on the OAuth tokens its login left
// in ~/.codex/auth.json. No API key, no subprocess — the turn is a streamed
// HTTP call, and tools are run here in Go.
//
// The endpoint is unofficial. Shapes are read defensively and failures say
// what to do about them.
package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/authstore"
	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/limits"
)

// Credentials are the codex CLI's OAuth tokens.
type Credentials struct {
	AccessToken  string
	RefreshToken string
	AccountID    string
	// IDToken lives an hour and is what the CLI reads the account and plan
	// from. Dropping it on refresh leaves the CLI an expired one it trusts.
	IDToken string
}

// Tool is one thing the model may call, run here.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
	Run         func(ctx context.Context, args json.RawMessage) (string, error)
}

// Opts configures one run.
type Opts struct {
	// Model is the model id; empty falls back to the codex CLI's own
	// configured model, then to a sensible default.
	Model string
	// Effort is the reasoning effort, if any ("low", "medium", "high").
	Effort string
	// Instructions is the system prompt: the agent's definition.
	Instructions string
	Tools        []Tool
	Credentials  Credentials

	// OnEvent hears the turn as it goes, in the shape Claude's driver uses,
	// so the conversation is written the same way whichever answered.
	OnEvent func(bridge.Event)

	// OnLimits hears the subscription windows the backend reports on each
	// response, when it does.
	OnLimits func(limits.Limits)

	// Home is where refreshed tokens are kept (see SaveCredentials), so the
	// next run does not refresh again. Empty keeps them in memory only.
	Home string

	// BaseURL and TokenURL default to the ChatGPT backend; a test points
	// them elsewhere. Client likewise.
	BaseURL  string
	TokenURL string
	Client   *http.Client
}

// Model is one the account may use.
type Model struct {
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	ContextWindow int    `json:"contextWindow,omitempty"`
	DefaultEffort string `json:"defaultEffort,omitempty"`
}

const (
	defaultBaseURL  = "https://chatgpt.com/backend-api/codex"
	defaultTokenURL = "https://auth.openai.com/oauth/token" // #nosec G101 -- an endpoint, not a secret
	// clientID is the codex CLI's public OAuth client id (PKCE, no secret).
	clientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	// maxRounds caps tool-call round trips, so a model that keeps asking
	// for files cannot run up the subscription forever.
	maxRounds = 40
)

// LoadCredentials reads the codex CLI's login. One store, since ade writes
// that file too — but a copy ade left behind before that is read once.
func LoadCredentials(home string) (Credentials, error) {
	cli, err := readAuth(authPath(home))
	left, leftErr := readAuth(legacyPath(home))
	if leftErr != nil {
		return cli, err
	}
	if err != nil {
		return left, nil
	}
	if left.AccountID == cli.AccountID && writtenAfter(legacyPath(home), authPath(home)) {
		return left, nil
	}
	return cli, nil
}

// writtenAfter is whether a is newer than b; doubt goes to the CLI's login.
func writtenAfter(a, b string) bool {
	first, err := os.Stat(a)
	if err != nil {
		return false
	}
	second, err := os.Stat(b)
	if err != nil {
		return false
	}
	return first.ModTime().After(second.ModTime())
}

// SaveCredentials puts tokens ade refreshed in the CLI's own auth.json, which
// a rotating refresh has otherwise voided. Read-modify-write: not ade's file.
func SaveCredentials(home string, creds Credentials) error {
	if err := saveTo(authPath(home), creds); err != nil {
		return err
	}
	_ = os.Remove(legacyPath(home)) // superseded, and holding a token now void
	return nil
}

// saveTo is the same write against one named file: the login, or a box copy.
func saveTo(path string, creds Credentials) error {
	unlock, err := authstore.Lock(path)
	if err != nil {
		return err
	}
	defer unlock()
	auth := map[string]any{}
	data, err := os.ReadFile(path) // #nosec G304 -- the user's own login file
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &auth); err != nil {
			// The pair in hand is the only live one, whatever the file holds.
			slog.Warn("the codex login file does not parse; writing a fresh one", "path", path, "err", err)
			auth = map[string]any{}
		}
	}
	tokens, _ := auth["tokens"].(map[string]any)
	if tokens == nil {
		tokens = map[string]any{}
	}
	if held, _ := tokens["account_id"].(string); held != "" && creds.AccountID != "" && held != creds.AccountID {
		return fmt.Errorf("%s is logged in as another account; ade's tokens are not written over it", path)
	}
	tokens["access_token"] = creds.AccessToken
	tokens["refresh_token"] = creds.RefreshToken
	if creds.AccountID != "" {
		tokens["account_id"] = creds.AccountID
	}
	if creds.IDToken != "" {
		tokens["id_token"] = creds.IDToken
	}
	auth["tokens"] = tokens
	// The CLI refreshes on this field's age: a stale one rotates the pair away.
	auth["last_refresh"] = time.Now().UTC().Format(time.RFC3339)
	out, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}
	return authstore.Write(path, out)
}

func authPath(home string) string { return filepath.Join(home, ".codex", "auth.json") }

func legacyPath(home string) string { return filepath.Join(home, ".ade", "codex-auth.json") }

// readAuth reads a file in the CLI's auth.json shape.
func readAuth(path string) (Credentials, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- the user's own login file
	if err != nil {
		return Credentials{}, fmt.Errorf("no codex login at %s; run `codex login` first", path)
	}
	var auth struct {
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			AccountID    string `json:"account_id"`
			IDToken      string `json:"id_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &auth); err != nil {
		return Credentials{}, fmt.Errorf("read %s: %w", path, err)
	}
	if auth.Tokens.AccessToken == "" || auth.Tokens.RefreshToken == "" {
		return Credentials{}, fmt.Errorf("%s has no tokens; run `codex login`", path)
	}
	return Credentials{
		AccessToken:  auth.Tokens.AccessToken,
		RefreshToken: auth.Tokens.RefreshToken,
		AccountID:    auth.Tokens.AccountID,
		IDToken:      auth.Tokens.IDToken,
	}, nil
}

// Models lists what the account may use, as the codex CLI asks for it.
func Models(ctx context.Context, opts Opts) ([]Model, error) {
	fill(&opts)
	res, err := modelsRequest(ctx, &opts)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusUnauthorized {
		_ = res.Body.Close()
		if err := refreshInto(ctx, &opts); err != nil {
			return nil, err
		}
		if res, err = modelsRequest(ctx, &opts); err != nil {
			return nil, err
		}
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("codex backend: HTTP %d listing models", res.StatusCode)
	}
	var payload struct {
		Models []struct {
			Slug          string `json:"slug"`
			DisplayName   string `json:"display_name"`
			Visibility    string `json:"visibility"`
			Priority      int    `json:"priority"`
			ContextWindow int    `json:"context_window"`
			DefaultEffort string `json:"default_reasoning_level"`
		} `json:"models"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("read the codex model list: %w", err)
	}
	sort.SliceStable(payload.Models, func(i, j int) bool { return payload.Models[i].Priority < payload.Models[j].Priority })
	var out []Model
	for _, m := range payload.Models {
		if m.Slug == "" || (m.Visibility != "" && m.Visibility != "list") {
			continue
		}
		name := m.DisplayName
		if name == "" {
			name = m.Slug
		}
		out = append(out, Model{Slug: m.Slug, Name: name, ContextWindow: m.ContextWindow, DefaultEffort: m.DefaultEffort})
	}
	return out, nil
}

func modelsRequest(ctx context.Context, opts *Opts) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(opts.BaseURL, "/")+"/models?client_version="+cliVersion(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+opts.Credentials.AccessToken)
	req.Header.Set("chatgpt-account-id", opts.Credentials.AccountID)
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("originator", "codex_cli_rs")
	req.Header.Set("Accept", "application/json")
	return opts.Client.Do(req)
}

// cliVersion is what the codex CLI on this machine calls itself; the backend
// gates its model list on it. A version from before the list existed is the
// fallback when there is no CLI.
func cliVersion() string {
	const fallback = "0.100.0"
	path, err := exec.LookPath("codex")
	if err != nil {
		return fallback
	}
	out, err := exec.Command(path, "--version").Output() // #nosec G204 -- codex from PATH
	if err != nil {
		return fallback
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return fallback
	}
	return fields[len(fields)-1]
}

// DefaultModel is the model the codex CLI is configured to use, read from
// ~/.codex/config.toml, so a subscription tracks what OpenAI currently
// offers it rather than an id hardcoded here. Empty when unset.
func DefaultModel(home string) string {
	return configValue(home, "model")
}

// DefaultEffort is the codex CLI's configured reasoning effort.
func DefaultEffort(home string) string {
	return configValue(home, "model_reasoning_effort")
}

// configValue reads one top-level "key = "value"" line from config.toml. The
// file is TOML, but this is all that is wanted from it, so no parser.
func configValue(home, key string) string {
	data, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml")) // #nosec G304 -- the user's own config
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			break // into a table; top-level keys are done
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		return strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return ""
}

// Run is one turn: the prompt, as many tool round trips as the model wants,
// and the text it ends on.
func Run(ctx context.Context, opts Opts, prompt string) (*bridge.Turn, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("codex: empty prompt")
	}
	if opts.Credentials.AccessToken == "" {
		return nil, errors.New("codex: no credentials")
	}
	fill(&opts)
	tools := map[string]Tool{}
	for _, tool := range opts.Tools {
		tools[tool.Name] = tool
	}

	input := []map[string]any{{
		"type": "message", "role": "user",
		"content": []map[string]any{{"type": "input_text", "text": prompt}},
	}}
	turn := &bridge.Turn{}
	for round := 0; round < maxRounds; round++ {
		out, err := stream(ctx, &opts, input)
		if err != nil {
			return turn, err
		}
		ev := bridge.Event{Type: "assistant", Text: out.text, Usage: out.usage}
		for _, call := range out.calls {
			ev.ToolUses = append(ev.ToolUses, bridge.ToolUse{ID: call.id, Name: call.name, Input: json.RawMessage(call.args)})
		}
		turn.Events = append(turn.Events, ev)
		if opts.OnEvent != nil && (ev.Text != "" || len(ev.ToolUses) > 0) {
			opts.OnEvent(ev)
		}
		if len(out.calls) == 0 {
			turn.Text = strings.TrimSpace(out.text)
			if turn.Text == "" {
				return turn, errors.New("codex: the model finished without saying anything")
			}
			return turn, nil
		}

		// The model's turn goes back as it came, then each call's answer.
		if out.text != "" {
			input = append(input, map[string]any{
				"type": "message", "role": "assistant",
				"content": []map[string]any{{"type": "output_text", "text": out.text}},
			})
		}
		results := bridge.Event{Type: "user"}
		for _, call := range out.calls {
			input = append(input, map[string]any{
				"type": "function_call", "name": call.name, "arguments": call.args, "call_id": call.id,
			})
			content, failed := runTool(ctx, tools, call)
			input = append(input, map[string]any{
				"type": "function_call_output", "call_id": call.id, "output": content,
			})
			results.ToolResults = append(results.ToolResults, bridge.ToolResult{ID: call.id, Content: content, IsError: failed})
		}
		turn.Events = append(turn.Events, results)
		if opts.OnEvent != nil {
			opts.OnEvent(results)
		}
	}
	return turn, fmt.Errorf("codex: gave up after %d rounds of tool calls", maxRounds)
}

// fill applies the defaults.
func fill(opts *Opts) {
	if opts.Model == "" {
		opts.Model = "gpt-5"
	}
	if opts.BaseURL == "" {
		opts.BaseURL = defaultBaseURL
	}
	if opts.TokenURL == "" {
		opts.TokenURL = defaultTokenURL
	}
	if opts.Client == nil {
		opts.Client = &http.Client{} // streaming, so no timeout; ctx bounds it
	}
}

// refreshInto refreshes the tokens on opts and keeps them for next time.
func refreshInto(ctx context.Context, opts *Opts) error {
	refreshed, err := refresh(ctx, opts.Client, opts.TokenURL, opts.Credentials)
	if err != nil {
		return err
	}
	opts.Credentials = refreshed
	if opts.Home != "" {
		// Best effort: a failed write leaves `codex` holding a voided token.
		if err := SaveCredentials(opts.Home, refreshed); err != nil {
			slog.Warn("write the refreshed codex login back to the CLI's file", "err", err)
		}
	}
	return nil
}

func runTool(ctx context.Context, tools map[string]Tool, call toolCall) (string, bool) {
	tool, ok := tools[call.name]
	if !ok {
		return "no such tool: " + call.name, true
	}
	out, err := tool.Run(ctx, json.RawMessage(call.args))
	if err != nil {
		return err.Error(), true
	}
	return out, false
}

type toolCall struct {
	id, name, args string
}

type streamed struct {
	text  string
	calls []toolCall
	usage *bridge.Usage
}

// stream makes one request and reads it to the end. A 401 refreshes the
// tokens once and tries again; the CLI's login is the fallback for that.
func stream(ctx context.Context, opts *Opts, input []map[string]any) (streamed, error) {
	res, err := request(ctx, opts, input)
	if err != nil {
		return streamed{}, err
	}
	if res.StatusCode == http.StatusUnauthorized {
		_ = res.Body.Close()
		if err := refreshInto(ctx, opts); err != nil {
			return streamed{}, err
		}
		if res, err = request(ctx, opts, input); err != nil {
			return streamed{}, err
		}
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		msg := strings.TrimSpace(string(body))
		var detail struct {
			Detail string `json:"detail"`
		}
		if json.Unmarshal(body, &detail) == nil && detail.Detail != "" {
			msg = detail.Detail
		}
		err := fmt.Errorf("codex backend: HTTP %d: %s", res.StatusCode, msg)
		// A model the account cannot use is the likely 400, and the fix is
		// to name one it can: say which.
		if res.StatusCode == http.StatusBadRequest && strings.Contains(msg, "model") {
			if models, listErr := Models(ctx, *opts); listErr == nil && len(models) > 0 {
				slugs := make([]string, 0, len(models))
				for _, m := range models {
					slugs = append(slugs, m.Slug)
				}
				err = fmt.Errorf("%w (this account can use: %s)", err, strings.Join(slugs, ", "))
			}
		}
		return streamed{}, err
	}
	if opts.OnLimits != nil {
		if found, ok := limits.FromCodexHeaders(res.Header); ok {
			opts.OnLimits(found)
		}
	}
	return readStream(res.Body)
}

func request(ctx context.Context, opts *Opts, input []map[string]any) (*http.Response, error) {
	payload := map[string]any{
		"model":               opts.Model,
		"instructions":        opts.Instructions,
		"input":               input,
		"tool_choice":         "auto",
		"parallel_tool_calls": true,
		"store":               false,
		"stream":              true,
		"include":             []string{},
	}
	if len(opts.Tools) > 0 {
		defs := make([]map[string]any, 0, len(opts.Tools))
		for _, tool := range opts.Tools {
			defs = append(defs, map[string]any{
				"type": "function", "name": tool.Name, "description": tool.Description,
				"parameters": tool.Parameters, "strict": false,
			})
		}
		payload["tools"] = defs
	}
	if opts.Effort != "" {
		payload["reasoning"] = map[string]any{"effort": opts.Effort}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(opts.BaseURL, "/")+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+opts.Credentials.AccessToken)
	req.Header.Set("chatgpt-account-id", opts.Credentials.AccountID)
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("originator", "codex_cli_rs")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	res, err := opts.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call the codex backend: %w", err)
	}
	return res, nil
}

// refresh swaps the refresh token for new tokens, as the CLI would.
func refresh(ctx context.Context, client *http.Client, tokenURL string, current Credentials) (Credentials, error) {
	payload, err := json.Marshal(map[string]string{
		"client_id": clientID, "grant_type": "refresh_token",
		"refresh_token": current.RefreshToken, "scope": "openid profile email",
	})
	if err != nil {
		return Credentials{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(payload))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return Credentials{}, fmt.Errorf("refresh the codex login: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Credentials{}, fmt.Errorf("refresh the codex login: HTTP %d; run `codex login` again", res.StatusCode)
	}
	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&tokens); err != nil {
		return Credentials{}, err
	}
	if strings.TrimSpace(tokens.AccessToken) == "" {
		return Credentials{}, errors.New("refresh the codex login: no access token came back")
	}
	next := current
	next.AccessToken = tokens.AccessToken
	if tokens.RefreshToken != "" {
		next.RefreshToken = tokens.RefreshToken
	}
	if tokens.IDToken != "" {
		next.IDToken = tokens.IDToken
	}
	return next, nil
}

type event struct {
	Type  string `json:"type"`
	Delta string `json:"delta"`
	Item  struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"item"`
	Response struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Usage *struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"response"`
	Message string `json:"message"`
}

// readStream reads the server-sent events of one response.
func readStream(body io.Reader) (streamed, error) {
	var out streamed
	var text strings.Builder
	completed := false
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var ev event
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "response.output_text.delta":
			text.WriteString(ev.Delta)
		case "response.output_item.done":
			if ev.Item.Type == "function_call" {
				id := ev.Item.CallID
				if id == "" {
					id = ev.Item.ID
				}
				out.calls = append(out.calls, toolCall{id: id, name: ev.Item.Name, args: ev.Item.Arguments})
			}
		case "response.completed":
			completed = true
			if u := ev.Response.Usage; u != nil {
				out.usage = &bridge.Usage{Input: u.Input, Output: u.Output}
			}
		case "response.failed":
			msg := "response failed"
			if ev.Response.Error != nil && ev.Response.Error.Message != "" {
				msg = ev.Response.Error.Message
			}
			return streamed{}, errors.New("codex backend: " + msg)
		case "error":
			if ev.Message != "" {
				return streamed{}, errors.New("codex backend: " + ev.Message)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return streamed{}, fmt.Errorf("read the codex stream: %w", err)
	}
	out.text = text.String()
	if !completed && out.text == "" && len(out.calls) == 0 {
		return streamed{}, errors.New("codex backend: the stream had nothing in it")
	}
	return out, nil
}
