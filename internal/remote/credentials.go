package remote

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

type Credentials struct {
	Claude []byte
	Codex  []byte
	// ClaudeAt and CodexAt are when the laptop's copies were last written. A
	// credential refreshes in place, so the newer file holds the live chain.
	ClaudeAt time.Time
	CodexAt  time.Time
}

// putFile replaces a file rather than writing to it. The worker runs as root
// and these are its files; the directory is ours, so a new name is the way in.
const putFile = `put() { cat > "$1.new" && mv -f "$1.new" "$1"; }`

// PrepareCredentials keeps the newest copy of each provider's login in the box.
// A session refreshes its token there, so an older copy must never overwrite it.
func (h *Host) PrepareCredentials(ctx context.Context, roots Roots, fallback Credentials) error {
	root := roots.Run + "/worker"
	script := `set -eu
umask 077
root=$1
mkdir -p "$root/claude" "$root/codex"
` + putFile + `
claude="$root/claude/.credentials.json"
if [ -s "$HOME/.claude/.credentials.json" ]; then
  [ -s "$claude" ] && [ ! "$HOME/.claude/.credentials.json" -nt "$claude" ] || \
    put "$claude" < "$HOME/.claude/.credentials.json"
elif [ ! -s "$claude" ] && command -v security >/dev/null 2>&1; then
  security find-generic-password -s "Claude Code-credentials" -w 2>/dev/null | put "$claude" || true
fi
codex="$root/codex/auth.json"
if [ -s "$HOME/.codex/auth.json" ]; then
  [ -s "$codex" ] && [ ! "$HOME/.codex/auth.json" -nt "$codex" ] || \
    put "$codex" < "$HOME/.codex/auth.json"
fi`
	if _, err := h.Run(ctx, "sh", "-c", script, "sh", root); err != nil {
		return fmt.Errorf("prepare credentials on %s: %w", h.Name, err)
	}
	for _, file := range []struct {
		path string
		data []byte
		at   time.Time
	}{
		{root + "/claude/.credentials.json", fallback.Claude, fallback.ClaudeAt},
		{root + "/codex/auth.json", fallback.Codex, fallback.CodexAt},
	} {
		if len(file.data) == 0 || !h.olderThan(ctx, file.path, file.at) {
			continue
		}
		command := "set -eu\numask 077\n" + putFile + "\nput \"$1\""
		if _, err := h.RunInput(ctx, file.data, "sh", "-c", command, "sh", file.path); err != nil {
			return fmt.Errorf("send provider credentials to %s: %w", h.Name, err)
		}
	}
	return nil
}

// olderThan reports a box copy worth replacing: missing, empty, or written
// before the one this laptop holds. GNU stat and BSD stat spell it differently.
func (h *Host) olderThan(ctx context.Context, path string, at time.Time) bool {
	if at.IsZero() {
		return !h.hasFile(ctx, path)
	}
	// Exit zero is a box copy at least as new as ours, which stays.
	_, err := h.Run(ctx, "sh", "-c",
		`[ -s "$1" ] || exit 1
box=$(stat -c %Y "$1" 2>/dev/null || stat -f %m "$1" 2>/dev/null) || exit 1
[ "$box" -ge "$2" ]`,
		"sh", path, strconv.FormatInt(at.Unix(), 10))
	return err != nil
}

func (h *Host) hasFile(ctx context.Context, path string) bool {
	_, err := h.Run(ctx, "test", "-s", path)
	return err == nil
}
