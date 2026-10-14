package web

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/review"
)

// report hands a finding to a batch, as a reviewer's filer does. Files nothing.
func report(t *testing.T, s *Server, batch, by string, f review.Finding) string {
	t.Helper()
	require.True(t, s.recordFound(batch, by, []review.Finding{f}))
	b := s.batches[batch]
	require.NotNil(t, b)
	return b.order[len(b.order)-1]
}

func finding(line int, summary string) review.Finding {
	return review.Finding{File: "a.go", Line: line, Summary: summary, Category: "correctness"}
}

// findingIn names the file, for a test that needs two separate threads: the
// fixture diff changes one line of a.go, so everything on it snaps together.
func findingIn(file string, summary string) review.Finding {
	return review.Finding{File: file, Line: 3, Summary: summary, Category: "correctness"}
}

func on(t *testing.T, s *Server, file string) comments.Thread {
	t.Helper()
	for _, got := range s.space().store.List() {
		if got.File == file {
			return got
		}
	}
	t.Fatalf("nothing filed on %s", file)
	return comments.Thread{}
}

func noneOn(t *testing.T, s *Server, file string) {
	t.Helper()
	for _, got := range s.space().store.List() {
		if got.File == file {
			t.Fatalf("%s was filed and should not have been", file)
		}
	}
}

func TestABatchHoldsEveryReviewersFindingsUnfiled(t *testing.T) {
	s := newTestServer(t)
	batch := s.newBatch([]string{"alice", "bob"})

	report(t, s, batch, "alice", finding(3, "zebra"))
	require.True(t, s.recordFound(batch, "bob", []review.Finding{finding(3, "apple"), finding(3, "mango")}))

	b := s.batches[batch]
	require.NotNil(t, b)
	require.Len(t, b.order, 3)
	assert.Equal(t, "alice", b.found[b.order[0]].by)
	assert.Equal(t, "bob", b.found[b.order[2]].by)
	assert.Empty(t, s.space().store.List(), "nothing reaches the diff until the batch settles")

	// The same finding twice from one reviewer does not grow the batch.
	require.True(t, s.recordFound(batch, "bob", []review.Finding{finding(3, "apple")}))
	assert.Len(t, s.batches[batch].order, 3)

	s.dropBatch(batch)
	assert.Nil(t, s.batches[batch])
}

// The point of the whole feature: three reviewers, one bug, one comment.
func TestThreeReviewersOnOnePointBecomeOneComment(t *testing.T) {
	s := newTestServer(t)
	batch := s.newBatch([]string{"alice", "bob", "carol"})
	report(t, s, batch, "alice", review.Finding{File: "a.go", Line: 3, Summary: "the nil check is the wrong way round", Category: "correctness", Severity: "medium"})
	report(t, s, batch, "bob", review.Finding{File: "a.go", Line: 5, Summary: "deref before the check", Category: "security", Severity: "high"})
	report(t, s, batch, "carol", review.Finding{File: "a.go", Line: 4, Summary: "nil is possible here", Category: "correctness", Severity: "low"})

	s.settleBatch(s.batches[batch])

	require.Len(t, s.space().store.List(), 1, "one point, one thread")
	got := on(t, s, "a.go")
	assert.Equal(t, "high", got.Severity, "the point is as bad as the worst thing said about it")
	assert.Contains(t, got.Comments[0].Body, "Found by @alice · @bob · @carol.")
	assert.Len(t, got.Comments, 3, "the other two wordings are replies, not rows on the diff")

	notes := messagesOfKind(s.conversation(), chat.KindNote)
	require.NotEmpty(t, notes)
	assert.Equal(t, "3 finding(s) from 3 reviewer(s) → 1 point(s).", notes[len(notes)-1].Body)
}

func TestFindingsBeyondTheWindowStayApart(t *testing.T) {
	s := newTestServer(t)
	batch := s.newBatch([]string{"alice"})
	report(t, s, batch, "alice", review.Finding{File: "far.go", Line: 3, Summary: "one", Category: "correctness"})
	report(t, s, batch, "alice", review.Finding{File: "far.go", Line: 300, Summary: "two", Category: "correctness"})

	points := cluster(s.batches[batch], map[string]*judgement{})
	assert.Len(t, points, 2)
}

