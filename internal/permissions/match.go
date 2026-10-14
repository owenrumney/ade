package permissions

import (
	"regexp"
	"strings"
)

// Rule is one line of permissions.json: "Bash(go test:*)", or a bare "Read"
// for every call of a tool.
type Rule string

type parsed struct {
	tool    string
	content string
	prefix  bool
	bare    bool
}

// bashTool is the one tool whose content is a command line rather than a target.
const bashTool = "Bash"

// noise is what a rule should not have to mention: redirections that change
// nothing a person would want to be asked about.
var noise = []string{"2>&1", "2>/dev/null", ">/dev/null", "1>/dev/null"}

// hexish is a commit sha or other opaque argument, which never generalises.
var hexish = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// Matches reports whether the rule covers this call. Target is the command
// line for Bash, the path or url otherwise.
func (r Rule) Matches(tool, target string) bool {
	p, ok := r.parse()
	if !ok || p.tool != tool {
		return false
	}
	if p.bare {
		return true
	}
	if tool != bashTool {
		return normalise(target) == normalise(p.content)
	}
	parts, opaque := scan(target)
	if opaque {
		return false
	}
	for _, segment := range parts {
		if !p.covers(segment) {
			return false
		}
	}
	return len(parts) > 0
}

// MatchesAny reports whether the rule covers any one segment of the command,
// which is what a deny rule needs: one bad segment condemns the line.
func (r Rule) MatchesAny(tool, target string) bool {
	p, ok := r.parse()
	if !ok || p.tool != tool {
		return false
	}
	if p.bare {
		return true
	}
	if tool != bashTool {
		return normalise(target) == normalise(p.content)
	}
	for _, segment := range Segments(target) {
		if p.covers(segment) {
			return true
		}
	}
	return false
}

func (r Rule) Tool() string {
	p, _ := r.parse()
	return p.tool
}

// Valid reports whether the rule is one the matcher understands. The card
// refuses to save anything else.
func (r Rule) Valid() bool {
	p, ok := r.parse()
	return ok && p.tool != "" && (p.bare || p.content != "")
}

// Segments is the command split into the commands it actually runs. A rule
// covers a segment, which is how few rules cover many pipelines.
func Segments(command string) []string {
	segments, _ := scan(command)
	return segments
}

// scan splits as bash would. opaque is a line whose segments cannot be trusted:
// a quote left open, or an unquoted paren, which is a subshell or a substitution.
func scan(command string) (segments []string, opaque bool) {
	var out []string
	var buf strings.Builder
	var quote rune
	// Noise goes before the split, not after: "2>&1" contains an operator and
	// would otherwise break the segment in half.
	runes := []rune(strip(command))
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		// A backslash escapes outside single quotes, so `\"` opens nothing and
		// `\;` separates nothing.
		if c == '\\' && quote != '\'' && i+1 < len(runes) {
			buf.WriteRune(c)
			buf.WriteRune(runes[i+1])
			i++
			continue
		}
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
			buf.WriteRune(c)
			continue
		case c == '\'' || c == '"':
			quote = c
			buf.WriteRune(c)
			continue
		case c == '(' || c == ')':
			opaque = true
		}
		if c == '\n' || c == ';' || c == '&' || c == '|' {
			// && and || are one operator; a lone & backgrounds and still ends
			// the command.
			if i+1 < len(runes) && runes[i+1] == c {
				i++
			}
			out = append(out, buf.String())
			buf.Reset()
			continue
		}
		buf.WriteRune(c)
	}
	out = append(out, buf.String())
	kept := make([]string, 0, len(out))
	for _, segment := range out {
		if segment = strings.TrimSpace(segment); segment != "" {
			kept = append(kept, segment)
		}
	}
	return kept, opaque || quote != 0
}

func (p parsed) covers(segment string) bool {
	segment = normalise(segment)
	content := normalise(p.content)
	if segment == content {
		return true
	}
	if !p.prefix || unsafe(segment) {
		return false
	}
	return strings.HasPrefix(segment, content+" ")
}

// unsafe is a segment no prefix rule may cover: what it runs is not decided by
// the words a prefix can see.
func unsafe(segment string) bool {
	return strings.Contains(segment, "$(") ||
		strings.Contains(segment, "`") ||
		strings.Contains(segment, ">")
}

// strip drops noise only as a whole token: "2>/dev/nullx" writes a file.
func strip(segment string) string {
	for _, n := range noise {
		var b strings.Builder
		rest := segment
		for {
			at := strings.Index(rest, n)
			if at < 0 {
				b.WriteString(rest)
				break
			}
			end := at + len(n)
			if end == len(rest) || strings.ContainsRune(" \t\n;&|", rune(rest[end])) {
				b.WriteString(rest[:at] + " ")
			} else {
				b.WriteString(rest[:end])
			}
			rest = rest[end:]
		}
		segment = b.String()
	}
	return strings.TrimSpace(segment)
}

// normalise collapses whitespace, so a rule is not defeated by two spaces.
func normalise(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func (r Rule) parse() (parsed, bool) {
	text := strings.TrimSpace(string(r))
	if text == "" {
		return parsed{}, false
	}
	open := strings.Index(text, "(")
	if open < 0 {
		return parsed{tool: text, bare: true}, true
	}
	if !strings.HasSuffix(text, ")") {
		return parsed{}, false
	}
	tool := strings.TrimSpace(text[:open])
	content := text[open+1 : len(text)-1]
	if tool == "" {
		return parsed{}, false
	}
	// Both wildcard spellings: ade writes ":*", and the rules already in a
	// Claude settings file are mostly " *".
	for _, mark := range []string{":*", " *", "*"} {
		if strings.HasSuffix(content, mark) {
			content = strings.TrimSuffix(content, mark)
			return parsed{tool: tool, content: strings.TrimSpace(content), prefix: true}, true
		}
	}
	return parsed{tool: tool, content: strings.TrimSpace(content)}, true
}
