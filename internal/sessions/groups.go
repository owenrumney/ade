package sessions

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/settings"
)

// Group is a stored session relationship enriched with the information its card
// needs. Title and LastUsed come from its newest member, not the saved record.
type Group struct {
	ID       string    `json:"id"`
	Created  time.Time `json:"created"`
	Members  []string  `json:"members"`
	Title    string    `json:"title"`
	LastUsed time.Time `json:"lastUsed"`
}

// Suggestion is the current eligible set for one checkout. Its fingerprint lets
// a dismissal expire precisely when that set changes.
type Suggestion struct {
	IDs         []string `json:"ids"`
	Fingerprint string   `json:"fingerprint"`
}

// Candidates are completed, ungrouped sessions from one checkout. Archived
// ones are left out: they were put away on purpose.
func Candidates(dir Dir) []Session {
	out := make([]Session, 0, len(dir.Sessions))
	for _, session := range dir.Sessions {
		if !session.Live && !session.Archived && session.Group == "" {
			out = append(out, session)
		}
	}
	return out
}

// SuggestionFingerprint identifies a candidate membership irrespective of the
// list's presentation order.
func SuggestionFingerprint(ids []string) string {
	ids = append([]string(nil), ids...)
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, "\x00")))
	return hex.EncodeToString(sum[:])
}

// Combine validates selected sessions against the current scan before writing
// their local relationship. Stored members are oldest first.
func Combine(repos []Repo, path string, ids []string) (Group, error) {
	dir, ok := FindCheckout(repos, path)
	if !ok {
		return Group{}, fmt.Errorf("that is not one of the checkouts on this machine")
	}
	if len(ids) < 2 {
		return Group{}, fmt.Errorf("pick at least two sessions")
	}

	selected := make([]Session, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return Group{}, fmt.Errorf("pick distinct sessions")
		}
		seen[id] = true
		session, found := Find(dir.Sessions, dir.Path, id)
		if !found {
			return Group{}, fmt.Errorf("that is not one of that checkout's sessions")
		}
		if session.Live {
			return Group{}, fmt.Errorf("a running session cannot be combined")
		}
		if session.Group != "" {
			return Group{}, fmt.Errorf("that session is already combined")
		}
		selected = append(selected, session)
	}

	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].LastUsed.Equal(selected[j].LastUsed) {
			return selected[i].ID < selected[j].ID
		}
		return selected[i].LastUsed.Before(selected[j].LastUsed)
	})
	members := make([]string, len(selected))
	for i, session := range selected {
		members[i] = session.ID
	}
	record, err := settings.CreateSessionGroup(dir.Path, members)
	if err != nil {
		return Group{}, err
	}
	return groupFrom(record, selected), nil
}

// Uncombine removes a session relationship without touching its transcripts.
// The store is machine-wide, so an id alone would reach another repository's.
func Uncombine(repos []Repo, id string) error {
	id = strings.TrimSpace(id)
	for _, record := range settings.SessionGroups() {
		if record.ID != id {
			continue
		}
		if _, ok := FindCheckout(repos, record.Dir); !ok {
			return fmt.Errorf("that is not one of the checkouts on this machine")
		}
		return settings.DeleteSessionGroup(id)
	}
	return fmt.Errorf("that session group does not exist")
}

func attachGroups(dir *Dir, records []settings.SessionGroup) {
	byID := make(map[string]int, len(dir.Sessions))
	for i, session := range dir.Sessions {
		byID[session.ID] = i
	}
	for _, record := range records {
		if record.Dir != dir.Path {
			continue
		}
		members := make([]Session, 0, len(record.Members))
		visible := make([]string, 0, len(record.Members))
		for _, id := range record.Members {
			at, ok := byID[id]
			if !ok {
				continue
			}
			members = append(members, dir.Sessions[at])
			visible = append(visible, id)
		}
		// Stamped only once the card is going to be drawn: a member marked for
		// a group with no card is a row the list has nowhere to put.
		if len(members) < 2 {
			continue
		}
		for _, session := range members {
			dir.Sessions[byID[session.ID]].Group = record.ID
		}
		shown := record
		shown.Members = visible
		dir.Groups = append(dir.Groups, groupFrom(shown, members))
	}
}

func attachSuggestion(dir *Dir, dismissed map[string]string) {
	candidates := Candidates(*dir)
	if len(candidates) < 2 {
		return
	}
	ids := make([]string, len(candidates))
	for i, session := range candidates {
		ids[i] = session.ID
	}
	fingerprint := SuggestionFingerprint(ids)
	if dismissed[dir.Path] == fingerprint {
		return
	}
	dir.Suggestion = &Suggestion{IDs: ids, Fingerprint: fingerprint}
}

func groupFrom(record settings.SessionGroup, members []Session) Group {
	newest := members[0]
	for _, session := range members[1:] {
		if session.LastUsed.After(newest.LastUsed) {
			newest = session
		}
	}
	return Group{
		ID:       record.ID,
		Created:  record.Created,
		Members:  append([]string(nil), record.Members...),
		Title:    newest.Title,
		LastUsed: newest.LastUsed,
	}
}
