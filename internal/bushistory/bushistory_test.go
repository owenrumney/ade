package bushistory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAConversationIsKeptPerPerson(t *testing.T) {
	log := Open(t.TempDir())
	require.NoError(t, log.Add("kim@x.io", Entry{ID: "q1", Kind: KindAsk, Mine: true, Question: "ship it?"}))
	require.NoError(t, log.Add("kim@x.io", Entry{ID: "q1", Kind: KindAnswer, Text: "yes"}))
	require.NoError(t, log.Add("sam@x.io", Entry{ID: "q2", Kind: KindAsk, Question: "lunch?"}))

	kim, err := log.With("kim@x.io")
	require.NoError(t, err)
	require.Len(t, kim, 2)
	assert.Equal(t, "ship it?", kim[0].Question)
	assert.True(t, kim[0].Mine)
	assert.Equal(t, "yes", kim[1].Text)

	nobody, err := log.With("lee@x.io")
	require.NoError(t, err)
	assert.Empty(t, nobody)
}

// A reconnect replays what the bus still holds. The same thing said twice is
// one line, or every restart would repeat the conversation.
func TestTheSameThingSaidTwiceIsKeptOnce(t *testing.T) {
	log := Open(t.TempDir())
	one := Entry{ID: "q1", Kind: KindAnswer, Text: "yes"}
	require.NoError(t, log.Add("kim@x.io", one))
	require.NoError(t, log.Add("kim@x.io", one))
	got, err := log.With("kim@x.io")
	require.NoError(t, err)
	assert.Len(t, got, 1)

	// Your answer to their question is not their answer to yours.
	require.NoError(t, log.Add("kim@x.io", Entry{ID: "q1", Kind: KindAnswer, Mine: true, Text: "no"}))
	got, err = log.With("kim@x.io")
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

// Only you can read it: it is private conversation.
func TestTheFilesAreYoursAlone(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, Open(home).Add("kim@x.io", Entry{ID: "q1", Kind: KindAsk}))
	info, err := os.Stat(filepath.Join(home, "bus", "kim@x.io.jsonl"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	dir, err := os.Stat(filepath.Join(home, "bus"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dir.Mode().Perm())
}

// The name comes off the bus, so nothing in it may leave the directory.
func TestANameCannotLeaveTheDirectory(t *testing.T) {
	for who, want := range map[string]string{
		"Kim@X.io":     "kim@x.io",
		"engineering":  "engineering",
		"*":            "",
		"..":           "",
		"../../etc/x":  "_.._etc_x",
		"a/b":          "a_b",
		"  ":           "",
		"kim+ops@x.io": "kim_ops@x.io",
	} {
		assert.Equal(t, want, Slug(who), who)
	}
	_, err := Open(t.TempDir()).With("..")
	assert.ErrorIs(t, err, ErrNobody)
}

// A write cut short is one lost line, not a lost conversation.
func TestAHalfWrittenLineIsSkipped(t *testing.T) {
	home := t.TempDir()
	log := Open(home)
	require.NoError(t, log.Add("kim@x.io", Entry{ID: "q1", Kind: KindAsk, Question: "a"}))
	path := filepath.Join(home, "bus", "kim@x.io.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = file.WriteString(`{"id":"q2","ki`)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	got, err := log.With("kim@x.io")
	require.NoError(t, err)
	assert.Len(t, got, 1)
}
