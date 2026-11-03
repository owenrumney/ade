package prlink

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var at = At{Owner: "owenrumney", Repo: "ade", SHA: "abc123"}

func blob(path, anchor string) string {
	return "https://github.com/owenrumney/ade/blob/abc123/" + path + anchor
}

func TestAPathAndLineBecomesALinkToThatLine(t *testing.T) {
	got := In("The guard in internal/web/ship.go:196 reads the wrong branch.", at, "", nil)
	assert.Equal(t,
		"The guard in [internal/web/ship.go:196]("+blob("internal/web/ship.go", "#L196")+") reads the wrong branch.",
		got)
}

func TestARangeKeepsBothEndsInTheTextAndTheAnchor(t *testing.T) {
	got := In("app/renderer/real.ts:217-224 does the filtering.", at, "", nil)
	assert.Contains(t, got, blob("app/renderer/real.ts", "#L217-L224"))
	assert.Contains(t, got, "[app/renderer/real.ts:217-224]")
}

func TestABacktickedReferenceKeepsItsCodeSpanInsideTheLink(t *testing.T) {
	got := In("see `internal/ship/veto.go:48`", at, "", nil)
	assert.Equal(t, "see [`internal/ship/veto.go:48`]("+blob("internal/ship/veto.go", "#L48")+")", got)
}

func TestABareFilenameIsResolvedAgainstWhatTheChangeTouches(t *testing.T) {
	known := Files([]string{"app/renderer/app.ts", "internal/web/ship.go"})
	got := In("app.ts:155 is the caller.", at, "", known)
	assert.Contains(t, got, blob("app/renderer/app.ts", "#L155"))
}

func TestANameInTwoDirectoriesIsLeftAlone(t *testing.T) {
	known := Files([]string{"a/view.ts", "b/view.ts"})
	assert.Equal(t, "view.ts:12 is one of them.", In("view.ts:12 is one of them.", at, "", known))
}

func TestABareFilenameWithNothingToResolveAgainstIsLeftAlone(t *testing.T) {
	assert.Equal(t, "app.ts:155 here.", In("app.ts:155 here.", at, "", nil))
}

func TestLineNumbersOnTheirOwnPointAtTheCommentsOwnFile(t *testing.T) {
	got := In("Lines 141-142 repeat the check above.", at, "internal/web/commit.go", nil)
	assert.Equal(t,
		"[Lines 141-142]("+blob("internal/web/commit.go", "#L141-L142")+") repeat the check above.",
		got)
}

func TestALineReferenceWithNoFileToPinItToIsLeftAlone(t *testing.T) {
	assert.Equal(t, "line 141 of something", In("line 141 of something", at, "", nil))
}

func TestAFencedBlockIsNotRewritten(t *testing.T) {
	body := "Look at internal/web/ship.go:12.\n\n```go\n// internal/web/ship.go:12 is the line\n```\n"
	got := In(body, at, "", nil)
	assert.Contains(t, got, "[internal/web/ship.go:12]")
	assert.Contains(t, got, "// internal/web/ship.go:12 is the line", "the code block keeps its text")
}

func TestALinkAlreadyWrittenIsNotLinkedAgain(t *testing.T) {
	body := "[internal/web/ship.go:12](https://example.com/x) and internal/web/ship.go:13"
	got := In(body, at, "", nil)
	assert.Contains(t, got, "[internal/web/ship.go:12](https://example.com/x)")
	assert.Contains(t, got, blob("internal/web/ship.go", "#L13"))
}

func TestAHalfBacktickedReferenceIsSomebodysProse(t *testing.T) {
	assert.Equal(t, "`internal/a.go:4 and more", In("`internal/a.go:4 and more", at, "", nil))
}

func TestWithoutACommitThereIsNothingToLinkTo(t *testing.T) {
	body := "internal/web/ship.go:196 reads the wrong branch."
	assert.Equal(t, body, In(body, At{Owner: "owenrumney", Repo: "ade"}, "", nil))
}

func TestWhatIsNotAReferenceIsLeftWhereItIs(t *testing.T) {
	for _, body := range []string{
		"the ratio was 3:2 across the board",
		"at 14:30 it still failed",
		"version 1.26:10 of nothing",
	} {
		assert.Equal(t, body, In(body, at, "", nil), body)
	}
}
