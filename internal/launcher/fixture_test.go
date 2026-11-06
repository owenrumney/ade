package launcher

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
)

// fixture builds a home with its own settings and checkouts, so a test can say
// what the scan finds instead of taking whatever is on the developer's disk.
type fixture struct {
	home  string
	code  string
	alive map[string]bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	code := filepath.Join(home, "code")
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".ade"), 0o700))
	require.NoError(t, os.MkdirAll(code, 0o700))
	settings := fmt.Sprintf(`{"scan_dirs":[%q]}`, code)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".ade", "settings.json"), []byte(settings), 0o600))

	return &fixture{home: home, code: code, alive: map[string]bool{}}
}

// checkout adds a repository. A port of zero leaves it with no server record
// at all, which is the ordinary case of somewhere you could start one.
func (m *fixture) checkout(t *testing.T, name string, port int, live bool) string {
	t.Helper()
	dir := filepath.Join(m.code, name)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o700))
	if port == 0 {
		return dir
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/?t=tok", port)
	require.NoError(t, session.Write(session.Path(sessions.GitDir(dir)), session.Record{
		PID: os.Getpid(), URL: url, Root: dir, StartedAt: time.Now(),
	}))
	m.alive[url] = live
	return dir
}

// prober answers from what the fixture said, so nothing is probed for real.
func (m *fixture) prober() sessions.Prober {
	return func(_ context.Context, rec *session.Record) (session.Health, bool) {
		if !m.alive[rec.URL] {
			return session.Health{}, false
		}
		return session.Health{Ade: true, Root: rec.Root, PID: rec.PID}, true
	}
}

func newLauncherOn(t *testing.T, m *fixture) *Server {
	t.Helper()
	s, err := New(0, "test")
	require.NoError(t, err)
	s.probe = m.prober()
	return s
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "%s", out)
}
