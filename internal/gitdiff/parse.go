package gitdiff

import (
	"regexp"
	"strconv"
	"strings"
)

var hunkRE = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$`)

// Parse reads `git diff` output into files, hunks and side-by-side rows.
func Parse(out string) []File {
	p := &parser{files: []File{}}
	for _, raw := range strings.Split(out, "\n") {
		p.feed(raw)
	}
	p.closeFile()
	// Never nil: this is serialised straight to the browser, and a nil slice
	// marshals as null, which is not something you can iterate.
	return p.files
}

type parser struct {
	files   []File
	file    *File
	hunk    *Hunk
	lines   []line
	oldNext int
	newNext int

	// Remaining lines the hunk header promised. Body lines beyond them are not
	// part of the hunk, which is how the trailing newline of git's output is
	// kept from becoming a phantom context line.
	oldLeft int
	newLeft int
}

func (p *parser) feed(raw string) {
	switch {
	case strings.HasPrefix(raw, "diff --git "):
		p.closeFile()
		p.startFile(raw)
		return
	case p.file == nil:
		return
	}

	// Only before the first hunk: inside one, a deleted "-- x" line reads as
	// "--- x" and would otherwise be swallowed as a file header.
	if p.hunk == nil && p.handleHeader(raw) {
		return
	}
	if m := hunkRE.FindStringSubmatch(raw); m != nil {
		p.closeHunk()
		p.startHunk(raw, m)
		return
	}
	if p.hunk != nil {
		p.handleBody(raw)
	}
}

// handleHeader consumes the metadata lines between `diff --git` and the first
// hunk, and reports whether raw was one of them.
func (p *parser) handleHeader(raw string) bool {
	switch {
	case strings.HasPrefix(raw, "new file mode"):
		p.file.Status = StatusAdded
	case strings.HasPrefix(raw, "deleted file mode"):
		p.file.Status = StatusDeleted
	case strings.HasPrefix(raw, "rename from "):
		p.file.OldPath = unquotePath(strings.TrimPrefix(raw, "rename from "))
		p.file.Status = StatusRenamed
	case strings.HasPrefix(raw, "rename to "):
		p.file.Path = unquotePath(strings.TrimPrefix(raw, "rename to "))
		p.file.Status = StatusRenamed
	case strings.HasPrefix(raw, "Binary files "), strings.HasPrefix(raw, "GIT binary patch"):
		p.file.Binary = true
	case strings.HasPrefix(raw, "--- "), strings.HasPrefix(raw, "+++ "),
		strings.HasPrefix(raw, "index "), strings.HasPrefix(raw, "old mode "),
		strings.HasPrefix(raw, "new mode "), strings.HasPrefix(raw, "similarity index "),
		strings.HasPrefix(raw, "dissimilarity index "), strings.HasPrefix(raw, "copy from "),
		strings.HasPrefix(raw, "copy to "):
	default:
		return false
	}
	return true
}

func (p *parser) handleBody(raw string) {
	if strings.HasPrefix(raw, `\ No newline at end of file`) {
		if n := len(p.lines); n > 0 {
			p.lines[n-1].noNewline = true
		}
		return
	}

	switch {
	case raw == "" || raw[0] == ' ':
		// git writes " " for an empty context line; a bare "" only appears as the
		// trailing artifact of splitting output that ends in a newline.
		if p.oldLeft <= 0 || p.newLeft <= 0 {
			return
		}
		p.lines = append(p.lines, line{kind: KindContext, old: p.oldNext, new: p.newNext, content: strings.TrimPrefix(raw, " ")})
		p.oldNext++
		p.newNext++
		p.oldLeft--
		p.newLeft--
	case raw[0] == '+':
		if p.newLeft <= 0 {
			return
		}
		p.lines = append(p.lines, line{kind: KindAdd, new: p.newNext, content: raw[1:]})
		p.newNext++
		p.newLeft--
		p.file.Additions++
	case raw[0] == '-':
		if p.oldLeft <= 0 {
			return
		}
		p.lines = append(p.lines, line{kind: KindDel, old: p.oldNext, content: raw[1:]})
		p.oldNext++
		p.oldLeft--
		p.file.Deletions++
	}
}

func (p *parser) startFile(raw string) {
	old, new := splitDiffGit(raw)
	p.file = &File{Path: new, Status: StatusModified}
	if old != new && old != "" {
		p.file.OldPath = old
	}
}

func (p *parser) startHunk(raw string, m []string) {
	p.oldNext = atoi(m[1])
	p.newNext = atoi(m[3])
	p.oldLeft = count(m[2])
	p.newLeft = count(m[4])
	p.hunk = &Hunk{
		Header:   strings.SplitN(raw, " @@", 2)[0] + " @@",
		Heading:  strings.TrimSpace(m[5]),
		OldStart: p.oldNext,
		NewStart: p.newNext,
	}
}

func (p *parser) closeHunk() {
	if p.hunk == nil {
		return
	}
	p.hunk.Rows = pair(p.lines)
	p.file.Hunks = append(p.file.Hunks, *p.hunk)
	p.hunk, p.lines = nil, nil
}

func (p *parser) closeFile() {
	if p.file == nil {
		return
	}
	p.closeHunk()
	p.files = append(p.files, *p.file)
	p.file = nil
}

// splitDiffGit pulls the a/ and b/ paths out of a `diff --git` line. Quoted
// paths are unambiguous; unquoted ones are split on the " b/" separator.
func splitDiffGit(raw string) (string, string) {
	rest := strings.TrimPrefix(raw, "diff --git ")
	if strings.HasPrefix(rest, `"`) {
		if a, b, ok := splitQuotedPair(rest); ok {
			return stripPrefix(a), stripPrefix(b)
		}
	}
	idx := strings.LastIndex(rest, " b/")
	if idx < 0 {
		return "", strings.TrimSpace(rest)
	}
	return stripPrefix(rest[:idx]), stripPrefix(rest[idx+1:])
}

func splitQuotedPair(rest string) (string, string, bool) {
	end := indexUnescapedQuote(rest[1:])
	if end < 0 {
		return "", "", false
	}
	a := rest[:end+2]
	b := strings.TrimSpace(rest[end+2:])
	return unquotePath(a), unquotePath(b), b != ""
}

func indexUnescapedQuote(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '"' {
			return i
		}
	}
	return -1
}

func stripPrefix(path string) string {
	path = unquotePath(strings.TrimSpace(path))
	for _, prefix := range []string{"a/", "b/"} {
		if strings.HasPrefix(path, prefix) {
			return path[2:]
		}
	}
	return path
}

func unquotePath(path string) string {
	path = strings.TrimSpace(path)
	if len(path) < 2 || path[0] != '"' {
		return path
	}
	if unquoted, err := strconv.Unquote(path); err == nil {
		return unquoted
	}
	return strings.Trim(path, `"`)
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// count reads a hunk header's line count, which is omitted when it is 1.
func count(s string) int {
	if s == "" {
		return 1
	}
	return atoi(s)
}
