package permissions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/owenrumney/ade/internal/settings"
)

type Verdict int

// Ask is the zero value on purpose: a verdict nobody reached is a question.
const (
	Ask Verdict = iota
	Allow
	Deny
)

// Decision is a verdict and why: Rules is what reached it, Unmatched the
// segments nothing covered, which is what the card proposes a rule for.
type Decision struct {
	Verdict   Verdict
	Rules     []Rule
	Unmatched []string
}

// File is ~/.ade/permissions.json.
type File struct {
	Version int    `json:"version"`
	Allow   []Rule `json:"allow"`
	Deny    []Rule `json:"deny"`
}

// Store is the rule file plus the session's own rules, which are never written.
type Store struct {
	path    string
	mu      sync.Mutex
	file    File
	session []Rule

	// free stops the asking for the rest of the session. Deny rules still
	// hold: free is about not being asked, not about the list you wrote.
	free bool
}

// Version is the shape of the file on disk, so a later change can tell.
const Version = 1

const name = "permissions.json"

// Open reads the rule file, seeding it on first run. A file that exists is
// taken as it is: nothing adds a rule the user cannot see.
func Open() (*Store, error) {
	dir, err := settings.Dir()
	if err != nil {
		return nil, err
	}
	return openAt(filepath.Join(dir, name))
}

// Decide is the gate: target is the command for Bash, the path or url otherwise.
func (s *Store) Decide(tool, target string) Decision {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rule := range s.file.Deny {
		if rule.MatchesAny(tool, target) {
			return Decision{Verdict: Deny, Rules: []Rule{rule}}
		}
	}
	if s.free {
		return Decision{Verdict: Allow, Rules: []Rule{FreeRule}}
	}
	if tool != bashTool {
		if rule, ok := s.allows(tool, target); ok {
			return Decision{Verdict: Allow, Rules: []Rule{rule}}
		}
		return Decision{Unmatched: []string{target}}
	}
	segments := Segments(target)
	if len(segments) == 0 {
		return Decision{Unmatched: []string{target}}
	}
	var used []Rule
	var left []string
	for _, segment := range segments {
		rule, ok := s.allows(tool, segment)
		if !ok {
			left = append(left, segment)
			continue
		}
		if !slices.Contains(used, rule) {
			used = append(used, rule)
		}
	}
	if len(left) > 0 {
		return Decision{Rules: used, Unmatched: left}
	}
	return Decision{Verdict: Allow, Rules: used}
}

// Remember adds a rule. Session rules stay in memory; the rest are written.
func (s *Store) Remember(rule Rule, deny, session bool) error {
	if !rule.Valid() {
		return fmt.Errorf("permissions: %q is not a rule", rule)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if session {
		if !slices.Contains(s.session, rule) {
			s.session = append(s.session, rule)
		}
		return nil
	}
	into := &s.file.Allow
	if deny {
		into = &s.file.Deny
	}
	if slices.Contains(*into, rule) {
		return nil
	}
	*into = append(*into, rule)
	return s.save()
}

// Forget drops a rule wherever it is held. A rule nobody can find is a rule
// nobody can withdraw, so this is as much of the feature as Remember.
func (s *Store) Forget(rule Rule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = slices.DeleteFunc(s.session, func(r Rule) bool { return r == rule })
	before := len(s.file.Allow) + len(s.file.Deny)
	s.file.Allow = slices.DeleteFunc(s.file.Allow, func(r Rule) bool { return r == rule })
	s.file.Deny = slices.DeleteFunc(s.file.Deny, func(r Rule) bool { return r == rule })
	if before == len(s.file.Allow)+len(s.file.Deny) {
		return nil
	}
	return s.save()
}

// FreeRule is what a decision names when the session is running free. Not a
// rule the matcher reads: a word for why nothing was asked.
const FreeRule Rule = "running free"

// SetFree stops the asking for this session, or starts it again. Nothing is
// written: a new session asks as it always did.
func (s *Store) SetFree(on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.free = on
}

func (s *Store) IsFree() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.free
}

// Rules is everything in force: what is on disk, and the session's own.
func (s *Store) Rules() (File, []Rule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return File{
		Version: s.file.Version,
		Allow:   slices.Clone(s.file.Allow),
		Deny:    slices.Clone(s.file.Deny),
	}, slices.Clone(s.session)
}

func openAt(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path) // #nosec G304 -- ade's own state directory
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &s.file); err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		return s, nil
	case !os.IsNotExist(err):
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	s.file = seeded()
	if err := s.save(); err != nil {
		return nil, err
	}
	return s, nil
}

// allows is the first rule covering this target, session rules first.
func (s *Store) allows(tool, target string) (Rule, bool) {
	for _, rule := range slices.Concat(s.session, s.file.Allow) {
		if rule.Matches(tool, target) {
			return rule, true
		}
	}
	return "", false
}

// save writes the file, atomically. Called with the lock held.
func (s *Store) save() error {
	s.file.Version = Version
	data, err := json.MarshalIndent(s.file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", name, err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(s.path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".ade-permissions-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", s.path, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", s.path, err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("replace %s: %w", s.path, err)
	}
	return nil
}
