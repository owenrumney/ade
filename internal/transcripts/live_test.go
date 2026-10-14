package transcripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionIDsInCommand(t *testing.T) {
	// Command lines taken verbatim from a real process table.
	tests := []struct {
		name    string
		command string
		want    []string
	}{
		{
			name:    "interactive claude resuming by id",
			command: "/opt/homebrew/bin/claude --dangerously-skip-permissions --resume 71af5313-8d74-499c-a84d-9b19bd182348",
			want:    []string{"71af5313-8d74-499c-a84d-9b19bd182348"},
		},
		{
			name:    "claude resuming by transcript path",
			command: "/opt/homebrew/bin/claude --resume /Users/owen/.claude/projects/-Users-owen-code-owenrumney-ade/028dc418-7b68-4763-a965-8eb5bfaf2001.jsonl",
			want:    []string{"028dc418-7b68-4763-a965-8eb5bfaf2001"},
		},
		{
			name:    "codex resume subcommand",
			command: "/opt/homebrew/bin/codex resume 019ed581-8248-73d2-8b59-7f0be08ecfdb",
			want:    []string{"019ed581-8248-73d2-8b59-7f0be08ecfdb"},
		},
		{
			name:    "pi session flag",
			command: "/opt/homebrew/bin/pi --session 019fd6de-0bfe-742c-9e86-5fed1a3f3556",
			want:    []string{"019fd6de-0bfe-742c-9e86-5fed1a3f3556"},
		},
		{
			name:    "watchtower open subcommand",
			command: "/Users/owen/go/bin/watchtower open wt-20260730-172219-e9f585",
			want:    []string{"wt-20260730-172219-e9f585"},
		},
		{
			// The mdview chat sidecars resume real session ids but are not
			// sessions anyone is sitting in.
			name:    "headless sidecar is ignored",
			command: "/opt/homebrew/bin/claude -p --input-format stream-json --output-format stream-json --verbose --append-system-prompt assisting --resume 7be39583-bf69-4a1e-a679-d97b73c078b4",
			want:    nil,
		},
		{
			name:    "pty host wrapper is ignored",
			command: "/opt/homebrew/Caskroom/claude-code/2.1.212/claude --bg-pty-host /tmp/cc-daemon-501/4b255d0b/pty/bc327e3e.sock 125 54 -- /opt/homebrew/Caskroom/claude-code/2.1.212/claude --session-id bc327e3e-4040-4f85-9a9d-fc680203d46d --resume /x/028dc418.jsonl",
			want:    nil,
		},
		{
			name:    "daemons are ignored",
			command: "/Users/owen/go/bin/watchtower daemon run",
			want:    nil,
		},
		{
			name:    "claude daemon is ignored",
			command: "/Users/owen/.local/bin/claude daemon run --json-path /Users/owen/.claude/daemon.json",
			want:    nil,
		},
		{
			// --fork-session means the resumed transcript is the source, not the
			// session now running, so only --session-id is live.
			name:    "forked session reports only the new id",
			command: "/opt/homebrew/Caskroom/claude-code/2.1.212/claude --session-id bc327e3e-4040-4f85-9a9d-fc680203d46d --fork-session --resume /Users/owen/.claude/projects/-x/028dc418-7b68-4763-a965-8eb5bfaf2001.jsonl --permission-mode bypassPermissions",
			want:    []string{"bc327e3e-4040-4f85-9a9d-fc680203d46d"},
		},
		{
			name:    "unrelated process",
			command: "/usr/bin/vim notes.md --resume something",
			want:    nil,
		},
		{
			name:    "a fresh session names no id",
			command: "/opt/homebrew/bin/claude --dangerously-skip-permissions",
			want:    nil,
		},
		{
			name:    "flag with no value does not panic",
			command: "/opt/homebrew/bin/claude --resume",
			want:    nil,
		},
		{
			name:    "empty",
			command: "",
			want:    nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sessionIDsInCommand(strings.Fields(tc.command))
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNormaliseSessionID(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"71af5313-8d74-499c-a84d-9b19bd182348", "71af5313-8d74-499c-a84d-9b19bd182348"},
		{"/x/y/028dc418-7b68-4763-a965-8eb5bfaf2001.jsonl", "028dc418-7b68-4763-a965-8eb5bfaf2001"},
		{"/x/2026-08-06T11-38-24-126Z_019fd6de-0bfe-742c-9e86-5fed1a3f3556.jsonl", "019fd6de-0bfe-742c-9e86-5fed1a3f3556"},
		{"wt-20260730-172219-e9f585", "wt-20260730-172219-e9f585"},
		{"--another-flag", ""},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, normaliseSessionID(tc.in))
		})
	}
}

