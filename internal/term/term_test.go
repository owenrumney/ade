package term

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// read drains the channel until want shows up, so a test does not depend on
// how a shell chops its output into writes.
func read(t *testing.T, out <-chan []byte, want string) string {
	t.Helper()

	var seen strings.Builder
	deadline := time.After(5 * time.Second)
	for {
		select {
		case chunk, ok := <-out:
			if !ok {
				t.Fatalf("the shell exited before %q, saw %q", want, seen.String())
			}
			seen.Write(chunk)
			if strings.Contains(seen.String(), want) {
				return seen.String()
			}
		case <-deadline:
			t.Fatalf("waited for %q, saw %q", want, seen.String())
		}
	}
}

func session(t *testing.T, dir string) *Session {
	t.Helper()

	s := New(dir)
	s.shell = []string{"/bin/sh"}
	t.Cleanup(s.Close)
	return s
}

func TestNewCommandRunsInTheCheckout(t *testing.T) {
	s := NewCommand(t.TempDir(), "/bin/sh", "-c", "printf editor-session")
	t.Cleanup(s.Close)

	_, _, out, err := s.Attach()
	require.NoError(t, err)
	assert.Contains(t, read(t, out, "editor-session"), "editor-session")
}

func TestSessionRunsAShellInTheCheckout(t *testing.T) {
	dir := t.TempDir()
	// macOS hands out /var, which is a symlink to /private/var, and pwd in the
	// shell prints the resolved one.
	real, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	s := session(t, dir)
	_, _, out, err := s.Attach()
	require.NoError(t, err)

	require.NoError(t, s.Write([]byte("pwd\n")))
	assert.Contains(t, read(t, out, real), real)
}

func TestAttachReplaysWhatIsAlreadyOnTheScreen(t *testing.T) {
	s := session(t, t.TempDir())
	_, _, first, err := s.Attach()
	require.NoError(t, err)

	require.NoError(t, s.Write([]byte("echo marker-one\n")))
	read(t, first, "marker-one")

	// A second browser, or the same one after a reload: it must come up to the
	// session that is running, not to a blank screen.
	_, replay, _, err := s.Attach()
	require.NoError(t, err)
	assert.Contains(t, string(replay), "marker-one")
}

func TestEveryAttachedBrowserSeesTheSameShell(t *testing.T) {
	s := session(t, t.TempDir())
	_, _, first, err := s.Attach()
	require.NoError(t, err)
	_, _, second, err := s.Attach()
	require.NoError(t, err)

	require.NoError(t, s.Write([]byte("echo shared\n")))
	assert.Contains(t, read(t, first, "shared"), "shared")
	assert.Contains(t, read(t, second, "shared"), "shared")
}

func TestDetachLeavesTheShellRunning(t *testing.T) {
	s := session(t, t.TempDir())
	watching, _, out, err := s.Attach()
	require.NoError(t, err)

	require.NoError(t, s.Write([]byte("echo before\n")))
	read(t, out, "before")

	s.Detach(watching)
	// Drained, not read once: the channel is buffered, so whatever the shell
	// wrote before the detach is still queued ahead of the close.
	assert.True(t, drained(out), "detaching closes the channel it was reading")

	// Closing the tab must not kill what is running in it.
	require.NoError(t, s.Write([]byte("echo after\n")))
	_, replay, again, err := s.Attach()
	require.NoError(t, err)
	assert.Contains(t, string(replay)+read(t, again, "after"), "after")
}

func TestAttachAfterTheShellExitedStartsANewOne(t *testing.T) {
	s := session(t, t.TempDir())
	_, _, out, err := s.Attach()
	require.NoError(t, err)

	require.NoError(t, s.Write([]byte("exit\n")))
	for range out { //nolint:revive // draining until the shell is gone is the point
	}

	_, replay, next, err := s.Attach()
	require.NoError(t, err)
	assert.NotContains(t, string(replay), "exit", "a new shell starts with a clean screen")

	require.NoError(t, s.Write([]byte("echo alive\n")))
	assert.Contains(t, read(t, next, "alive"), "alive")
}

func TestResizeReachesTheShell(t *testing.T) {
	s := session(t, t.TempDir())
	_, _, out, err := s.Attach()
	require.NoError(t, err)

	require.NoError(t, s.Resize(100, 40))
	require.NoError(t, s.Write([]byte("stty size\n")))
	assert.Contains(t, read(t, out, "40 100"), "40 100")
}

