// Package prlink turns the file and line references in a review comment into
// links to the code on GitHub. The stored note keeps its plain text: this runs
// on the way out, so what is read here is what was written.
package prlink

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// At is the commit a link points into. Blank fields mean no link can be made,
// and the body goes out as it was written.
type At struct {
	Owner string
	Repo  string
	SHA   string
}

func (a At) ok() bool { return a.Owner != "" && a.Repo != "" && a.SHA != "" }

// Known resolves a bare filename a reviewer typed — "app.ts" — to its path in
// the repository. It returns "" for a name it cannot place, or one that is in
// more than one directory: a link to the wrong file is worse than none.
type Known func(name string) string

// pointed is `path/to/file.go:155` or `app.ts:155-160`, with the backticks a
// reviewer usually wraps it in. The extension is what keeps a bare word out.
var pointed = regexp.MustCompile("(`?)([\\w./-]+\\.[A-Za-z][\\w]*):(\\d+)(?:-(\\d+))?(`?)")

// here is `line 141`, `lines 141-142` and the dash a model writes instead.
var here = regexp.MustCompile(`(?i)\blines?\s+(\d+)(?:\s*[-–—]\s*(\d+))?`)

// linked is a markdown link already written, left alone so its own text is not
// linked again inside it.
var linked = regexp.MustCompile(`\[[^\]]*\]\([^)]*\)`)

// In rewrites one comment. file is the one the comment is pinned to, which is
// what a bare "line 141" means; "" for a comment pinned to nothing.
func In(body string, at At, file string, known Known) string {
	if !at.ok() || strings.TrimSpace(body) == "" {
		return body
	}
	return outsideCode(body, func(text string) string {
		return outside(linked, text, func(text string) string {
			text = pointed.ReplaceAllStringFunc(text, func(match string) string {
				return point(match, at, known)
			})
			if file == "" {
				return text
			}
			return here.ReplaceAllStringFunc(text, func(match string) string {
				return lines(match, at, file)
			})
		})
	})
}

func point(match string, at At, known Known) string {
	parts := pointed.FindStringSubmatch(match)
	open, name, first, last, close := parts[1], parts[2], parts[3], parts[4], parts[5]
	// One backtick without its pair is somebody's prose, not a code span.
	if open != close {
		return match
	}
	where := name
	if !strings.Contains(name, "/") {
		if known == nil {
			return match
		}
		if where = known(name); where == "" {
			return match
		}
	}
	return mark(open+name+":"+span(first, last)+close, at, where, first, last)
}

func lines(match string, at At, file string) string {
	parts := here.FindStringSubmatch(match)
	return mark(match, at, file, parts[1], parts[2])
}

// mark is the markdown link, with the reviewer's own words as its text.
func mark(text string, at At, where, first, last string) string {
	anchor := "#L" + first
	if last != "" {
		anchor += "-L" + last
	}
	return fmt.Sprintf("[%s](https://github.com/%s/%s/blob/%s/%s%s)",
		text, at.Owner, at.Repo, at.SHA, strings.TrimPrefix(where, "/"), anchor)
}

// span keeps the reference reading as it was typed, so 141-142 does not come
// back as 141.
func span(first, last string) string {
	if last == "" {
		return first
	}
	return first + "-" + last
}

// outsideCode runs the rewrite on everything but fenced blocks: a link inside
// one is printed as its markdown rather than followed.
func outsideCode(body string, rewrite func(string) string) string {
	var out []string
	fenced := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			out = append(out, line)
			continue
		}
		if fenced {
			out = append(out, line)
			continue
		}
		out = append(out, rewrite(line))
	}
	return strings.Join(out, "\n")
}

// outside runs the rewrite on what the pattern does not match, leaving every
// match where it was.
func outside(skip *regexp.Regexp, text string, rewrite func(string) string) string {
	var b strings.Builder
	at := 0
	for _, where := range skip.FindAllStringIndex(text, -1) {
		b.WriteString(rewrite(text[at:where[0]]))
		b.WriteString(text[where[0]:where[1]])
		at = where[1]
	}
	b.WriteString(rewrite(text[at:]))
	return b.String()
}

// Or asks each in turn. The change's own paths go first: a name it touches is
// the one the reviewer means, even where the tree has another of that name.
func Or(first, second Known) Known {
	return func(name string) string {
		if first != nil {
			if where := first(name); where != "" {
				return where
			}
		}
		if second == nil {
			return ""
		}
		return second(name)
	}
}

// Files resolves a bare filename against the paths a change touches. A name in
// two directories resolves to neither.
func Files(paths []string) Known {
	by := map[string]string{}
	for _, each := range paths {
		name := path.Base(each)
		if _, twice := by[name]; twice {
			by[name] = ""
			continue
		}
		by[name] = each
	}
	return func(name string) string { return by[name] }
}
