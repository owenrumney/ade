package transcripts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/design"
	"github.com/owenrumney/ade/internal/transcripts"
)

func claudeLine(t *testing.T, kind string, content any) string {
	t.Helper()
	line, err := json.Marshal(map[string]any{
		"type": kind, "timestamp": "2026-09-24T10:00:00Z", "cwd": "/work/ade",
		"message": map[string]any{"role": kind, "content": content},
	})
	require.NoError(t, err)
	return string(line) + "\n"
}

func writeTranscript(t *testing.T, lines ...string) *transcripts.Session {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "")), 0o600))
	return &transcripts.Session{Agent: transcripts.AgentClaude, ID: "s", Path: path}
}

func bodies(said []transcripts.Text) []string {
	var out []string
	for _, s := range said {
		out = append(out, string(s.Role)+": "+s.Body)
	}
	return out
}

func TestProseKeepsWhatWasSaidAndDropsTheWrapper(t *testing.T) {
	turn := design.TurnPrompt(design.Doc{Path: ".git/ade/docs/x.md"}, "", "", "move the watermark after publish",
		design.Heard{Who: "security", Text: "high: token leak in takeover.go:75"})
	session := writeTranscript(t,
		claudeLine(t, "user", turn),
		claudeLine(t, "assistant", []map[string]any{
			{"type": "text", "text": "Moved it."},
			{"type": "tool_use", "name": "Bash", "input": map[string]any{"command": "go test ./..."}},
		}),
		claudeLine(t, "user", []map[string]any{{"type": "tool_result", "content": "ok  ade 0.1s"}}),
		claudeLine(t, "user", design.BuildPrompt(".git/ade/docs/x.md")),
		claudeLine(t, "user", design.TitlePrompt("hi", "hello")),
	)

	said, end, err := transcripts.Prose(session, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"@security: high: token leak in takeover.go:75",
		"you: move the watermark after publish",
		"agent: Moved it.",
		"you: Implement the change described in .git/ade/docs/x.md.",
	}, bodies(said))

	info, err := os.Stat(session.Path)
	require.NoError(t, err)
	assert.Equal(t, info.Size(), end)
	for _, s := range said {
		assert.NotContains(t, s.Body, "Do NOT run git commit")
		assert.NotContains(t, s.Body, "go test")
	}
}

func TestProseReadsOnFromTheWatermark(t *testing.T) {
	first := claudeLine(t, "user", "first thing")
	session := writeTranscript(t, first, claudeLine(t, "user", "second thing"))

	said, _, err := transcripts.Prose(session, int64(len(first)), 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"you: second thing"}, bodies(said))

	said, _, err = transcripts.Prose(session, int64(len(first))-5, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"you: second thing"}, bodies(said), "a mark inside a line starts at the next one")
}

func TestProseLeavesAHalfWrittenLineForNextTime(t *testing.T) {
	first := claudeLine(t, "user", "first thing")
	session := writeTranscript(t, first, `{"type":"user","message":{"role":"user","content":"half`)

	said, end, err := transcripts.Prose(session, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"you: first thing"}, bodies(said))
	assert.Equal(t, int64(len(first)), end)
}

func TestProseFromCodexReadsTheEventStreamOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	lines := `{"type":"event_msg","payload":{"type":"user_message","message":"add a cap"}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"add a cap"}]}}
{"type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{}"}}
{"type":"event_msg","payload":{"type":"agent_message","message":"Capped at 8."}}
{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Capped at 8."}]}}
`
	require.NoError(t, os.WriteFile(path, []byte(lines), 0o600))

	said, _, err := transcripts.Prose(&transcripts.Session{Agent: transcripts.AgentCodex, Path: path}, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"you: add a cap", "agent: Capped at 8."}, bodies(said))
}

func TestAFoldKeepsTheAgentsReply(t *testing.T) {
	said := transcripts.Unwrap(strings.Join(strings.Fields(design.FoldPrompt("architect", "x.md", "split the store")), " "))
	assert.Equal(t, []string{"@architect: split the store"}, bodies(said))
}

func TestProseStopsBeforeTheLineThatWouldPassTheLimit(t *testing.T) {
	first := claudeLine(t, "user", "first thing")
	session := writeTranscript(t, first, claudeLine(t, "user", "second thing"))

	said, end, err := transcripts.Prose(session, 0, 20)
	require.NoError(t, err)
	assert.Equal(t, []string{"you: first thing"}, bodies(said))
	assert.Equal(t, int64(len(first)), end, "the rest waits for the next read")
}

// Eighteen of sixty rows in the switcher were ade asking an agent to name a
// conversation. They have a cwd like any session and are not one.
func TestAOneShotAdeWroteIsNotASession(t *testing.T) {
	machine := []string{
		"Name this conversation in at most six words, as a title for a list of sessions: what the work is",
		"Several reviewers looked at the change below. These are the findings they reported",
		"Rewrite these code review notes as comments to leave on a pull request. Keep each one short.",
		"You are applying review comments left on a diff of the current working tree.",
	}
	for _, prompt := range machine {
		assert.Truef(t, transcripts.Machine(&transcripts.Session{FirstPrompt: prompt}), "%q", prompt)
	}

	// A run ade launched is still a conversation you go back to.
	mine := []string{
		"Read ticket trk-1093 using the tools you have for it, then draft the design",
		"Implement the change described in .git/ade/docs/thing.md",
		"why is the commit panel empty?",
		"",
	}
	for _, prompt := range mine {
		assert.Falsef(t, transcripts.Machine(&transcripts.Session{FirstPrompt: prompt}), "%q", prompt)
	}
	assert.False(t, transcripts.Machine(nil))
}
