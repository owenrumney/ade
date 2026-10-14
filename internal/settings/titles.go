package settings

import "strings"

// titles is what the agent called each session after its first exchange, kept
// beside archived for the same reason: a session is derived from its transcript
// and has nowhere else to keep a name.
var titles = &notes{store{name: "titles.json"}}

// TitlesPath is that file.
func TitlesPath() (string, error) { return titles.path() }

// Titles is every session title, by id.
func Titles() map[string]string { return titles.all() }

// SetTitle names a session. An empty title forgets the name.
func SetTitle(id, title string) error {
	return titles.set(strings.TrimSpace(id), strings.TrimSpace(title))
}

// yours is the sessions you named by hand, which ade never overwrites.
var yours = &flags{store{name: "titles-yours.json"}}

// NamedByYou reports whether this session's title is one you typed.
func NamedByYou(id string) bool { return yours.all()[strings.TrimSpace(id)] }

// SetTitleByYou names a session, and records that nothing may rename it.
func SetTitleByYou(id, title string) error {
	id = strings.TrimSpace(id)
	if err := titles.set(id, strings.TrimSpace(title)); err != nil {
		return err
	}
	return yours.set(id, strings.TrimSpace(title) != "")
}
