// Package lineage records which turn of the conversation changed which lines,
// so a line in the diff can be traced back to the exchange that produced it.
//
// A line is attributed to the turn that changed it, not to the commit that
// later carried it. Lineage is branch-local and dies with the branch: the store
// is keyed by the slugged branch name the way comments keys its threads, and a
// rebase is not chased.
package lineage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/statedir"
)

// Range is a run of lines one turn wrote, pinned by content rather than
// position. Anchor is the range's first line: anchors survive edits above them,
// which a bare line number does not.
type Range struct {
	File   string          `json:"file"`
	Start  int             `json:"start"`
	Lines  int             `json:"lines"`
	Anchor comments.Anchor `json:"anchor"`

	// Commit is the sha this range landed in. A turn can land in two commits,
	// or in one commit and a staged hunk, so the stamp is per range.
	Commit string `json:"commit,omitempty"`
}

// Entry is one turn and what it changed.
type Entry struct {
	// Session is the agent session, and MessageID the message within it that
	// this turn began with: your prompt, which is the answer to "why is this
	// line here".
	Session   string    `json:"session"`
	MessageID string    `json:"messageId"`
	At        time.Time `json:"at"`

	// Summary is a line of the prompt, so the gutter can say what the turn was
	// about without loading the conversation.
	Summary string `json:"summary"`

	// Prompt is the ask in full: the agent's transcript is deleted on its
	// retention period, so a record assembled later has nothing to read.
	Prompt string `json:"prompt,omitempty"`

	Ranges []Range `json:"ranges"`
}

// Turn is one turn that touched a line, without the ranges that got us there.
type Turn struct {
	Session   string    `json:"session"`
	MessageID string    `json:"messageId"`
	At        time.Time `json:"at"`
	Summary   string    `json:"summary"`
	Commit    string    `json:"commit,omitempty"`

	// Prompt is the ask in full, and stays on this side: the browser has the
	// summary, and a record decides for itself what it publishes.
	Prompt string `json:"-"`
}

// Store is a branch's lineage.
type Store struct {
	mu      sync.Mutex
	path    string
	entries []Entry
}

// maxSummary bounds a stored prompt line. The gutter shows it on one row.
const maxSummary = 200

// Open opens the lineage for a branch, creating it on first write.
func Open(gitDir, branch string) (*Store, error) {
	path := filepath.Join(statedir.Git(gitDir), comments.Slug(branch)+".lineage.json")
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read lineage: %w", err)
	}
	if err := json.Unmarshal(data, &s.entries); err != nil {
		return nil, fmt.Errorf("parse lineage: %w", err)
	}
	return s, nil
}

// Path is where this store is written.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// Add records a turn. An entry with no ranges is dropped: a turn that wrote
// nothing has no lineage to show.
func (s *Store) Add(e Entry) error {
	if s == nil || len(e.Ranges) == 0 {
		return nil
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	e.Summary = clip(e.Summary)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e)
	return s.save()
}

// List is every entry, oldest first.
func (s *Store) List() []Entry {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Entry(nil), s.entries...)
}

// At is the turns that touched a line of a file, newest first. Ranges are
// re-found against the current diff, so edits above a line do not misattribute
// it; a range whose content has gone is skipped rather than guessed at.
func (s *Store) At(diff *gitdiff.Diff, file string, line int) []Turn {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []Turn
	for i := len(s.entries) - 1; i >= 0; i-- {
		e := s.entries[i]
		for _, r := range e.Ranges {
			if r.File != file || !covers(diff, r, line) {
				continue
			}
			out = append(out, Turn{
				Session:   e.Session,
				MessageID: e.MessageID,
				At:        e.At,
				Summary:   e.Summary,
				Prompt:    e.Prompt,
				Commit:    r.Commit,
			})
			break
		}
	}
	return out
}

// Marks is the lines of each file that have lineage, so the diff can draw its
// gutter without asking per line.
func (s *Store) Marks(diff *gitdiff.Diff) map[string][]int {
	if s == nil {
		return map[string][]int{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := map[string]map[int]bool{}
	for _, e := range s.entries {
		for _, r := range e.Ranges {
			start, ok := where(diff, r)
			if !ok {
				continue
			}
			lines := seen[r.File]
			if lines == nil {
				lines = map[int]bool{}
				seen[r.File] = lines
			}
			for n := start; n < start+r.Lines; n++ {
				lines[n] = true
			}
		}
	}

	out := make(map[string][]int, len(seen))
	for file, lines := range seen {
		nums := make([]int, 0, len(lines))
		for n := range lines {
			nums = append(nums, n)
		}
		sort.Ints(nums)
		out[file] = nums
	}
	return out
}

// Prune drops entries for files no longer in the diff. Lineage grows one entry
// per turn and nothing else removes them.
func (s *Store) Prune(diff *gitdiff.Diff) error {
	if s == nil || diff == nil {
		return nil
	}
	live := map[string]bool{}
	for i := range diff.Files {
		live[diff.Files[i].Path] = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	kept := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		ranges := make([]Range, 0, len(e.Ranges))
		for _, r := range e.Ranges {
			if live[r.File] {
				ranges = append(ranges, r)
			}
		}
		if len(ranges) == 0 {
			continue
		}
		e.Ranges = ranges
		kept = append(kept, e)
	}
	if len(kept) == len(s.entries) {
		return nil
	}
	s.entries = kept
	return s.save()
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("create lineage dir: %w", err)
	}
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encode lineage: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write lineage: %w", err)
	}
	return nil
}

// where is a range's first line on the current diff.
func where(diff *gitdiff.Diff, r Range) (int, bool) {
	return comments.Find(diff, r.File, comments.SideRight, r.Anchor, r.Start)
}

func covers(diff *gitdiff.Diff, r Range, line int) bool {
	start, ok := where(diff, r)
	if !ok {
		return false
	}
	return line >= start && line < start+r.Lines
}

func clip(s string) string {
	if len(s) <= maxSummary {
		return s
	}
	return s[:maxSummary] + "…"
}
