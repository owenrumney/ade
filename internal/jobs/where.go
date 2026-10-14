package jobs

import (
	"context"
	"fmt"

	"github.com/owenrumney/ade/internal/memory"
	"github.com/owenrumney/ade/internal/sessions"
)

// ErrNoCheckout is a job whose project is not on this machine. It is skipped
// and says so; a scheduled run does not clone.
type ErrNoCheckout struct{ Project string }

func (e ErrNoCheckout) Error() string {
	return fmt.Sprintf("%s is not checked out on this machine", e.Project)
}

// Checkout is where a job runs: its project's main checkout from the session
// scan, or fallback. A project is a remote, which survives its worktrees.
func Checkout(ctx context.Context, job Job, repos []sessions.Repo, fallback string) (string, error) {
	if job.Project == "" {
		return fallback, nil
	}
	for _, repo := range repos {
		if identityOf(ctx, repo.Path).Remote == job.Project {
			return repo.Path, nil
		}
	}
	return "", ErrNoCheckout{Project: job.Project}
}

// identityOf is a checkout's project, from the ledger where it has been seen
// before and from git the first time. Only a new checkout costs git.
func identityOf(ctx context.Context, path string) memory.Identity {
	if id, known := memory.Lookup(path); known {
		return id
	}
	id, err := memory.Resolve(ctx, path)
	if err != nil {
		return memory.Identity{}
	}
	_ = memory.Record(path, id)
	return id
}
