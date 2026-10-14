package settings

import (
	"fmt"
	"strings"
)

// Sessions are derived fresh from transcript scans, so there is nowhere on one
// to keep a flag. These two files are those flags, machine-wide because the
// launcher that reads them belongs to no checkout.
var (
	archived = &flags{store{name: "archived.json"}}

	// hidden is the sessions never listed: the one-shot runs ade makes for
	// itself — a commit message, a rewritten comment, a pull request
	// description — each of which leaves a transcript that would otherwise look
	// like a conversation you could come back to.
	hidden = &flags{store{name: "hidden.json"}}
)

// ArchivedPath is the file naming the sessions you have put away.
func ArchivedPath() (string, error) { return archived.path() }

// Archived is the set of archived session ids.
func Archived() map[string]bool { return archived.all() }

// Forget takes sessions out of every list for good. The transcript itself is
// left where the agent wrote it: that is Claude Code's own history, not ade's.
func Forget(ids ...string) (int, error) {
	wanted := make([]string, 0, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			wanted = append(wanted, id)
		}
	}
	if len(wanted) == 0 {
		return 0, fmt.Errorf("no session named")
	}

	if err := DropSessionsFromGroups(wanted...); err != nil {
		return 0, err
	}

	was := hidden.all()
	added := 0
	for _, id := range wanted {
		if err := hidden.set(id, true); err != nil {
			return added, err
		}
		if !was[id] {
			added++
		}
		// Never listed again, so leaving it archived only grows a file that
		// nothing will read.
		if err := archived.set(id, false); err != nil {
			return added, err
		}
		if err := SetTitle(id, ""); err != nil {
			return added, err
		}
	}
	return added, nil
}

// SetArchived marks a session as put away, or brings it back.
func SetArchived(id string, isArchived bool) error { return archived.set(id, isArchived) }

// SetArchivedMany puts a batch away, or brings it back, and says how many
// changed. A blank id is skipped rather than stranding the rest of the list.
func SetArchivedMany(ids []string, isArchived bool) (int, error) {
	was := archived.all()
	changed := 0
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if err := archived.set(id, isArchived); err != nil {
			return changed, err
		}
		if was[id] != isArchived {
			changed++
		}
	}
	return changed, nil
}

// HiddenPath is the file naming the sessions that are never listed.
func HiddenPath() (string, error) { return hidden.path() }

// Hidden is the set of session ids never to list.
func Hidden() map[string]bool { return hidden.all() }

// Hide adds a session to the never-listed set.
func Hide(id string) error { return hidden.set(id, true) }
