// Package ledger counts how much of what the agent wrote a human has read: the
// window reports what a viewport held, lineage says what the agent wrote.
package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/statedir"
)

// The dwell bands, in milliseconds. Under Skim is a glance and earns nothing,
// Read is what a line needs, and Cap is the most one range can earn.
const (
	Skim = 400
	Read = 1000
	Cap  = 8000
)

type Span struct {
	File  string `json:"file"`
	Start int    `json:"start"`
	Lines int    `json:"lines"`
}

type Seen struct {
	Span
	MS int `json:"ms"`
}

// maxSpan bounds one reported range. A viewport holds tens of lines and the
// longest hunk far fewer than this, so anything bigger is not a diff.
const maxSpan = 2000

// record is one line the window has reported, pinned by content so an edit
// above it does not move it and a rewrite of the line itself drops it.
type record struct {
	File   string          `json:"file"`
	Line   int             `json:"line"`
	Anchor comments.Anchor `json:"anchor"`

	MS   int  `json:"ms,omitempty"`
	Tick bool `json:"tick,omitempty"`
}

// sane rejects a span no drawn diff could have reported, which is what stops a
// posted line count from being a loop the server cannot finish.
func sane(span Span) bool {
	return span.File != "" && span.Start >= 1 && span.Lines >= 1 && span.Lines <= maxSpan
}

type Store struct {
	mu      sync.Mutex
	path    string
	records []record
}

func Open(gitDir, branch string) (*Store, error) {
	s := &Store{path: Path(gitDir, branch)}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read ledger: %w", err)
	}
	if err := json.Unmarshal(data, &s.records); err != nil {
		return nil, fmt.Errorf("parse ledger: %w", err)
	}
	return s, nil
}

// Path is where a branch's ledger is written.
func Path(gitDir, branch string) string {
	return filepath.Join(statedir.Git(gitDir), comments.Slug(branch)+".ledger.json")
}

