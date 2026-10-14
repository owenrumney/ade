package jobs

import (
	"context"

	"github.com/owenrumney/ade/internal/bridge"
)

// ColdRunner fires a job as a cold `claude -p`, which every unattended run in
// ade already is. See the design doc for the envelope it runs in.
func ColdRunner(bin string) Runner {
	return func(ctx context.Context, req Request) (Result, error) {
		// Both lists, so the run neither stalls on a prompt it cannot answer
		// nor gains a tool the envelope did not name. No MCP: nothing posts.
		turn, err := bridge.OneShot(ctx, bridge.OneShotOpts{
			Bin:            bin,
			Dir:            req.Dir,
			Prompt:         req.Prompt,
			Tools:          req.Tools,
			AllowedTools:   req.Tools,
			PermissionMode: "default",
			NoMCP:          true,
		})
		if turn == nil {
			return Result{}, err
		}
		return Result{Text: turn.Text, Session: turn.SessionID, CostUSD: turn.CostUSD}, err
	}
}