func TestLivenessFromRecentWrites(t *testing.T) {
	home := fakeHome(t)
	sessions := Scan()
	require.NotEmpty(t, sessions)

	// The fixtures were written moments ago, so every one reads as live.
	live := Liveness(sessions)
	for _, s := range sessions {
		assert.True(t, live[s.Path], "%s should be live: just written", s.Agent)
	}

	// Backdate one past the window and it should go quiet.
	claude := filepath.Join(home, ".claude", "projects", "-work-coast", "aaaa-1111.jsonl")
	stale := time.Now().Add(-2 * LiveWindow)
	require.NoError(t, touch(claude, stale))

	live = Liveness(Scan())
	assert.False(t, live[claude], "a transcript older than the window is not live")
}

func TestStateOf(t *testing.T) {
	tests := []struct {
		name   string
		status string
		live   bool
		want   State
	}{
		{"live beats everything", "archived", true, StateLive},
		{"watchtower archived", "archived", false, StateArchived},
		{"watchtower active", "active", false, StateActive},
		{"no status is idle", "", false, StateIdle},
		{"unknown status is idle", "stopped", false, StateIdle},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, StateOf(&Session{Status: tc.status}, tc.live))
		})
	}
}

// touch backdates a file's modification time.
func touch(path string, when time.Time) error {
	return os.Chtimes(path, when, when)
}

func TestAgentOfCommand(t *testing.T) {
	tests := []struct {
		name            string
		command         string
		wantAgent       Agent
		wantInteractive bool
	}{
		{"interactive claude", "/opt/homebrew/bin/claude --dangerously-skip-permissions", AgentClaude, true},
		{"interactive pi", "/opt/homebrew/bin/pi", AgentPi, true},
		{"headless sidecar", "/opt/homebrew/bin/claude -p --input-format stream-json", "", false},
		{"daemon", "/Users/owen/go/bin/watchtower daemon run", "", false},
		{"pty host", "/x/claude --bg-pty-host /tmp/s.sock", "", false},
		{"unrelated", "/usr/bin/vim notes.md", "", false},
		{"empty", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agent, interactive := agentOfCommand(strings.Fields(tc.command))
			assert.Equal(t, tc.wantInteractive, interactive)
			assert.Equal(t, tc.wantAgent, agent)
		})
	}
}

func TestUnderDir(t *testing.T) {
	assert.True(t, underDir("/w/coast", "/w/coast"))
	assert.True(t, underDir("/w/coast/.scratch", "/w/coast"), "agents wander into subdirectories")
	assert.True(t, underDir("/w/coast/a/b", "/w/coast/"))
	assert.False(t, underDir("/w/coast-fix-497", "/w/coast"), "a sibling is not inside")
	assert.False(t, underDir("/w", "/w/coast"))
}

func TestNewestSessionUnderPicksTheMostRecent(t *testing.T) {
	now := time.Now()
	old := &Session{Agent: AgentClaude, CWD: "/w/coast", Path: "old", LastUsed: now.Add(-time.Hour)}
	recent := &Session{Agent: AgentClaude, CWD: "/w/coast", Path: "recent", LastUsed: now}
	other := &Session{Agent: AgentPi, CWD: "/w/coast", Path: "pi", LastUsed: now.Add(time.Hour)}
	elsewhere := &Session{Agent: AgentClaude, CWD: "/w/other", Path: "elsewhere", LastUsed: now.Add(time.Hour)}
	sessions := []*Session{old, recent, other, elsewhere}

	got := newestSessionUnder(sessions, agentDir{agent: AgentClaude, cwd: "/w/coast/.scratch"})
	require.NotNil(t, got)
	assert.Equal(t, "recent", got.Path, "same agent, inside the directory, most recent")

	assert.Nil(t, newestSessionUnder(sessions, agentDir{agent: AgentCodex, cwd: "/w/coast"}),
		"a different agent's process says nothing about these sessions")
}

func TestLivenessMatchesAFreshSessionByDirectory(t *testing.T) {
	home := fakeHome(t)
	claude := filepath.Join(home, ".claude", "projects", "-work-coast", "fresh-0001.jsonl")
	writeFixture(t, claude,
		`{"type":"user","timestamp":"2026-08-01T10:00:00Z","cwd":"/work/coast","message":{"role":"user","content":"still thinking"}}`+"\n")

	// Backdate it past the write window: only a directory match can find it.
	stale := time.Now().Add(-2 * LiveWindow)
	require.NoError(t, touch(claude, stale))

	var target *Session
	for _, s := range Scan() {
		if s.ID == "fresh-0001" {
			target = s
		}
	}
	require.NotNil(t, target)

	live := map[string]bool{}
	if s := newestSessionUnder([]*Session{target}, agentDir{agent: AgentClaude, cwd: "/work/coast"}); s != nil {
		live[s.Path] = true
	}
	assert.True(t, live[target.Path], "a fresh session waiting on input is still live")
}
