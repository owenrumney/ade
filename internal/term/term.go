// Package term runs a login shell behind a pty so a browser can drive it.
package term

import (
	"errors"
	"fmt"
	"github.com/owenrumney/ade/internal/gotmp"
	"github.com/owenrumney/ade/internal/settings"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/creack/pty"
)

// Session is one shell for one checkout, shared by every browser attached to
// it. The shell outlives any single connection: closing the tab, or reloading
// the page, leaves what is running alone and reattaches to it.
type Session struct {
	dir string

	// shell is what to run. Empty means the user's login shell.
	shell []string

	mu sync.Mutex
	sh *shell
}

// shell is one running pty. It is replaced wholesale when the last one exited,
// so a reader either sees a live shell or none, never a half-dead one.
type shell struct {
	pty *os.File
	cmd *exec.Cmd

	mu     sync.Mutex
	subs   map[int]chan []byte
	nextID int
	// scroll is the tail of what has been printed, replayed to a browser that
	// attaches after the fact so a reload does not come up to a blank screen.
	scroll []byte
	alive  bool
	cols   uint16
	rows   uint16
}

// scrollMax is how much output is kept for replay. Enough for a few screens of
// context, small enough that an idle session is not holding a log file.
const scrollMax = 256 << 10

// chunkQueue is how many chunks a browser may fall behind before it is dropped.
// Dropping it costs a reconnect and a replay, which redraws correctly; blocking
// the reader instead would stall the shell for everyone.
const chunkQueue = 256

// readChunk is the most output taken from the pty in one read.
const readChunk = 32 << 10

// ErrGone means the shell exited while the caller held a handle on it.
var ErrGone = errors.New("terminal has exited")

// New returns a session that will run a shell in dir. Nothing starts until the
// first attach, so a ade whose terminal is never opened never forks one.
func New(dir string) *Session {
	return &Session{dir: dir}
}

// NewCommand runs argv in dir instead of a login shell.
func NewCommand(dir string, argv ...string) *Session {
	return &Session{dir: dir, shell: argv}
}

// Attach subscribes to the shell's output, starting it if it is not running.
// The returned bytes are what has already been printed; the channel carries
// what follows, and is closed when the shell exits or the subscriber is
// dropped for falling behind.
func (s *Session) Attach() (id int, replay []byte, out <-chan []byte, err error) {
	sh, err := s.live()
	if err != nil {
		return 0, nil, nil, err
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()
	if !sh.alive {
		return 0, nil, nil, ErrGone
	}

	sh.nextID++
	id = sh.nextID
	ch := make(chan []byte, chunkQueue)
	sh.subs[id] = ch

	replay = make([]byte, len(sh.scroll))
	copy(replay, sh.scroll)
	return id, replay, ch, nil
}

// Detach drops one subscriber. The shell keeps running with none.
func (s *Session) Detach(id int) {
	s.mu.Lock()
	sh := s.sh
	s.mu.Unlock()
	if sh == nil {
		return
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()
	if ch, ok := sh.subs[id]; ok {
		delete(sh.subs, id)
		close(ch)
	}
}

// Write sends keystrokes to the shell.
func (s *Session) Write(p []byte) error {
	s.mu.Lock()
	sh := s.sh
	s.mu.Unlock()
	if sh == nil {
		return ErrGone
	}
	if _, err := sh.pty.Write(p); err != nil {
		return fmt.Errorf("write to the terminal: %w", err)
	}
	return nil
}

// Resize tells the shell how big the window is. Programs that redraw on
// SIGWINCH — an editor, a pager — depend on it.
func (s *Session) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return nil
	}

	s.mu.Lock()
	sh := s.sh
	s.mu.Unlock()
	if sh == nil {
		return ErrGone
	}

	sh.mu.Lock()
	sh.cols, sh.rows = cols, rows
	sh.mu.Unlock()

	if err := pty.Setsize(sh.pty, &pty.Winsize{Cols: cols, Rows: rows}); err != nil {
		return fmt.Errorf("resize the terminal: %w", err)
	}
	return nil
}

// Close kills the shell and everything it started. Called when ade shuts down.
func (s *Session) Close() {
	s.mu.Lock()
	sh := s.sh
	s.sh = nil
	s.mu.Unlock()
	if sh == nil {
		return
	}
	_ = sh.pty.Close()
	if sh.cmd.Process != nil {
		// The pty is the process group leader's controlling terminal, so the
		// negative pid reaches anything the shell left running.
		_ = killGroup(sh.cmd.Process.Pid)
	}
}

// live returns the running shell, starting one if the last exited.
func (s *Session) live() (*shell, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sh != nil {
		s.sh.mu.Lock()
		alive := s.sh.alive
		s.sh.mu.Unlock()
		if alive {
			return s.sh, nil
		}
	}

	sh, err := s.spawn()
	if err != nil {
		return nil, err
	}
	s.sh = sh
	return sh, nil
}