func TestAClusterDoesNotWalkDownTheFile(t *testing.T) {
	s := newTestServer(t)
	batch := s.newBatch([]string{"alice"})
	for _, line := range []int{3, 12, 21, 30} {
		report(t, s, batch, "alice", review.Finding{File: "walk.go", Line: line, Summary: "x", Category: "correctness"})
	}

	// A window measured from the last line would have made all four one.
	points := cluster(s.batches[batch], map[string]*judgement{})
	assert.Len(t, points, 2, "measured from the first line of a cluster, not the last")
}

func TestSettleBatchFlagsADisputeAndFilesTheObjectionAsAReply(t *testing.T) {
	s := newTestServer(t)
	batch := s.newBatch([]string{"alice", "bob", "carol"})
	key := report(t, s, batch, "alice", finding(3, "the nil check is the wrong way round"))

	b := s.batches[batch]
	b.verdicts = []rebuttalVerdict{{by: "bob", Verdict: review.Verdict{
		Ref: key, Verdict: review.VerdictDispute, Reason: "the check is above the deref",
	}}}
	s.settleBatch(b)

	got := on(t, s, "a.go")
	assert.True(t, got.Disputed)
	assert.False(t, got.Withdrawn, "carol said nothing, so it is not withdrawn")
	assert.Empty(t, got.DuplicateOf)
	require.Len(t, got.Comments, 2)
	assert.Equal(t, "bob", got.Comments[1].Author)
	assert.Equal(t, "the check is above the deref", got.Comments[1].Body)

	notes := messagesOfKind(s.conversation(), chat.KindNote)
	require.NotEmpty(t, notes)
	assert.Contains(t, notes[len(notes)-1].Body, "1 disputed: the objection is a reply on the comment.")
}

// The change in one line: the round decides what exists, not what is annotated.
func TestSettleBatchNeverFilesWhatEveryOtherReviewerDisputes(t *testing.T) {
	s := newTestServer(t)
	batch := s.newBatch([]string{"alice", "bob", "carol"})
	strike := report(t, s, batch, "alice", findingIn("struck.go", "struck"))
	defend := report(t, s, batch, "alice", findingIn("defended.go", "defended"))

	b := s.batches[batch]
	b.verdicts = []rebuttalVerdict{
		{by: "bob", Verdict: review.Verdict{Ref: strike, Verdict: review.VerdictDispute, Reason: "no"}},
		{by: "carol", Verdict: review.Verdict{Ref: strike, Verdict: review.VerdictDispute, Reason: "also no"}},
		{by: "bob", Verdict: review.Verdict{Ref: defend, Verdict: review.VerdictDispute, Reason: "wrong"}},
		{by: "carol", Verdict: review.Verdict{Ref: defend, Verdict: review.VerdictAgree}},
	}
	s.settleBatch(b)

	noneOn(t, s, "struck.go")
	got := on(t, s, "defended.go")
	assert.True(t, got.Disputed)
	assert.False(t, got.Withdrawn, "one reviewer defended it")

	notes := messagesOfKind(s.conversation(), chat.KindNote)
	body := notes[len(notes)-1].Body
	assert.Contains(t, body, "2 finding(s) from 3 reviewer(s) → 1 point(s).")
	assert.Contains(t, body, "1 dropped: one reviewer, objected by the others.")
	assert.Contains(t, body, "objected by @bob · @carol")
}

// Unanimity was the old bar: one "agree" kept what two had shot down.
func TestSettleBatchDropsWhenObjectionsOutnumberDefences(t *testing.T) {
	s := newTestServer(t)
	batch := s.newBatch([]string{"alice", "bob", "carol", "dave"})
	key := report(t, s, batch, "alice", findingIn("x.go", "outvoted"))

	b := s.batches[batch]
	b.verdicts = []rebuttalVerdict{
		{by: "bob", Verdict: review.Verdict{Ref: key, Verdict: review.VerdictDispute, Reason: "no"}},
		{by: "carol", Verdict: review.Verdict{Ref: key, Verdict: review.VerdictDispute, Reason: "also no"}},
		{by: "dave", Verdict: review.Verdict{Ref: key, Verdict: review.VerdictAgree}},
	}
	s.settleBatch(b)

	noneOn(t, s, "x.go")
}

