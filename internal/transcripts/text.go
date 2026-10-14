package transcripts

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// noiseTags wrap machinery injected into the user turn: hook output, slash
// commands, environment dumps. Stripped before judging whether a prompt exists.
var noiseTags = []string{
	"system-reminder",
	"command-name",
	"command-message",
	"command-args",
	"command-contents",
	"local-command-stdout",
	"local-command-stderr",
	"local-command-caveat",
	"user-prompt-submit-hook",
	"user-memory-input",
	"environment_context",
	"user_instructions",
	"instructions",
}

// noisePrefixes mark a whole turn as machine-generated.
var noisePrefixes = []string{
	"caveat: the messages below",
	"this session is being continued from a previous conversation",
	"# agents.md instructions",
	"[request interrupted",
	"api error",
}

var whitespaceRE = regexp.MustCompile(`\s+`)

// collapse trims and squeezes whitespace onto one line.
func collapse(text string) string {
	return strings.TrimSpace(whitespaceRE.ReplaceAllString(text, " "))
}

// cleanPrompt strips injected machinery and collapses whitespace, returning ""
// when nothing the user actually typed remains.
func cleanPrompt(text string) string {
	if text == "" {
		return ""
	}
	for _, tag := range noiseTags {
		text = stripTag(text, tag)
	}
	text = collapse(text)
	if text == "" || !hasLetter(text) {
		return ""
	}
	lower := strings.ToLower(text)
	for _, prefix := range noisePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return ""
		}
	}
	return text
}

// truncateRunes cuts to at most limit bytes without splitting a rune. Slicing
// raw bytes here corrupts any multi-byte character it lands inside.
func truncateRunes(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

// truncate shortens to at most width columns, ellipsising when it cuts.
func truncate(text string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

// stripTag removes every <tag>…</tag> span, case-insensitively; an unclosed tag
// drops the remainder. Hand-rolled because RE2 has no backreferences.
func stripTag(text, tag string) string {
	open, closing := "<"+tag, "</"+tag+">"
	for {
		start := indexOpenTag(text, open)
		if start < 0 {
			return text
		}
		gt := strings.IndexByte(text[start:], '>')
		if gt < 0 {
			return text[:start]
		}
		afterOpen := start + gt + 1
		// A self-closing tag has no body to skip.
		if text[afterOpen-2] == '/' {
			text = text[:start] + " " + text[afterOpen:]
			continue
		}
		end := indexFold(text[afterOpen:], closing)
		if end < 0 {
			return text[:start]
		}
		text = text[:start] + " " + text[afterOpen+end+len(closing):]
	}
}

// indexOpenTag finds "<tag" only where tag is the complete element name, so
// <command-name> does not match a search for <command>.
func indexOpenTag(text, open string) int {
	offset := 0
	for {
		i := indexFold(text[offset:], open)
		if i < 0 {
			return -1
		}
		at := offset + i
		next := at + len(open)
		if next >= len(text) || !isNameByte(text[next]) {
			return at
		}
		offset = next
	}
}

// indexFold is the byte offset of the first case-insensitive match of sub in
// text, or -1. Folds Unicode; offsets index text, so callers can slice safely.
func indexFold(text, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	// Fast path for an ASCII first byte, which covers tags and most queries. An
	// ASCII byte only occurs at a rune boundary, so the offset stays valid.
	if sub[0] < utf8.RuneSelf {
		lower, upper := lowerASCII(sub[0]), upperASCII(sub[0])
		for i := 0; i < len(text); i++ {
			if text[i] != lower && text[i] != upper {
				continue
			}
			if hasPrefixFold(text[i:], sub) {
				return i
			}
		}
		return -1
	}
	for i := range text {
		if hasPrefixFold(text[i:], sub) {
			return i
		}
	}
	return -1
}

// hasPrefixFold reports whether text begins with sub under simple case folding.
// Rune by rune, because the two cases can differ in byte length.
func hasPrefixFold(text, sub string) bool {
	offset := 0
	for _, want := range sub {
		if offset >= len(text) {
			return false
		}
		got, size := utf8.DecodeRuneInString(text[offset:])
		if got != want && !equalFoldRune(got, want) {
			return false
		}
		offset += size
	}
	return true
}

// equalFoldRune walks a rune's fold orbit, so É matches é and К matches к.
func equalFoldRune(a, b rune) bool {
	for f := unicode.SimpleFold(a); f != a; f = unicode.SimpleFold(f) {
		if f == b {
			return true
		}
	}
	return false
}

func lowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 'a' - 'A'
	}
	return b
}

func upperASCII(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - 'a' + 'A'
	}
	return b
}

func isNameByte(b byte) bool {
	return b == '-' || b == '_' || b == '.' ||
		(b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z')
}

func hasLetter(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
