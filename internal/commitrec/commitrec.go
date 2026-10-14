// Package commitrec is one commit's record: what was asked, and what review
// made of it. Markdown, because the shared wiki takes nothing else.
package commitrec

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Ask is one turn's prompt, the intent behind the lines the commit carries.
type Ask struct {
	At        time.Time `json:"at"`
	Session   string    `json:"session,omitempty"`
	MessageID string    `json:"messageId,omitempty"`
	Text      string    `json:"text"`
}

// Claim is a review finding against these lines and what became of it: fixed
// and dismissed are different answers to "why is this here?".
type Claim struct {
	File     string `json:"file"`
	Line     int    `json:"line,omitempty"`
	Author   string `json:"author,omitempty"`
	Category string `json:"category,omitempty"`
	Severity string `json:"severity,omitempty"`
	Fate     string `json:"fate"`
	Text     string `json:"text"`
}

// Record is one commit's answer to "why is this here?". Keys is every patch-id
// it has been known by, newest first, because an amend changes the key.
type Record struct {
	Repo    string    `json:"repo"`
	Keys    []string  `json:"keys"`
	Commit  string    `json:"commit,omitempty"`
	Branch  string    `json:"branch,omitempty"`
	Subject string    `json:"subject,omitempty"`
	At      time.Time `json:"at"`
	PR      int       `json:"pr,omitempty"`
	Merge   string    `json:"merge,omitempty"`
	Asked   []Ask     `json:"asked,omitempty"`
	Review  []Claim   `json:"review,omitempty"`
	Pages   []string  `json:"pages,omitempty"`
}

// Key is the record's current key, the one Path writes it under.
func (r Record) Key() string {
	if len(r.Keys) == 0 {
		return ""
	}
	return r.Keys[0]
}

// Rekey puts a new key at the front, keeping the old ones so a lookup by either
// still lands. Re-keying to a key already held only reorders.
func (r *Record) Rekey(key string) {
	if key == "" || key == r.Key() {
		return
	}
	kept := []string{key}
	for _, k := range r.Keys {
		if k != key {
			kept = append(kept, k)
		}
	}
	r.Keys = kept
}

// Absorb takes on the keys of the record this one replaces, keeping its own at
// the front, so a lookup by the older key still lands.
func (r *Record) Absorb(old Record) {
	held := map[string]bool{}
	for _, k := range r.Keys {
		held[k] = true
	}
	for _, k := range old.Keys {
		if !held[k] {
			r.Keys = append(r.Keys, k)
			held[k] = true
		}
	}
}

// Dir is where a repository's records live in the wiki.
func Dir(repo string) string { return path.Join("repos", repo, "commits") }

// Path is one record's wiki-relative path.
func Path(repo, key string) string { return path.Join(Dir(repo), key+".md") }

// ValidKey reports whether key is a patch-id and so safe to file under. A key
// read back from the wiki becomes a path, and anyone can push to the wiki.
func ValidKey(key string) bool { return keyRE.MatchString(key) }

// Render writes the record as the Markdown that is published.
func Render(r Record) string {
	var b strings.Builder

	subject := r.Subject
	if subject == "" {
		subject = "commit " + short(r.Commit)
	}
	fmt.Fprintf(&b, "# %s\n\n", subject)

	field(&b, "keys", strings.Join(r.Keys, " "))
	field(&b, "commit", r.Commit)
	field(&b, "branch", r.Branch)
	if !r.At.IsZero() {
		field(&b, "at", r.At.UTC().Format(time.RFC3339))
	}
	if r.PR > 0 {
		field(&b, "pr", strconv.Itoa(r.PR))
	}
	field(&b, "merge", r.Merge)

	if len(r.Asked) > 0 {
		b.WriteString("\n## Asked\n")
		for _, a := range r.Asked {
			fmt.Fprintf(&b, "\n### %s\n\n", askHeading(a))
			// Fenced: an ask is whatever was typed, and a heading in it would
			// otherwise read back as this file's own structure.
			fence := fenceFor(a.Text)
			b.WriteString(fence + "\n")
			b.WriteString(strings.TrimRight(a.Text, "\n"))
			b.WriteString("\n" + fence + "\n")
		}
	}

	if len(r.Review) > 0 {
		b.WriteString("\n## Review\n\n")
		for _, c := range r.Review {
			fmt.Fprintf(&b, "- **%s** — %s — %s\n", c.Fate, claimWhere(c), oneLine(c.Text))
		}
	}

	if len(r.Pages) > 0 {
		b.WriteString("\n## Pages\n\n")
		for _, p := range r.Pages {
			fmt.Fprintf(&b, "- %s\n", p)
		}
	}
	return b.String()
}

var (
	fieldRE   = regexp.MustCompile(`^- ([a-z]+): (.*)$`)
	askRE     = regexp.MustCompile(`^### (\S+)(?: — session (\S+))?(?:, message (\S+))?$`)
	claimRE   = regexp.MustCompile(`^- \*\*([a-z]+)\*\* — (.+?) — (.*)$`)
	whereRE   = regexp.MustCompile(`^(.*?)(?::(\d+))?(?: \(([^)]*)\))?$`)
	pageRE    = regexp.MustCompile(`^- (\S+)$`)
	headingRE = regexp.MustCompile(`^## (.+)$`)
	keyRE     = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
	fenceRE   = regexp.MustCompile("^`{3,}$")
)

