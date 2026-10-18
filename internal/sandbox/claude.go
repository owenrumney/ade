package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/owenrumney/ade/internal/authstore"
)

// Claude is what a sandboxed Claude Code needs from the host.
type Claude struct {
	// Home is the host home, which is also the guest's: Claude Code keeps
	// its transcripts under $HOME/.claude/projects keyed by the working
	// directory, and ade reads them from the host, so both paths must agree.
	Home string
	// Root is the checkout, mounted at its own path for the same reason.
	Root string
	// GitDir is the repository's git directory when Root is a worktree,
	// whose .git file points there; mounted too, or git inside sees nothing.
	GitDir string
	// Docs is where ade keeps design documents. A worktree's are often the
	// main checkout's .scratch, outside Root and GitDir both, and a write
	// there with no mount behind it stays in the container.
	Docs string
	// Extra is other directories the work needs.
	Extra   []Mount
	Env     map[string]string
	MCPHost string
	// Servers are ADE's own MCP servers, merged over the user's in the box's
	// config. ADE names them; a clash is ADE's to avoid.
	Servers map[string]json.RawMessage
	Argv    []string
	Image   string
	// MakeMountPoints creates a mount's target before the run.
	MakeMountPoints bool
}

// keychainService is where Claude Code on macOS keeps its login.
const keychainService = "Claude Code-credentials"

// Spec uses ADE's ~/.claude box for copied configuration and login state.
// Only data Claude must read or ADE must index is mounted from the host.
func (c Claude) Spec(box string) (Spec, error) {
	if c.Home == "" || c.Root == "" || box == "" {
		return Spec{}, errors.New("sandbox: home, root and box are required")
	}
	image := c.Image
	if image == "" {
		image = DefaultImage
	}
	claudeDir := filepath.Join(c.Home, ".claude")
	projects := filepath.Join(claudeDir, "projects")
	if err := os.MkdirAll(projects, 0o750); err != nil {
		return Spec{}, err
	}
	for _, name := range []string{"CLAUDE.md", "settings.json", "remote-settings.json"} {
		if err := copyInto(filepath.Join(claudeDir, name), filepath.Join(box, name)); err != nil {
			return Spec{}, err
		}
	}
	// The plugin index is copied so the guest's plugins dir stays writable; the
	// plugin code it points at is mounted read-only below.
	for _, name := range []string{"installed_plugins.json", "known_marketplaces.json"} {
		if err := copyInto(filepath.Join(claudeDir, "plugins", name), filepath.Join(box, "plugins", name)); err != nil {
			return Spec{}, err
		}
	}
	mcp, err := mcpInto(filepath.Join(c.Home, ".claude.json"), filepath.Join(box, mcpFile), c.MCPHost, c.Servers)
	if err != nil {
		return Spec{}, err
	}
	npmrc := ""
	if c.Env["NODE_AUTH_TOKEN"] != "" && c.Env["NPM_CONFIG_USERCONFIG"] == "" {
		if err := npmrcInto(filepath.Join(box, npmrcFile)); err != nil {
			return Spec{}, err
		}
		npmrc = filepath.Join(claudeDir, npmrcFile)
	}
	roots := []string{c.Root}
	if c.GitDir != "" {
		roots = append(roots, c.GitDir)
	}
	signing, err := gitconfigInto(filepath.Join(c.Home, ".gitconfig"), filepath.Join(box, gitconfigFile), c.Home, roots)
	if err != nil {
		return Spec{}, err
	}
	var args []string
	if mcp {
		args = []string{"--mcp-config", filepath.Join(claudeDir, mcpFile)}
	}
	cache := Cache(c.Home)
	if err := os.MkdirAll(cache, 0o750); err != nil {
		return Spec{}, err
	}
	handToGuest(box, projects, cache)
	mounts := []Mount{
		{Host: c.Root, Guest: c.Root, RW: true},
		{Host: box, Guest: claudeDir, RW: true},
		{Host: projects, Guest: projects, RW: true},
		{Host: cache, Guest: CacheDir, RW: true},
	}
	if c.GitDir != "" && !within(c.GitDir, c.Root) {
		mounts = append(mounts, Mount{Host: c.GitDir, Guest: c.GitDir, RW: true})
	}
	// Where the design document lives, and .scratch, which is usually a
	// symlink out of the checkout: the link comes with Root's mount, its
	// target does not.
	for _, dir := range []string{c.Docs, filepath.Join(c.Root, ".scratch")} {
		for _, m := range c.reach(dir) {
			if !held(mounts, m.Guest) {
				mounts = append(mounts, m)
			}
		}
	}
	carried := []string{}
	for _, name := range []string{"agents", "commands", "skills", "plugins/cache", "plugins/marketplaces"} {
		path := filepath.Join(claudeDir, name)
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			mounts = append(mounts, Mount{Host: path, Guest: path})
			carried = append(carried, path)
		}
	}
	// After all of them, so a link from one into another is already covered.
	for _, path := range carried {
		mounts = append(mounts, linked(path, mounts)...)
	}
	// A directory, not the two files: Apple's runtime binds only directories.
	if len(signing) > 0 {
		staged, err := signingInto(filepath.Join(box, signingDir), signing)
		if err != nil {
			return Spec{}, err
		}
		mounts = append(mounts, Mount{Host: staged, Guest: filepath.Dir(signing[0])})
	}
	mounts = append(mounts, c.Extra...)
	node, err := c.nodeModules()
	if err != nil {
		return Spec{}, err
	}
	for _, m := range node {
		if !held(mounts, m.Guest) {
			mounts = append(mounts, m)
		}
	}

	// Claude Code refuses --dangerously-skip-permissions as root, and the
	// image runs as root; IS_SANDBOX is its own word for "this is a
	// container, the boundary is around you". True here.
	env := map[string]string{
		"HOME":              c.Home,
		"IS_SANDBOX":        "1",
		"GIT_CONFIG_GLOBAL": filepath.Join(claudeDir, gitconfigFile),
	}
	for k, v := range c.Env {
		env[k] = v
	}
	if npmrc != "" {
		env["NPM_CONFIG_USERCONFIG"] = npmrc
	}
	return Spec{
		Image:   image,
		Name:    ContainerName(c.Root),
		Workdir: c.Root,
		Mounts:  mounts,
		Env:     env,
		Argv:    c.Argv,
		Args:    args,
	}, nil
}

