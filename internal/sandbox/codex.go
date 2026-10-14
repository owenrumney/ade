package sandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/authstore"
	"github.com/owenrumney/ade/internal/skills"
)

// Codex is what a sandboxed Codex session needs from the host.
type Codex struct {
	Home   string
	Root   string
	GitDir string
	Docs   string
	Extra  []Mount
	Env    map[string]string
	Image  string
}

// CodexBox is ADE-owned state exposed to Codex instead of the host's ~/.codex.
func CodexBox(home string) string {
	if root := strings.TrimSpace(os.Getenv("ADE_CREDENTIAL_ROOT")); root != "" {
		return filepath.Join(root, "codex")
	}
	return filepath.Join(home, ".ade", "codex")
}

// CodexConfig is the config.toml the sandboxed process reads, host side.
// Its `[otel]` section is the reason the box copies it at all.
func CodexConfig(home string) string {
	return filepath.Join(CodexBox(home), "config.toml")
}

// CodexAuth is the login the sandboxed process reads, host side.
func CodexAuth(home string) string {
	return filepath.Join(CodexBox(home), "auth.json")
}

// CodexHostAuth is the CLI's own, which an unsandboxed run reads in place.
func CodexHostAuth(home string) string {
	return filepath.Join(home, ".codex", "auth.json")
}

// CodexHostConfig is the user's own, which an unsandboxed run reads in place.
func CodexHostConfig(home string) string {
	return filepath.Join(home, ".codex", "config.toml")
}

// Spec builds Codex's narrow sandbox. Sessions are the only host Codex state
// mounted so ADE can index and resume them; the rest is copied.
func (c Codex) Spec() (Spec, error) {
	if c.Home == "" || c.Root == "" {
		return Spec{}, errors.New("sandbox: home and root are required")
	}
	codexDir := filepath.Join(c.Home, ".codex")
	sessions := filepath.Join(codexDir, "sessions")
	box := CodexBox(c.Home)
	if err := os.MkdirAll(sessions, 0o750); err != nil {
		return Spec{}, err
	}
	if err := os.MkdirAll(box, 0o700); err != nil {
		return Spec{}, err
	}
	if err := CodexCredentials(c.Home); err != nil {
		return Spec{}, err
	}
	// Without config.toml the user's [otel] section never reaches the process,
	// which is the whole reason the driver runs codex at all.
	if err := copyInto(CodexHostConfig(c.Home), CodexConfig(c.Home)); err != nil {
		return Spec{}, err
	}
	cache := Cache(c.Home)
	if err := os.MkdirAll(cache, 0o750); err != nil {
		return Spec{}, err
	}
	// Codex reads $CODEX_HOME/skills: a link to each skill Claude has, the
	// user's and the enabled plugins', with their directories mounted to match.
	found, err := skills.All(c.Home)
	if err != nil {
		return Spec{}, fmt.Errorf("skills for codex: %w", err)
	}
	if err := skills.Link(c.Home, filepath.Join(box, "skills"), found); err != nil {
		return Spec{}, err
	}
	handToGuest(box, sessions, cache)

	mounts := []Mount{
		{Host: c.Root, Guest: c.Root, RW: true},
		{Host: box, Guest: codexDir, RW: true},
		{Host: sessions, Guest: sessions, RW: true},
		{Host: cache, Guest: CacheDir, RW: true},
	}
	if c.GitDir != "" && !within(c.GitDir, c.Root) {
		mounts = append(mounts, Mount{Host: c.GitDir, Guest: c.GitDir, RW: true})
	}
	for _, dir := range []string{skills.Dir(c.Home), filepath.Join(c.Home, ".claude", "plugins", "cache")} {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			mounts = append(mounts, Mount{Host: dir, Guest: dir})
		}
	}
	claude := Claude{Home: c.Home, Root: c.Root, GitDir: c.GitDir}
	for _, dir := range []string{c.Docs, filepath.Join(c.Root, ".scratch")} {
		for _, mount := range claude.reach(dir) {
			if !held(mounts, mount.Guest) {
				mounts = append(mounts, mount)
			}
		}
	}
	mounts = append(mounts, c.Extra...)
	node, err := claude.nodeModules()
	if err != nil {
		return Spec{}, err
	}
	for _, mount := range node {
		if !held(mounts, mount.Guest) {
			mounts = append(mounts, mount)
		}
	}

	env := map[string]string{"HOME": c.Home, "IS_SANDBOX": "1"}
	for key, value := range c.Env {
		env[key] = value
	}
	image := c.Image
	if image == "" {
		image = DefaultImage
	}
	return Spec{Image: image, Name: ContainerName(c.Root), Workdir: c.Root, Mounts: mounts, Env: env}, nil
}

