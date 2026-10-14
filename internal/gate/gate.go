// Package gate carries one tool call to ade and prints what ade decided. It
// always prints a decision: unreachable means denied, never allowed by default.
package gate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Decision is the PreToolUse answer, in the shape Claude Code reads.
type Decision struct {
	Output Output `json:"hookSpecificOutput"`
}

// Output is what ade decided, and why. The reason reaches the model.
type Output struct {
	HookEventName            string         `json:"hookEventName"`
	PermissionDecision       string         `json:"permissionDecision"`
	PermissionDecisionReason string         `json:"permissionDecisionReason,omitempty"`
	UpdatedInput             map[string]any `json:"updatedInput,omitempty"`
}

// Env names where the server is and what authorizes the call to it.
const (
	URLVar   = "ADE_HOOK_URL"
	TokenVar = "ADE_HOOK_TOKEN"
)

// Grace bounds the wait. Under the hook's own timeout on purpose: ade's answer
// has to arrive as a decision, not as a hook Claude Code gave up on.
const Grace = 330 * time.Second

// Run carries one hook call to ade and writes the answer. The error is for the
// log; a decision is always written, whatever went wrong.
func Run(ctx context.Context, in io.Reader, out io.Writer) error {
	decision, err := ask(ctx, in)
	if err != nil {
		decision = deny("ade could not decide this call: " + err.Error() +
			". Nothing was run. Say so rather than trying another way round it.")
	}
	encoded, marshalErr := json.Marshal(decision)
	if marshalErr != nil {
		return marshalErr
	}
	if _, writeErr := out.Write(append(encoded, '\n')); writeErr != nil {
		return writeErr
	}
	return err
}

func ask(ctx context.Context, in io.Reader) (Decision, error) {
	url, token := os.Getenv(URLVar), os.Getenv(TokenVar)
	if url == "" {
		return Decision{}, errors.New("no " + URLVar + " in this session")
	}
	body, err := io.ReadAll(io.LimitReader(in, 4<<20))
	if err != nil {
		return Decision{}, fmt.Errorf("read the call: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, Grace)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Decision{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return Decision{}, fmt.Errorf("reach ade: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	answer, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return Decision{}, fmt.Errorf("read ade's answer: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return Decision{}, fmt.Errorf("ade answered %d", response.StatusCode)
	}
	var decided Decision
	if err := json.Unmarshal(answer, &decided); err != nil {
		return Decision{}, fmt.Errorf("decode ade's answer: %w", err)
	}
	if decided.Output.PermissionDecision == "" {
		return Decision{}, errors.New("ade decided nothing")
	}
	return decided, nil
}

func deny(reason string) Decision {
	return Decision{Output: Output{
		HookEventName:            "PreToolUse",
		PermissionDecision:       "deny",
		PermissionDecisionReason: reason,
	}}
}