// mcpFile is the user's MCP servers, in the box for --mcp-config.
const mcpFile = "ade-mcp.json"

// gitconfigFile is the host's git config, in the box for GIT_CONFIG_GLOBAL.
const gitconfigFile = "gitconfig"

const signingDir = "ssh"

const npmrcFile = "ade-npmrc"

// gitconfigInto puts the host's ~/.gitconfig in the box, with the checkout
// marked safe. A file, so it cannot be mounted — the copy is what
// GIT_CONFIG_GLOBAL points at, and a write inside the container stays there
// rather than landing on the host's.
//
// Signing is turned off in the copy unless the key it names is mounted.
//
// Without it the agent has no identity and every commit it tries fails; and
// the image runs as root over a checkout owned by the host's user, which git
// calls dubious ownership and refuses to read at all. Only the paths ade
// mounts are named safe, not the wildcard.
// Design: .git/ade/docs/sandbox-commit-signing.md.
func gitconfigInto(src, dst, home string, roots []string) ([]string, error) {
	data, err := os.ReadFile(src) // #nosec G304 -- under home
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var b strings.Builder
	b.Write(data)
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		b.WriteString("\n")
	}
	b.WriteString("[safe]\n")
	for _, root := range roots {
		fmt.Fprintf(&b, "\tdirectory = %s\n", root)
	}
	// Last wins. With no key in the box, signing on would fail every commit.
	keys := signingKey(data, home)
	if len(keys) == 0 {
		b.WriteString("[commit]\n\tgpgsign = false\n[tag]\n\tgpgsign = false\n")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(dst, []byte(b.String()), 0o600); err != nil {
		return nil, err
	}
	return keys, nil
}

var signingKeyRule = regexp.MustCompile(`(?mi)^\s*signingkey\s*=\s*(\S+)\s*$`)

