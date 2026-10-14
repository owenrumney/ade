// Package delegate is the capability the server hands the agent it starts, for
// the few things the agent cannot see from inside its own session.
package delegate

import (
	"bytes"
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
)

// Env names the capability; absent means ade did not start us.
const (
	EnvURL   = "ADE_DELEGATE_URL"
	EnvToken = "ADE_DELEGATE_TOKEN"
)

const Header = "X-Ade-Delegate-Token"

const timeout = 10 * time.Second

const maxBody = 1 << 22

// ErrNoSession is asking from outside a running ade.
var ErrNoSession = errors.New("no ade session: ADE_DELEGATE_URL is not set, so there is nothing to read")

// Get reads a delegate endpoint into out; what names it in an error.
func Get(ctx context.Context, what, path string, query url.Values, out any) error {
	return send(ctx, http.MethodGet, what, path, query, nil, out)
}

// Post sends body as JSON to a delegate endpoint and reads the answer into
// out. It is how an agent hands ade something it cannot write itself.
func Post(ctx context.Context, what, path string, body, out any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return send(ctx, http.MethodPost, what, path, nil, encoded, out)
}

func send(ctx context.Context, method, what, path string, query url.Values, body []byte, out any) error {
	base, token := os.Getenv(EnvURL), os.Getenv(EnvToken)
	if base == "" || token == "" {
		return ErrNoSession
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	address := strings.TrimSuffix(base, "/") + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, address, reader)
	if err != nil {
		return err
	}
	req.Header.Set(Header, token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("ask ade %s: %w", what, err)
	}
	defer func() { _ = res.Body.Close() }()
	answer, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("ade answered %s: %s", res.Status, strings.TrimSpace(string(answer)))
	}
	if err := json.Unmarshal(answer, out); err != nil {
		return fmt.Errorf("read the answer: %w", err)
	}
	return nil
}
