package session

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// healthServer stands in for a running ade. Like the real one, it says what
// the agent is doing only to a caller with its token, "tok"; the URL it
// returns carries that token the way a record does.
func healthServer(t *testing.T, health Health) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		answer := health
		if r.URL.Query().Get("t") != "tok" {
			answer.Branch, answer.Agent = "", nil
		}
		require.NoError(t, json.NewEncoder(w).Encode(answer))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/?t=tok"
}

func TestRoundTrip(t *testing.T) {
	gitDir := t.TempDir()

	_, ok := Read(Path(gitDir))
	assert.False(t, ok, "nothing recorded yet")

	rec := Record{PID: 4821, URL: "http://127.0.0.1:53914/?t=abc", Root: "/repo", StartedAt: time.Now()}
	require.NoError(t, Write(Path(gitDir), rec))

	got, ok := Read(Path(gitDir))
	require.True(t, ok)
	assert.Equal(t, rec.PID, got.PID)
	assert.Equal(t, rec.URL, got.URL)
	assert.Equal(t, rec.Root, got.Root)

	// The URL carries the session token, so the file must not be readable by
	// anyone else on the machine.
	info, err := os.Stat(Path(gitDir))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	require.NoError(t, Remove(Path(gitDir)))
	_, ok = Read(Path(gitDir))
	assert.False(t, ok)
	require.NoError(t, Remove(Path(gitDir)), "removing what is not there is success")
}

func TestReadTreatsRubbishAsAbsent(t *testing.T) {
	tests := map[string]string{
		"not json":    "{{{",
		"no pid":      `{"url":"http://x/?t=a"}`,
		"no url":      `{"pid":1}`,
		"empty":       "",
		"null fields": `{"pid":0,"url":""}`,
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			gitDir := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(gitDir, "ade"), 0o750))
			require.NoError(t, os.WriteFile(Path(gitDir), []byte(body), 0o600))

			// The only sensible response to an unreadable record is to start a
			// server, so this must not be an error.
			_, ok := Read(Path(gitDir))
			assert.False(t, ok)
		})
	}
}

func TestLive(t *testing.T) {
	mine := os.Getpid()

	t.Run("a live server that is ours", func(t *testing.T) {
		url := healthServer(t, Health{Ade: true, Root: "/repo", PID: mine})
		assert.True(t, Live(context.Background(), &Record{PID: mine, URL: url, Root: "/repo"}))
	})

	t.Run("a dead pid is not live", func(t *testing.T) {
		url := healthServer(t, Health{Ade: true, Root: "/repo", PID: mine})
		// Nothing answers on this pid; the health check would pass on its own,
		// which is exactly why both are needed.
		assert.False(t, Live(context.Background(), &Record{PID: 99999999, URL: url, Root: "/repo"}))
	})

	t.Run("a live pid with nothing listening is not live", func(t *testing.T) {
		assert.False(t, Live(context.Background(), &Record{
			PID: mine, URL: "http://127.0.0.1:1/?t=a", Root: "/repo"}))
	})

	t.Run("another process on the port is not ours", func(t *testing.T) {
		// PID reuse: the recorded pid is alive, but the port belongs to
		// something else now.
		url := healthServer(t, Health{Ade: true, Root: "/elsewhere", PID: 12345})
		assert.False(t, Live(context.Background(), &Record{PID: mine, URL: url, Root: "/repo"}))
	})

	t.Run("not ade at all", func(t *testing.T) {
		url := healthServer(t, Health{Ade: false, Root: "/repo", PID: mine})
		assert.False(t, Live(context.Background(), &Record{PID: mine, URL: url, Root: "/repo"}))
	})

	t.Run("no record", func(t *testing.T) {
		assert.False(t, Live(context.Background(), nil))
	})

	t.Run("probe says what the agent is doing", func(t *testing.T) {
		url := healthServer(t, Health{Ade: true, Root: "/repo", PID: mine, Branch: "feat",
			Agent: &AgentHealth{Running: true, Kind: "chat", SideRuns: []string{"security"}}})
		health, ok := Probe(context.Background(), &Record{PID: mine, URL: url, Root: "/repo"})
		require.True(t, ok)
		assert.Equal(t, "feat", health.Branch)
		require.NotNil(t, health.Agent)
		assert.True(t, health.Agent.Running)
		assert.Equal(t, []string{"security"}, health.Agent.SideRuns)
	})

	t.Run("a probe without the token is live but told nothing more", func(t *testing.T) {
		url := healthServer(t, Health{Ade: true, Root: "/repo", PID: mine, Branch: "feat",
			Agent: &AgentHealth{Running: true}})
		base, err := baseOf(url)
		require.NoError(t, err)
		health, ok := Probe(context.Background(), &Record{PID: mine, URL: base + "/", Root: "/repo"})
		require.True(t, ok)
		assert.Empty(t, health.Branch)
		assert.Nil(t, health.Agent)
	})
}

func TestBaseOf(t *testing.T) {
	tests := []struct {
		name  string
		url   string
		want  string
		isErr bool
	}{
		{name: "with a token", url: "http://127.0.0.1:53914/?t=abc", want: "http://127.0.0.1:53914"},
		{name: "bare", url: "http://127.0.0.1:53914", want: "http://127.0.0.1:53914"},
		{name: "not http", url: "https://example.com/", isErr: true},
		{name: "empty", url: "", isErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := baseOf(tt.url)
			if tt.isErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStopWithNothingRunning(t *testing.T) {
	_, err := Stop(Path(t.TempDir()), time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no ade is running")
}

func TestStopClearsARecordForADeadProcess(t *testing.T) {
	gitDir := t.TempDir()
	require.NoError(t, Write(Path(gitDir), Record{PID: 99999999, URL: "http://127.0.0.1:1/?t=a"}))

	_, err := Stop(Path(gitDir), time.Second)
	require.NoError(t, err)

	_, ok := Read(Path(gitDir))
	assert.False(t, ok, "a record for a process that is gone is cleared, not left to confuse")
}

func TestAWorkerSharesTheRecordWithItsHostsGroup(t *testing.T) {
	record := filepath.Join(t.TempDir(), "ade", "server.json")
	t.Setenv(HostGroupEnv, strconv.Itoa(os.Getgid()))

	require.NoError(t, Write(record, Record{PID: 1, URL: "http://127.0.0.1:1/?t=x"}))

	file, err := os.Stat(record)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), file.Mode().Perm(), "the group reads the record")
	dir, err := os.Stat(filepath.Dir(record))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o750), dir.Mode().Perm(), "and can get to it")
}

func TestTheRecordIsPrivateEverywhereElse(t *testing.T) {
	for _, gid := range []string{"", "not a number", "-1"} {
		record := filepath.Join(t.TempDir(), "ade", "server.json")
		t.Setenv(HostGroupEnv, gid)

		require.NoError(t, Write(record, Record{PID: 1, URL: "http://127.0.0.1:1/?t=x"}))

		file, err := os.Stat(record)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), file.Mode().Perm(), gid)
	}
}
