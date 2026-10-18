package remote

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// EditorServer is one running on the host: the port it bound on the host's
// loopback, and the token to present.
type EditorServer struct {
	Commit string
	Port   int
	Token  string
	Pid    int
}

// EditorServerWant is which server to have running. Unvalidated starts it
// with the client version check off, for a tarball built elsewhere.
type EditorServerWant struct {
	Commit      string
	URL         string
	Unvalidated bool
}

// commitName is what may name a tree. It goes into a path and into an rm,
// and it comes from a request.
var commitName = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// Arch is the host's os and architecture in the names the download template
// uses. Refused rather than guessed where ade publishes no server.
func (h *Host) Arch(ctx context.Context) (os, arch string, err error) {
	out, err := h.Run(ctx, "uname", "-s", "-m")
	if err != nil {
		return "", "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return "", "", fmt.Errorf("%s did not say what it is", h.Name)
	}
	if !strings.EqualFold(fields[0], "linux") {
		return "", "", fmt.Errorf("ade builds a server for linux arm64 and x64 only, and %s is %s", h.Name, fields[0])
	}
	switch fields[1] {
	case "aarch64", "arm64":
		return "linux", "arm64", nil
	case "x86_64", "amd64":
		return "linux", "x64", nil
	default:
		return "", "", fmt.Errorf("ade builds a server for linux arm64 and x64 only, and %s is %s", h.Name, fields[1])
	}
}

// EditorServer installs this commit if the host lacks it, starts one if none
// is running, and reports where it is. Reuse is one round trip.
func (h *Host) EditorServer(ctx context.Context, want EditorServerWant) (EditorServer, error) {
	if !commitName.MatchString(want.Commit) {
		return EditorServer{}, fmt.Errorf("%q does not name a build of the editor", want.Commit)
	}
	extra := ""
	if want.Unvalidated {
		extra = "--disable-client-validation"
	}
	// Fresh per start, read from stdin so it is in neither argv nor ps.
	token, err := newEditorToken()
	if err != nil {
		return EditorServer{}, err
	}
	out, err := h.RunInput(ctx, []byte(token+"\n"),
		"sh", "-c", editorServerStart, "ade", want.Commit, want.URL, extra)
	if err != nil {
		return EditorServer{}, err
	}
	server, err := readEditorServer(string(out))
	if err != nil {
		return EditorServer{}, fmt.Errorf("the editor server on %s: %w", h.Name, err)
	}
	server.Commit = want.Commit
	return server, nil
}

// StopEditorServer ends the one this launcher started, where the forwards
// come down: its only way in has gone with them.
func (h *Host) StopEditorServer(ctx context.Context, commit string) error {
	if !commitName.MatchString(commit) {
		return nil
	}
	_, err := h.Run(ctx, "sh", "-c", editorServerStop, "ade", commit)
	return err
}

// PruneEditorServers keeps this commit, the one before it and anything live;
// each tree is ~120 MiB. Listed first, so no glob reaches the host's shell.
func (h *Host) PruneEditorServers(ctx context.Context, commit string) error {
	out, err := h.Run(ctx, "sh", "-c", editorServerList, "ade")
	if err != nil {
		return err
	}
	drop := editorServersToDrop(commit, string(out))
	if len(drop) == 0 {
		return nil
	}
	_, err = h.Run(ctx, append([]string{"sh", "-c", editorServerRemove, "ade"}, drop...)...)
	return err
}

// editorServersToDrop is what a prune removes, newest first.
func editorServersToDrop(commit, listing string) []string {
	keep := map[string]bool{commit: true}
	var trees []string
	for line := range strings.SplitSeq(listing, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !commitName.MatchString(fields[0]) {
			continue
		}
		name, live := fields[0], fields[1] == "live"
		trees = append(trees, name)
		if live {
			keep[name] = true
		}
	}
	// The newest other tree, so reopening the window a bump replaced does
	// not download 120 MiB again.
	for _, name := range trees {
		if name != commit {
			keep[name] = true
			break
		}
	}
	var drop []string
	for _, name := range trees {
		if !keep[name] {
			drop = append(drop, name)
		}
	}
	return drop
}

func readEditorServer(out string) (EditorServer, error) {
	var server EditorServer
	for line := range strings.SplitSeq(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "port":
			server.Port, _ = strconv.Atoi(value)
		case "pid":
			server.Pid, _ = strconv.Atoi(value)
		case "token":
			server.Token = value
		}
	}
	if server.Port <= 0 || server.Token == "" {
		return EditorServer{}, fmt.Errorf("did not say where it is listening")
	}
	return server, nil
}

func newEditorToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("make a connection token: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// editorServerStart installs, reuses or starts. Detached, or it dies with the
// ssh channel; the pid is the child's own, which $! after setsid may not be.
const editorServerStart = `set -eu
commit=$1; url=$2; extra=$3
dir="$HOME/.ade-editor-server"
bin="$dir/bin"
tree="$bin/$commit"
exe="$tree/bin/ade-editor-server"
pidfile="$bin/$commit.pid"
portfile="$bin/$commit.port"
tokenfile="$bin/$commit.token"
log="$bin/$commit.log"
umask 077
mkdir -p "$bin"
read -r fresh

running() {
  [ -f "$pidfile" ] && [ -f "$portfile" ] && [ -f "$tokenfile" ] || return 1
  pid=$(cat "$pidfile")
  case "$pid" in ''|*[!0-9]*) return 1;; esac
  kill -0 "$pid" 2>/dev/null || return 1
  tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null | grep -q "bin/$commit/" || return 1
}

if running; then
  printf 'pid=%s\nport=%s\ntoken=%s\n' "$(cat "$pidfile")" "$(cat "$portfile")" "$(cat "$tokenfile")"
  exit 0
fi

if [ ! -x "$exe" ]; then
  rm -rf "$tree" "$tree.part"
  mkdir -p "$tree.part"
  curl -fsSL "$url" -o "$bin/$commit.tar.gz" ||
    { echo "could not download $url" >&2; exit 1; }
  tar xzf "$bin/$commit.tar.gz" -C "$tree.part"
  rm -f "$bin/$commit.tar.gz"
  # A reh tarball may or may not carry a wrapping directory.
  inner=$(find "$tree.part" -maxdepth 3 -type f -name ade-editor-server | head -1)
  [ -n "$inner" ] || { echo "the tarball at $url has no ade-editor-server in it" >&2; exit 1; }
  root=$(dirname "$(dirname "$inner")")
  [ "$root" = "$tree.part" ] || mv "$root" "$tree"
  [ -d "$tree" ] || mv "$tree.part" "$tree"
  rm -rf "$tree.part"
  chmod +x "$tree/bin/ade-editor-server"
fi

printf '%s' "$fresh" > "$tokenfile"
chmod 600 "$tokenfile"
: > "$log"
rm -f "$portfile"
lead=
command -v setsid >/dev/null 2>&1 && lead=setsid
# shellcheck disable=SC2086
nohup $lead sh -c 'printf %s "$$" > "$1"; shift; exec "$@"' sh "$pidfile" \
  "$exe" --start-server --host 127.0.0.1 --port 0 \
  --connection-token-file "$tokenfile" \
  --server-data-dir "$dir" --extensions-dir "$dir/extensions" \
  --enable-remote-auto-shutdown --accept-server-license-terms $extra \
  > "$log" 2>&1 &

port=
tries=0
while [ "$tries" -lt 150 ]; do
  port=$(sed -n 's/.*bound to 127\.0\.0\.1:\([0-9][0-9]*\).*/\1/p;s/.*listening on \([0-9][0-9]*\).*/\1/p' "$log" | head -1)
  [ -n "$port" ] && break
  if [ -f "$pidfile" ]; then
    pid=$(cat "$pidfile")
    case "$pid" in ''|*[!0-9]*) ;; *) kill -0 "$pid" 2>/dev/null || break;; esac
  fi
  tries=$((tries + 1))
  sleep 0.2
done
if [ -z "$port" ]; then
  echo "the editor server did not report a port:" >&2
  tail -5 "$log" >&2
  exit 1
fi
printf '%s\n' "$port" > "$portfile"
printf 'pid=%s\nport=%s\ntoken=%s\n' "$(cat "$pidfile")" "$port" "$fresh"`

const editorServerStop = `set -eu
commit=$1
bin="$HOME/.ade-editor-server/bin"
pidfile="$bin/$commit.pid"
[ -f "$pidfile" ] || exit 0
pid=$(cat "$pidfile")
case "$pid" in ''|*[!0-9]*) exit 0;; esac
if tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null | grep -q "bin/$commit/"; then
  kill "$pid" 2>/dev/null || true
fi
rm -f "$pidfile" "$bin/$commit.port"`

// editorServerList is the trees newest first, each with whether one is live.
// The cmdline check is for a pid file that survived a reboot.
const editorServerList = `set -eu
bin="$HOME/.ade-editor-server/bin"
[ -d "$bin" ] || exit 0
find "$bin" -mindepth 1 -maxdepth 1 -type d -exec ls -dt {} + | while read -r tree; do
  name=${tree##*/}
  state=dead
  pidfile="$bin/$name.pid"
  if [ -f "$pidfile" ]; then
    pid=$(cat "$pidfile")
    case "$pid" in ''|*[!0-9]*) pid=;; esac
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null &&
      tr '\0' ' ' < "/proc/$pid/cmdline" 2>/dev/null | grep -q "bin/$name/"; then
      state=live
    fi
  fi
  printf '%s %s\n' "$name" "$state"
done`

const editorServerRemove = `set -eu
bin="$HOME/.ade-editor-server/bin"
for name in "$@"; do
  rm -rf "$bin/$name" "$bin/$name.pid" "$bin/$name.port" "$bin/$name.token" "$bin/$name.log"
done`
