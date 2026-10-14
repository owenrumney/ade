package settings

import (
	"strings"
	"time"
)

// seen is when you last looked at each session: a fact about you rather than
// about the checkout, so it lives here and not beside the code.
var seen = &notes{store{name: "seen-sessions.json"}}

func SeenPath() (string, error) { return seen.path() }

func Seen() map[string]time.Time {
	out := map[string]time.Time{}
	for id, at := range seen.all() {
		if when, err := time.Parse(time.RFC3339, at); err == nil {
			out[id] = when
		}
	}
	return out
}

// KeepSeen forgets every id that is not one of keep, so the file does not
// grow a permanent entry for each session ever deleted.
func KeepSeen(keep map[string]bool) error {
	return seen.keep(keep)
}

// MarkSeen records a look. An empty id is a conversation not yet named.
func MarkSeen(id string, at time.Time) error {
	if id = strings.TrimSpace(id); id == "" {
		return nil
	}
	// Nano, not seconds: a look and the reply it answers can land in the same
	// second, and a truncated look reads as older than the reply.
	return seen.set(id, at.UTC().Format(time.RFC3339Nano))
}

// MergeSeen folds another record of your looks into this one's and reports
// whether anything moved. Newest wins: a dismissal anywhere reaches here.
func MergeSeen(marks map[string]time.Time) (bool, error) {
	mine := Seen()
	changed := false
	for id, at := range marks {
		if id = strings.TrimSpace(id); id == "" || at.IsZero() {
			continue
		}
		if was, ok := mine[id]; ok && !at.After(was) {
			continue
		}
		if err := MarkSeen(id, at); err != nil {
			return changed, err
		}
		changed = true
	}
	return changed, nil
}
