// Package gitdiff turns git's unified diff output into the structure the
// browser renders and comments anchor to.
package gitdiff

// FromText builds a Diff from an already-fetched unified diff, for a pull request
// whose branch is not checked out locally.
func FromText(base, head, out string) *Diff {
	return &Diff{Base: base, Head: head, Mode: ModeSnapshot, Files: Parse(out)}
}

// Status values for a changed file.
const (
	StatusAdded    = "added"
	StatusModified = "modified"
	StatusDeleted  = "deleted"
	StatusRenamed  = "renamed"
)

// Line kinds. A cell is one side of a side-by-side row.
const (
	KindContext = "context"
	KindAdd     = "add"
	KindDel     = "del"
)

// Diff is a whole review: every changed file between base and head.
type Diff struct {
	Base  string `json:"base"`
	Head  string `json:"head"`
	Mode  string `json:"mode"`
	Files []File `json:"files"`

	// Note explains an unusual base, such as a repository with no base branch.
	Note string `json:"note,omitempty"`

	// Commits is how many commits HEAD is ahead of the base. Zero means this is
	// the base branch, or nothing has been committed on top of it yet.
	Commits int `json:"commits"`
}

type File struct {
	Path      string `json:"path"`
	OldPath   string `json:"oldPath,omitempty"`
	Status    string `json:"status"`
	Binary    bool   `json:"binary"`
	Untracked bool   `json:"untracked,omitempty"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Hunks     []Hunk `json:"hunks"`
}

type Hunk struct {
	Header   string `json:"header"`
	Heading  string `json:"heading,omitempty"`
	OldStart int    `json:"oldStart"`
	NewStart int    `json:"newStart"`
	Rows     []Row  `json:"rows"`
}

// Row is one side-by-side line pair. A nil cell renders as padding.
type Row struct {
	Left  *Cell `json:"left"`
	Right *Cell `json:"right"`
}

type Cell struct {
	Line      int    `json:"line"`
	Kind      string `json:"kind"`
	Content   string `json:"content"`
	NoNewline bool   `json:"noNewline,omitempty"`

	// Tokens is Content split for syntax highlighting, filled in by the
	// highlight package. Empty means render Content as plain text.
	Tokens []Token `json:"tokens,omitempty"`
}

// Token is one run of Cell.Content. An empty Class is unstyled text, which is
// most of a line, so it stays off the wire.
type Token struct {
	Class string `json:"c,omitempty"`
	Text  string `json:"t"`

	// Edit marks a run that differs from the line this one replaced, filled in
	// by the words package.
	Edit bool `json:"e,omitempty"`
}

// line is the parser's intermediate form, before pairing into rows.
type line struct {
	kind      string
	old       int
	new       int
	content   string
	noNewline bool
}

// pair turns a hunk's sequential lines into side-by-side rows. Runs of deletions
// are zipped against the additions that follow them, as GitHub does.
func pair(lines []line) []Row {
	var rows []Row
	var dels, adds []line

	flush := func() {
		for i := 0; i < len(dels) || i < len(adds); i++ {
			var row Row
			if i < len(dels) {
				row.Left = cellOf(dels[i])
			}
			if i < len(adds) {
				row.Right = cellOf(adds[i])
			}
			rows = append(rows, row)
		}
		dels, adds = nil, nil
	}

	for _, ln := range lines {
		switch ln.kind {
		case KindDel:
			// An addition already seen means this deletion starts a new run.
			if len(adds) > 0 {
				flush()
			}
			dels = append(dels, ln)
		case KindAdd:
			adds = append(adds, ln)
		default:
			flush()
			rows = append(rows, Row{
				Left:  &Cell{Line: ln.old, Kind: KindContext, Content: ln.content, NoNewline: ln.noNewline},
				Right: &Cell{Line: ln.new, Kind: KindContext, Content: ln.content, NoNewline: ln.noNewline},
			})
		}
	}
	flush()
	return rows
}

func cellOf(ln line) *Cell {
	num := ln.new
	if ln.kind == KindDel {
		num = ln.old
	}
	return &Cell{Line: num, Kind: ln.kind, Content: ln.content, NoNewline: ln.noNewline}
}
