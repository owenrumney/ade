// Package remote reaches another machine over ssh, so ade can run there and be
// driven from a browser here. It never interprets a host name: whatever
// `ssh <name>` does in a terminal is what this gets.
package remote

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/settings"
)

// Runner executes one ssh command line. Output is split because the far side's
// shell profile writes to both: stdout carries the answer, stderr the reason it
// failed. A field on Host so a test can assert the argv without a machine.
type Runner func(ctx context.Context, argv []string, stdin []byte) (stdout, stderr []byte, err error)

// Host is an ssh destination and the multiplexed connection to it.
type Host struct {
	// Name is what goes to ssh: an alias from the user's config, or user@host.
	Name string

	sock string
	ssh  Runner
	// local is this machine, reached by running rather than by ssh.
	local bool
	// stale is why the last connect ran the worker image it already had.
	stale error
}

// Stale is why this machine's worker may be older than the published image,
// or nil when the pull worked. A machine nobody dialled has nothing to report,
// so no caller has to hold a host to ask.
func (h *Host) Stale() error {
	if h == nil {
		return nil
	}
	return h.stale
}

// controlPersist keeps the master alive briefly after the last channel closes,
// so a reconnect within the gap costs no handshake.
const controlPersist = "60"

// connectTimeout bounds the handshake. Dialling happens inside an HTTP request
// the page is waiting on, so an unreachable host has to fail rather than hang.
const connectTimeout = "10"

// closeTimeout bounds tearing the master down, which is local socket work.
const closeTimeout = 5 * time.Second

// sentinel fences the command's own output. A shell profile is free to print,
// and on the way to running an interactive shell plenty do.
const sentinel = "__ade_out__"

// New prepares a host. It connects to nothing; Dial does that.
func New(name string) (*Host, error) {
	name = strings.TrimSpace(name)
	// The same rule the settings page enforces, so a host that saved cannot
	// fail here with a different complaint.
	if err := settings.CheckHostName(name); err != nil {
		return nil, err
	}
	dir, err := settings.Dir()
	if err != nil {
		return nil, err
	}
	sockets := filepath.Join(dir, "ssh")
	if err := os.MkdirAll(sockets, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", sockets, err)
	}
	return &Host{Name: name, sock: filepath.Join(sockets, name+".sock"), ssh: execRunner}, nil
}

// Socket is the control socket's path, for a message that has to name it.
func (h *Host) Socket() string { return h.sock }

// Dial opens the multiplexed master, or adopts one already running.
// ControlMaster is required rather than an optimisation: forwards are added to
// a live connection later, and only a control socket allows that.
func (h *Host) Dial(ctx context.Context) error {
	// Nothing to multiplex to this machine.
	if h.local {
		return nil
	}
	if h.Alive(ctx) {
		return nil
	}
	// BatchMode refuses rather than prompts. There is nowhere to type a
	// passphrase from a browser-driven connect, so a host that needs one has
	// to say so instead of hanging until the request times out.
	_, errOut, err := h.ssh(ctx, []string{
		"-M", "-S", h.sock, "-N", "-f",
		"-o", "ControlPersist=" + controlPersist,
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=" + connectTimeout,
		h.Name,
	}, nil)
	if err != nil {
		return fmt.Errorf("ssh %s: %s", h.Name, oneLine(errOut, err))
	}
	if !h.Alive(ctx) {
		return fmt.Errorf("ssh %s: the connection went away as soon as it was made", h.Name)
	}
	return nil
}

// Alive reports whether the master is up. This machine always is.
func (h *Host) Alive(ctx context.Context) bool {
	if h.local {
		return true
	}
	_, _, err := h.ssh(ctx, []string{"-S", h.sock, "-O", "check", h.Name}, nil)
	return err == nil
}

// Run executes one command on the far side, through a login shell. `ssh host
// cmd` runs without one, so a ade on a PATH set in .profile is not found and
// the failure reads as "no ade over there" when it is installed and working.
func (h *Host) Run(ctx context.Context, argv ...string) ([]byte, error) {
	return h.RunInput(ctx, nil, argv...)
}

// RunInput sends stdin to one remote command without putting it in argv.
func (h *Host) RunInput(ctx context.Context, input []byte, argv ...string) ([]byte, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("a remote command needs an argv")
	}
	args := []string{"-S", h.sock, h.Name, "--", login(argv)}
	out, errOut, err := h.ssh(ctx, args, input)
	if err != nil {
		return nil, fmt.Errorf("%s on %s: %s", argv[0], h.Name, oneLine(errOut, err))
	}
	return fenced(out), nil
}

// fenced is what the command itself printed, with whatever the profile said on
// the way in dropped.
func fenced(out []byte) []byte {
	if cut := bytes.LastIndex(out, []byte(sentinel)); cut >= 0 {
		return out[cut+len(sentinel):]
	}
	return out
}

// Close tears the master down. A connection that is already gone is success.
func (h *Host) Close() error {
	// There is no connection to this machine to drop.
	if h.local {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	if !h.Alive(ctx) {
		return nil
	}
	if _, errOut, err := h.ssh(ctx, []string{"-S", h.sock, "-O", "exit", h.Name}, nil); err != nil {
		return fmt.Errorf("close %s: %s", h.Name, oneLine(errOut, err))
	}
	return nil
}

// login reads the login profile without requiring a TTY. Interactive zsh
// profiles commonly configure zle, which fails for an ssh command channel.
func login(argv []string) string {
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		quoted = append(quoted, quote(arg))
	}
	command := "printf %s " + quote(sentinel) + "; " + strings.Join(quoted, " ")
	return `exec "${SHELL:-/bin/sh}" -lc ` + quote(command)
}

// quote makes one argument safe for a shell to read as a single word.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func execRunner(ctx context.Context, argv []string, stdin []byte) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, "ssh", argv...) // #nosec G204 -- a validated destination and argv built here
	var out, errOut bytes.Buffer
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	return out.Bytes(), errOut.Bytes(), err
}

// oneLine is what ssh said, for an error a person reads. ssh puts the useful
// part on the last line of stderr and the rest is banner.
func oneLine(out []byte, err error) string {
	for _, line := range reverse(strings.Split(strings.TrimSpace(string(out)), "\n")) {
		if line = strings.TrimSpace(line); line != "" && !boilerplate(line) {
			return line
		}
	}
	return err.Error()
}

// boilerplate is a last line that says nothing about what went wrong. docker
// ends every failure with one, and it would otherwise be all the user sees.
func boilerplate(line string) bool {
	return strings.HasPrefix(line, "Run '") || strings.HasPrefix(line, "See '")
}

func reverse(in []string) []string {
	out := make([]string, 0, len(in))
	for i := len(in) - 1; i >= 0; i-- {
		out = append(out, in[i])
	}
	return out
}
