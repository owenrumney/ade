package sessions

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/transcripts"
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

// optionRE is a line carrying an answer rather than asking for one. The
// space matters: "**Shall I?**" opens with a marker and is prose.
var optionRE = regexp.MustCompile(`^([-*+>]\s|\d+[.)]\s|\|)`)

// WaitingIn is the rule applied to every session in repos, oldest first.
func WaitingIn(repos []Repo, running map[string]bool, looked map[string]time.Time, archived map[string]bool) []Row {
	var out []Row
	for _, repo := range repos {
		for _, dir := range repo.Checkouts {
			for _, session := range dir.Sessions {
				row, ok := Waiting(session, dir.Path, running[session.ID], archived[session.ID], looked)
				if ok {
					out = append(out, row)
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastUsed.Before(out[j].LastUsed) })
	return out
}

// Waiting is the rule for one session: a running one is working, and a turn
// that ended in a statement is work you can read later.
func Waiting(session Session, dir string, running, archived bool, looked map[string]time.Time) (Row, bool) {
	if running || session.Archived || archived || session.LastUsed.IsZero() {
		return Row{}, false
	}
	at, ok := looked[session.ID]
	if ok && !session.LastUsed.After(at) {
		return Row{}, false
	}
	// Last, and only on what is already unread: reading a transcript's tail is
	// the expensive part of this walk.
	question, ok2 := asked(session)
	if !ok2 {
		return Row{}, false
	}
	row := Row{
		ID: session.ID, Dir: dir, Name: session.Name,
		Branch: session.Branch, Title: session.Title,
		Asked:    question,
		LastUsed: session.LastUsed,
	}
	if ok {
		row.Seen = &at
	}
	return row, true
}

// asked is whether the agent finished by putting something to you, and the
// line it asked with.
func asked(session Session) (string, bool) {
	if session.Path == "" {
		return "", true
	}
	// Only what is read as a statement is dropped: an agent whose transcript
	// cannot be walked is still one that wants you.
	last := transcripts.LastAgent(&transcripts.Session{
		Agent: transcripts.Agent(session.Agent),
		Path:  session.Path,
	})
	if last == "" {
		return "", true
	}
	return Question(last)
}

// IsQuestion is whether a reply ends by putting something to you: its last
// line of prose, past any options and any code block, asks for an answer.
func IsQuestion(reply string) bool {
	_, ok := Question(reply)
	return ok
}

// Question is that line, for a row that puts the question rather than only
// the name of whoever asked it.
func Question(reply string) (string, bool) {
	lines := strings.Split(reply, "\n")
	inside := fenced(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || inside[i] || optionRE.MatchString(line) {
			continue
		}
		if !strings.HasSuffix(strings.TrimRight(line, "*_`)]\"'"), "?") {
			return "", false
		}
		return strings.Trim(line, "*_`"), true
	}
	return "", false
}

// fenced marks the lines inside ``` blocks, the fences themselves included.
func fenced(lines []string) []bool {
	inside := make([]bool, len(lines))
	open := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inside[i] = true
			open = !open
			continue
		}
		inside[i] = open
	}
	return inside
}
