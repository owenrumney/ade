package comments

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/gitdiff"
)

// Sides of a side-by-side row. Left is the base revision, right is the new one.
const (
	SideLeft  = "left"
	SideRight = "right"
)

// reanchorWindow bounds how far from its old position a thread may be found
// again, so a coincidental content match far away is not mistaken for the line.
const reanchorWindow = 200

// Anchor pins a thread to a line by content rather than position, so it can be
// found again after the agent edits the file.
type Anchor struct {
	ContextHash string `json:"contextHash"`
	Content     string `json:"content"`
}

// Locate builds an anchor for a line, reporting false when no such line exists.
func Locate(diff *gitdiff.Diff, file, side string, lineNo int) (Anchor, bool) {
	f := findFile(diff, file)
	if f == nil {
		return Anchor{}, false
	}
	for _, hunk := range f.Hunks {
		cells := cellsOn(hunk, side)
		for i, c := range cells {
			if c.Line == lineNo {
				return Anchor{ContextHash: hashAt(cells, i), Content: c.Content}, true
			}
		}
	}
	return Anchor{}, false
}

// HasFile reports whether the diff contains a file at all, which is a
// different question from whether a line in it can be anchored.
func HasFile(diff *gitdiff.Diff, file string) bool {
	return findFile(diff, file) != nil
}

// LocateNearest anchors to lineNo, or to the closest anchorable line within
// window, returning the line it settled on.
func LocateNearest(diff *gitdiff.Diff, file, side string, lineNo, window int) (Anchor, int, bool) {
	if anchor, ok := Locate(diff, file, side, lineNo); ok {
		return anchor, lineNo, true
	}
	f := findFile(diff, file)
	if f == nil {
		return Anchor{}, lineNo, false
	}

	best, found := 0, false
	for _, hunk := range f.Hunks {
		for _, c := range cellsOn(hunk, side) {
			if absDiff(c.Line, lineNo) > window {
				continue
			}
			if !found || absDiff(c.Line, lineNo) < absDiff(best, lineNo) {
				best, found = c.Line, true
			}
		}
	}
	if !found {
		return Anchor{}, lineNo, false
	}
	anchor, ok := Locate(diff, file, side, best)
	return anchor, best, ok
}

// Find is where anchored content now lives on the diff, or false when it has
// gone. near is where it used to be: a coincidental content match further than
// reanchorWindow from it is not the line, and an exact hit at near wins.
func Find(diff *gitdiff.Diff, file, side string, anchor Anchor, near int) (int, bool) {
	f := findFile(diff, file)
	if f == nil {
		return 0, false
	}

	best, found := 0, false
	for _, hunk := range f.Hunks {
		cells := cellsOn(hunk, side)
		for i, c := range cells {
			if hashAt(cells, i) != anchor.ContextHash {
				continue
			}
			if c.Line == near {
				return near, true
			}
			if !found || absDiff(c.Line, near) < absDiff(best, near) {
				best, found = c.Line, true
			}
		}
	}
	if found && absDiff(best, near) <= reanchorWindow {
		return best, true
	}
	return 0, false
}

// reanchor moves a thread to wherever its anchored content now lives, marking it
// orphaned when the content is gone. The clock starts on the pass that first
// finds it gone, and is reset by a pass that finds it again.
func reanchor(t *Thread, diff *gitdiff.Diff, now time.Time) {
	if t.Scope == ScopeRepo {
		t.Orphaned, t.OrphanedAt = false, nil
		return
	}
	line, ok := Find(diff, t.File, t.Side, t.Anchor, t.Line)
	if !ok {
		t.Orphaned = true
		if t.OrphanedAt == nil {
			when := now
			t.OrphanedAt = &when
		}
		return
	}
	t.Line = line
	t.Orphaned, t.OrphanedAt = false, nil
}

// hashAt fingerprints a line together with its immediate neighbours in the same
// hunk, so an unrelated hunk elsewhere in the file does not invalidate it.
func hashAt(cells []*gitdiff.Cell, i int) string {
	var b strings.Builder
	for _, j := range []int{i - 1, i, i + 1} {
		if j >= 0 && j < len(cells) {
			b.WriteString(cells[j].Content)
		}
		b.WriteByte(0)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:8])
}

func cellsOn(hunk gitdiff.Hunk, side string) []*gitdiff.Cell {
	cells := make([]*gitdiff.Cell, 0, len(hunk.Rows))
	for i := range hunk.Rows {
		cell := hunk.Rows[i].Right
		if side == SideLeft {
			cell = hunk.Rows[i].Left
		}
		if cell != nil {
			cells = append(cells, cell)
		}
	}
	return cells
}

func findFile(diff *gitdiff.Diff, path string) *gitdiff.File {
	if diff == nil {
		return nil
	}
	for i := range diff.Files {
		if diff.Files[i].Path == path {
			return &diff.Files[i]
		}
	}
	return nil
}

func absDiff(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}
