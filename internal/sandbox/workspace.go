package sandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/owenrumney/ade/internal/statedir"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Workspace is what a repository's sandbox has beyond the checkout: other
// directories the work reads, and variables the tools inside want. It is
// the repository's, not a session's — every worktree of it gets the same —
// and lives in the main git directory so it is never committed.
type Workspace struct {
	// Paths are mounted at their host path, read-only unless marked.
	Paths []Path `json:"paths"`
	// Env is set inside the container. Values land in a 0600 file under
	// .git; still, a key that is only needed on the host should stay there.
	Env map[string]string `json:"env"`
}

// Path is one mounted directory or file.
type Path struct {
	Path string `json:"path"`
	RW   bool   `json:"rw,omitempty"`
}

var envKeyRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// WorkspacePath is the file for a repository, under its main git directory.
func WorkspacePath(gitDir string) string {
	if gitDir == "" {
		return ""
	}
	return filepath.Join(statedir.Git(gitDir), "sandbox.json")
}

// SessionPath is one session's own file, under its worktree's git directory:
// the other checkouts it may edit, which are not every worktree's business.
func SessionPath(gitDir string) string {
	if gitDir == "" {
		return ""
	}
	return filepath.Join(statedir.Git(gitDir), "session.json")
}

// LoadWorkspace reads the file; none is an empty workspace.
func LoadWorkspace(file string) (Workspace, error) {
	ws := Workspace{Paths: []Path{}, Env: map[string]string{}}
	if file == "" {
		return ws, nil
	}
	data, err := os.ReadFile(file) // #nosec G304 -- ade's own state under .git
	if errors.Is(err, os.ErrNotExist) {
		return ws, nil
	}
	if err != nil {
		return ws, fmt.Errorf("read %s: %w", file, err)
	}
	if err := json.Unmarshal(data, &ws); err != nil {
		return ws, fmt.Errorf("parse %s: %w", file, err)
	}
	if ws.Paths == nil {
		ws.Paths = []Path{}
	}
	if ws.Env == nil {
		ws.Env = map[string]string{}
	}
	return ws, nil
}

// CheckWorkspace is ws with its paths and variables checked, so the runtime
// never refuses a mount after the agent has been asked to work.
func CheckWorkspace(home string, ws Workspace) (Workspace, error) {
	clean := Workspace{Paths: []Path{}, Env: map[string]string{}}
	seen := map[string]bool{}
	for _, p := range ws.Paths {
		path := strings.TrimSpace(p.Path)
		if path == "" {
			continue
		}
		if home != "" && strings.HasPrefix(path, "~/") {
			path = filepath.Join(home, path[2:])
		}
		if !filepath.IsAbs(path) {
			return ws, fmt.Errorf("%s: a mounted path must be absolute", path)
		}
		path = filepath.Clean(path)
		if home != "" && within(path, filepath.Join(home, ".claude")) {
			return ws, fmt.Errorf("%s: ~/.claude is mounted piecemeal by ade; nothing else of it goes in", path)
		}
		if home != "" && within(path, filepath.Join(home, ".pi")) {
			return ws, fmt.Errorf("%s: ~/.pi is mounted piecemeal by ade; nothing else of it goes in", path)
		}
		if _, err := os.Stat(path); err != nil {
			return ws, fmt.Errorf("%s: %w", path, err)
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		clean.Paths = append(clean.Paths, Path{Path: path, RW: p.RW})
	}
	for k, v := range ws.Env {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if !envKeyRE.MatchString(k) {
			return ws, fmt.Errorf("%q: not a variable name", k)
		}
		if k == "HOME" {
			return ws, errors.New("HOME is the host's inside the container; claude's transcripts depend on it")
		}
		clean.Env[k] = v
	}
	sort.Slice(clean.Paths, func(i, j int) bool { return clean.Paths[i].Path < clean.Paths[j].Path })
	return clean, nil
}

// SaveWorkspace checks and writes the file.
func SaveWorkspace(file, home string, ws Workspace) (Workspace, error) {
	if file == "" {
		return ws, errors.New("no git directory to keep the sandbox in")
	}
	clean, err := CheckWorkspace(home, ws)
	if err != nil {
		return ws, err
	}

	data, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return ws, err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		return ws, err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return ws, err
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
		return ws, err
	}
	return clean, nil
}

// Under is ws laid over base — the global sandbox settings, say, with a
// repository's own on top: ws wins where both name a path or a variable.
func (ws Workspace) Under(base Workspace) Workspace {
	out := Workspace{Paths: []Path{}, Env: map[string]string{}}
	for _, env := range []map[string]string{base.Env, ws.Env} {
		for k, v := range env {
			out.Env[k] = v
		}
	}
	at := map[string]int{}
	for _, p := range append(append([]Path{}, base.Paths...), ws.Paths...) {
		if i, ok := at[p.Path]; ok {
			out.Paths[i] = p
			continue
		}
		at[p.Path] = len(out.Paths)
		out.Paths = append(out.Paths, p)
	}
	sort.Slice(out.Paths, func(i, j int) bool { return out.Paths[i].Path < out.Paths[j].Path })
	return out
}

// Mounts is the workspace's paths as mounts, at their own path.
func (ws Workspace) Mounts() []Mount {
	out := make([]Mount, 0, len(ws.Paths))
	for _, p := range ws.Paths {
		out = append(out, Mount{Host: p.Path, Guest: p.Path, RW: p.RW})
	}
	return out
}

// Redacted stands in for a secret; sent back unchanged it means keep the value.
const Redacted = "••••••••"

//nolint:gochecknoglobals // a compiled pattern, not state
var secretKey = regexp.MustCompile(
	`(?i)PASSW|PASSPHRASE|TOKEN|SECRET|CREDENTIAL|APIKEY|PRIVATE|WEBHOOK|_DSN$|^DSN$|_KEY|^KEY$|_PAT$|_AUTH`)

func Secret(key string) bool { return secretKey.MatchString(key) }

func RedactEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		if v != "" && Secret(k) {
			v = Redacted
		}
		out[k] = v
	}
	return out
}

// RestoreEnv puts the stored value back behind each marker. A marker with no
// value behind it is a renamed key, and erasing the secret is refused.
func RestoreEnv(env, was map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(env))
	for k, v := range env {
		if v == Redacted {
			if was[k] == "" {
				return nil, fmt.Errorf("%s: type the value, or keep the name it was saved under", k)
			}
			v = was[k]
		}
		out[k] = v
	}
	return out, nil
}
