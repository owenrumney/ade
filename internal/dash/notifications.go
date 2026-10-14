package dash

import (
	"context"
	"log/slog"

	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/settings"
)

// Notification is one thread in your GitHub inbox. Dismissing is ade's own
// state, kept off ghpr.Notification, which is what GitHub said.
type Notification struct {
	ghpr.Notification
	Dismissed bool `json:"dismissed,omitempty"`
}

// reviewRequested is what a stale notification is usually about: it outlives
// the pull request it asked about.
const reviewRequested = "review_requested"

// RefreshNotifications reads your GitHub inbox. Standing rules are applied
// here; a dismissal goes out as a flag instead. See the design doc.
func (b *Board) RefreshNotifications(ctx context.Context) {
	inbox, err := ghpr.Notifications(ctx, b.deps.Dir)
	if err != nil {
		slog.Debug("list notifications", "err", err)
		return
	}
	b.setNotifications(b.live(ctx, keep(inbox)))
}

// live drops review requests whose pull request has closed. One batched ask;
// a state that could not be read keeps the notification.
func (b *Board) live(ctx context.Context, inbox []ghpr.Notification) []ghpr.Notification {
	var refs []ghpr.PRRef
	for _, n := range inbox {
		if n.Reason == reviewRequested && n.Type == "PullRequest" && n.Number > 0 {
			refs = append(refs, ghpr.PRRef{Repo: n.Repo, Number: n.Number})
		}
	}
	if len(refs) == 0 {
		return inbox
	}
	states, err := ghpr.PRStates(ctx, b.deps.Dir, refs)
	if err != nil {
		// Partial answers are still worth using: what came back is read below,
		// and what did not is kept.
		slog.Debug("read pull request states", "err", err)
	}

	out := make([]ghpr.Notification, 0, len(inbox))
	for _, n := range inbox {
		if n.Reason == reviewRequested {
			if state, ok := states[ghpr.PRRef{Repo: n.Repo, Number: n.Number}.Key()]; ok && state != "OPEN" {
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

// keep drops what the rules say never to show.
func keep(inbox []ghpr.Notification) []ghpr.Notification {
	cfg, err := settings.Load()
	if err != nil {
		return inbox
	}
	out := make([]ghpr.Notification, 0, len(inbox))
	for _, n := range inbox {
		if cfg.Ignore.SkipNotification(n.Repo, n.Reason) {
			continue
		}
		out = append(out, n)
	}
	return out
}

func (b *Board) setNotifications(inbox []ghpr.Notification) {
	b.mu.Lock()
	changed := len(inbox) != len(b.notifications)
	for i := range inbox {
		if !changed && inbox[i] != b.notifications[i] {
			changed = true
		}
	}
	b.notifications = inbox
	b.mu.Unlock()
	if changed {
		b.changed()
	}
}

// Notifications is the inbox as last read. Dismissed ones are sent, not
// dropped, so the page can show what you put away and undo it.
func (b *Board) Notifications() []Notification {
	b.mu.RLock()
	inbox := append([]ghpr.Notification(nil), b.notifications...)
	b.mu.RUnlock()

	aside := settings.Dismissed()
	out := make([]Notification, 0, len(inbox))
	for _, n := range inbox {
		out = append(out, Notification{
			Notification: n,
			Dismissed:    settings.IsDismissed(aside, n.ID, n.UpdatedAt),
		})
	}
	return out
}

// Dismiss puts a notification aside as it stands, or brings it back. Only one
// the poll returned, and nothing is marked read on GitHub.
func (b *Board) Dismiss(id string, dismissed bool) error {
	b.mu.RLock()
	at := ""
	known := false
	for _, n := range b.notifications {
		if n.ID == id {
			at, known = n.UpdatedAt, true
			break
		}
	}
	b.mu.RUnlock()
	if !known {
		return ErrNotInInbox
	}
	// Dismissing records the state it was in; undismissing forgets it.
	if !dismissed {
		at = ""
	}
	if err := settings.SetDismissed(id, at); err != nil {
		return err
	}
	b.changed()
	return nil
}
