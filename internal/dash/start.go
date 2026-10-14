package dash

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/owenrumney/ade/internal/gitdiff"
)

// ticketIDRE is a Linear identifier: TEAM-123. It reaches a command line and a
// branch name, so nothing else will do.
var ticketIDRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,9}-[0-9]{1,8}$`)

// StartTicket runs ade create --linear in a repository's main checkout, making
// the worktree, the document and the first draft. Only one the scan found.
func (b *Board) StartTicket(ctx context.Context, dir, ticket string) (string, string, error) {
	ticket = strings.ToUpper(strings.TrimSpace(ticket))
	if !ticketIDRE.MatchString(ticket) {
		return "", "", fmt.Errorf("%q is not a Linear ticket reference", ticket)
	}
	repos, err := b.repos()
	if err != nil {
		return "", "", err
	}
	main := ""
	for _, repo := range repos {
		if repo.Path == dir {
			main = repo.Path
			break
		}
	}
	if main == "" {
		return "", "", fmt.Errorf("%s is not one of the repositories under your scan directories", dir)
	}
	if b.deps.Create == nil {
		return "", "", fmt.Errorf("this server cannot start work on %s", ticket)
	}
	url, err := b.deps.Create(ctx, main, ticket)
	if err != nil {
		return "", "", err
	}
	return url, gitdiff.WorktreePath(main, strings.ToLower(ticket)), nil
}
