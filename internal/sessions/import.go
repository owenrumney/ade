package sessions

import (
	"cmp"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/transcripts"
)

// What Resolve refuses, so a caller can answer with the right status.
var (
	ErrNotAnID    = errors.New("that is not a session id")
	ErrNoSuchID   = errors.New("no transcript with that id on this machine")
	ErrUnreadable = errors.New("that transcript has nothing in it")
	ErrNotYours   = errors.New("that conversation is not one to return to")
	ErrGone       = errors.New("the directory that session was recorded in is gone")
)

// uuid is the shape Claude Code names a session, checked before any glob.
var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Resolve finds one session by id, wherever its transcript was recorded. Into
// is the checkout adopting it, or empty to take the one it was recorded in.
func Resolve(id, into string) (Session, error) {
	id = strings.TrimSpace(id)
	if !uuid.MatchString(id) {
		return Session{}, ErrNotAnID
	}
	path := transcripts.ClaudeAnywhere(id)
	if path == "" {
		return Session{}, ErrNoSuchID
	}
	found := transcripts.ClaudeSession(path)
	if found == nil || found.CWD == "" {
		return Session{}, ErrUnreadable
	}
	if found.Subagent != "" || transcripts.Machine(found) {
		return Session{}, ErrNotYours
	}

	dir := cmp.Or(strings.TrimSpace(into), found.CWD)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return Session{}, ErrGone
	}
	from := ""
	if dir != found.CWD {
		from = found.CWD
	}
	return Session{
		ID:        found.ID,
		Agent:     string(found.Agent),
		Model:     found.Model,
		Path:      found.Path,
		Dir:       dir,
		Name:      filepath.Base(dir),
		Branch:    found.Branch,
		Title:     cmp.Or(settings.Titles()[found.ID], Title(found)),
		LastUsed:  found.LastUsed,
		Resumable: found.Agent == transcripts.AgentClaude || found.Agent == transcripts.AgentCodex,
		Pinned:    true,
		From:      from,
	}, nil
}

// Pin records a session against its checkout, so the window stops bounding it.
func Pin(id, dir string) error { return settings.SetImported(id, dir) }

// Unpin lets the window bound a session again. The transcript is untouched.
func Unpin(id string) error { return settings.SetImported(id, "") }
