package ratchet

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const root = "../.."

// record writes the counts the walk found instead of checking them:
// `go test ./internal/ratchet -record` is how a count that fell is banked.
var record = flag.Bool("record", false, "write today's counts into "+File)

func TestTheCountsOnlyEverFall(t *testing.T) {
	now, err := Measure(root)
	require.NoError(t, err)
	if *record {
		require.NoError(t, Record(filepath.Join(root, File), now))
		t.Logf("recorded %s", File)
		return
	}
	recorded, err := Load(filepath.Join(root, File))
	require.NoError(t, err)

	for _, name := range Names(recorded) {
		was, is := recorded[name], now[name]
		assert.LessOrEqualf(t, is.Count, was.Count,
			"%s rose from %d to %d. The worst is %s — bring it down rather than raising %s.",
			name, was.Count, is.Count, is.Worst, File)
		// A fall passes: the design says the test lets one fall. Recording it
		// is how the ceiling follows, and `-record` is how.
		if is.Count < was.Count {
			t.Logf("%s fell from %d to %d: run with -record so it cannot go back up.",
				name, was.Count, is.Count)
		}
	}
	for _, name := range Names(now) {
		_, ok := recorded[name]
		assert.Truef(t, ok, "%s is counted but not recorded in %s", name, File)
	}
}

func TestAGoroutineCountsUntilItDefersARecover(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "bare.go", `package p
func f() { go func() { work() }() }
func work() {}`)
	write(t, dir, "held.go", `package p
func g() {
	go func() {
		defer func() { _ = recover() }()
	}()
}`)
	write(t, dir, "named.go", `package p
func h() { go work() }`)

	counts, err := Measure(dir)
	require.NoError(t, err)
	assert.Equal(t, 1, counts[Unrecovered].Count, "only the one with no recover counts")
	assert.Equal(t, "bare.go", counts[Unrecovered].Worst)
}

func TestNolintAndLongFilesAreCountedWithTheirWorstFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "one.go", "package p\n\n//nolint:gosec\nvar a = 1\n")
	write(t, dir, "two.go", "package p\n\n//nolint:gosec\n//nolint:errcheck\nvar b = 1\n")
	long := "package p\n"
	for range longFile + 1 {
		long += "// a line\n"
	}
	write(t, dir, "big.go", long)

	counts, err := Measure(dir)
	require.NoError(t, err)
	assert.Equal(t, 3, counts[Nolint].Count)
	assert.Equal(t, "two.go", counts[Nolint].Worst)
	assert.Equal(t, 1, counts[LongFiles].Count)
	assert.Equal(t, "big.go", counts[LongFiles].Worst)
}

func TestATestFileIsNotCounted(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "p_test.go", "package p\n\n//nolint:gosec\nvar a = 1\n")
	counts, err := Measure(dir)
	require.NoError(t, err)
	assert.Equal(t, 0, counts[Nolint].Count)
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
}
