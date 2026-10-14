package review

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"

	"github.com/owenrumney/ade/internal/gitdiff"
)

func textTurn(text string) *bridge.Turn {
	return &bridge.Turn{Text: text}
}

func TestPrompt(t *testing.T) {
	t.Run("passes the level through", func(t *testing.T) {
		assert.Contains(t, Prompt("high", ""), "/code-review high")
	})

	t.Run("defaults an unknown level", func(t *testing.T) {
		assert.Contains(t, Prompt("banana", ""), "/code-review medium")
		assert.Contains(t, Prompt("", ""), "/code-review medium")
	})

	t.Run("embeds the diff and forbids git and edits", func(t *testing.T) {
		p := Prompt("medium", "=== a.go ===\n     1 + x := 1")
		assert.Contains(t, p, "<diff>")
		assert.Contains(t, p, "1 + x := 1", "the diff itself must be in the prompt")
		assert.Contains(t, p, "do not run git")
		assert.Contains(t, p, "do not edit anything")
		assert.Contains(t, p, "fenced JSON block")
	})
}

func TestRenderDiff(t *testing.T) {
	files := gitdiff.Parse(`diff --git a/cart.go b/cart.go
index 1..2 100644
--- a/cart.go
+++ b/cart.go
@@ -1,4 +1,5 @@ package cart
 package cart

-var limit = 1
+var limit = 2
+var extra = 3`)
	require.NotEmpty(t, files)
	out := RenderDiff(&gitdiff.Diff{Files: files})

	t.Run("labels the file with its status and counts", func(t *testing.T) {
		assert.Contains(t, out, "=== cart.go (modified, +2 -1) ===")
	})

	t.Run("numbers new lines so findings can be anchored", func(t *testing.T) {
		assert.Contains(t, out, "     3 + var limit = 2")
		assert.Contains(t, out, "     4 + var extra = 3")
		assert.Contains(t, out, "     1   package cart")
	})

	t.Run("deletions have no new-file line number", func(t *testing.T) {
		assert.Contains(t, out, "       - var limit = 1")
	})

	t.Run("nil diff renders nothing", func(t *testing.T) {
		assert.Empty(t, RenderDiff(nil))
	})
}

func TestRenderDiffMarksBinaryFiles(t *testing.T) {
	files := gitdiff.Parse(`diff --git a/logo.png b/logo.png
index 1..2 100644
Binary files a/logo.png and b/logo.png differ`)
	require.NotEmpty(t, files)

	out := RenderDiff(&gitdiff.Diff{Files: files})
	assert.Contains(t, out, "binary file")
}

func TestReadOnlyToolsCannotEditOrShell(t *testing.T) {
	tools := ReadOnlyTools()

	assert.Equal(t, []string{"Read", "Grep", "Glob"}, tools)
	for _, banned := range []string{"Edit", "Write", "NotebookEdit", "Bash"} {
		assert.NotContains(t, tools, banned, "a reviewer must not be able to change the code")
	}
}

func TestParseTurnFromTextBlock(t *testing.T) {
	turn := textTurn("Found some things.\n\n" +
		"```json\n" +
		`[{"file":"internal/cart/cart.go","line":24,"category":"correctness","summary":"Top slices past the end","failure_scenario":"Top(items, len(items)) panics"}]` +
		"\n```")

	findings, ok, err := ParseTurn(turn)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, findings, 1)

	f := findings[0]
	assert.Equal(t, "internal/cart/cart.go", f.File)
	assert.Equal(t, 24, f.Line)
	assert.Equal(t, "correctness", f.Category)
	assert.Equal(t, "Top slices past the end", f.Summary)
}

func TestParseTurnPrefersTheToolCall(t *testing.T) {
	turn := &bridge.Turn{
		Text: "```json\n[{\"file\":\"from-text.go\",\"line\":1,\"summary\":\"text\"}]\n```",
		Events: []bridge.Event{{
			ToolUses: []bridge.ToolUse{{
				Name:  reportTool,
				Input: []byte(`{"findings":[{"file":"from-tool.go","line":2,"summary":"tool"}]}`),
			}},
		}},
	}

	findings, ok, err := ParseTurn(turn)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, findings, 1)
	assert.Equal(t, "from-tool.go", findings[0].File)
}

func TestParseTurnEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		turn       *bridge.Turn
		count      int
		structured bool
		isErr      bool
	}{
		{
			name:       "empty array means a clean review",
			structured: true,
			turn:       textTurn("Nothing found.\n```json\n[]\n```"),
			count:      0,
		},
		{
			name:  "prose with no block is not an error",
			turn:  textTurn("I reviewed it and found nothing worth reporting."),
			count: 0,
		},
		{
			name:       "bare object payload",
			structured: true,
			turn:       textTurn("```json\n{\"findings\":[{\"file\":\"a.go\",\"line\":3,\"summary\":\"x\"}]}\n```"),
			count:      1,
		},
		{
			name:       "unlabelled fence",
			structured: true,
			turn:       textTurn("```\n[{\"file\":\"a.go\",\"line\":3,\"summary\":\"x\"}]\n```"),
			count:      1,
		},
		{
			name:       "prefers the last block",
			structured: true,
			turn:       textTurn("```json\n[]\n```\nrevised:\n```json\n[{\"file\":\"a.go\",\"line\":9,\"summary\":\"x\"}]\n```"),
			count:      1,
		},
		{
			name:       "drops findings with no file",
			structured: true,
			turn:       textTurn("```json\n[{\"file\":\"\",\"line\":3,\"summary\":\"x\"}]\n```"),
			count:      0,
		},
		{
			name:       "drops findings with no line",
			structured: true,
			turn:       textTurn("```json\n[{\"file\":\"a.go\",\"line\":0,\"summary\":\"x\"}]\n```"),
			count:      0,
		},
		{
			name:       "drops findings with nothing to say",
			structured: true,
			turn:       textTurn("```json\n[{\"file\":\"a.go\",\"line\":3,\"summary\":\"  \"}]\n```"),
			count:      0,
		},
		{
			name:       "falls back to short_summary",
			structured: true,
			turn:       textTurn("```json\n[{\"file\":\"a.go\",\"line\":3,\"short_summary\":\"terse\"}]\n```"),
			count:      1,
		},
		{
			name:  "empty reply is an error",
			turn:  textTurn("   "),
			isErr: true,
		},
		{
			name:  "nil turn is an error",
			turn:  nil,
			isErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings, ok, err := ParseTurn(tt.turn)
			if tt.isErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, findings, tt.count)
			assert.Equal(t, tt.structured, ok)
		})
	}
}

func TestRelative(t *testing.T) {
	root := "/Users/owen/code/thing"
	findings := []Finding{
		{File: root + "/internal/cart/cart.go", Line: 1, Summary: "a"},
		{File: "internal/cart/cart.go", Line: 2, Summary: "b"},
		{File: "./main.go", Line: 3, Summary: "c"},
	}

	got := Relative(findings, root)
	assert.Equal(t, "internal/cart/cart.go", got[0].File, "absolute paths are made relative")
	assert.Equal(t, "internal/cart/cart.go", got[1].File, "relative paths are left alone")
	assert.Equal(t, "main.go", got[2].File)
}

func TestBody(t *testing.T) {
	t.Run("summary and scenario", func(t *testing.T) {
		body := Body(Finding{
			Summary:         "Top slices past the end of items.",
			FailureScenario: "Top(items, len(items)) panics.",
		})
		assert.Equal(t, "Top slices past the end of items.\n\nFails when: Top(items, len(items)) panics.", body)
	})

	t.Run("marks an unconfirmed finding", func(t *testing.T) {
		body := Body(Finding{Summary: "Might race.", Verdict: "PLAUSIBLE"})
		assert.Equal(t, "(plausible) Might race.", body)
	})

	t.Run("a confirmed finding is not prefixed", func(t *testing.T) {
		body := Body(Finding{Summary: "Definitely wrong.", Verdict: "CONFIRMED"})
		assert.Equal(t, "Definitely wrong.", body)
	})

	t.Run("summary only", func(t *testing.T) {
		assert.Equal(t, "Just this.", Body(Finding{Summary: "Just this."}))
	})
}

func TestParseTurnNormalisesSeverity(t *testing.T) {
	turn := &bridge.Turn{Text: "```json\n" + `[
	  {"file": "a.go", "line": 1, "summary": "one", "severity": "CRITICAL"},
	  {"file": "b.go", "line": 2, "summary": "two", "severity": "nonsense"},
	  {"file": "c.go", "line": 3, "summary": "three"}
	]` + "\n```"}

	found, structured, err := ParseTurn(turn)
	require.NoError(t, err)
	require.True(t, structured)
	require.Len(t, found, 3)
	assert.Equal(t, "critical", found[0].Severity)
	assert.Equal(t, "medium", found[1].Severity, "an unknown rank is medium, not dropped")
	assert.Equal(t, "medium", found[2].Severity, "a missing rank is medium")
}

