package ledger

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// diffOf builds a diff where every line of body is an addition, which is what a
// new file looks like and is enough to anchor against.
func diffOf(path string, lines []string) *gitdiff.Diff {
	out := "diff --git a/" + path + " b/" + path + "\n" +
		"--- /dev/null\n+++ b/" + path + "\n" +
		fmt.Sprintf("@@ -0,0 +1,%d @@\n", len(lines))
	for _, l := range lines {
		out += "+" + l + "\n"
	}
	return gitdiff.FromText("HEAD", "worktree", out)
}

func lines(n int) []string {
	out := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, fmt.Sprintf("line %d", i))
	}
	return out
}

func store(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), ".git"), "feature/one")
	require.NoError(t, err)
	return s
}

func agentLines(file string, nums ...int) map[string][]int {
	return map[string][]int{file: nums}
}

func TestADwellWithNoTickCountsForNothing(t *testing.T) {
	diff := diffOf("a.go", lines(10))
	s := store(t)
	require.NoError(t, s.Note(diff, []Seen{{Span: Span{File: "a.go", Start: 1, Lines: 5}, MS: 4000}}))

	report := s.Count(diff, agentLines("a.go", 1, 2, 3))
	assert.Equal(t, 3, report.Agent)
	assert.Equal(t, 0, report.Read, "the tick is the claim and nothing claimed it")
}

func TestATickWithNoDwellCountsForNothing(t *testing.T) {
	diff := diffOf("a.go", lines(10))
	s := store(t)
	require.NoError(t, s.Tick(diff, Span{File: "a.go", Start: 1, Lines: 5}, true))

	report := s.Count(diff, agentLines("a.go", 1, 2, 3))
	assert.Equal(t, 3, report.Unread, "a tick without the dwell behind it is a claim with no evidence")
}

func TestADwellAndATickTogetherAreRead(t *testing.T) {
	diff := diffOf("a.go", lines(10))
	s := store(t)
	require.NoError(t, s.Note(diff, []Seen{{Span: Span{File: "a.go", Start: 1, Lines: 5}, MS: Read}}))
	require.NoError(t, s.Tick(diff, Span{File: "a.go", Start: 1, Lines: 5}, true))

	report := s.Count(diff, agentLines("a.go", 1, 2, 3, 7, 8))
	assert.Equal(t, 5, report.Agent)
	assert.Equal(t, 3, report.Read, "only the lines under both")
	assert.Equal(t, 2, report.Unread)
}

func TestASkimEarnsNothingAndTwoSkimsStillDoNot(t *testing.T) {
	diff := diffOf("a.go", lines(4))
	s := store(t)
	span := Span{File: "a.go", Start: 1, Lines: 4}
	require.NoError(t, s.Tick(diff, span, true))
	for range 4 {
		require.NoError(t, s.Note(diff, []Seen{{Span: span, MS: Skim - 1}}))
	}
	assert.Equal(t, 4, s.Count(diff, agentLines("a.go", 1, 2, 3, 4)).Unread)
}

func TestDwellAccumulatesUpToTheCap(t *testing.T) {
	diff := diffOf("a.go", lines(4))
	s := store(t)
	span := Span{File: "a.go", Start: 1, Lines: 4}
	require.NoError(t, s.Tick(diff, span, true))

	require.NoError(t, s.Note(diff, []Seen{{Span: span, MS: 500}}))
	assert.Equal(t, 1, s.Count(diff, agentLines("a.go", 1)).Unread, "half a second is not read yet")

	require.NoError(t, s.Note(diff, []Seen{{Span: span, MS: 500}}))
	assert.Equal(t, 1, s.Count(diff, agentLines("a.go", 1)).Read)

	for range 10 {
		require.NoError(t, s.Note(diff, []Seen{{Span: span, MS: 5000}}))
	}
	s.mu.Lock()
	most := 0
	for _, r := range s.records {
		most = max(most, r.MS)
	}
	s.mu.Unlock()
	assert.Equal(t, Cap, most, "one line cannot earn more than the cap")
}

