package sessions

import (
	"sort"
	"time"
)

type Row struct {
	ID       string    `json:"id"`
	Dir      string    `json:"dir"`
	Name     string    `json:"name"`
	Branch   string    `json:"branch,omitempty"`
	Title    string    `json:"title,omitempty"`
	LastUsed time.Time `json:"lastUsed"`

	// Asked is the line the turn ended on, so the row can put the question
	// rather than only the session's name. Empty when it could not be read.
	Asked string `json:"asked,omitempty"`

	Seen *time.Time `json:"seen,omitempty"`
}

// WaitingIn is the rule applied to every session in repos, oldest first.
// badges is what each session's own agent last reported, by session id.
func WaitingIn(repos []Repo, badges map[string]string, looked map[string]time.Time) []Row {
	var out []Row
	for _, repo := range repos {
		for _, dir := range repo.Checkouts {
			for _, session := range dir.Sessions {
				if row, ok := Waiting(session, dir.Path, badges[session.ID], looked); ok {
					out = append(out, row)
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastUsed.Before(out[j].LastUsed) })
	return out
}

// Waiting is the rule for one session: its agent stopped to ask, or is blocked.
func Waiting(session Session, dir, badge string, looked map[string]time.Time) (Row, bool) {
	if session.Archived || (badge != "needs" && badge != "blocked") {
		return Row{}, false
	}
	at, seen := looked[session.ID]
	if seen && !session.LastUsed.After(at) {
		return Row{}, false
	}
	row := Row{
		ID: session.ID, Dir: dir, Name: session.Name,
		Branch: session.Branch, Title: session.Title,
		LastUsed: session.LastUsed,
	}
	if seen {
		row.Seen = &at
	}
	return row, true
}
