package overlap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/gitdiff"
)

// node spells a worktree as "sha:patch" pairs, which is all the arithmetic
// reads. A pair with no patch is a merge.
func node(path string, commits ...string) Node {
	n := Node{Commits: []gitdiff.Rev{}}
	n.Path, n.Name, n.Branch = path, path, path
	for _, one := range commits {
		sha, patch := one, ""
		for i := 0; i < len(one); i++ {
			if one[i] == ':' {
				sha, patch = one[:i], one[i+1:]
				break
			}
		}
		n.Commits = append(n.Commits, gitdiff.Rev{SHA: sha, PatchID: patch})
	}
	n.Redundant = len(n.Commits) == 0
	return n
}

func kinds(edges []Edge, kind string) []Edge {
	out := []Edge{}
	for _, e := range edges {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func TestNothingRelatesTwoUnrelatedWorktrees(t *testing.T) {
	got := Relate([]Node{node("a", "1:p1"), node("b", "2:p2")})
	assert.Empty(t, got)
}

func TestSharedCommitsAreCountedOnce(t *testing.T) {
	got := Relate([]Node{
		node("a", "1:p1", "2:p2", "3:p3"),
		node("b", "1:p1", "2:p2", "9:p9"),
	})

	shares := kinds(got, KindShares)
	require.Len(t, shares, 1, "a pair earns one shares edge, not one each way")
	assert.Equal(t, 2, shares[0].Count)
	assert.Empty(t, kinds(got, KindDuplicates), "shared history is not duplicated work")
}

// The headline case: the same patch on two branches under two SHAs.
func TestACherryPickIsDuplicatedWorkAndNotSharedHistory(t *testing.T) {
	got := Relate([]Node{
		node("a", "1:p1", "2:p2"),
		node("b", "7:p1", "8:p8"),
	})

	dupes := kinds(got, KindDuplicates)
	require.Len(t, dupes, 1)
	assert.Equal(t, 1, dupes[0].Count)
	assert.Empty(t, kinds(got, KindShares), "no SHA is on both")
}

// A patch held twice on one branch is that branch's business. The edge is
// about work in two places, and the shared commit is already the shares edge.
func TestAPatchRepeatedWithinOneBranchIsNotCrossBranchDuplication(t *testing.T) {
	got := Relate([]Node{
		node("a", "1:p1", "2:p2"),
		node("b", "1:p1", "7:p1"),
	})

	require.Len(t, kinds(got, KindShares), 1)
	assert.Equal(t, 1, kinds(got, KindShares)[0].Count)
	assert.Empty(t, kinds(got, KindDuplicates),
		"p1 is in common once, and that once is the shared commit")
}

func TestABranchAheadOnTheSameLineAbsorbsTheOneBehindIt(t *testing.T) {
	got := Relate([]Node{
		node("behind", "1:p1"),
		node("ahead", "1:p1", "2:p2"),
	})

	absorbed := kinds(got, KindAbsorbed)
	require.Len(t, absorbed, 1, "only the one behind is absorbed")
	assert.Equal(t, "behind", absorbed[0].From)
	assert.Equal(t, "ahead", absorbed[0].To)
}

func TestAbsorbedFollowsThePatchSoARebaseStillCounts(t *testing.T) {
	got := Relate([]Node{
		node("rebased", "9:p1"),
		node("original", "1:p1", "2:p2"),
	})

	absorbed := kinds(got, KindAbsorbed)
	require.Len(t, absorbed, 1)
	assert.Equal(t, "rebased", absorbed[0].From)
}

// Two worktrees holding exactly the same patches absorb each other. Removing
// both would lose the work, so one of the pair has to survive.
func TestTwoIdenticalWorktreesOnlyAbsorbOneWay(t *testing.T) {
	nodes := []Node{node("a", "1:p1"), node("b", "7:p1")}
	got := Relate(nodes)

	absorbed := kinds(got, KindAbsorbed)
	require.Len(t, absorbed, 1)
	assert.Equal(t, "b", absorbed[0].From, "the tie goes to the earlier path")

	left := Removable(Graph{Nodes: nodes, Edges: got})
	assert.Equal(t, []string{"b"}, left)
}

func TestAWorktreeWithNothingOfItsOwnIsNotAbsorbedByEverything(t *testing.T) {
	nodes := []Node{node("empty"), node("a", "1:p1"), node("b", "2:p2")}
	got := Relate(nodes)

	assert.Empty(t, kinds(got, KindAbsorbed), "redundant already says it")
	assert.Equal(t, []string{"empty"}, Removable(Graph{Nodes: nodes, Edges: got}))
}

func TestTheMainCheckoutIsNeverRedundantAndNeverAbsorbed(t *testing.T) {
	main := node("main")
	main.Main = true
	main.Redundant = false
	nodes := []Node{main, node("a", "1:p1")}

	got := Relate(nodes)
	assert.Empty(t, kinds(got, KindAbsorbed))
	assert.Empty(t, Removable(Graph{Nodes: nodes, Edges: got}))
}

// A merge has no patch-id, so it cannot be the thing that makes a worktree
// look absorbed: its diff was never read.
func TestAMergeCountsAsSharedHistoryAndNeverAsAPatch(t *testing.T) {
	got := Relate([]Node{
		node("a", "1:", "2:p2"),
		node("b", "1:", "3:p3"),
	})

	require.Len(t, kinds(got, KindShares), 1)
	assert.Equal(t, 1, kinds(got, KindShares)[0].Count)
	assert.Empty(t, kinds(got, KindDuplicates))
	assert.Empty(t, kinds(got, KindAbsorbed))
}

// A worktree git could not be read in has unknown commits, not none. Relating
// it would call it redundant and offer to remove it.
func TestAWorktreeThatCouldNotBeReadEarnsNoEdgeAndIsNotRemovable(t *testing.T) {
	broken := node("broken")
	broken.Why = "not a git repository"
	nodes := []Node{broken, node("a", "1:p1")}

	got := Relate(nodes)
	assert.Empty(t, got)
	assert.Empty(t, Removable(Graph{Nodes: nodes, Edges: got}))
}

func TestRemovableIsCommitsOnlyAndSaysNothingAboutUncommittedWork(t *testing.T) {
	nodes := []Node{node("behind", "1:p1"), node("ahead", "1:p1", "2:p2")}

	// Nothing here knows that "behind" has uncommitted changes; the scan does,
	// and the two verdicts are combined by the caller, not here.
	assert.Equal(t, []string{"behind"}, Removable(Graph{Nodes: nodes, Edges: Relate(nodes)}))
}

func TestEdgesComeBackInAStableOrder(t *testing.T) {
	nodes := []Node{
		node("c", "1:p1", "2:p2"),
		node("a", "1:p1"),
		node("b", "1:p1", "9:p9"),
	}

	first := Relate(nodes)
	assert.Equal(t, first, Relate(nodes), "the same nodes give the same edges")
	for i := 1; i < len(first); i++ {
		assert.LessOrEqual(t, first[i-1].From, first[i].From, "sorted by from")
	}
}