// CodexCredentials keeps whichever login holds the live refresh chain and
// brings the other up to it, as Credentials does for Claude.
func CodexCredentials(home string) error {
	host, box := CodexHostAuth(home), CodexAuth(home)
	held, heldErr := os.ReadFile(box) // #nosec G304 -- ade's own state directory
	data, err := os.ReadFile(host)    // #nosec G304 -- the user's own login file
	if errors.Is(err, os.ErrNotExist) {
		// No host login to fall back to; the copy is all there is.
		return nil
	}
	if err != nil {
		return err
	}
	if heldErr == nil && codexLater(held, data) {
		writeCodexBack(host, held)
		return nil
	}
	if !json.Valid(data) {
		return errors.New("the codex login is not JSON")
	}
	// Atomically: a container from the last turn may be reading this one.
	return authstore.Write(box, data)
}

// SyncCodexCredentials writes the copy back at the end of a run.
func SyncCodexCredentials(home string) {
	box := CodexAuth(home)
	held, err := os.ReadFile(box) // #nosec G304 -- ade's own state directory
	if err != nil {
		slog.Debug("no codex login in the box to write back", "path", box, "err", err)
		return
	}
	writeCodexBack(CodexHostAuth(home), held)
}

// writeCodexBack puts held in the CLI's own file, re-read under the lock in
// case another worktree landed a later one. Best effort: failing costs a login.
func writeCodexBack(path string, held []byte) {
	if !wholeCodex(held) {
		slog.Warn("the container's codex login is not a whole one; the host's is left as it is")
		return
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		slog.Debug("no host codex login to write the container's back to", "path", path)
		return
	}
	unlock, err := authstore.Lock(path)
	if err != nil {
		slog.Warn("lock the host's codex login", "path", path, "err", err)
		return
	}
	defer unlock()
	current, err := os.ReadFile(path) // #nosec G304 -- the user's own login file
	if err != nil {
		slog.Warn("re-read the host's codex login", "path", path, "err", err)
		return
	}
	if !codexLater(held, current) {
		slog.Debug("the host's codex login is already the later one", "path", path)
		return
	}
	if err := authstore.Write(path, held); err != nil {
		slog.Warn("write the refreshed codex login back to the host", "path", path, "err", err)
		return
	}
	slog.Info("the codex login refreshed in the container is now the host's", "path", path)
}

// codexLater is which copy refreshed last, by the stamp both sides write.
func codexLater(held, host []byte) bool {
	return codexRefreshedAt(held) != 0 && codexRefreshedAt(held) > codexRefreshedAt(host)
}

// codexRefreshedAt is when a login last refreshed; zero if it cannot be read.
func codexRefreshedAt(data []byte) int64 {
	var held struct {
		LastRefresh string `json:"last_refresh"`
	}
	if err := json.Unmarshal(data, &held); err != nil {
		return 0
	}
	at, err := time.Parse(time.RFC3339, held.LastRefresh)
	if err != nil {
		return 0
	}
	return at.UnixMilli()
}

// wholeCodex is whether a login has both halves of a usable pair.
func wholeCodex(data []byte) bool {
	var held struct {
		Tokens struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &held); err != nil {
		return false
	}
	return held.Tokens.AccessToken != "" && held.Tokens.RefreshToken != ""
}