func TestAScrolledDiffDoesNotKeepAddingRecords(t *testing.T) {
	diff := diffOf("a.go", lines(10))
	s := store(t)
	// The same lines, reported from one viewport position and then another.
	for start := range 8 {
		require.NoError(t, s.Note(diff, []Seen{{Span: Span{File: "a.go", Start: start + 1, Lines: 3}, MS: Read}}))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	assert.LessOrEqual(t, len(s.records), 10, "a line is one record however often it is reported")
}

func TestASpanLongerThanAnyDiffIsRefused(t *testing.T) {
	diff := diffOf("a.go", lines(4))
	s := store(t)
	huge := Span{File: "a.go", Start: 1, Lines: 1_000_000_000}
	require.NoError(t, s.Note(diff, []Seen{{Span: huge, MS: Read}}))
	require.NoError(t, s.Tick(diff, huge, true))

	s.mu.Lock()
	defer s.mu.Unlock()
	assert.Empty(t, s.records, "a line count no viewport could hold is not a report")
}

func TestADropTakesTheWholeLedger(t *testing.T) {
	gitDir := filepath.Join(t.TempDir(), ".git")
	diff := diffOf("a.go", lines(4))
	s, err := Open(gitDir, "main")
	require.NoError(t, err)
	span := Span{File: "a.go", Start: 1, Lines: 4}
	require.NoError(t, s.Note(diff, []Seen{{Span: span, MS: Read}}))
	require.NoError(t, s.Tick(diff, span, true))

	require.NoError(t, s.Drop())
	assert.Equal(t, 4, s.Count(diff, agentLines("a.go", 1, 2, 3, 4)).Unread)
	_, statErr := os.Stat(Path(gitDir, "main"))
	assert.True(t, os.IsNotExist(statErr), "the file goes with it")
}

func TestUntickingTakesTheClaimBack(t *testing.T) {
	diff := diffOf("a.go", lines(4))
	s := store(t)
	span := Span{File: "a.go", Start: 1, Lines: 4}
	require.NoError(t, s.Note(diff, []Seen{{Span: span, MS: Read}}))
	require.NoError(t, s.Tick(diff, span, true))
	require.Equal(t, 1, s.Count(diff, agentLines("a.go", 2)).Read)

	require.NoError(t, s.Tick(diff, span, false))
	assert.Equal(t, 1, s.Count(diff, agentLines("a.go", 2)).Unread)
}

func TestARewrittenLineLosesItsReadMark(t *testing.T) {
	before := diffOf("a.go", lines(6))
	s := store(t)
	span := Span{File: "a.go", Start: 1, Lines: 6}
	require.NoError(t, s.Note(before, []Seen{{Span: span, MS: Read}}))
	require.NoError(t, s.Tick(before, span, true))
	require.Equal(t, 6, s.Count(before, agentLines("a.go", 1, 2, 3, 4, 5, 6)).Read)

	// Line 4, in the middle of what was read, and not the line the span
	// started on: the mark goes with it and the rest of the span stands.
	rewritten := lines(6)
	rewritten[3] = "the agent wrote this again"
	after := diffOf("a.go", rewritten)
	report := s.Count(after, agentLines("a.go", 1, 2, 3, 4, 5, 6))
	assert.Equal(t, 3, report.Unread, "the rewritten line and the two it anchors")
	assert.Equal(t, 3, report.Read)
}

func TestAnEditAboveAReadSpanDoesNotMoveItsMarks(t *testing.T) {
	before := diffOf("a.go", lines(8))
	s := store(t)
	span := Span{File: "a.go", Start: 5, Lines: 3}
	require.NoError(t, s.Note(before, []Seen{{Span: span, MS: Read}}))
	require.NoError(t, s.Tick(before, span, true))

	after := diffOf("a.go", append([]string{"new line on top"}, lines(8)...))
	report := s.Count(after, agentLines("a.go", 6, 7, 8))
	assert.Equal(t, 3, report.Read, "the span followed its content down one line")
}

func TestAFileIsViewedWhenEveryChangedLineIsTicked(t *testing.T) {
	diff := diffOf("a.go", lines(4))
	s := store(t)
	require.NoError(t, s.Tick(diff, Span{File: "a.go", Start: 1, Lines: 3}, true))
	require.False(t, s.Count(diff, nil).Files[0].Viewed)

	require.NoError(t, s.Tick(diff, Span{File: "a.go", Start: 4, Lines: 1}, true))
	file := s.Count(diff, nil).Files[0]
	assert.True(t, file.Viewed)
	assert.Equal(t, []Span{{File: "a.go", Start: 1, Lines: 4}}, file.Ticked, "adjacent ticks read as one run")
}

func TestCountAnswersForEveryFileInTheDiff(t *testing.T) {
	diff := gitdiff.FromText("HEAD", "worktree",
		"diff --git a/a.go b/a.go\n--- /dev/null\n+++ b/a.go\n@@ -0,0 +1,2 @@\n+one\n+two\n"+
			"diff --git a/b.go b/b.go\n--- /dev/null\n+++ b/b.go\n@@ -0,0 +1,2 @@\n+three\n+four\n")
	s := store(t)
	report := s.Count(diff, map[string][]int{"a.go": {1, 2}, "b.go": {1}})

	require.Len(t, report.Files, 2)
	assert.Equal(t, 3, report.Agent)
	assert.Equal(t, 3, report.Unread)
	assert.Equal(t, "a.go", report.Files[0].Path)
	assert.Equal(t, 2, report.Files[0].Unread)
}

func TestTheLedgerSurvivesAReopen(t *testing.T) {
	gitDir := filepath.Join(t.TempDir(), ".git")
	diff := diffOf("a.go", lines(4))
	s, err := Open(gitDir, "feature/one")
	require.NoError(t, err)
	span := Span{File: "a.go", Start: 1, Lines: 4}
	require.NoError(t, s.Note(diff, []Seen{{Span: span, MS: Read}}))
	require.NoError(t, s.Tick(diff, span, true))

	again, err := Open(gitDir, "feature/one")
	require.NoError(t, err)
	assert.Equal(t, 4, again.Count(diff, agentLines("a.go", 1, 2, 3, 4)).Read)

	other, err := Open(gitDir, "feature/two")
	require.NoError(t, err)
	assert.Equal(t, 4, other.Count(diff, agentLines("a.go", 1, 2, 3, 4)).Unread,
		"the ledger is branch-local")
}

func TestPruneDropsWhatTheDiffCannotPlace(t *testing.T) {
	gitDir := filepath.Join(t.TempDir(), ".git")
	diff := diffOf("a.go", lines(4))
	s, err := Open(gitDir, "main")
	require.NoError(t, err)
	require.NoError(t, s.Note(diff, []Seen{{Span: Span{File: "a.go", Start: 1, Lines: 4}, MS: Read}}))

	require.NoError(t, s.Prune(diffOf("b.go", lines(4))))
	data, err := os.ReadFile(Path(gitDir, "main"))
	require.NoError(t, err)
	assert.Equal(t, "[]", string(data))
}

func TestNothingIsStoredForASpanTheDiffDoesNotHave(t *testing.T) {
	diff := diffOf("a.go", lines(4))
	s := store(t)
	require.NoError(t, s.Note(diff, []Seen{{Span: Span{File: "gone.go", Start: 1, Lines: 2}, MS: Read}}))
	require.NoError(t, s.Tick(diff, Span{File: "gone.go", Start: 1, Lines: 2}, true))

	_, err := os.Stat(s.path)
	assert.True(t, os.IsNotExist(err), "an unplaceable report writes no file")
}

func TestANilStoreAnswersEmpty(t *testing.T) {
	var s *Store
	assert.NoError(t, s.Note(diffOf("a.go", lines(2)), []Seen{{Span: Span{File: "a.go", Start: 1, Lines: 1}, MS: Read}}))
	assert.NoError(t, s.Tick(nil, Span{File: "a.go", Start: 1, Lines: 1}, true))
	assert.NoError(t, s.Prune(nil))
	unread, total := s.Unread(diffOf("a.go", lines(2)), agentLines("a.go", 1))
	assert.Equal(t, 1, unread)
	assert.Equal(t, 1, total)
}
