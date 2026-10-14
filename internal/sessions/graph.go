package sessions

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/session"
)

// The graph is the machine as ade sees it right now: every repository, each
// checkout under it, and for each checkout the server running there and
// what its agent is doing. Both the launcher and a running server offer it;
// this is the shape they share.

// GraphRepo is one repository and its checkouts.
type GraphRepo struct {
	Path      string          `json:"path"`
	Name      string          `json:"name"`
	Owner     string          `json:"owner,omitempty"`
	Checkouts []GraphCheckout `json:"checkouts"`
}

// GraphCheckout is one checkout, and the server on it if one is live. A
// checkout with no server is still drawn: it is somewhere to start one.
// Here marks the checkout the asking server is itself serving.
type GraphCheckout struct {
	Path     string       `json:"path"`
	Name     string       `json:"name"`
	Branch   string       `json:"branch,omitempty"`
	Worktree bool         `json:"worktree,omitempty"`
	Here     bool         `json:"here,omitempty"`
	Sessions int          `json:"sessions"`
	Server   *GraphServer `json:"server,omitempty"`
}

// GraphServer is a live ade and what its agent is up to. No URL: opening a
// checkout goes through the open endpoint, which knows the token; this does
// not.
type GraphServer struct {
	PID       int                  `json:"pid"`
	StartedAt time.Time            `json:"startedAt"`
	Agent     *session.AgentHealth `json:"agent,omitempty"`
}

// Prober asks a recorded server about itself. A parameter so a test can
// stand one in for the real loopback request; nil is the real one.
type Prober func(ctx context.Context, rec *session.Record) (session.Health, bool)

// Graph probes every checkout's server record at once: a machine with
// twenty checkouts should not answer in twenty probe timeouts. A record
// whose server does not answer is a checkout with no server, and is left
// where it is: the next ade there replaces it.
func Graph(ctx context.Context, repos []Repo, probe Prober) []GraphRepo {
	if probe == nil {
		probe = session.Probe
	}
	out := make([]GraphRepo, 0, len(repos))
	var wg sync.WaitGroup
	for _, repo := range repos {
		node := GraphRepo{Path: repo.Path, Name: repo.Name, Owner: repo.Owner, Checkouts: make([]GraphCheckout, len(repo.Checkouts))}
		for i, dir := range repo.Checkouts {
			node.Checkouts[i] = GraphCheckout{
				Path: dir.Path, Name: dir.Name, Branch: dir.Branch, Worktree: dir.Worktree, Here: dir.Here,
				Sessions: len(dir.Sessions),
			}
			rec, ok := session.Read(session.Path(GitDir(dir.Path)))
			if !ok {
				continue
			}
			wg.Add(1)
			go func(slot *GraphCheckout, rec *session.Record) {
				defer wg.Done()
				health, live := probe(ctx, rec)
				if !live {
					return
				}
				slot.Server = &GraphServer{PID: rec.PID, StartedAt: rec.StartedAt, Agent: health.Agent}
				if health.Branch != "" {
					slot.Branch = health.Branch
				}
			}(&node.Checkouts[i], rec)
		}
		out = append(out, node)
	}
	wg.Wait()
	return out
}

// stopWait is how long a server gets to go after being asked.
const stopWait = 10 * time.Second

// StopServer asks the ade serving dir to shut down, and waits for it to go.
func StopServer(dir string) error {
	_, err := session.Stop(session.Path(GitDir(dir)), stopWait)
	return err
}

// CancelAgent stops whatever the ade serving dir is running — the main
// agent's turn and every named agent beside it — the way its own Stop
// button would. The recorded URL carries the token, so this can ask.
func CancelAgent(ctx context.Context, dir string) error {
	rec, ok := session.Read(session.Path(GitDir(dir)))
	if !ok {
		return errors.New("no ade is running there")
	}
	return Ask(ctx, rec.URL, http.MethodPost, "/api/cancel", "")
}