// signingKey is the pair to mount, empty unless both halves are there.
func signingKey(config []byte, home string) []string {
	found := signingKeyRule.FindSubmatch(config)
	if found == nil || home == "" {
		return nil
	}
	named := underHome(string(found[1]), home)
	if named == "" || !isFile(named) {
		return nil
	}
	secret := strings.TrimSuffix(named, ".pub")
	if secret == named || !isFile(secret) {
		return nil
	}
	return []string{named, secret}
}

// underHome resolves the ~ git stores literally, and refuses an escape.
func underHome(path, home string) string {
	if after, ok := strings.CutPrefix(path, "~/"); ok {
		path = filepath.Join(home, after)
	}
	if !filepath.IsAbs(path) || !within(path, home) {
		return ""
	}
	return path
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// mcpInto takes the user-level MCP servers out of ~/.claude.json — a file,
// so it cannot be mounted, and one that holds much else — and writes them
// to the box as the file --mcp-config reads. Reports whether there are
// any; with none the copy is taken out. A server that is a command needs
// that command in the image; one that is a URL needs the network.
func mcpInto(src, dst, host string, own map[string]json.RawMessage) (bool, error) {
	data, err := os.ReadFile(src) // #nosec G304 -- under home
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	var user struct {
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &user); err != nil {
			return false, fmt.Errorf("parse %s: %w", src, err)
		}
	}
	if len(user.Servers) == 0 && len(own) == 0 {
		if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
		return false, nil
	}
	servers := map[string]json.RawMessage{}
	for name, server := range user.Servers {
		servers[name] = rewriteLoopbackURL(server, host)
	}
	for name, server := range own {
		// ADE's own server has to win, or the tool it serves is not the one
		// this session was given; say so rather than drop it silently.
		if _, clash := servers[name]; clash {
			slog.Warn("ade's mcp server replaces your own for this run", "name", name)
		}
		servers[name] = rewriteLoopbackURL(server, host)
	}
	out, err := json.MarshalIndent(map[string]any{"mcpServers": servers}, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return false, err
	}
	return true, os.WriteFile(dst, out, 0o600)
}

// signingInto copies the pair into dst, modes and all.
func signingInto(dst string, keys []string) (string, error) {
	if err := os.RemoveAll(dst); err != nil {
		return "", fmt.Errorf("sandbox: clear %s: %w", dst, err)
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return "", fmt.Errorf("sandbox: %s: %w", dst, err)
	}
	for _, key := range keys {
		data, err := os.ReadFile(key) // #nosec G304 -- a signing key named by your own gitconfig
		if err != nil {
			return "", fmt.Errorf("sandbox: read %s: %w", key, err)
		}
		mode := fs.FileMode(0o600)
		if info, err := os.Stat(key); err == nil {
			mode = info.Mode().Perm()
		}
		if err := os.WriteFile(filepath.Join(dst, filepath.Base(key)), data, mode); err != nil {
			return "", fmt.Errorf("sandbox: stage %s: %w", key, err)
		}
	}
	return dst, nil
}

func npmrcInto(dst string) error {
	const config = "//npm.pkg.github.com/:_authToken=${NODE_AUTH_TOKEN}\n"
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return os.WriteFile(dst, []byte(config), 0o600)
}

func rewriteLoopbackURL(raw json.RawMessage, host string) json.RawMessage {
	if host == "" {
		return raw
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return raw
	}
	rewriteURL(&value, host)
	out, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return out
}

func rewriteURL(value *any, host string) {
	switch got := (*value).(type) {
	case string:
		parsed, err := url.Parse(got)
		if err == nil && loopback(parsed.Hostname()) {
			port := parsed.Port()
			parsed.Host = host
			if port != "" {
				parsed.Host = net.JoinHostPort(host, port)
			}
			*value = parsed.String()
		}
	case []any:
		for i := range got {
			rewriteURL(&got[i], host)
		}
	case map[string]any:
		for key, child := range got {
			rewriteURL(&child, host)
			got[key] = child
		}
	}
}

func loopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// Box is the directory that is the guest's ~/.claude.
func Box(home string) string {
	if root := strings.TrimSpace(os.Getenv("ADE_CREDENTIAL_ROOT")); root != "" {
		return filepath.Join(root, "claude")
	}
	return filepath.Join(home, ".ade", "claude")
}

