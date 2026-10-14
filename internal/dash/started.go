package dash

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/owenrumney/ade/internal/linear"
	"github.com/owenrumney/ade/internal/sessions"
)

// Started is where a ticket is already being worked on. See the design doc.
type Started struct {
	Dir    string `json:"dir"`
	Branch string `json:"branch"`
	// Session is the conversation to resume there, empty for a checkout that
	// has one but no agent has run in yet.
	Session string `json:"session,omitempty"`
}

// unsafeSlug must agree with web.Slug, or a ticket already started reads as
// unstarted.
var unsafeSlug = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// TicketBranch is the branch `ade create --linear <id>` makes: the ticket,
// slugged and lowercased.
func TicketBranch(identifier string) string {
	base := filepath.Base(strings.TrimSuffix(identifier, filepath.Ext(identifier)))
	return strings.ToLower(strings.Trim(unsafeSlug.ReplaceAllString(base, "-"), "-."))
}

// Started is, for each ticket, the checkout already on its branch. Matching is
// on the branch rather than the document; see the design doc.
func (b *Board) Started(issues []linear.Issue) map[string]Started {
	if len(issues) == 0 {
		return nil
	}
	repos, err := b.repos()
	if err != nil {
		return nil
	}

	// One pass, indexed by branch: every ticket asks the same question.
	onBranch := map[string]Started{}
	for _, repo := range repos {
		for _, dir := range repo.Checkouts {
			if dir.Branch == "" {
				continue
			}
			key := strings.ToLower(dir.Branch)
			if _, taken := onBranch[key]; taken {
				continue
			}
			onBranch[key] = Started{Dir: dir.Path, Branch: dir.Branch, Session: newestSession(dir)}
		}
	}

	out := map[string]Started{}
	for _, issue := range issues {
		for _, branch := range []string{issue.BranchName, TicketBranch(issue.Identifier)} {
			if branch == "" {
				continue
			}
			if found, ok := onBranch[strings.ToLower(branch)]; ok {
				out[issue.Identifier] = found
				break
			}
		}
	}
	return out
}

// newestSession is the conversation to resume: the one last used, archived
// ones aside.
func newestSession(dir sessions.Dir) string {
	best := ""
	for _, s := range dir.Sessions {
		if s.Archived {
			continue
		}
		if best == "" {
			best = s.ID
		}
	}
	return best
}