func TestRenderSummaryIsTheShapeOfTheChangeNotTheWholeOfIt(t *testing.T) {
	assert.Empty(t, RenderSummary(nil))
	assert.Empty(t, RenderSummary(&gitdiff.Diff{}), "no files is nothing to say")

	got := RenderSummary(&gitdiff.Diff{
		Base: "origin/main",
		Files: []gitdiff.File{
			{Path: "a.go", Status: "modified", Additions: 3, Deletions: 1},
			{Path: "b.go", Status: "added", Additions: 9},
		},
	})
	assert.Contains(t, got, "2 file(s) changed against origin/main.")
	assert.Contains(t, got, "git diff origin/main -- <path>")
	assert.Contains(t, got, "a.go (modified, +3 -1)")
	assert.Contains(t, got, "b.go (added, +9 -0)")
	assert.NotContains(t, got, "@@", "the hunks are what it does not carry")

	// A base ade could not work out still leaves a runnable command.
	assert.Contains(t, RenderSummary(&gitdiff.Diff{
		Files: []gitdiff.File{{Path: "a.go", Status: "modified"}},
	}), "git diff HEAD -- <path>")
}

func TestRenderSummaryCapsTheFileList(t *testing.T) {
	files := make([]gitdiff.File, maxSummaryFiles+5)
	for i := range files {
		files[i] = gitdiff.File{Path: fmt.Sprintf("f%d.go", i), Status: "modified"}
	}
	got := RenderSummary(&gitdiff.Diff{Base: "main", Files: files})
	assert.Contains(t, got, "f0.go")
	assert.Contains(t, got, "... and 5 more")
	assert.NotContains(t, got, fmt.Sprintf("f%d.go", maxSummaryFiles+1))
}

// The block below is copied from the shipped reviewer prompts. If the parser
// stops accepting it, the reviewers file nothing and the diff stays empty —
// which is exactly the failure this test exists to catch.
func TestTheStreamerAcceptsTheBlockTheShippedPromptsAskFor(t *testing.T) {
	const asPrompted = "Something to say first.\n\n" +
		"```ade-finding\n" +
		`{"file": "internal/cart/cart.go", "line": 24, "category": "correctness", "severity": "high", "summary": "one sentence", "failure_scenario": "concrete inputs then wrong result"}` +
		"\n```\n\nAnd a line after it.\n"

	got := NewStreamer().Push(asPrompted)
	require.Len(t, got, 1, "the prompted block must parse")
	assert.Equal(t, "internal/cart/cart.go", got[0].File)
	assert.Equal(t, 24, got[0].Line)
	assert.Equal(t, "correctness", got[0].Category)
	assert.Equal(t, "high", got[0].Severity)
	assert.Equal(t, "one sentence", got[0].Summary)
	assert.Equal(t, "concrete inputs then wrong result", got[0].FailureScenario)
}

func TestBodyLeadsWithTheHeadlineAndPutsTheRestUnderIt(t *testing.T) {
	// The headline is what you read down a diff; the detail follows it. No
	// markup: the thread view renders a body as text.
	body := Body(Finding{
		Headline:        "The purge deletes the cache while a sandboxed run reads it.",
		Summary:         "handleDiskPurge checks sandboxRunning and then deletes, with no lock between",
		FailureScenario: "a run starts after the check; its mounted caches vanish mid-build",
	})

	first, rest, _ := strings.Cut(body, "\n")
	assert.Equal(t, "The purge deletes the cache while a sandboxed run reads it.", first)
	assert.NotContains(t, body, "<details>")
	assert.Contains(t, rest, "handleDiskPurge checks sandboxRunning")
	assert.Contains(t, rest, "Fails when: a run starts after the check")
}

func TestAFindingWithNoHeadlineStillReads(t *testing.T) {
	// Reviewers predate the field, and a shipped definition may not know it.
	body := Body(Finding{Summary: "the token is never refreshed", FailureScenario: "it expires"})
	assert.Equal(t, "the token is never refreshed\n\nFails when: it expires", body,
		"with nothing short to lead with there is nothing to fold behind")
}

func TestAHeadlineOnItsOwnHasNothingUnderIt(t *testing.T) {
	body := Body(Finding{Headline: "The handler drops the error."})
	assert.Equal(t, "The handler drops the error.", body)
	assert.NotContains(t, body, "<details>")
}

func TestThePlausibleMarkStaysInFront(t *testing.T) {
	body := Body(Finding{Verdict: "PLAUSIBLE", Headline: "The cache grows without a bound."})
	assert.True(t, strings.HasPrefix(body, "(plausible) The cache grows"), body)
}
