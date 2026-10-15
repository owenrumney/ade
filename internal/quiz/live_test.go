package quiz

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/outgoing"
	"github.com/owenrumney/ade/internal/review"
)

// TestLiveQuizOfABranch runs the real prompt over a real branch diff and
// prints the questions. The whole feature is the bet that they are worth
// answering, and nothing in Parse can tell a sharp question from a shallow
// one, so this is read rather than asserted.
//
//	ADE_LIVE=1 ADE_LIVE_DIR=~/code/ade ADE_LIVE_BASE=origin/main go test ./internal/quiz -run Live -v
//
// The bar, set before reading: at least two questions you would have got
// wrong or had to open the file to answer, and at most one of five that is
// trivia answerable from the hunk beside it. Below it, turn exactly one knob
// — ADE_LIVE_MODEL first, then the context, then quiz.md.
func TestLiveQuizOfABranch(t *testing.T) {
	if os.Getenv("ADE_LIVE") == "" {
		t.Skip("set ADE_LIVE=1")
	}
	dir := os.Getenv("ADE_LIVE_DIR")
	if dir == "" {
		dir = "../.."
	}
	base := os.Getenv("ADE_LIVE_BASE")
	if base == "" {
		base = "origin/main"
	}

	diff, err := gitdiff.Load(context.Background(), gitdiff.Options{Repo: dir, Scope: gitdiff.ScopeBranch, Base: base})
	require.NoError(t, err)
	require.NotEmpty(t, diff.Files, "no diff to quiz in %s against %s", dir, base)

	model := outgoing.FastModel
	if m := os.Getenv("ADE_LIVE_MODEL"); m != "" {
		model = m
	}
	start := time.Now()
	turn, err := bridge.OneShot(context.Background(), bridge.OneShotOpts{
		Model: model, Dir: dir, Prompt: Prompt(review.RenderDiff(diff)),
		AllowedTools: Tools(), NoMCP: true,
	})
	require.NoError(t, err)
	elapsed := time.Since(start)

	paths := map[string]bool{}
	for _, file := range diff.Files {
		paths[file.Path] = true
	}
	got := Shuffle(Parse(turn.Text, func(p string) bool { return paths[p] }), rand.New(rand.NewSource(time.Now().UnixNano()))) // #nosec G404 -- a shuffle, not a secret

	fmt.Printf("\n%d question(s) on %d file(s) in %s on %s\n\n",
		len(got), len(diff.Files), elapsed.Round(time.Millisecond), model)
	for _, q := range got {
		where := fmt.Sprintf("%s:%d", q.File, q.Line)
		if q.About != "" {
			where += " · about " + q.About
		}
		fmt.Printf("  %s  %s\n    %s\n", q.Ref, where, q.Question)
		for i, option := range q.Options {
			mark := " "
			if letter(i) == q.Answer {
				mark = "*"
			}
			fmt.Printf("    %s %s. %s\n", mark, letter(i), option)
		}
		fmt.Printf("    why: %s\n\n", strings.TrimSpace(q.Why))
	}
	require.GreaterOrEqual(t, len(got), MinQuestions)
}