// CacheDir is where the image keeps what is slow to make again — mise's
// toolchains, go's build and module caches, npm's — and Cache is the host
// directory mounted there, so they outlive the container.
const CacheDir = "/cache"

// Cache is the host side of CacheDir.
func Cache(home string) string {
	return filepath.Join(home, ".ade", "cache")
}

// Credentials is the login the container sees: ade's own copy in Box,
// made from the host's — the Keychain on macOS, where Claude Code keeps it,
// or ~/.claude/.credentials.json.
//
// One session, and a refresh rotates the token, so the later refresh is the
// only chain that works: it is kept, and the host's own store brought up to it.
func Credentials(ctx context.Context, home string) (string, error) {
	dir := Box(home)
	path := filepath.Join(dir, ".credentials.json")
	held, heldErr := os.ReadFile(path) // #nosec G304 -- ade's own state directory
	data, store, err := hostCredentials(ctx, home)
	if err != nil {
		// No host login to fall back to; the copy is all there is.
		if heldErr == nil && expiry(held) != 0 {
			return path, nil
		}
		return "", err
	}
	if heldErr == nil && later(held, data) {
		writeBack(ctx, home, store, held)
		return path, nil
	}
	if !json.Valid(data) {
		return "", errors.New("the claude login is not JSON")
	}
	// Atomically: a container from the last turn may be reading this one.
	if err := authstore.Write(path, data); err != nil {
		return "", err
	}
	return path, nil
}

// SyncCredentials writes the copy back at the end of a run. Credentials does
// it too, a run later — by then the host has already asked for a login.
func SyncCredentials(ctx context.Context, home string) {
	path := filepath.Join(Box(home), ".credentials.json")
	held, err := os.ReadFile(path) // #nosec G304 -- ade's own state directory
	if err != nil {
		slog.Debug("no claude login in the box to write back", "path", path, "err", err)
		return
	}
	_, store, err := hostCredentials(ctx, home)
	if err != nil {
		slog.Debug("no host claude login to write the container's back to", "err", err)
		return
	}
	writeBack(ctx, home, store, held)
}

// later is which copy holds the live refresh chain, reading expiresAt as when
// it last refreshed rather than as a liveness test. Zero is a failed refresh.
func later(held, host []byte) bool {
	return expiry(held) != 0 && expiry(held) > expiry(host)
}

// writeBack puts held in the host's store, re-read under the lock in case
// another worktree landed a later one. Best effort: failing costs a login.
func writeBack(ctx context.Context, home string, store hostStore, held []byte) {
	if store != storeFile && store != storeKeychain {
		slog.Debug("the container's claude login is the newer one, but there is no host store to put it in")
		return
	}
	if !whole(held) {
		slog.Warn("the container's claude login is not a whole one; the host's is left as it is")
		return
	}
	path := hostFile(home)
	unlock, err := authstore.Lock(path)
	if err != nil {
		slog.Warn("lock the host's claude login", "path", path, "err", err)
		return
	}
	defer unlock()
	current, err := readStore(ctx, store, path)
	if err != nil {
		slog.Warn("re-read the host's claude login", "store", store, "err", err)
		return
	}
	// The no-op too: an unchanged host has the same expiry, not a lower one.
	if !later(held, current) {
		slog.Debug("the host's claude login is already the later one", "store", store)
		return
	}
	if err := writeStore(ctx, store, path, held); err != nil {
		slog.Warn("write the refreshed claude login back to the host", "store", store, "err", err)
		return
	}
	slog.Info("the claude login refreshed in the container is now the host's", "store", store)
}

