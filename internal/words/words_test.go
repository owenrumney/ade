package words

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/gitdiff"
)

// marked renders the cell with the edited runs in brackets, which is what the
// highlight looks like on the page.
func marked(cell *gitdiff.Cell) string {
	if len(cell.Tokens) == 0 {
		return cell.Content
	}
	var b strings.Builder
	for _, token := range cell.Tokens {
		if token.Edit {
			b.WriteString("[" + token.Text + "]")
			continue
		}
		b.WriteString(token.Text)
	}
	return b.String()
}

func rowOf(before, after string) *gitdiff.Row {
	return &gitdiff.Row{
		Left:  &gitdiff.Cell{Line: 1, Kind: gitdiff.KindDel, Content: before},
		Right: &gitdiff.Cell{Line: 1, Kind: gitdiff.KindAdd, Content: after},
	}
}

func TestOnlyTheWordsThatChangedAreMarked(t *testing.T) {
	cases := []struct {
		name          string
		before, after string
		left, right   string
	}{
		{
			name:   "one word in a long line",
			before: `return fmt.Errorf("reading the config: %w", err)`,
			after:  `return fmt.Errorf("reading the sandbox: %w", err)`,
			left:   `return fmt.Errorf("reading the [config]: %w", err)`,
			right:  `return fmt.Errorf("reading the [sandbox]: %w", err)`,
		},
		{
			name:   "a word added in the middle",
			before: "if err != nil {",
			after:  "if err != nil && !done {",
			left:   "if err != nil {",
			right:  "if err != nil [&& !done ]{",
		},
		{
			name:   "indentation alone",
			before: "\tcount++",
			after:  "\t\tcount++",
			left:   "[\t]count++",
			right:  "[\t\t]count++",
		},
		{
			name:   "two separate edits",
			before: "a := one + two",
			after:  "b := one + three",
			left:   "[a] := one + [two]",
			right:  "[b] := one + [three]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := rowOf(tc.before, tc.after)
			pair(row)
			assert.Equal(t, tc.left, marked(row.Left))
			assert.Equal(t, tc.right, marked(row.Right))
		})
	}
}

func TestLinesWithNothingInCommonAreLeftAlone(t *testing.T) {
	// A replacement, not an edit. Marking it would light up both lines end to
	// end, which says less than the plain red and green already do.
	row := rowOf("\t\treturn nil, errors.New(\"no such session\")", "\t\tlog.Printf(\"dropped %d\", n)")
	pair(row)
	assert.Nil(t, row.Left.Tokens, "the deletion was marked")
	assert.Nil(t, row.Right.Tokens, "the addition was marked")
}

func TestTheMarksSplitTheSyntaxTokensRatherThanReplacingThem(t *testing.T) {
	row := rowOf(`x := "old"`, `x := "new"`)
	row.Left.Tokens = []gitdiff.Token{{Text: "x := "}, {Class: "s", Text: `"old"`}}
	row.Right.Tokens = []gitdiff.Token{{Text: "x := "}, {Class: "s", Text: `"new"`}}
	pair(row)

	assert.Equal(t, `x := "[old]"`, marked(row.Left))
	assert.Equal(t, `x := "[new]"`, marked(row.Right))
	for _, token := range row.Right.Tokens {
		if token.Edit {
			assert.Equal(t, "s", token.Class, "the string class was dropped by the split")
		}
	}
}

// A token run that does not rebuild the line cannot be cut by byte offset, and
// cutting it anyway would panic or silently shuffle the text.
func TestTokensThatDoNotRebuildTheLineAreRebuiltFromIt(t *testing.T) {
	row := rowOf("alpha beta", "alpha gamma")
	row.Right.Tokens = []gitdiff.Token{{Class: "k", Text: "nonsense"}}
	pair(row)
	assert.Equal(t, "alpha [gamma]", marked(row.Right))
}

func TestOnlyADeletionPairedWithAnAdditionIsMarked(t *testing.T) {
	row := &gitdiff.Row{
		Left:  &gitdiff.Cell{Kind: gitdiff.KindContext, Content: "same"},
		Right: &gitdiff.Cell{Kind: gitdiff.KindContext, Content: "same"},
	}
	pair(row)
	assert.Nil(t, row.Right.Tokens)

	lone := &gitdiff.Row{Right: &gitdiff.Cell{Kind: gitdiff.KindAdd, Content: "added"}}
	pair(lone)
	assert.Nil(t, lone.Right.Tokens)
}

func TestAMachineWrittenLineIsNotWorthTheMatch(t *testing.T) {
	// A minified bundle is one line of thousands of words. The quadratic match
	// is skipped rather than stalling the poll that asked for the diff.
	long := strings.Repeat("a,", maxWords+1)
	row := rowOf(long+"b", long+"c")
	pair(row)
	assert.Nil(t, row.Right.Tokens)
}

func TestApplyWalksEveryHunkOfEveryFile(t *testing.T) {
	diff := &gitdiff.Diff{Files: []gitdiff.File{{
		Path:  "main.go",
		Hunks: []gitdiff.Hunk{{Rows: []gitdiff.Row{*rowOf("port := 8080", "port := 9090")}}},
	}}}
	Apply(diff)

	row := diff.Files[0].Hunks[0].Rows[0]
	require.NotNil(t, row.Right.Tokens)
	assert.Equal(t, "port := [9090]", marked(row.Right))
}

// interleaved builds a line of n words, changing every third so the common head
// and tail trim nothing and the quadratic match runs on the whole line.
func interleaved(t *testing.T, n int, seed string) *gitdiff.Row {
	t.Helper()
	before := make([]string, n)
	after := make([]string, n)
	for i := range before {
		before[i] = fmt.Sprintf("w%d", i)
		after[i] = before[i]
		if i%3 == 0 {
			after[i] = seed + before[i]
		}
	}
	return rowOf(strings.Join(before, " "), strings.Join(after, " "))
}

// maxRows bounds how many rows are looked at, not what looking costs: each one
// is an (n+1)*(m+1) grid, so a diff of long lines needs its own budget.
func TestTheQuadraticMatchIsBoundedByGridCellsRatherThanRows(t *testing.T) {
	// Well inside maxRows, and well past maxCells at 100 words a side.
	rows := make([]gitdiff.Row, 400)
	for i := range rows {
		rows[i] = *interleaved(t, 100, "x")
	}
	diff := &gitdiff.Diff{Files: []gitdiff.File{{
		Path: "bundle.js", Hunks: []gitdiff.Hunk{{Rows: rows}},
	}}}
	Apply(diff)

	marks := func(cell *gitdiff.Cell) bool { return strings.Contains(marked(cell), "[") }
	assert.True(t, marks(diff.Files[0].Hunks[0].Rows[0].Right), "the first row went unmarked")
	assert.False(t, marks(diff.Files[0].Hunks[0].Rows[399].Right),
		"the budget was spent and the last row was matched anyway")
}

func TestABinaryFileHasNoLinesToMark(t *testing.T) {
	diff := &gitdiff.Diff{Files: []gitdiff.File{{Path: "icon.png", Binary: true}}}
	Apply(diff)
	Apply(nil)
}
