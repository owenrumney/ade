// Package highlight adds subtle syntax classes to a diff's cells.
package highlight

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/owenrumney/ade/internal/gitdiff"
)

// maxCells bounds the work on a huge diff. Past it the remaining files render
// as plain text rather than stalling the poll.
const maxCells = 40000

// classes maps chroma token categories onto the few CSS classes the diff needs.
// Anything not listed renders unstyled, which keeps most of a line off the wire.
var classes = map[chroma.TokenType]string{
	chroma.Keyword:        "k",
	chroma.Name:           "",
	chroma.NameFunction:   "f",
	chroma.NameClass:      "t",
	chroma.NameBuiltin:    "f",
	chroma.NameDecorator:  "f",
	chroma.NameTag:        "t",
	chroma.NameAttribute:  "t",
	chroma.LiteralString:  "s",
	chroma.LiteralNumber:  "n",
	chroma.Comment:        "c",
	chroma.CommentPreproc: "c",
	chroma.Operator:       "o",
	chroma.Punctuation:    "o",
}

// Apply fills in Tokens for every cell it can lex. Files with no known lexer are
// left alone, so an unrecognised language still renders as plain text.
func Apply(diff *gitdiff.Diff) {
	if diff == nil {
		return
	}
	budget := maxCells
	for i := range diff.Files {
		file := &diff.Files[i]
		if file.Binary || budget <= 0 {
			continue
		}
		lexer := lexerFor(file.Path)
		if lexer == nil {
			continue
		}
		for j := range file.Hunks {
			budget -= applyHunk(lexer, &file.Hunks[j])
		}
	}
}

// Lines highlights a whole file, returning one token run per source line. An
// unrecognised extension gives back nils, which render as plain text — the same
// fallback Apply takes for a diff.
func Lines(path string, source []string) [][]gitdiff.Token {
	lexer := lexerFor(path)
	if lexer == nil {
		return make([][]gitdiff.Token, len(source))
	}
	lines := tokenise(lexer, source)
	for i := range lines {
		if !styled(lines[i]) {
			lines[i] = nil
		}
	}
	return lines
}

// lexerFor picks a lexer by filename, ignoring the plaintext fallback so an
// unknown extension costs nothing.
func lexerFor(path string) chroma.Lexer {
	lexer := lexers.Match(path)
	if lexer == nil {
		return nil
	}
	return chroma.Coalesce(lexer)
}

// applyHunk lexes each side of the hunk as one block, so a string or comment
// spanning several lines is still understood, then hands the tokens back out
// line by line. It returns how many cells it touched.
func applyHunk(lexer chroma.Lexer, hunk *gitdiff.Hunk) int {
	left := make([]*gitdiff.Cell, 0, len(hunk.Rows))
	right := make([]*gitdiff.Cell, 0, len(hunk.Rows))
	for i := range hunk.Rows {
		if cell := hunk.Rows[i].Left; cell != nil {
			left = append(left, cell)
		}
		if cell := hunk.Rows[i].Right; cell != nil {
			right = append(right, cell)
		}
	}
	applySide(lexer, left)
	applySide(lexer, right)
	return len(left) + len(right)
}

func applySide(lexer chroma.Lexer, cells []*gitdiff.Cell) {
	if len(cells) == 0 {
		return
	}
	source := make([]string, len(cells))
	for i, cell := range cells {
		source[i] = cell.Content
	}

	lines := tokenise(lexer, source)
	for i, cell := range cells {
		if styled(lines[i]) {
			cell.Tokens = lines[i]
		}
	}
}

// tokenise lexes the lines as one block, so a string or comment spanning several
// of them is still understood, then hands the tokens back out line by line. The
// result always has one entry per source line, empty where nothing was styled.
func tokenise(lexer chroma.Lexer, source []string) [][]gitdiff.Token {
	lines := make([][]gitdiff.Token, len(source))

	iterator, err := lexer.Tokenise(nil, strings.Join(source, "\n"))
	if err != nil {
		return lines
	}

	at := 0
	for _, token := range iterator.Tokens() {
		class := classFor(token.Type)
		// A token's value can span lines, so it is split back out and the line
		// cursor advanced for each newline it carries.
		parts := strings.Split(token.Value, "\n")
		for i, part := range parts {
			if i > 0 {
				at++
			}
			if at >= len(lines) {
				break
			}
			if part != "" {
				lines[at] = appendRun(lines[at], class, part)
			}
		}
	}
	return lines
}

// appendRun merges into the previous token when the class matches. Chroma
// coalesces by its own token types, and several of those collapse onto one class
// here, so this is where most of the payload is saved.
func appendRun(line []gitdiff.Token, class, text string) []gitdiff.Token {
	if n := len(line); n > 0 && line[n-1].Class == class {
		line[n-1].Text += text
		return line
	}
	return append(line, gitdiff.Token{Class: class, Text: text})
}

// classFor walks up the token type to its category, so NameFunction and its
// subtypes land on the same class without listing every one.
func classFor(t chroma.TokenType) string {
	for _, candidate := range []chroma.TokenType{t, t.SubCategory(), t.Category()} {
		if class, ok := classes[candidate]; ok {
			return class
		}
	}
	return ""
}

// styled reports whether a line has anything worth sending. A line of entirely
// unstyled text renders identically from Content alone.
func styled(tokens []gitdiff.Token) bool {
	for _, token := range tokens {
		if token.Class != "" {
			return true
		}
	}
	return false
}
