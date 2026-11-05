package launcher

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/sessions"
)

// firstMessageWait bounds handing the opening message over. The agent's reply
// is not waited for — only that the session took the message.
const firstMessageWait = 15 * time.Second

// sendFirstPrompt starts the agent's terminal on the task. With nothing to
// say it is left alone: the window starts it when its tab is opened.
func sendFirstPrompt(ctx context.Context, sessionURL, prompt string) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || sessionURL == "" {
		return
	}
	body, err := json.Marshal(map[string]string{"prompt": prompt})
	if err != nil {
		return
	}
	if err := sendPrompt(ctx, sessionURL, string(body)); err != nil {
		slog.Warn("could not start the agent's terminal on its first message", "err", err)
	}
}

var sendPrompt = func(ctx context.Context, sessionURL, body string) error {
	_, err := sessions.AskFor(ctx, sessionURL, http.MethodPost, "/api/agent/terminal", body, firstMessageWait)
	return err
}

// ticketPrompt is what a session started for a ticket is told when nothing else was typed.
func ticketPrompt(ticket string) string {
	return "Start on ticket " + ticket + ". Read it with the tools you have for it, then say how you would begin."
}
