package settings

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ErrStore is a group that could not be written down, as opposed to one that
// was refused. A caller reports it as ade's problem rather than the user's.
var ErrStore = errors.New("session groups could not be written")

// SessionGroup is the local relationship between sessions that belong to one
// piece of work. The sessions themselves remain owned by their transcripts.
type SessionGroup struct {
	ID      string    `json:"id"`
	Dir     string    `json:"dir"`
	Created time.Time `json:"created"`
	Members []string  `json:"members"`
}

var (
	groups          = &groupStore{store: store{name: "session-groups.json"}}
	dismissedGroups = &notes{store{name: "dismissed-session-groups.json"}}
)

type groupStore struct{ store }

// SessionGroupsPath is where session-group relationships are kept.
func SessionGroupsPath() (string, error) { return groups.path() }

// SessionGroups returns every valid group stored on this machine.
func SessionGroups() []SessionGroup {
	groups.mu.Lock()
	defer groups.mu.Unlock()
	return groups.read()
}

// CreateSessionGroup records one group. Members must already be in chronological
// order; validating that they are real sessions belongs to the caller's scan.
func CreateSessionGroup(dir string, members []string) (SessionGroup, error) {
	dir = strings.TrimSpace(dir)
	members = uniqueMembers(members)
	if dir == "" {
		return SessionGroup{}, fmt.Errorf("name a workspace")
	}
	if len(members) < 2 {
		return SessionGroup{}, fmt.Errorf("pick at least two sessions")
	}

	groups.mu.Lock()
	defer groups.mu.Unlock()

	have := groups.read()
	used := map[string]bool{}
	for _, group := range have {
		for _, id := range group.Members {
			used[id] = true
		}
	}
	for _, id := range members {
		if used[id] {
			return SessionGroup{}, fmt.Errorf("that session is already combined")
		}
	}

	id, err := newGroupID()
	if err != nil {
		return SessionGroup{}, err
	}
	group := SessionGroup{ID: id, Dir: dir, Created: time.Now().UTC(), Members: members}
	have = append(have, group)
	if err := groups.save(have); err != nil {
		return SessionGroup{}, fmt.Errorf("%w: %w", ErrStore, err)
	}
	return group, nil
}

// DeleteSessionGroup removes the relationship, leaving its sessions alone.
func DeleteSessionGroup(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("name a session group")
	}
	groups.mu.Lock()
	defer groups.mu.Unlock()

	have := groups.read()
	out := have[:0]
	found := false
	for _, group := range have {
		if group.ID == id {
			found = true
			continue
		}
		out = append(out, group)
	}
	if !found {
		return fmt.Errorf("that session group does not exist")
	}
	if err := groups.save(out); err != nil {
		return fmt.Errorf("%w: %w", ErrStore, err)
	}
	return nil
}

// DropSessionsFromGroups forgets deleted sessions and discards groups that no
// longer have enough members to mean anything.
func DropSessionsFromGroups(ids ...string) error {
	removed := map[string]bool{}
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			removed[id] = true
		}
	}
	if len(removed) == 0 {
		return nil
	}

	groups.mu.Lock()
	defer groups.mu.Unlock()

	have := groups.read()
	out := make([]SessionGroup, 0, len(have))
	changed := false
	for _, group := range have {
		members := make([]string, 0, len(group.Members))
		for _, id := range group.Members {
			if removed[id] {
				changed = true
				continue
			}
			members = append(members, id)
		}
		if len(members) < 2 {
			if len(members) != len(group.Members) {
				changed = true
			}
			continue
		}
		group.Members = members
		out = append(out, group)
	}
	if !changed {
		return nil
	}
	return groups.save(out)
}

// DismissedSessionGroupSuggestions records the candidate fingerprint last put
// aside for each checkout.
func DismissedSessionGroupSuggestions() map[string]string { return dismissedGroups.all() }

// SetSessionGroupSuggestionDismissed hides a candidate set until its membership
// changes. Passing an empty fingerprint brings the suggestion back.
func SetSessionGroupSuggestionDismissed(dir, fingerprint string) error {
	return dismissedGroups.set(strings.TrimSpace(dir), strings.TrimSpace(fingerprint))
}

func (g *groupStore) read() []SessionGroup {
	var raw []SessionGroup
	g.load(&raw)
	out := make([]SessionGroup, 0, len(raw))
	for _, group := range raw {
		group.ID = strings.TrimSpace(group.ID)
		group.Dir = strings.TrimSpace(group.Dir)
		group.Members = uniqueMembers(group.Members)
		if group.ID == "" || group.Dir == "" || group.Created.IsZero() || len(group.Members) < 2 {
			continue
		}
		out = append(out, group)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}

func uniqueMembers(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func newGroupID() (string, error) {
	data := make([]byte, 8)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("make session group id: %w", err)
	}
	return "group-" + hex.EncodeToString(data), nil
}