func TestResizeIgnoresAnEmptyWindow(t *testing.T) {
	s := session(t, t.TempDir())
	_, _, _, err := s.Attach()
	require.NoError(t, err)

	// A hidden pane measures zero, and passing that on would reflow the shell
	// to one column.
	assert.NoError(t, s.Resize(0, 0))
	require.NoError(t, s.Resize(90, 30))
	assert.NoError(t, s.Resize(0, 30))
}

func TestWritingToAClosedSessionSaysSo(t *testing.T) {
	s := New(t.TempDir())
	s.shell = []string{"/bin/sh"}
	_, _, _, err := s.Attach()
	require.NoError(t, err)

	s.Close()
	assert.ErrorIs(t, s.Write([]byte("echo\n")), ErrGone)
	assert.ErrorIs(t, s.Resize(80, 24), ErrGone)
}

func TestTheScrollbackIsCapped(t *testing.T) {
	s := session(t, t.TempDir())
	_, _, out, err := s.Attach()
	require.NoError(t, err)

	require.NoError(t, s.Write([]byte("head -c 400000 /dev/zero | tr '\\0' 'x'; echo; echo done-flooding\n")))
	read(t, out, "done-flooding")

	_, replay, _, err := s.Attach()
	require.NoError(t, err)
	assert.LessOrEqual(t, len(replay), scrollMax, "an idle session must not hold a log file")
}

func TestTheEnvironmentSaysWhatTheBrowserCanDraw(t *testing.T) {
	t.Setenv("TERM", "dumb")
	t.Setenv("COLUMNS", "9")

	env := environ()
	assert.Contains(t, env, "TERM=xterm-256color")
	assert.Contains(t, env, "COLORTERM=truecolor")
	assert.Contains(t, env, "TERM_PROGRAM=ade")
	assert.NotContains(t, env, "TERM=dumb")
	assert.NotContains(t, env, "COLUMNS=9", "the pty owns the size, not an inherited one")
}

func TestTheShellIsTheUsersOwn(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	assert.Equal(t, []string{"/bin/sh"}, loginShell())

	// A $SHELL naming something that is not there is worse than no $SHELL.
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "nope"))
	shell := loginShell()
	require.Len(t, shell, 1)
	_, err := os.Stat(shell[0])
	assert.NoError(t, err)
}

func TestALoginShellReadsTheProfile(t *testing.T) {
	// argv[0] with a leading dash is how a terminal emulator asks for a login
	// shell, and it is why the prompt here matches the one in Ghostty.
	s := New(t.TempDir())
	t.Cleanup(s.Close)

	sh, err := s.spawn()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(sh.cmd.Args[0], "-"), "argv[0] is %q", sh.cmd.Args[0])
}

func TestTheEditorIsTheUsersOwn(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")

	// Neither variable set: whichever editor the machine has, and nothing at
	// all when it has none.
	editor := Editor()
	assert.Subset(t, []string{"nvim", "vim", "vi", "nano"}, editor)

	// $EDITOR wins, flags and all, and is skipped when it names nothing real.
	t.Setenv("EDITOR", "sh -c :")
	assert.Equal(t, []string{"sh", "-c", ":"}, Editor())
	t.Setenv("EDITOR", filepath.Join(t.TempDir(), "nope"))
	assert.Equal(t, editor, Editor())

	// $VISUAL is looked at first.
	t.Setenv("VISUAL", "sh")
	t.Setenv("EDITOR", "sh -c :")
	assert.Equal(t, []string{"sh"}, Editor())
}

func TestEditorNamesOnlyAnEditorThatIsThere(t *testing.T) {
	// A slim container image has no vim. Naming it anyway is a pane that
	// retries an exec that cannot work, once a second, forever.
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	assert.Nil(t, Editor())

	nano := filepath.Join(dir, "nano")
	require.NoError(t, os.WriteFile(nano, []byte("#!/bin/sh\n"), 0o755))
	assert.Equal(t, []string{"nano"}, Editor())

	t.Setenv("EDITOR", "nano -w")
	assert.Equal(t, []string{"nano", "-w"}, Editor())
}

// drained reports whether a channel reaches its close, ignoring whatever is
// already queued in it.
func drained(out <-chan []byte) bool {
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-out:
			if !ok {
				return true
			}
		case <-deadline:
			return false
		}
	}
}