// spawn starts the shell. It is a login shell, argv[0] marked with a leading
// dash the way a terminal emulator does, so the profile that sets a prompt and
// a PATH is read and the shell here matches the one in Ghostty.
func (s *Session) spawn() (*shell, error) {
	argv := s.shell
	if len(argv) == 0 {
		argv = loginShell()
	}

	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // the user's own shell, from $SHELL
	cmd.Dir = s.dir
	cmd.Env = environ()
	if len(s.shell) == 0 {
		cmd.Args[0] = "-" + baseName(argv[0])
	}

	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		slog.Warn("could not start a shell", "bin", argv[0], "dir", s.dir, "err", err)
		return nil, fmt.Errorf("start a shell in %s: %w", s.dir, err)
	}
	// The binary alone: a boxed takeover's argv carries tokens, and the log is served.
	slog.Info("shell started", "bin", argv[0], "dir", s.dir, "pid", cmd.Process.Pid)

	sh := &shell{pty: f, cmd: cmd, subs: map[int]chan []byte{}, alive: true, cols: 80, rows: 24}
	go sh.pump()
	return sh, nil
}

// pump reads the shell's output until it exits, feeding every subscriber and
// the replay buffer.
func (sh *shell) pump() {
	buf := make([]byte, readChunk)
	for {
		n, err := sh.pty.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			sh.broadcast(chunk)
		}
		if err != nil {
			break
		}
	}

	_ = sh.cmd.Wait()
	_ = sh.pty.Close()
	slog.Debug("shell exited", "pid", sh.cmd.Process.Pid)

	sh.mu.Lock()
	sh.alive = false
	for id, ch := range sh.subs {
		delete(sh.subs, id)
		close(ch)
	}
	sh.mu.Unlock()
}

// broadcast appends to the replay buffer and hands the chunk to every
// subscriber, dropping any that is too far behind to take it.
func (sh *shell) broadcast(chunk []byte) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	sh.scroll = append(sh.scroll, chunk...)
	if len(sh.scroll) > scrollMax {
		sh.scroll = append([]byte(nil), sh.scroll[len(sh.scroll)-scrollMax:]...)
	}

	for id, ch := range sh.subs {
		select {
		case ch <- chunk:
		default:
			delete(sh.subs, id)
			close(ch)
		}
	}
}

// loginShell is the shell to run, preferring the one the user has chosen.
func loginShell() []string {
	if sh := os.Getenv("SHELL"); sh != "" {
		if _, err := os.Stat(sh); err == nil {
			return []string{sh}
		}
	}
	if runtime.GOOS == "darwin" {
		return []string{"/bin/zsh"}
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		return []string{"/bin/bash"}
	}
	return []string{"/bin/sh"}
}

// Editor is what the editor tab runs: $VISUAL, then $EDITOR, then whichever
// of nvim and vim is installed. The variables may carry flags, so they split.
func Editor() []string {
	for _, name := range []string{"VISUAL", "EDITOR"} {
		argv := strings.Fields(os.Getenv(name))
		if len(argv) == 0 {
			continue
		}
		if _, err := exec.LookPath(argv[0]); err == nil {
			return argv
		}
	}
	// vi and nano before giving up: a slim container image has no vim, and
	// naming one that is not there is a pane that retries forever.
	for _, name := range []string{"nvim", "vim", "vi", "nano"} {
		if _, err := exec.LookPath(name); err == nil {
			return []string{name}
		}
	}
	return nil
}

// environ is the shell's environment. TERM is xterm-256color rather than
// Ghostty's own xterm-ghostty: the terminfo for that is installed alongside
// Ghostty and would be missing on the far side of an ssh, and xterm.js
// implements what it describes anyway.
func environ() []string {
	env := os.Environ()
	drop := map[string]bool{"TERM": true, "COLORTERM": true, "TERM_PROGRAM": true, "LINES": true, "COLUMNS": true, "ADE_PALETTE": true}

	kept := env[:0]
	for _, entry := range env {
		name, _, ok := cut(entry, '=')
		if ok && drop[name] {
			continue
		}
		kept = append(kept, entry)
	}
	// ADE_PALETTE says what ade is wearing, for a config that wants to match:
	// a truecolor colorscheme picks its own colours and never asks.
	out := append(kept, "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=ade")
	if palette := currentPalette(); palette != "" {
		out = append(out, "ADE_PALETTE="+palette)
	}
	return gotmp.Apply(out)
}

// currentPalette is the saved palette, or empty: a shell missing the variable
// beats a shell that will not start.
func currentPalette() string {
	cfg, err := settings.Load()
	if err != nil {
		return ""
	}
	return settings.PaletteID(cfg.Palette)
}

func cut(s string, sep byte) (before, after string, found bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
