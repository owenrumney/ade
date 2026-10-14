package outgoing

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/comments"
)

func TestLiveRewriteIsQuick(t *testing.T) {
	if os.Getenv("ADE_LIVE") == "" {
		t.Skip("set ADE_LIVE=1")
	}
	threads := []comments.Thread{
		note("t1", "internal/web/agent.go", 42, "retry loop never resets the counter, so a second failure exits early"),
		note("t2", "cmd/ade/main.go", 118, "--port flag parsed but never passed to the server"),
		note("t3", "internal/ghpr/ghpr.go", 300, "timeout is 0 when the setting is missing; every call hangs forever"),
	}

	// ADE_LIVE_MODEL tries another model with the same prompt, for comparing.
	model := FastModel
	if m := os.Getenv("ADE_LIVE_MODEL"); m != "" {
		model = m
	}
	start := time.Now()
	turn, err := bridge.OneShot(context.Background(), bridge.OneShotOpts{
		Model: model, Dir: ".", Prompt: Prompt(threads),
		AllowedTools: Tools(), NoMCP: true,
	})
	require.NoError(t, err)
	elapsed := time.Since(start)

	got, err := Parse(turn.Text)
	require.NoError(t, err)
	fmt.Printf("\n%d comment(s) in %s on %s\n\n", len(got), elapsed.Round(time.Millisecond), model)
	for _, th := range threads {
		fmt.Printf("  %s:%d\n    was: %s\n    now: %s\n\n", th.File, th.Line, th.Comments[0].Body, got[th.ID])
	}
	require.Len(t, got, 3)
}