func readStore(ctx context.Context, store hostStore, path string) ([]byte, error) {
	if store == storeKeychain {
		// The item was read once to get here, so a failure now is a locked or
		// refused Keychain: comparing against nothing would overwrite it.
		return readKeychain(ctx)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- under home
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return data, nil
}

// writeStore puts a login where the CLI reads it. A failed Keychain write never
// falls back to the file: on macOS the CLI does not read it, so it repairs nothing.
func writeStore(ctx context.Context, store hostStore, path string, held []byte) error {
	if store == storeKeychain {
		account, err := keychainAccount(ctx)
		if err != nil {
			return err
		}
		return writeKeychain(ctx, account, held)
	}
	return authstore.Write(path, held)
}

// maxLogin caps what is read back out of the box; a login is a few hundred bytes.
const maxLogin = 16 << 10

// whole is a login worth putting in the host's store. The container writes the
// box copy, so a truncated or shapeless one must not replace the host's.
func whole(data []byte) bool {
	if len(data) == 0 || len(data) > maxLogin {
		return false
	}
	var held struct {
		OAuth struct {
			AccessToken  string `json:"accessToken"`
			RefreshToken string `json:"refreshToken"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &held); err != nil {
		return false
	}
	return held.OAuth.AccessToken != "" && held.OAuth.RefreshToken != ""
}

// expiry is when a login's access token runs out, in milliseconds, and zero
// for one that cannot be read or that failed its last refresh.
func expiry(data []byte) int64 {
	var held struct {
		OAuth struct {
			ExpiresAt int64 `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &held); err != nil {
		return 0
	}
	return held.OAuth.ExpiresAt
}

// ForgetCredentials drops the copy, for a login that has stopped working.
func ForgetCredentials(home string) error {
	err := os.Remove(filepath.Join(Box(home), ".credentials.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// hostStore is which store a login came from; on macOS the CLI reads the
// Keychain and not the file, so the two are not interchangeable.
type hostStore int

const (
	storeNone hostStore = iota
	storeKeychain
	storeFile
)

func (s hostStore) String() string {
	switch s {
	case storeKeychain:
		return "keychain"
	case storeFile:
		return "file"
	}
	return "none"
}

// hostFile is Claude Code's login off macOS, and a Mac's fallback.
func hostFile(home string) string {
	return filepath.Join(home, ".claude", ".credentials.json")
}

// onDarwin is a variable so a test can take the Keychain path on any host.
var onDarwin = runtime.GOOS == "darwin"

var readKeychain = func(ctx context.Context) ([]byte, error) {
	out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", keychainService, "-w").Output() // #nosec G204 -- fixed
	if err != nil {
		return nil, fmt.Errorf("read the claude login from the keychain: %w", err)
	}
	return []byte(strings.TrimSpace(string(out))), nil
}

var keychainAcct = regexp.MustCompile(`"acct"<blob>="(.*)"`)

// keychainAccount is the acct the existing item carries; guessing it writes a
// second item under the same service, after which the read returns either.
var keychainAccount = func(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "security", "find-generic-password", "-s", keychainService).CombinedOutput() // #nosec G204 -- fixed
	if err != nil {
		return "", fmt.Errorf("read the claude login's keychain attributes: %w", err)
	}
	match := keychainAcct.FindSubmatch(out)
	if match == nil {
		return "", errors.New("the claude keychain item carries no account attribute")
	}
	return string(match[1]), nil
}

// writeKeychain updates the item in place: -U keeps the ACL, which already names
// /usr/bin/security because Claude Code writes it the same way. See the doc.
var writeKeychain = func(ctx context.Context, account string, data []byte) error {
	// The login is on argv for the life of the call, where any same-uid process
	// can read it. Accepted: add-generic-password takes no other form.
	cmd := exec.CommandContext(ctx, "security", "add-generic-password", "-U", //  #nosec G204 -- fixed flags, attributes off the item
		"-s", keychainService, "-a", account, "-w", string(data))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("write the claude login to the keychain: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// hostCredentials is the host's live Claude login: the macOS Keychain first,
// since a stale ~/.claude/.credentials.json no longer refreshes there.
func hostCredentials(ctx context.Context, home string) ([]byte, hostStore, error) {
	file := hostFile(home)
	var keychain error
	if onDarwin {
		data, err := readKeychain(ctx)
		if err == nil {
			return data, storeKeychain, nil
		}
		keychain = err
	}
	if data, err := os.ReadFile(file); err == nil { // #nosec G304 -- under home
		return data, storeFile, nil
	}
	if keychain != nil {
		return nil, storeNone, fmt.Errorf("no claude login in the keychain or at %s: %w", file, keychain)
	}
	return nil, storeNone, fmt.Errorf("no claude login at %s; run `claude` once to log in", file)
}

// copyInto puts the host's file in the box, or takes a stale copy out when
// the host has none, so the guest sees what the host has now.
func copyInto(src, dst string) error {
	data, err := os.ReadFile(src) // #nosec G304 -- under ~/.claude
	if errors.Is(err, os.ErrNotExist) {
		if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}

// reach is what dir needs to be writable in the container, given that Root
// and GitDir are already mounted. Nothing, usually — but a design directory
// outside both has to be bound at the path the agent was told to write, and a
// symlink that is itself in the container needs its target bound instead, or
// following the link inside leads nowhere.
func (c Claude) reach(dir string) []Mount {
	if dir == "" {
		return nil
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil
	}
	if !c.mounted(dir) {
		return []Mount{{Host: resolved, Guest: dir, RW: true}}
	}
	if !c.mounted(resolved) {
		return []Mount{{Host: resolved, Guest: resolved, RW: true}}
	}
	return nil
}

// mounted is whether a path is already in the container by way of Root or
// GitDir.
func (c Claude) mounted(path string) bool {
	return within(path, c.Root) || (c.GitDir != "" && within(path, c.GitDir))
}

// nodeModulesDepth bounds the search for packages below the checkout root.
const nodeModulesDepth = 3

// NodeModulesDir is the host side of one package's container-private
// node_modules, keyed by the guest path so two checkouts never share one.
func NodeModulesDir(home, guest string) string {
	sum := sha256.Sum256([]byte(guest))
	name := filepath.Base(filepath.Dir(guest))
	return filepath.Join(Cache(home), "node_modules", fmt.Sprintf("%s-%x", name, sum[:6]))
}

// nodeModules gives each package in the checkout its own node_modules in the
// container: a native binding is one platform's, and the checkout is shared.
func (c Claude) nodeModules() ([]Mount, error) {
	var out []Mount
	for _, dir := range packageDirs(c.Root, nodeModulesDepth) {
		guest := filepath.Join(dir, "node_modules")
		host := NodeModulesDir(c.Home, guest)
		if err := os.MkdirAll(host, 0o750); err != nil {
			return nil, fmt.Errorf("sandbox: node_modules for %s: %w", dir, err)
		}
		// errno 17 otherwise: the target has to exist before the mount.
		if c.MakeMountPoints {
			if err := os.MkdirAll(guest, 0o750); err != nil {
				return nil, fmt.Errorf("sandbox: mount point %s: %w", guest, err)
			}
		}
		out = append(out, Mount{Host: host, Guest: guest, RW: true})
	}
	return out, nil
}

// packageDirs are the directories at or under root that hold a package.json.
func packageDirs(root string, depth int) []string {
	var out []string
	var walk func(dir string, level int)
	walk = func(dir string, level int) {
		if info, err := os.Stat(filepath.Join(dir, "package.json")); err == nil && !info.IsDir() {
			out = append(out, dir)
		}
		if level == depth {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() && !skipWalk(e.Name()) {
				walk(filepath.Join(dir, e.Name()), level+1)
			}
		}
	}
	walk(root, 0)
	return out
}

// skipWalk keeps the search off dependency and build trees, and out of dot
// directories: a package.json in one is not the checkout's own.
func skipWalk(name string) bool {
	switch name {
	case "node_modules", "dist", "build", "vendor", "bin", "target":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// linked mounts what a symlinked entry points at. Mounting the directory
// carries the link and not its target, so a stowed skill dangles without this.
func linked(dir string, have []Mount) []Mount {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Mount
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		at := filepath.Join(dir, entry.Name())
		target, err := os.Readlink(at)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, target)
		}
		// Dangling on the host, or a file: apple's container mounts directories.
		resolved, err := filepath.EvalSymlinks(at)
		if err != nil {
			continue
		}
		if info, err := os.Stat(resolved); err != nil || !info.IsDir() {
			continue
		}
		if covered(have, target) || covered(out, target) {
			continue
		}
		out = append(out, Mount{Host: resolved, Guest: target})
	}
	return out
}

// covered is whether a mount already brings this path into the guest.
func covered(mounts []Mount, guest string) bool {
	for _, m := range mounts {
		if within(guest, m.Guest) {
			return true
		}
	}
	return false
}

func held(mounts []Mount, guest string) bool {
	for _, m := range mounts {
		if m.Guest == guest {
			return true
		}
	}
	return false
}

func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}
