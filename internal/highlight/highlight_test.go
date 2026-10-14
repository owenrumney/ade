package highlight

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/gitdiff"
)

func cell(content string) *gitdiff.Cell {
	return &gitdiff.Cell{Kind: gitdiff.KindContext, Content: content}
}

func rows(cells ...*gitdiff.Cell) []gitdiff.Row {
	out := make([]gitdiff.Row, 0, len(cells))
	for _, c := range cells {
		out = append(out, gitdiff.Row{Left: c, Right: c})
	}
	return out
}

func diffOf(path string, cells ...*gitdiff.Cell) *gitdiff.Diff {
	return &gitdiff.Diff{Files: []gitdiff.File{{
		Path:  path,
		Hunks: []gitdiff.Hunk{{Rows: rows(cells...)}},
	}}}
}

// text rebuilds a cell from its tokens, which must always equal the original.
func text(c *gitdiff.Cell) string {
	if len(c.Tokens) == 0 {
		return c.Content
	}
	out := ""
	for _, t := range c.Tokens {
		out += t.Text
	}
	return out
}

func classesOf(c *gitdiff.Cell) []string {
	var out []string
	for _, t := range c.Tokens {
		if t.Class != "" {
			out = append(out, t.Class)
		}
	}
	return out
}

func TestApplyClassifiesGo(t *testing.T) {
	line := cell(`func main() { s := "hi" // note`)
	Apply(diffOf("main.go", line))

	require.NotEmpty(t, line.Tokens)
	assert.Equal(t, line.Content, text(line), "tokens must rebuild the line exactly")
	assert.Contains(t, classesOf(line), "k", "func is a keyword")
	assert.Contains(t, classesOf(line), "s", "the string literal")
	assert.Contains(t, classesOf(line), "c", "the comment")
}

func TestApplyKeepsContentIntact(t *testing.T) {
	tests := []struct {
		name string
		path string
		line string
	}{
		{name: "leading whitespace", path: "a.go", line: "\t\tif x != nil {"},
		{name: "html-like content", path: "a.go", line: `s := "<script>alert(1)</script>"`},
		{name: "unicode", path: "a.go", line: `s := "héllo → 世界"`},
		{name: "python", path: "a.py", line: "def go(x): return x + 1  # add"},
		{name: "empty line", path: "a.go", line: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := cell(tt.line)
			Apply(diffOf(tt.path, line))
			assert.Equal(t, tt.line, text(line))
		})
	}
}

func TestApplySkipsWhatItCannotHelp(t *testing.T) {
	t.Run("unknown extension", func(t *testing.T) {
		line := cell("some plain words")
		Apply(diffOf("notes.zzz", line))
		assert.Empty(t, line.Tokens)
	})

	t.Run("binary file", func(t *testing.T) {
		line := cell("func main() {}")
		diff := diffOf("main.go", line)
		diff.Files[0].Binary = true
		Apply(diff)
		assert.Empty(t, line.Tokens)
	})

	t.Run("a line with nothing to style stays plain", func(t *testing.T) {
		line := cell("        ")
		Apply(diffOf("main.go", line))
		assert.Empty(t, line.Tokens, "whitespace only needs no tokens on the wire")
	})

	t.Run("nil diff", func(t *testing.T) {
		assert.NotPanics(t, func() { Apply(nil) })
	})
}

func TestApplyLexesAcrossLinesInAHunk(t *testing.T) {
	// The comment opens on the first line and closes on the third, so a
	// line-at-a-time lexer would miss the middle one.
	open, middle, closing := cell("/* start"), cell("   still a comment"), cell("   end */")
	Apply(diffOf("a.go", open, middle, closing))

	assert.Contains(t, classesOf(middle), "c", "the middle line is inside the block comment")
	assert.Equal(t, "   still a comment", text(middle))
}

func TestApplyAssignsTokensToTheRightLine(t *testing.T) {
	first, second := cell("var a = 1"), cell(`var b = "two"`)
	Apply(diffOf("a.go", first, second))

	assert.Equal(t, "var a = 1", text(first))
	assert.Equal(t, `var b = "two"`, text(second))
	assert.Contains(t, classesOf(first), "n", "1 is the number, and it is on the first line")
	assert.NotContains(t, classesOf(first), "s", "the string belongs to the second line")
	assert.Contains(t, classesOf(second), "s")
}
