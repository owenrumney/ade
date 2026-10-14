package transcripts

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rewrite replaces a transcript's contents and puts its size and modification
// time back, so the only way to tell the two apart is to read the file. What a
// second scan reports says whether it did.
func rewrite(t *testing.T, path, content string) {
	t.Helper()
	before, err := os.Stat(path)
	require.NoError(t, err)
	require.Len(t, content, int(before.Size()), "the stand-in has to be the same size")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	require.NoError(t, os.Chtimes(path, before.ModTime(), before.ModTime()))
}

func TestASecondScanReusesWhatItAlreadyRead(t *testing.T) {
	home := fakeHome(t)
	path := filepath.Join(home, ".claude", "projects", "-work-coast", "aaaa-1111.jsonl")

	first := Scan()
	require.NotEmpty(t, first)

	line := `{"type":"user","timestamp":"2026-08-01T10:00:00Z","cwd":"/work/coast","message":{"role":"user","content":"something else entirely"}}`
	rewrite(t, path, line+"\n"+padTo(len(claudeFixture)-len(line)-1))

	for _, s := range Scan() {
		if s.Path == path {
			assert.Equal(t, "fix the flaky pricing test", s.FirstPrompt, "an unchanged file should not be read twice")
			return
		}
	}
	t.Fatal("the coast session went missing from the second scan")
}

func TestAnAppendedTranscriptIsReadAgain(t *testing.T) {
	home := fakeHome(t)
	path := filepath.Join(home, ".claude", "projects", "-work-coast", "bbbb-2222.jsonl")
	writeFixture(t, path, `{"type":"user","timestamp":"2026-08-01T10:00:00Z","cwd":"/work/coast","message":{"role":"user","content":"first go"}}`+"\n")

	before := Statistics(Scan())
	writeFixture(t, path, `{"type":"user","timestamp":"2026-08-01T10:00:00Z","cwd":"/work/coast","message":{"role":"user","content":"first go"}}
{"type":"assistant","timestamp":"2026-08-01T11:00:00Z","requestId":"r9","message":{"id":"m9","role":"assistant","model":"claude-opus-5","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":10,"output_tokens":20}}}
`)

	after := Statistics(Scan())
	assert.Equal(t, before.Requests+1, after.Requests, "a transcript that grew has to be parsed again")
	assert.Greater(t, after.Cost, before.Cost)
}

func TestTheCacheIsWrittenWhereAdeKeepsItsState(t *testing.T) {
	home := fakeHome(t)
	Statistics(Scan())

	for _, name := range []string{"index.json", "summaries.json"} {
		_, err := os.Stat(filepath.Join(home, ".ade", "cache", "transcripts", name))
		assert.NoError(t, err, "%s should have been written", name)
	}
}

func TestAnEntryFromAnotherVersionIsIgnored(t *testing.T) {
	home := fakeHome(t)
	Scan()
	path := filepath.Join(home, ".ade", "cache", "transcripts", "index.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"version":0,"entries":{"/gone":{}}}`), 0o600))

	// A fresh HOME is what makes the cache read the file again.
	t.Setenv("HOME", home)
	assert.NotEmpty(t, Scan(), "a cache this build cannot read is discarded, not fatal")
}

func TestStatisticsCountsWhenTheRequestsWereMade(t *testing.T) {
	fakeHome(t)
	stats := Statistics(Scan())

	require.Len(t, stats.ByHour, 24, "a dial needs the quiet hours as well")
	require.Len(t, stats.ByWeekday, 7)
	assert.Equal(t, "00", stats.ByHour[0].Key)
	assert.Equal(t, []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}, keysOf(stats.ByWeekday))

	hours, weekdays := 0, 0
	for _, group := range stats.ByHour {
		hours += group.Sessions
	}
	for _, group := range stats.ByWeekday {
		weekdays += group.Sessions
	}
	assert.Positive(t, stats.Requests)
	assert.Equal(t, stats.Requests, hours, "every request falls in exactly one hour")
	assert.Equal(t, stats.Requests, weekdays, "every request falls in exactly one weekday")
}

func TestStampChangesWithTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	require.NoError(t, os.WriteFile(path, []byte("one\n"), 0o600))
	first, ok := stampOf(path)
	require.True(t, ok)

	require.NoError(t, os.WriteFile(path, []byte("one\ntwo\n"), 0o600))
	require.NoError(t, os.Chtimes(path, time.Unix(first.Mod, 0), time.Unix(first.Mod, 0)))
	second, ok := stampOf(path)
	require.True(t, ok)
	assert.NotEqual(t, first, second, "a file that grew within the same second still differs")

	_, ok = stampOf(filepath.Join(t.TempDir(), "never.jsonl"))
	assert.False(t, ok, "a file that is not there has no stamp to cache against")
}

func keysOf(groups []Group) []string {
	out := make([]string, 0, len(groups))
	for _, group := range groups {
		out = append(out, group.Key)
	}
	return out
}

// padTo is filler that parses as nothing, so a stand-in transcript can be made
// the same length as the one it replaces.
func padTo(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, n)
	for i := range out {
		out[i] = ' '
	}
	out[n-1] = '\n'
	return string(out)
}
