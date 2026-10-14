package memory

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// The states an entry can be in. Proposed is not among them: a candidate lives
// in proposed.json until it is accepted or rejected.
const (
	StateAccepted   = "accepted"
	StateSuperseded = "superseded"
)

// Entry is one thing ade knows about the project.
type Entry struct {
	ID      string `json:"id"`
	Heading string `json:"heading"`
	Body    string `json:"body"`

	// Session is the transcript this came from, so a claim can be checked.
	Session string `json:"session,omitempty"`
	Source  string `json:"source,omitempty"`

	State string `json:"state"`
	// Replaces is the entry this one superseded, when it did.
	Replaces string `json:"replaces,omitempty"`

	Added time.Time `json:"added"`
	// LastSeen is when a run last found this fact again. Shown as unconfirmed
	// when that was long ago, never dropped.
	LastSeen time.Time `json:"lastSeen,omitempty"`
}

// Candidate is an entry a run proposed and nobody has ruled on.
type Candidate struct {
	Entry
	// ReplacesHeading is what the run said it supersedes, by heading, before
	// the heading has been matched to an entry.
	ReplacesHeading string `json:"replacesHeading,omitempty"`
}

// memoryState is memory.json.
type memoryState struct {
	Entries []Entry `json:"entries"`
	// Rejected is the fingerprint of every candidate the user threw out, so a
	// later run over the same ground does not propose it again.
	Rejected []string `json:"rejected,omitempty"`
}

// Entries is every entry, accepted first, newest first within each state.
func (s *Store) Entries() []Entry {
	state := s.memory()
	sort.SliceStable(state.Entries, func(i, j int) bool {
		if (state.Entries[i].State == StateAccepted) != (state.Entries[j].State == StateAccepted) {
			return state.Entries[i].State == StateAccepted
		}
		return state.Entries[i].Added.After(state.Entries[j].Added)
	})
	return state.Entries
}

// Accepted is what reaches a prompt.
func (s *Store) Accepted() []Entry {
	var out []Entry
	for _, e := range s.Entries() {
		if e.State == StateAccepted {
			out = append(out, e)
		}
	}
	return out
}

// Proposed is what is waiting to be reviewed.
func (s *Store) Proposed() []Candidate {
	var out []Candidate
	readJSON(s.path(proposedFile), &out)
	return out
}

// Propose adds candidates, dropping any the user has already rejected and any
// already waiting. A run over ground it has covered before is expected.
func (s *Store) Propose(found []Candidate) (added int, err error) {
	writeMu.Lock()
	defer writeMu.Unlock()

	state := s.memory()
	rejected := map[string]bool{}
	for _, print := range state.Rejected {
		rejected[print] = true
	}

	var waiting []Candidate
	readJSON(s.path(proposedFile), &waiting)
	seen := map[string]bool{}
	for _, c := range waiting {
		seen[Fingerprint(c.Heading, c.Body)] = true
	}

	for _, c := range found {
		c.Heading, c.Body = strings.TrimSpace(c.Heading), strings.TrimSpace(c.Body)
		if c.Heading == "" || c.Body == "" {
			continue
		}
		print := Fingerprint(c.Heading, c.Body)
		if rejected[print] || seen[print] {
			continue
		}
		seen[print] = true
		if c.ID == "" {
			c.ID = newID()
		}
		c.Added = time.Now()
		waiting = append(waiting, c)
		added++
	}
	if added == 0 {
		return 0, nil
	}
	return added, writeJSON(s.path(proposedFile), waiting)
}

