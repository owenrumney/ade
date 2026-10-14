package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/sessions"
)

// firstMessageWait bounds handing the opening message over. The agent's reply
// is not waited for — only that the session took the message.
const firstMessageWait = 15 * time.Second

// sendFirstMessage gives a new session the task it was started for. A failure
// is logged, not returned: the session is open either way.
func sendFirstMessage(ctx context.Context, sessionURL, prompt string) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || sessionURL == "" {
		return
	}
	body, err := json.Marshal(map[string]string{"message": prompt})
	if err != nil {
		return
	}
	if err := sendFirst(ctx, sessionURL, string(body)); err != nil {
		slog.Warn("could not hand the session its first message", "err", err)
	}
}

var sendFirst = func(ctx context.Context, sessionURL, body string) error {
	_, err := sessions.AskFor(ctx, sessionURL, http.MethodPost, "/api/chat", body, firstMessageWait)
	return err
}

// Brief is a design document handed to a new session, and whether to start
// building from it once it lands.
type Brief struct {
	Name  string `json:"name"`
	Body  string `json:"body"`
	Build bool   `json:"build"`
}

// sendBrief writes the document into the session, then asks it to build only
// once that landed: an agent given no brief invents one rather than stopping.
func sendBrief(ctx context.Context, sessionURL string, brief *Brief) error {
	if brief == nil || sessionURL == "" || strings.TrimSpace(brief.Body) == "" {
		return nil
	}
	body, err := json.Marshal(map[string]string{"name": brief.Name, "body": brief.Body})
	if err != nil {
		return err
	}
	if err := askSession(ctx, sessionURL, "/api/design/push", string(body)); err != nil {
		return fmt.Errorf("hand the session its design document: %w", err)
	}
	if !brief.Build {
		return nil
	}
	if err := askSession(ctx, sessionURL, "/api/design/go", "{}"); err != nil {
		return fmt.Errorf("start the build: %w", err)
	}
	return nil
}

var askSession = func(ctx context.Context, sessionURL, path, body string) error {
	_, err := sessions.AskFor(ctx, sessionURL, http.MethodPost, path, body, firstMessageWait)
	return err
}