// Parse reads back what Render wrote. Repo is not in the file — it is the path
// the file was found under — so the caller supplies it.
func Parse(repo, text string) (Record, error) {
	r := Record{Repo: repo}
	section := ""
	fence, opening := "", false
	var ask *Ask

	flush := func() {
		if ask != nil {
			ask.Text = strings.TrimSpace(ask.Text)
			r.Asked = append(r.Asked, *ask)
			ask = nil
		}
	}

	for line := range strings.SplitSeq(text, "\n") {
		// Inside an ask's fence nothing is structure: the text is what was typed.
		if fence != "" {
			if strings.TrimSpace(line) == fence {
				fence = ""
			} else if ask != nil {
				ask.Text += line + "\n"
			}
			continue
		}
		if opening && section == "asked" {
			if trimmed := strings.TrimSpace(line); fenceRE.MatchString(trimmed) {
				fence, opening = trimmed, false
				continue
			}
			opening = strings.TrimSpace(line) == ""
		}
		if strings.HasPrefix(line, "# ") {
			r.Subject = strings.TrimSpace(line[2:])
			continue
		}
		if m := headingRE.FindStringSubmatch(line); m != nil {
			flush()
			section = strings.ToLower(m[1])
			continue
		}
		switch section {
		case "":
			if m := fieldRE.FindStringSubmatch(line); m != nil {
				readField(&r, m[1], strings.TrimSpace(m[2]))
			}
		case "asked":
			if m := askRE.FindStringSubmatch(line); m != nil {
				flush()
				at, _ := time.Parse(time.RFC3339, m[1])
				ask = &Ask{At: at, Session: m[2], MessageID: m[3]}
				opening = true
				continue
			}
			if ask != nil {
				ask.Text += line + "\n"
			}
		case "review":
			if m := claimRE.FindStringSubmatch(line); m != nil {
				r.Review = append(r.Review, readClaim(m))
			}
		case "pages":
			if m := pageRE.FindStringSubmatch(line); m != nil {
				r.Pages = append(r.Pages, m[1])
			}
		}
	}
	flush()

	if len(r.Keys) == 0 {
		return Record{}, fmt.Errorf("commit record has no key")
	}
	return r, nil
}

func readField(r *Record, name, value string) {
	switch name {
	case "keys":
		for _, key := range strings.Fields(value) {
			if ValidKey(key) {
				r.Keys = append(r.Keys, key)
			}
		}
	case "commit":
		r.Commit = value
	case "branch":
		r.Branch = value
	case "at":
		r.At, _ = time.Parse(time.RFC3339, value)
	case "pr":
		r.PR, _ = strconv.Atoi(value)
	case "merge":
		r.Merge = value
	}
}

func readClaim(m []string) Claim {
	c := Claim{Fate: m[1], Text: strings.TrimSpace(m[3])}
	w := whereRE.FindStringSubmatch(strings.TrimSpace(m[2]))
	if w == nil {
		c.File = strings.TrimSpace(m[2])
		return c
	}
	c.File = strings.TrimSpace(w[1])
	if c.File == wholeChange {
		c.File = ""
	}
	c.Line, _ = strconv.Atoi(w[2])
	for i, part := range strings.Split(w[3], ", ") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch i {
		case 0:
			c.Category = part
		case 1:
			c.Severity = part
		default:
			c.Author = part
		}
	}
	return c
}

// fenceFor is a run of backticks longer than any in the text it must enclose.
func fenceFor(text string) string {
	longest, run := 0, 0
	for _, c := range text {
		if c == '`' {
			run++
			longest = max(longest, run)
			continue
		}
		run = 0
	}
	return strings.Repeat("`", max(3, longest+1))
}

func field(b *strings.Builder, name, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(b, "- %s: %s\n", name, value)
}

func askHeading(a Ask) string {
	at := a.At.UTC().Format(time.RFC3339)
	if a.At.IsZero() {
		at = "unknown"
	}
	if a.Session == "" {
		return at
	}
	if a.MessageID == "" {
		return at + " — session " + a.Session
	}
	return at + " — session " + a.Session + ", message " + a.MessageID
}

// wholeChange is how a repo-scoped claim reads: it has no file to name.
const wholeChange = "the change as a whole"

func claimWhere(c Claim) string {
	where := c.File
	if where == "" {
		where = wholeChange
	}
	if c.Line > 0 {
		where += ":" + strconv.Itoa(c.Line)
	}
	var tags []string
	for _, tag := range []string{c.Category, c.Severity, c.Author} {
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	if len(tags) > 0 {
		where += " (" + strings.Join(tags, ", ") + ")"
	}
	return where
}

func oneLine(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= 200 {
		return s
	}
	return s[:200] + "…"
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
