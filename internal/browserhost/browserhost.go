// Package browserhost is the bridge between the Firefox extension and ade.
// A browser cannot run a command, so the extension speaks Firefox's native
// messaging protocol to this host, which turns "Open in Ade" on a pull request
// page into `ade --pr N` in the checkout that pull request belongs to.
package browserhost

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
)

// Request is a message from the extension.
type Request struct {
	Action string `json:"action"`
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Number int    `json:"number"`
}

// Response is what goes back. The extension shows Error on the button, so it
// is written for someone reading it there rather than in a log.
type Response struct {
	OK      bool   `json:"ok"`
	Dir     string `json:"dir,omitempty"`
	URL     string `json:"url,omitempty"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

// maxMessage is the protocol's limit on a single message. Ours are three
// fields, so anything near it is a framing error rather than a big request.
const maxMessage = 1 << 20

// launchWait bounds the `ade --pr` call. ade backgrounds itself and prints a
// URL, so it returns in about a second; this is only for when it does not.
const launchWait = 60 * time.Second

// pathWait bounds asking the login shell for its PATH, which runs the user's
// profile and is therefore as slow as they have made it.
const pathWait = 10 * time.Second

// sentinel fences the PATH in that shell's output. A login shell prints
// whatever the profile prints, so the value cannot just be read off the end.
const sentinel = "__ade_path__"

// shellPath is read once: the host is short-lived, and a connectNative port
// would otherwise run the profile for every message.
var shellPath = sync.OnceValue(loginPath)

// Serve reads messages until in is closed, answering each on out.
//
// Firefox spawns the host per sendNativeMessage and closes stdin after one
// message, or keeps it open for a connectNative port. Reading to EOF serves
// both. Nothing but a response may be written to out: stdout is the protocol.
func Serve(ctx context.Context, in io.Reader, out io.Writer, version string) error {
	reader := bufio.NewReader(in)
	for {
		req, err := read(reader)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := write(out, handle(ctx, req, version)); err != nil {
			return err
		}
	}
}

// Resolve finds the checkout that owner/repo is cloned into, by matching the
// remotes of every checkout under the configured scan roots.
func Resolve(owner, repo string) (string, error) {
	if owner == "" || repo == "" {
		return "", errors.New("no repository in the page's url")
	}
	cfg, err := settings.Load()
	if err != nil {
		return "", err
	}

	want := strings.ToLower(owner + "/" + repo)
	var nearest string
	var nearestSlug string
	for _, dir := range sessions.Dirs(cfg.Roots(), cfg.Ignored) {
		// The main checkout is where a PR checkout gets made, and it is the one
		// with a real .git directory to read the remotes out of.
		if dir.Worktree {
			continue
		}
		for _, url := range remotes(dir.Path) {
			found := slug(url)
			if found == want {
				return dir.Path, nil
			}
			// A GitHub rename leaves every clone's remote on the old name, so
			// the page's slug and the checkout's disagree.
			if nearest == "" && renamedFrom(dir.Path, owner, repo, found) {
				nearest, nearestSlug = dir.Path, found
			}
		}
	}
	if nearest != "" {
		return "", fmt.Errorf("%s is a checkout of %s, not %s/%s — if it was renamed on GitHub, run: git -C %s remote set-url origin git@github.com:%s/%s.git",
			nearest, nearestSlug, owner, repo, nearest, owner, repo)
	}
	return "", fmt.Errorf("no checkout of %s/%s under your scan directories; add one in ade's settings", owner, repo)
}

// ResolveSession finds the matching worktree when the closed page knows its
// branch, or falls back to the main checkout when it does not.
func ResolveSession(owner, repo, branch string) (string, error) {
	main, err := Resolve(owner, repo)
	if err != nil || branch == "" {
		return main, err
	}
	cfg, err := settings.Load()
	if err != nil {
		return "", err
	}
	return sessionCheckout(main, branch, sessions.Dirs(cfg.Roots(), cfg.Ignored)), nil
}

func sessionCheckout(main, branch string, dirs []sessions.Dir) string {
	for _, dir := range dirs {
		if dir.Main == main && dir.Branch == branch {
			return dir.Path
		}
	}
	return main
}

// renamedFrom reports whether a checkout looks like owner/repo under a name it
// has since lost: same owner, and a directory named for the page's repository.
// The directory is the tell; a rename moves the GitHub name, not the clone.
func renamedFrom(dir, owner, repo, found string) bool {
	foundOwner, _, ok := strings.Cut(found, "/")
	if !ok || foundOwner != strings.ToLower(owner) {
		return false
	}
	return strings.EqualFold(filepath.Base(dir), repo)
}

func handle(ctx context.Context, req Request, version string) Response {
	switch req.Action {
	case "ping":
		return Response{OK: true, Version: version}
	case "open":
		if req.Number <= 0 {
			return Response{Error: "no pull request number in the page's url"}
		}
		dir, err := Resolve(req.Owner, req.Repo)
		if err != nil {
			return Response{Error: err.Error()}
		}
		url, err := launch(ctx, dir, "--pr", fmt.Sprint(req.Number))
		if err != nil {
			return Response{Error: err.Error()}
		}
		return Response{OK: true, Dir: dir, URL: url}
	case "session":
		dir, err := ResolveSession(req.Owner, req.Repo, req.Branch)
		if err != nil {
			return Response{Error: err.Error()}
		}
		// The tab that asked is the one that will land on it, so ade prints the
		// address rather than opening a second tab beside it.
		url, err := launch(ctx, dir, "--no-open")
		if err != nil {
			return Response{Error: err.Error()}
		}
		return Response{OK: true, Dir: dir, URL: url}
	default:
		return Response{Error: fmt.Sprintf("unknown action %q", req.Action)}
	}
}

// launch runs ade in dir and returns the address it printed. ade detaches, so
// this waits only for it to report that the server came up.
func launch(ctx context.Context, dir string, args ...string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("find ade: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, launchWait)
	defer cancel()

	cmd := exec.CommandContext(ctx, exe, args...) // #nosec G204 -- ade's own binary, with arguments built here
	cmd.Dir = dir
	cmd.Env = environ(shellPath())
	out, err := cmd.CombinedOutput()
	if err != nil {
		if msg := lastLine(string(out)); msg != "" {
			return "", errors.New(msg)
		}
		return "", fmt.Errorf("ade %s: %w", strings.Join(args, " "), err)
	}
	return address(string(out)), nil
}

// address is the URL ade printed, which is the last line it writes on the way
// up. A last line that is not one means it said something else, not that the
// page has somewhere to go.
func address(out string) string {
	line := lastLine(out)
	if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
		return line
	}
	return ""
}

// loginPath is the PATH a terminal would have.
//
// Firefox starts the host from the desktop session, whose PATH is the bare
// system one — ade needs git, gh and claude, and a package manager puts none
// of them there. Empty means the shell could not say, and the host's own
// environment is used unchanged.
func loginPath() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), pathWait)
	defer cancel()

	cmd := exec.CommandContext(ctx, shell, "-l", "-c", `printf '%s%s' `+sentinel+` "$PATH"`) // #nosec G204 -- the user's own login shell
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	_, path, ok := strings.Cut(string(out), sentinel)
	if !ok {
		return ""
	}
	return strings.TrimSpace(path)
}

// environ is this process's environment with path in place of its PATH.
func environ(path string) []string {
	if path == "" {
		return nil
	}
	env := os.Environ()
	for i, entry := range env {
		if strings.HasPrefix(entry, "PATH=") {
			env[i] = "PATH=" + path
			return env
		}
	}
	return append(env, "PATH="+path)
}

func read(r *bufio.Reader) (Request, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return Request{}, io.EOF
		}
		return Request{}, err
	}
	// Native endian, which is what the protocol specifies.
	size := binary.NativeEndian.Uint32(header[:])
	if size == 0 || size > maxMessage {
		return Request{}, fmt.Errorf("message of %d bytes is not one of ours", size)
	}

	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return Request{}, err
	}
	var req Request
	if err := json.Unmarshal(body, &req); err != nil {
		return Request{}, fmt.Errorf("parse message: %w", err)
	}
	return req, nil
}

func write(w io.Writer, resp Response) error {
	body, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	var header [4]byte
	binary.NativeEndian.PutUint32(header[:], uint32(len(body))) // #nosec G115 -- a response is a handful of fields
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

// remotes reads the remote urls out of a checkout's git config, rather than
// shelling out to git once per repository on the machine.
func remotes(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, ".git", "config")) // #nosec G304 -- a checkout found by the scan
	if err != nil {
		return nil
	}

	var out []string
	inRemote := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inRemote = strings.HasPrefix(line, `[remote "`)
			continue
		}
		if !inRemote {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == "url" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

// slug reduces a remote url to owner/repo, lowercased, or "" if it is not one
// we can read. Both forms of GitHub url end in the same two path elements.
func slug(url string) string {
	url = strings.TrimSuffix(strings.TrimSpace(url), ".git")
	if i := strings.LastIndex(url, ":"); i >= 0 && !strings.Contains(url[i+1:], "/") {
		// scp-style git@host:owner/repo has no slash after the colon only when
		// it is malformed; a well-formed one does, so this trims the host.
		url = url[i+1:]
	} else if i := strings.Index(url, "://"); i >= 0 {
		url = url[i+3:]
		if j := strings.Index(url, "/"); j >= 0 {
			url = url[j+1:]
		}
	} else if i := strings.Index(url, ":"); i >= 0 {
		url = url[i+1:]
	}

	parts := strings.Split(strings.Trim(url, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	return strings.ToLower(parts[len(parts)-2] + "/" + parts[len(parts)-1])
}

func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}