// Confirm records that a run saw these facts again, by fingerprint. It is what
// makes an unconfirmed entry mean something.
func (s *Store) Confirm(prints ...string) error {
	writeMu.Lock()
	defer writeMu.Unlock()

	wanted := map[string]bool{}
	for _, print := range prints {
		wanted[print] = true
	}
	state := s.memory()
	now, changed := time.Now(), false
	for i, e := range state.Entries {
		if e.State == StateAccepted && wanted[Fingerprint(e.Heading, e.Body)] {
			state.Entries[i].LastSeen = now
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return writeJSON(s.path(memoryFile), state)
}

// Accept moves a candidate into memory; heading and body override what was
// proposed. A candidate naming what it replaces supersedes it here.
func (s *Store) Accept(id, heading, body string) (Entry, error) {
	writeMu.Lock()
	defer writeMu.Unlock()

	var waiting []Candidate
	readJSON(s.path(proposedFile), &waiting)
	found, rest := takeCandidate(waiting, id)
	if found == nil {
		return Entry{}, fmt.Errorf("no proposed entry %s", id)
	}

	entry := found.Entry
	if heading = strings.TrimSpace(heading); heading != "" {
		entry.Heading = heading
	}
	if body = strings.TrimSpace(body); body != "" {
		entry.Body = body
	}
	if entry.Heading == "" || entry.Body == "" {
		return Entry{}, fmt.Errorf("an entry needs a heading and a body")
	}
	entry.State, entry.Added, entry.LastSeen = StateAccepted, time.Now(), time.Now()

	state := s.memory()
	if target := resolveReplaces(state.Entries, entry.Replaces, found.ReplacesHeading); target != "" {
		entry.Replaces = target
		for i := range state.Entries {
			if state.Entries[i].ID == target {
				state.Entries[i].State = StateSuperseded
			}
		}
	} else {
		entry.Replaces = ""
	}
	state.Entries = append(state.Entries, entry)

	if err := writeJSON(s.path(memoryFile), state); err != nil {
		return Entry{}, err
	}
	return entry, writeJSON(s.path(proposedFile), rest)
}

// Reject drops a candidate and remembers not to propose it again.
func (s *Store) Reject(id string) error {
	writeMu.Lock()
	defer writeMu.Unlock()

	var waiting []Candidate
	readJSON(s.path(proposedFile), &waiting)
	found, rest := takeCandidate(waiting, id)
	if found == nil {
		return fmt.Errorf("no proposed entry %s", id)
	}

	state := s.memory()
	print := Fingerprint(found.Heading, found.Body)
	if !contains(state.Rejected, print) {
		state.Rejected = append(state.Rejected, print)
	}
	if err := writeJSON(s.path(memoryFile), state); err != nil {
		return err
	}
	return writeJSON(s.path(proposedFile), rest)
}

// Edit rewrites an accepted entry. The user's edit is a confirmation of it.
func (s *Store) Edit(id, heading, body string) error {
	writeMu.Lock()
	defer writeMu.Unlock()

	heading, body = strings.TrimSpace(heading), strings.TrimSpace(body)
	if heading == "" || body == "" {
		return fmt.Errorf("an entry needs a heading and a body")
	}
	state := s.memory()
	for i := range state.Entries {
		if state.Entries[i].ID != id {
			continue
		}
		state.Entries[i].Heading, state.Entries[i].Body = heading, body
		state.Entries[i].LastSeen = time.Now()
		return writeJSON(s.path(memoryFile), state)
	}
	return fmt.Errorf("no entry %s", id)
}

// Forget deletes an entry outright, superseded or not.
func (s *Store) Forget(id string) error {
	writeMu.Lock()
	defer writeMu.Unlock()

	state := s.memory()
	kept := make([]Entry, 0, len(state.Entries))
	for _, e := range state.Entries {
		if e.ID != id {
			kept = append(kept, e)
		}
	}
	if len(kept) == len(state.Entries) {
		return fmt.Errorf("no entry %s", id)
	}
	state.Entries = kept
	return writeJSON(s.path(memoryFile), state)
}

// Fingerprint identifies a fact by its text, so the same one proposed twice is
// recognised without an id the run could not have known.
func Fingerprint(heading, body string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(heading)) + "\x00" + strings.ToLower(strings.TrimSpace(body))))
	return hex.EncodeToString(sum[:8])
}

func (s *Store) memory() memoryState {
	var state memoryState
	readJSON(s.path(memoryFile), &state)
	return state
}

// resolveReplaces matches what a run says it supersedes, by id or heading.
// Unmatched is dropped: a replacement of nothing is a new entry.
func resolveReplaces(entries []Entry, id, heading string) string {
	heading = strings.TrimSpace(heading)
	for _, e := range entries {
		if e.State != StateAccepted {
			continue
		}
		if id != "" && e.ID == id {
			return e.ID
		}
		if heading != "" && strings.EqualFold(e.Heading, heading) {
			return e.ID
		}
	}
	return ""
}

func takeCandidate(waiting []Candidate, id string) (*Candidate, []Candidate) {
	rest := make([]Candidate, 0, len(waiting))
	var found *Candidate
	for i, c := range waiting {
		if c.ID == id && found == nil {
			found = &waiting[i]
			continue
		}
		rest = append(rest, c)
	}
	return found, rest
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func newID() string {
	var raw [6]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("150405.000000000")))[:12]
	}
	return hex.EncodeToString(raw[:])
}
