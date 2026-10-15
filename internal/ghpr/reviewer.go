package ghpr

import (
	"context"
	"fmt"
	"strings"
)

// LoginOfCommit is the account owning the author address of sha. Empty means
// GitHub matched no account, not that the lookup failed.
func LoginOfCommit(ctx context.Context, dir, repo, sha string) (string, error) {
	if repo == "" || sha == "" {
		return "", nil
	}
	out, err := run(ctx, dir, "api", "repos/"+repo+"/commits/"+sha, "--jq", ".author.login // \"\"")
	if err != nil {
		return "", fmt.Errorf("resolve the author of %s: %w", short(sha), err)
	}
	return strings.TrimSpace(out), nil
}

// Assignable is who the repository accepts. GitHub drops an unrecognised
// assignee silently, so the list is checked before asking.
func Assignable(ctx context.Context, dir, repo string) ([]string, error) {
	if repo == "" {
		return nil, nil
	}
	out, err := run(ctx, dir, "api", "--paginate", "repos/"+repo+"/assignees", "--jq", ".[].login")
	if err != nil {
		return nil, fmt.Errorf("read who %s accepts: %w", repo, err)
	}
	var logins []string
	for _, line := range strings.Split(out, "\n") {
		if login := strings.TrimSpace(line); login != "" {
			logins = append(logins, login)
		}
	}
	return logins, nil
}

// AddReviewer asks login to review. pr may be a number or a URL. Reviewer and
// not assignee: the assignee owns the work, which is still the author.
func AddReviewer(ctx context.Context, dir, pr, login string) error {
	pr, login = strings.TrimSpace(pr), strings.TrimSpace(login)
	if pr == "" || login == "" {
		return fmt.Errorf("a reviewer needs a pull request and a handle")
	}
	_, err := write(ctx, dir, "pr", "edit", pr, "--add-reviewer", login)
	return err
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
