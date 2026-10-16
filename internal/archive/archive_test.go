package archive

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/wiki"
)

func doc(t *testing.T, body string) Doc {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "purge-design-docs.md"), []byte(body), 0o600))
	return Doc{Dir: dir, File: "purge-design-docs.md"}
}

func TestThePageOpensWithThePullRequestItCameFrom(t *testing.T) {
	page, err := Page("# Purge design docs\n\nArchive distils.", "https://github.com/owner/repo/pull/12")
	require.NoError(t, err)
	assert.Equal(t, "# Purge design docs\n\nPull request: https://github.com/owner/repo/pull/12\n\nArchive distils.", page)
}

func TestNoPullRequestIsNotAnError(t *testing.T) {
	page, err := Page("# Purge design docs\n\nArchive distils.", "")
	require.NoError(t, err)
	assert.Equal(t, "# Purge design docs\n\nArchive distils.", page)
}

func TestAFenceRoundTheWholeAnswerIsNotPartOfThePage(t *testing.T) {
	page, err := Page("```markdown\n# Design\n\nBody.\n```", "")
	require.NoError(t, err)
	assert.Equal(t, "# Design\n\nBody.", page)
}

func TestAFenceInsideThePageIsKept(t *testing.T) {
	page, err := Page("# Design\n\n```go\nx := 1\n```", "")
	require.NoError(t, err)
	assert.Contains(t, page, "```go\nx := 1\n```")
}

func TestWhatIsNotAPage(t *testing.T) {
	_, err := Page("   ", "")
	assert.Error(t, err, "the model wrote nothing")

	_, err = Page("# Big\n\n"+strings.Repeat("x", 16<<10), "")
	assert.Error(t, err, "a page over the cap is refused before it reaches a draft")
}

func TestThePromptCarriesTheDocumentAndRefusesAPreamble(t *testing.T) {
	prompt := Prompt("owner/repo", "x.md", "# X\n\nThe argument.")
	assert.Contains(t, prompt, "The argument.")
	assert.Contains(t, prompt, "owner/repo")
	assert.Contains(t, prompt, "no preamble")
}

func TestArchivingWithoutAWikiChangesNothing(t *testing.T) {
	d := doc(t, "# Purge design docs\n\nThe argument.")
	_, err := One(context.Background(), nil, "owner/repo", d, "", func(context.Context, string) (string, error) {
		return "# Page", nil
	})
	require.Error(t, err)
	assert.FileExists(t, filepath.Join(d.Dir, d.File), "nothing is deleted unless a page was published")
}

func TestADistillationThatFailsLeavesTheDocument(t *testing.T) {
	d := doc(t, "# Purge design docs\n\nThe argument.")
	_, err := One(context.Background(), nil, "owner/repo", d, "", func(context.Context, string) (string, error) {
		return "", errors.New("the model is not there")
	})
	require.Error(t, err)
	assert.FileExists(t, filepath.Join(d.Dir, d.File))
}

func TestArchivingPublishesThePageThenDeletesTheDocument(t *testing.T) {
	remote := newRemote(t)
	store := wiki.New(t.TempDir(), remote)
	d := doc(t, "# Purge design docs\n\nSix hundred lines of argument.")

	out, err := One(context.Background(), store, "owner/repo", d, "https://github.com/owner/repo/pull/12",
		func(_ context.Context, prompt string) (string, error) {
			assert.Contains(t, prompt, "Six hundred lines of argument.")
			return "# Purge design docs\n\nArchive distils, then deletes.", nil
		})
	require.NoError(t, err)
	assert.Equal(t, "published", out.State)
	assert.Equal(t, "repos/owner/repo/designs/purge-design-docs.md", out.Page)
	assert.NoFileExists(t, filepath.Join(d.Dir, d.File))

	page := remotePage(t, remote, out.Page)
	assert.Contains(t, page, "Pull request: https://github.com/owner/repo/pull/12")
	assert.Contains(t, page, "Archive distils, then deletes.")
}

func TestAPublishThatFailsKeepsTheDocument(t *testing.T) {
	// A remote that is not there: the clone fails, so nothing was ever out.
	store := wiki.New(t.TempDir(), filepath.Join(t.TempDir(), "gone.git"))
	d := doc(t, "# Purge design docs\n\nThe argument.")

	_, err := One(context.Background(), store, "owner/repo", d, "", func(context.Context, string) (string, error) {
		return "# Purge design docs\n\nDistilled.", nil
	})
	require.Error(t, err)
	assert.FileExists(t, filepath.Join(d.Dir, d.File), "it will be a candidate again next week")
}

func TestSweepSaysWhatMergedAndWhatWasAbandoned(t *testing.T) {
	st := State{
		Docs: []Doc{
			{Dir: "/d", File: "purge-design-docs.md"},
			{Dir: "/d", File: "editor-fork.md"},
			{Dir: "/d", File: "quiz.md"},
			{Dir: "/d", File: "agentbus-harness.md"},
			{Dir: "/d", File: "my-notes.md"},
		},
		Branches: []string{"main", "feat/quiz"},
		Heads: []ghpr.Head{
			{Number: 8, Branch: "feat/purge-design-docs", State: "MERGED", URL: "u8"},
			{Number: 9, Branch: "editor-fork", State: "CLOSED", URL: "u9"},
			{Number: 6, Branch: "feat/quiz", State: "OPEN", URL: "u6"},
			{Number: 4, Branch: "agentbus-harness", State: "OPEN", URL: "u4"},
		},
	}

	merged, abandoned := Sweep(st)
	require.Len(t, merged, 1)
	assert.Equal(t, "purge-design-docs.md", merged[0].File)
	assert.Equal(t, "u8", merged[0].PR)
	require.Len(t, abandoned, 1)
	assert.Equal(t, "editor-fork.md", abandoned[0].File)
}

func TestADocumentWithNoBranchBehindItIsNeverSweptUp(t *testing.T) {
	st := State{Docs: []Doc{{Dir: "/d", File: "my-notes.md"}}}
	merged, abandoned := Sweep(st)
	assert.Empty(t, merged)
	assert.Empty(t, abandoned)
}

func TestAMergedBranchStillCheckedOutIsLeftAlone(t *testing.T) {
	st := State{
		Docs:     []Doc{{Dir: "/d", File: "purge-design-docs.md"}},
		Branches: []string{"purge-design-docs"},
		Heads:    []ghpr.Head{{Branch: "purge-design-docs", State: "MERGED"}},
	}
	merged, abandoned := Sweep(st)
	assert.Empty(t, merged, "the branch is still here, so the work may not be")
	assert.Empty(t, abandoned)
}

func TestOpenWinsOverAnEarlierPullRequestOfTheSameName(t *testing.T) {
	st := State{
		Docs: []Doc{{Dir: "/d", File: "retry.md"}},
		Heads: []ghpr.Head{
			{Number: 1, Branch: "retry", State: "CLOSED"},
			{Number: 2, Branch: "retry", State: "OPEN"},
		},
	}
	merged, abandoned := Sweep(st)
	assert.Empty(t, merged)
	assert.Empty(t, abandoned, "work in flight is neither")
}
