package fixtures

import (
	"context"

	"github.com/owenrumney/ade/internal/bridge"
)

// editTools is what a fixture run may use: enough to make a change and run
// the tests, and nothing that reaches out of the worktree.
var editTools = []string{"Read", "Grep", "Glob", "Edit", "Write", "MultiEdit", "Bash"} //nolint:gochecknoglobals // a list, not state

// ColdRunner runs a fixture as a cold `claude -p` in the worktree, with no
// transcript: a fixture run read back as a conversation would be noise.
func ColdRunner(bin string) Runner {
	return func(ctx context.Context, req Request) (Response, error) {
		turn, err := bridge.OneShot(ctx, bridge.OneShotOpts{
			Bin:                  bin,
			Dir:                  req.Dir,
			Model:                req.Model,
			Prompt:               req.Prompt,
			Tools:                editTools,
			AllowedTools:         editTools,
			PermissionMode:       "acceptEdits",
			NoMCP:                true,
			NoSessionPersistence: true,
		})
		if turn == nil {
			return Response{}, err
		}
		return Response{Text: turn.Text, CostUSD: turn.CostUSD}, err
	}
}
