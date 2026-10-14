package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/owenrumney/ade/internal/settings"
)

// Prompt is how the machine you drive from says work should be done: the role
// you picked and the standing instruction you typed. A worker has neither.
type Prompt struct {
	// RoleName is the selected role, and Role its markdown. Empty is none.
	RoleName string
	Role     string
	// Instructions is the standing instruction for every session.
	Instructions string
	// Dashboard is the kiosk to report into, under the worker's own name. Off
	// clears the block: a machine told to stop must stop.
	Dashboard settings.Dashboard
}

// mkRoles makes the roles directory, as the worker: the ssh user may read its
// HOME and may not write there.
const mkRoles = `umask 077; mkdir -p "$HOME/.ade/roles"; rm -f "$HOME"/.ade/roles/*.md`

// noFile is what readSettings prints when there is nothing yet: this is a
// read-modify-write of settings a worker set, and a wrong guess discards them.
const noFile = "__ade_no_settings__"

const readSettings = `if [ -f "$HOME/$1" ]; then cat "$HOME/$1"; else printf %s "` + noFile + `"; fi`

// PreparePrompt puts this machine's prompt in the worker's HOME, so a session
// started there runs under it. Needs the worker up, like every write to it.
func (h *Host) PreparePrompt(ctx context.Context, p Prompt) error {
	if _, err := h.Run(ctx, "docker", "exec", workerName, "sh", "-c", mkRoles); err != nil {
		return fmt.Errorf("prepare the prompt on %s: %w", h.Name, err)
	}
	if p.RoleName != "" && p.Role != "" {
		at := ".ade/roles/" + p.RoleName + ".md"
		if _, err := h.RunInput(ctx, []byte(p.Role), "docker", "exec", "-i", workerName, "sh", "-c", writeHome, "sh", at); err != nil {
			return fmt.Errorf("send the role to %s: %w", h.Name, err)
		}
	}

	body, err := h.promptSettings(ctx, p)
	if err != nil {
		return err
	}
	if _, err := h.RunInput(ctx, body, "docker", "exec", "-i", workerName, "sh", "-c", writeHome, "sh", ".ade/settings.json"); err != nil {
		return fmt.Errorf("send the settings to %s: %w", h.Name, err)
	}
	return nil
}

// promptSettings sets the keys this machine owns and keeps the rest: a session
// over there may have changed a setting of its own, not this machine's to discard.
func (h *Host) promptSettings(ctx context.Context, p Prompt) ([]byte, error) {
	out := map[string]any{}
	got, err := h.Run(ctx, "docker", "exec", workerName, "sh", "-c", readSettings, "sh", ".ade/settings.json")
	if err != nil {
		return nil, fmt.Errorf("read the worker's settings on %s: %w", h.Name, err)
	}
	// Empty is safe to start from: Run only returns at all once the command
	// has run, so nothing here is the file saying nothing.
	if body := strings.TrimSpace(string(got)); body != noFile && body != "" {
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			return nil, fmt.Errorf("parse the worker's settings on %s: %w", h.Name, err)
		}
	}
	out["default_role"] = p.RoleName
	out["instructions"] = p.Instructions
	if p.Dashboard.On() {
		out["dashboard"] = p.Dashboard
	} else {
		delete(out, "dashboard")
	}
	return json.Marshal(out)
}
