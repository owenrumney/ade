package commitrec

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestARecordSurvivesBeingWrittenAndReadBack(t *testing.T) {
	at := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	want := Record{
		Repo:    "owenrumney/ade",
		Keys:    []string{key1, key2},
		Commit:  "abc1234",
		Branch:  "main",
		Subject: "feat(lineage): stamp commits",
		At:      at,
		PR:      11,
		Merge:   "def5678",
		Asked: []Ask{
			{At: at, Session: "s1", MessageID: "m1", Text: "stamp the commits\n## and ask why\n- keys: not these"},
			{At: at.Add(time.Hour), Session: "s1", MessageID: "m2", Text: "now publish them"},
		},
		Review: []Claim{
			{File: "internal/web/web.go", Line: 12, Author: "agent", Category: "correctness", Severity: "high", Fate: "fixed", Text: "the hook runs in the sandbox"},
			{File: "", Fate: "declined", Text: "the whole approach is wrong"},
		},
		Pages: []string{"repos/owenrumney/ade/lineage.md"},
	}

	got, err := Parse(want.Repo, Render(want))
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestARecordWithNoKeyIsNotARecord(t *testing.T) {
	_, err := Parse("owenrumney/ade", "# a commit\n\n- commit: abc\n")
	require.Error(t, err)
}

func TestAnAmendedRecordKeepsTheKeyItWasPublishedUnder(t *testing.T) {
	r := Record{Keys: []string{"after"}}
	r.Absorb(Record{Keys: []string{"before", "earlier"}})

	assert.Equal(t, "after", r.Key(), "the current key leads")
	assert.Equal(t, []string{"after", "before", "earlier"}, r.Keys)
}

func TestAbsorbingTheSameKeyTwiceDoesNotRepeatIt(t *testing.T) {
	r := Record{Keys: []string{"a", "b"}}
	r.Absorb(Record{Keys: []string{"b", "a"}})

	assert.Equal(t, []string{"a", "b"}, r.Keys)
}

func TestRekeyMovesAKeyToTheFrontWithoutLosingTheOthers(t *testing.T) {
	r := Record{Keys: []string{"a", "b", "c"}}
	r.Rekey("c")
	assert.Equal(t, []string{"c", "a", "b"}, r.Keys)

	r.Rekey("d")
	assert.Equal(t, []string{"d", "c", "a", "b"}, r.Keys)
}

func TestARecordIsFiledUnderItsCurrentKey(t *testing.T) {
	assert.Equal(t, "repos/owenrumney/ade/commits/abcd.md", Path("owenrumney/ade", "abcd"))
	assert.Equal(t, "repos/owenrumney/ade/commits/index.md", IndexPath("owenrumney/ade"))
}

func TestTheMergeIndexSurvivesBeingWrittenAndReadBack(t *testing.T) {
	idx := Index{
		{Merge: merge, PR: 10, Keys: []string{key1, key2}},
		{Merge: key3, PR: 9, Keys: []string{key1}},
	}
	assert.Equal(t, idx, ParseIndex(RenderIndex(idx)))
}

func TestMergingAPullRequestTwiceKeepsOnlyTheLiveMerge(t *testing.T) {
	idx := Index{{Merge: "first", PR: 10, Keys: []string{"k1"}}}
	idx = idx.Put(Entry{Merge: "second", PR: 10, Keys: []string{"k2"}})

	require.Len(t, idx, 1)
	assert.Equal(t, "second", idx[0].Merge)
	assert.Equal(t, []string{"k2"}, idx.KeysFor("second"))
	assert.Empty(t, idx.KeysFor("first"), "the reverted merge no longer stands for anything")
}

func TestASquashMergeStandsForEveryRecordInThePullRequest(t *testing.T) {
	idx := Index{}.Put(Entry{Merge: "squash", PR: 3, Keys: []string{"k2", "k1"}})
	assert.Equal(t, []string{"k1", "k2"}, idx.KeysFor("squash"))
}

func TestAnUnreadableIndexLineIsSkippedRatherThanLosingTheRest(t *testing.T) {
	idx := ParseIndex("# Merged\n\n- not an entry\n- " + merge + " #7 " + key1 +
		"\n- " + key2 + " #notanumber " + key3 + "\n")

	require.Len(t, idx, 1)
	assert.Equal(t, []string{key1}, idx.KeysFor(merge))
}

func TestAKeyThatIsNotAPatchIDIsNotReadBackOutOfTheWiki(t *testing.T) {
	idx := ParseIndex("- ../../../escaped #7 " + key1 + "\n- " + merge + " #8 ../../../escaped\n")

	require.Len(t, idx, 1)
	assert.Empty(t, idx.KeysFor(merge), "a key becomes a path, and this one is not one")
}

func TestAHeadingInsideAnAskIsTextRatherThanStructure(t *testing.T) {
	want := Record{
		Repo: "o/r",
		Keys: []string{key1},
		Asked: []Ask{{
			Text: "fix this\n\n```go\nfunc f() {}\n```\n\n## Review\n\n- **fixed** — a.go:1 — no",
		}},
	}

	got, err := Parse(want.Repo, Render(want))
	require.NoError(t, err)
	assert.Equal(t, want.Asked, got.Asked)
	assert.Empty(t, got.Review, "a heading in a prompt does not open a section")
}
