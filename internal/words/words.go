// Package words marks the runs of a changed line that actually changed, so a
// one-character edit does not paint a hundred-character line.
package words

import (
	"unicode"
	"unicode/utf8"

	"github.com/owenrumney/ade/internal/gitdiff"
)

// maxRows bounds the work on a huge diff, as highlight's own budget does.
const maxRows = 20000

// maxWords is where a line stops being source and starts being a lock file.
const maxWords = 200

// maxCells bounds the quadratic match over the whole diff. The per-row cost is
// an (n+1)*(m+1) grid, so a budget in rows caps the count and not the work.
const maxCells = 8_000_000

// minCommon: below a quarter of the longer line in common, the marks are confetti.
const minCommon = 4

// span is a byte range of a line's content.
type span struct{ start, end int }

// Apply marks the changed runs of every line paired with a line it replaced.
// Run it after highlight: it splits the tokens that pass has already filled in.
func Apply(diff *gitdiff.Diff) {
	if diff == nil {
		return
	}
	rows, cells := maxRows, maxCells
	for i := range diff.Files {
		file := &diff.Files[i]
		if file.Binary {
			continue
		}
		for j := range file.Hunks {
			for k := range file.Hunks[j].Rows {
				if rows <= 0 || cells <= 0 {
					return
				}
				rows--
				cells -= pair(&file.Hunks[j].Rows[k])
			}
		}
	}
}

// pair marks a row holding a deletion against the addition that replaced it,
// reporting the grid cells the match cost.
func pair(row *gitdiff.Row) int {
	left, right := row.Left, row.Right
	if left == nil || right == nil {
		return 0
	}
	if left.Kind != gitdiff.KindDel || right.Kind != gitdiff.KindAdd {
		return 0
	}
	leftSpans, rightSpans, cost := changed(left.Content, right.Content)
	left.Tokens = mark(left.Content, left.Tokens, leftSpans)
	right.Tokens = mark(right.Content, right.Tokens, rightSpans)
	return cost
}

// changed returns the byte ranges of each line the other side does not have, and
// what the match cost. Too little in common gets nothing: that is a replacement.
func changed(a, b string) ([]span, []span, int) {
	aw, bw := split(a), split(b)
	if len(aw) > maxWords || len(bw) > maxWords {
		return nil, nil, 0
	}

	aSame, bSame, cost := align(aw, bw)
	kept := 0
	for i, same := range aSame {
		if same {
			kept += len(aw[i])
		}
	}
	if longer := max(len(a), len(b)); kept*minCommon < longer {
		return nil, nil, cost
	}
	return runs(aw, aSame), runs(bw, bSame), cost
}

// split cuts a line into the units a reader compares: a word, a run of
// whitespace, or a single character of punctuation.
func split(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		end := i + size
		if run := sameClass(r); run != nil {
			for end < len(s) {
				next, n := utf8.DecodeRuneInString(s[end:])
				if !run(next) {
					break
				}
				end += n
			}
		}
		out = append(out, s[i:end])
		i = end
	}
	return out
}

// sameClass says what else joins the run this rune starts, nil if it stands alone.
func sameClass(r rune) func(rune) bool {
	switch {
	case isWord(r):
		return isWord
	case unicode.IsSpace(r):
		return unicode.IsSpace
	}
	return nil
}

func isWord(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// align reports which words each side shares. The common head and tail go first,
// leaving the quadratic match below a handful of words to chew on.
func align(a, b []string) ([]bool, []bool, int) {
	aSame := make([]bool, len(a))
	bSame := make([]bool, len(b))

	head := 0
	for head < len(a) && head < len(b) && a[head] == b[head] {
		aSame[head], bSame[head] = true, true
		head++
	}
	tail := 0
	for head+tail < len(a) && head+tail < len(b) && a[len(a)-1-tail] == b[len(b)-1-tail] {
		aSame[len(a)-1-tail], bSame[len(b)-1-tail] = true, true
		tail++
	}

	mid, other := a[head:len(a)-tail], b[head:len(b)-tail]
	n, m := len(mid), len(other)
	if n == 0 || m == 0 {
		return aSame, bSame, 0
	}

	// Longest common subsequence, filled from the end so the walk runs forwards.
	stride := m + 1
	grid := make([]int32, (n+1)*stride)
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case mid[i] == other[j]:
				grid[i*stride+j] = grid[(i+1)*stride+j+1] + 1
			case grid[(i+1)*stride+j] >= grid[i*stride+j+1]:
				grid[i*stride+j] = grid[(i+1)*stride+j]
			default:
				grid[i*stride+j] = grid[i*stride+j+1]
			}
		}
	}
	for i, j := 0, 0; i < n && j < m; {
		switch {
		case mid[i] == other[j]:
			aSame[head+i], bSame[head+j] = true, true
			i, j = i+1, j+1
		case grid[(i+1)*stride+j] >= grid[i*stride+j+1]:
			i++
		default:
			j++
		}
	}
	return aSame, bSame, (n + 1) * stride
}

// runs turns a word-by-word verdict into byte ranges, joining neighbours.
func runs(words []string, same []bool) []span {
	var out []span
	at := 0
	for i, word := range words {
		if !same[i] {
			if n := len(out); n > 0 && out[n-1].end == at {
				out[n-1].end = at + len(word)
			} else {
				out = append(out, span{at, at + len(word)})
			}
		}
		at += len(word)
	}
	return out
}

// mark splits a line's tokens at the span edges and flags the pieces inside one.
// A line with no syntax tokens gets them here: the mark has to hang off something.
func mark(content string, tokens []gitdiff.Token, spans []span) []gitdiff.Token {
	if len(spans) == 0 {
		return tokens
	}
	// A token run that does not rebuild the line cannot be cut by byte offset.
	width := 0
	for _, token := range tokens {
		width += len(token.Text)
	}
	if width != len(content) {
		tokens = []gitdiff.Token{{Text: content}}
	}

	out := make([]gitdiff.Token, 0, len(tokens)+2*len(spans))
	at := 0
	for _, token := range tokens {
		start, end := at, at+len(token.Text)
		at = end
		for cut := start; cut < end; {
			next, edit := boundary(spans, cut, end)
			out = append(out, gitdiff.Token{
				Class: token.Class,
				Text:  token.Text[cut-start : next-start],
				Edit:  edit,
			})
			cut = next
		}
	}
	return out
}

// boundary returns where the run at `at` ends — the next span edge or `end` —
// and whether that run is inside a span.
func boundary(spans []span, at, end int) (int, bool) {
	for _, s := range spans {
		if at < s.start {
			return min(s.start, end), false
		}
		if at < s.end {
			return min(s.end, end), true
		}
	}
	return end, false
}
