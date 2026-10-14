package settings

import "strings"

// dismissals is the notifications you have put aside, each with the time it was
// showing when you did.
//
// A set of ids would be wrong here. GitHub reuses a thread's id when new
// activity arrives on it, so an id alone would keep a thread hidden for good —
// dismiss a pull request today and you would never hear that someone replied.
// The time is what makes a dismissal mean "not this, as it stands".
var dismissals = &notes{store{name: "dismissed-notifications.json"}}

// DismissedPath is that file.
func DismissedPath() (string, error) { return dismissals.path() }

// Dismissed is every dismissal, by thread id, valued with the notification's
// updated time as it was when dismissed.
func Dismissed() map[string]string { return dismissals.all() }

// SetDismissed puts a notification aside as it stands. An empty updatedAt
// brings it back.
func SetDismissed(id, updatedAt string) error {
	return dismissals.set(strings.TrimSpace(id), strings.TrimSpace(updatedAt))
}

// IsDismissed reports whether a notification is still put aside: it is, until
// something happens on the thread after the moment it was dismissed.
//
// The times are RFC 3339 as GitHub sends them, which sorts correctly as text,
// so this needs no parsing and cannot fail on a shape it did not expect.
func IsDismissed(aside map[string]string, id, updatedAt string) bool {
	at, ok := aside[id]
	if !ok {
		return false
	}
	return updatedAt <= at
}