// Note adds the dwell the window reported, a line at a time. A line the diff
// cannot anchor is dropped: it is not on screen any more, whatever was.
func (s *Store) Note(diff *gitdiff.Diff, seen []Seen) error {
	if s == nil || len(seen) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	for _, one := range seen {
		if one.MS < Skim || !sane(one.Span) {
			continue
		}
		for line := one.Start; line < one.Start+one.Lines; line++ {
			at := s.find(diff, one.File, line, false)
			if at < 0 {
				anchor, ok := comments.Locate(diff, one.File, comments.SideRight, line)
				if !ok {
					continue
				}
				s.records = append(s.records, record{File: one.File, Line: line, Anchor: anchor})
				at = len(s.records) - 1
			}
			if s.records[at].MS >= Cap {
				continue
			}
			s.records[at].MS = min(s.records[at].MS+one.MS, Cap)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.save()
}

func (s *Store) Tick(diff *gitdiff.Diff, span Span, on bool) error {
	if s == nil || !sane(span) {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	for line := span.Start; line < span.Start+span.Lines; line++ {
		at := s.find(diff, span.File, line, true)
		switch {
		case on && at >= 0:
		case on:
			anchor, ok := comments.Locate(diff, span.File, comments.SideRight, line)
			if !ok {
				continue
			}
			s.records = append(s.records, record{
				File: span.File, Line: line, Anchor: anchor, Tick: true,
			})
			changed = true
		case at < 0:
		default:
			s.records = append(s.records[:at], s.records[at+1:]...)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return s.save()
}

type FileCount struct {
	Path   string `json:"path"`
	Agent  int    `json:"agent"`
	Read   int    `json:"read"`
	Unread int    `json:"unread"`

	// Viewed is whether every hunk of the file carries a tick.
	Viewed bool   `json:"viewed"`
	Ticked []Span `json:"ticked"`
}

type Report struct {
	Files  []FileCount `json:"files"`
	Agent  int         `json:"agent"`
	Read   int         `json:"read"`
	Unread int         `json:"unread"`
}

// Count is what has been read of what the agent wrote. agent is
// lineage.Marks(diff): the lines of each file some turn wrote.
func (s *Store) Count(diff *gitdiff.Diff, agent map[string][]int) Report {
	out := Report{Files: []FileCount{}}
	if diff == nil {
		return out
	}
	dwelt, ticked := s.placed(diff)

	for i := range diff.Files {
		path := diff.Files[i].Path
		count := FileCount{Path: path, Ticked: spansOf(path, ticked[path]),
			Viewed: allTicked(&diff.Files[i], ticked[path])}
		for _, line := range agent[path] {
			count.Agent++
			if dwelt[path][line] && ticked[path][line] {
				count.Read++
			}
		}
		count.Unread = count.Agent - count.Read
		out.Files = append(out.Files, count)
		out.Agent += count.Agent
		out.Read += count.Read
		out.Unread += count.Unread
	}
	return out
}

// Unread is the agent lines nobody has read, which is what the gate and the
// commit trailer report.
func (s *Store) Unread(diff *gitdiff.Diff, agent map[string][]int) (unread, total int) {
	report := s.Count(diff, agent)
	return report.Unread, report.Agent
}

// Prune drops records the diff can no longer place: a file that has gone, or a
// line the agent has rewritten since it was read.
func (s *Store) Prune(diff *gitdiff.Diff) error {
	if s == nil || diff == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	kept := make([]record, 0, len(s.records))
	for _, r := range s.records {
		if _, ok := comments.Find(diff, r.File, comments.SideRight, r.Anchor, r.Line); ok {
			kept = append(kept, r)
		}
	}
	if len(kept) == len(s.records) {
		return nil
	}
	s.records = kept
	return s.save()
}

// Drop throws the whole ledger away, which is what a rebase does to it: the
// lines it read are not the lines being shipped any more.
func (s *Store) Drop() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.records = nil
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove ledger: %w", err)
	}
	return nil
}

// placed is the lines the records still find on the current diff: those dwelt
// on long enough to count, and those under a tick.
func (s *Store) placed(diff *gitdiff.Diff) (dwelt, ticked map[string]map[int]bool) {
	dwelt, ticked = map[string]map[int]bool{}, map[string]map[int]bool{}
	if s == nil {
		return dwelt, ticked
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.records {
		if !r.Tick && r.MS < Read {
			continue
		}
		line, ok := comments.Find(diff, r.File, comments.SideRight, r.Anchor, r.Line)
		if !ok {
			continue
		}
		into := dwelt
		if r.Tick {
			into = ticked
		}
		if into[r.File] == nil {
			into[r.File] = map[int]bool{}
		}
		into[r.File][line] = true
	}
	return dwelt, ticked
}

// find is the index of a record for a line, matched on content rather than
// position. Caller holds s.mu.
func (s *Store) find(diff *gitdiff.Diff, file string, line int, tick bool) int {
	anchor, ok := comments.Locate(diff, file, comments.SideRight, line)
	if !ok {
		return -1
	}
	for i, r := range s.records {
		if r.Tick == tick && r.File == file && r.Anchor.ContextHash == anchor.ContextHash {
			return i
		}
	}
	return -1
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("create ledger dir: %w", err)
	}
	data, err := json.MarshalIndent(s.records, "", "  ")
	if err != nil {
		return fmt.Errorf("encode ledger: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write ledger: %w", err)
	}
	return nil
}

// allTicked reports whether every changed line of a file is under a tick. A
// file with no changed lines on the right is not viewed by omission.
func allTicked(file *gitdiff.File, ticked map[int]bool) bool {
	lines := 0
	for _, hunk := range file.Hunks {
		for i := range hunk.Rows {
			cell := hunk.Rows[i].Right
			if cell == nil || cell.Kind == gitdiff.KindContext {
				continue
			}
			lines++
			if !ticked[cell.Line] {
				return false
			}
		}
	}
	return lines > 0
}

// spansOf is the ticked lines as runs, so the window can draw a hunk's box
// without being told line by line.
func spansOf(path string, lines map[int]bool) []Span {
	nums := make([]int, 0, len(lines))
	for n := range lines {
		nums = append(nums, n)
	}
	sort.Ints(nums)

	out := []Span{}
	for i, n := range nums {
		if i > 0 && n == nums[i-1]+1 {
			out[len(out)-1].Lines++
			continue
		}
		out = append(out, Span{File: path, Start: n, Lines: 1})
	}
	return out
}