// Silence is not a judgement: one objection among three is worth reading.
func TestSettleBatchDoesNotDropOnASingleObjection(t *testing.T) {
	s := newTestServer(t)
	batch := s.newBatch([]string{"alice", "bob", "carol", "dave"})
	key := report(t, s, batch, "alice", findingIn("y.go", "one objection"))

	b := s.batches[batch]
	b.verdicts = []rebuttalVerdict{
		{by: "bob", Verdict: review.Verdict{Ref: key, Verdict: review.VerdictDispute, Reason: "no"}},
	}
	s.settleBatch(b)

	got := on(t, s, "y.go")
	assert.True(t, got.Disputed)
	assert.False(t, got.Withdrawn, "two reviewers said nothing at all")
}

// A reviewer objecting to a point it reported itself is not a judgement of it.
func TestAReviewerDoesNotJudgeItsOwnPoint(t *testing.T) {
	s := newTestServer(t)
	batch := s.newBatch([]string{"alice", "bob"})
	key := report(t, s, batch, "alice", findingIn("own.go", "mine"))

	b := s.batches[batch]
	b.verdicts = []rebuttalVerdict{
		{by: "alice", Verdict: review.Verdict{Ref: key, Verdict: review.VerdictDispute, Reason: "?"}},
	}
	s.settleBatch(b)

	assert.False(t, on(t, s, "own.go").Disputed)
}

// Within one file a named duplicate joins, however far apart the lines are.
func TestADuplicateVerdictJoinsTwoFindingsOfOneFile(t *testing.T) {
	s := newTestServer(t)
	batch := s.newBatch([]string{"alice", "bob"})
	first := report(t, s, batch, "alice", review.Finding{File: "dup.go", Line: 3, Summary: "one", Category: "correctness"})
	later := report(t, s, batch, "bob", review.Finding{File: "dup.go", Line: 400, Summary: "two", Category: "design"})

	b := s.batches[batch]
	b.verdicts = []rebuttalVerdict{{by: "alice", Verdict: review.Verdict{
		Ref: later, Verdict: review.VerdictDuplicate, Of: first,
	}}}
	points := cluster(b, judgements(b))

	require.Len(t, points, 1, "a reviewer saying so beats the distance between the lines")
	assert.Equal(t, []string{"alice", "bob"}, points[0].by)
}

func TestSettleBatchSaysWhatItFiledEvenWhenNoOneObjected(t *testing.T) {
	s := newTestServer(t)
	batch := s.newBatch([]string{"alice", "bob"})
	key := report(t, s, batch, "alice", finding(3, "stands"))

	b := s.batches[batch]
	b.verdicts = []rebuttalVerdict{
		{by: "bob", Verdict: review.Verdict{Ref: key, Verdict: review.VerdictAgree}},
		{by: "bob", Verdict: review.Verdict{Ref: "never reported", Verdict: review.VerdictDispute, Reason: "?"}},
	}
	s.settleBatch(b)

	got := on(t, s, "a.go")
	assert.False(t, got.Disputed)
	assert.False(t, got.Withdrawn)
	assert.Len(t, got.Comments, 1)

	notes := messagesOfKind(s.conversation(), chat.KindNote)
	assert.Equal(t, "1 finding(s) from 2 reviewer(s) → 1 point(s).", notes[len(notes)-1].Body)
}

func TestABatchDrainsIntoOneRebuttalRoundAndThenSettles(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	s.agent.bin = filepath.Join(t.TempDir(), "no-such-claude")
	defineUserAgent(t, "alice", "---\nname: alice\n---\nread\n")
	defineUserAgent(t, "bob", "---\nname: bob\n---\nread\n")

	batch := s.newBatch([]string{"alice", "bob"})
	report(t, s, batch, "alice", finding(3, "the nil check is the wrong way round"))

	// The reviewers are done: the round starts. Only bob runs — alice is not
	// shown her own finding, and has nothing else to judge.
	s.batchDrained(batch)
	runs := s.sideRuns()
	require.Len(t, runs, 1)
	assert.Equal(t, "bob", runs[0].Name)

	// It fails without claude, drains, and settles. Once: the second drain
	// takes the batch away rather than starting another round.
	assert.Eventually(t, func() bool { return len(s.sideRuns()) == 0 }, 5e9, 1e7)
	assert.Eventually(t, func() bool {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return s.batches[batch] == nil && s.held[batch] == 0
	}, 5e9, 1e7)
	assert.Empty(t, s.sideRuns())
	assert.Len(t, s.space().store.List(), 1, "and the held finding is filed on the way out")
}

