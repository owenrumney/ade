package audit_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/audit"
)

func lines(t *testing.T) []map[string]any {
	t.Helper()
	dir, err := audit.Dir()
	require.NoError(t, err)
	raw, err := os.ReadFile(filepath.Join(dir, time.Now().UTC().Format("2006-01-02")+".jsonl"))
	require.NoError(t, err)

	var out []map[string]any
	for _, line := range splitLines(string(raw)) {
		var one map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &one))
		out = append(out, one)
	}
	return out
}

func splitLines(raw string) []string {
	var out []string
	start := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\n' {
			continue
		}
		if i > start {
			out = append(out, raw[start:i])
		}
		start = i + 1
	}
	if start < len(raw) {
		out = append(out, raw[start:])
	}
	return out
}

func TestAStepSaysWhetherItPassedAndWhyNot(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	trace := audit.Trace()
	audit.Note(audit.Entry{Trace: trace, Kind: "connect", What: "connect pi@pi5.local", Where: "pi@pi5.local", From: "launcher"})
	audit.Step(trace, "pi@pi5.local", "dial over ssh", time.Now(), nil)
	audit.Step(trace, "pi@pi5.local", "find the checkout roots", time.Now(), errors.New("no route to host"))

	got := lines(t)
	require.Len(t, got, 3)

	// The parent claims nothing about itself: the panel marks it failed from
	// the step that did, so a half-written attempt never reads as fine.
	assert.Equal(t, "connect", got[0]["kind"])
	assert.NotContains(t, got[0], "ok")
	assert.Equal(t, trace, got[0]["trace"])

	assert.Equal(t, true, got[1]["ok"])
	assert.Equal(t, false, got[2]["ok"])
	assert.Equal(t, "no route to host", got[2]["why"])
	assert.Equal(t, "launcher", got[2]["from"])
	// Same trace on every line, or the panel draws three unrelated rows.
	assert.Equal(t, trace, got[2]["trace"])
}

func TestTheTrailIsNeverTheReasonSomethingFailed(t *testing.T) {
	// A directory that cannot be made: Note must still return quietly.
	home := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(home, ".ade"), []byte("not a directory"), 0o600))
	t.Setenv("HOME", home)

	assert.NotPanics(t, func() {
		audit.Note(audit.Entry{Kind: "connect", What: "connect somewhere"})
	})
}

func TestEveryAttemptGetsItsOwnTrace(t *testing.T) {
	assert.NotEqual(t, audit.Trace(), audit.Trace())
}