func TestABatchWithNothingFoundDoesNotRunARound(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	defineUserAgent(t, "alice", "---\nname: alice\n---\nread\n")

	batch := s.newBatch([]string{"alice"})
	s.batchDrained(batch)

	assert.Empty(t, s.sideRuns(), "there is nothing to judge")
	s.mu.RLock()
	defer s.mu.RUnlock()
	assert.Nil(t, s.batches[batch], "and the batch is not left waiting for a run that will not come")
}

func TestAReviewCostsOneSlotPerReviewerAndHoldsNothing(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	s.agent.bin = filepath.Join(t.TempDir(), "no-such-claude")
	defineUserAgent(t, "commenter", "---\nname: commenter\n---\nfile them\n")
	for _, name := range []string{"alice", "bob", "carol"} {
		defineUserAgent(t, name, "---\nname: "+name+"\n---\nread\n")
	}

	// Three at once: reviewers hold their own findings, so there is no
	// @commenter slot to keep back and a review costs one slot each. Holding
	// two per reviewer is what refused this against a cap of four.
	rec := do(t, s, "POST", "/api/review", `{"agents":["alice","bob","carol"]}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	s.mu.RLock()
	held := s.heldSlots()
	s.mu.RUnlock()
	assert.Zero(t, held, "nothing is held for a hand-off that no longer happens")
}

// The pass joins what distance cannot: two readings of one bug, far apart.
func TestTheClusteringPassJoinsWhatThePositionalPassCouldNot(t *testing.T) {
	s := newTestServer(t)
	batch := s.newBatch([]string{"alice", "bob"})
	first := report(t, s, batch, "alice", review.Finding{File: "far.go", Line: 3, Summary: "one", Category: "correctness"})
	later := report(t, s, batch, "bob", review.Finding{File: "far.go", Line: 400, Summary: "two", Category: "design"})

	b := s.batches[batch]
	b.groups = [][]string{{first, later}}

	points := cluster(b, map[string]*judgement{})
	require.Len(t, points, 1)
	assert.Equal(t, []string{"alice", "bob"}, points[0].by)
}

func TestOnlyACrowdedFileIsWorthComparing(t *testing.T) {
	s := newTestServer(t)
	batch := s.newBatch([]string{"alice", "bob"})
	report(t, s, batch, "alice", review.Finding{File: "alone.go", Line: 3, Summary: "one"})
	report(t, s, batch, "alice", review.Finding{File: "crowded.go", Line: 3, Summary: "two"})
	report(t, s, batch, "bob", review.Finding{File: "crowded.go", Line: 9, Summary: "three"})

	assert.Equal(t, []string{"crowded.go"}, crowdedFiles(s.batches[batch]))
}

func TestABatchWithNoCrowdedFileSkipsTheClusteringPass(t *testing.T) {
	s := newTestServer(t)
	batch := s.newBatch([]string{"alice"})
	report(t, s, batch, "alice", finding(3, "alone"))

	assert.False(t, s.startClustering(batch, s.batches[batch]), "nothing to compare")
	assert.Empty(t, s.sideRuns())
}

// The clustering pass failing is a worse grouping, not a lost review.
func TestAFailedComparisonStillFilesTheFindings(t *testing.T) {
	s := newTestServer(t)
	batch := s.newBatch([]string{"alice", "bob"})
	report(t, s, batch, "alice", review.Finding{File: "a.go", Line: 3, Summary: "one"})

	b := s.batches[batch]
	b.unread = 2
	s.settleBatch(b)

	assert.Len(t, s.space().store.List(), 1)
	notes := messagesOfKind(s.conversation(), chat.KindNote)
	assert.Contains(t, notes[len(notes)-1].Body, "2 file(s) grouped by position only")
}

// Three phases through one mechanism: reviewers, the round, the comparison.
func TestEachPhaseWithNothingToRunIsSkipped(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	defineUserAgent(t, "alice", "---\nname: alice\n---\nread\n")

	batch := s.newBatch([]string{"alice"})
	report(t, s, batch, "alice", finding(3, "alone"))
	s.batchDrained(batch)

	assert.Empty(t, s.sideRuns(), "alice judges nothing of her own and has one file")
	s.mu.RLock()
	defer s.mu.RUnlock()
	assert.Nil(t, s.batches[batch], "it settled rather than waiting on a phase with no work")
}
