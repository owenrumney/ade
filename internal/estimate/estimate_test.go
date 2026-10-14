package estimate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/design"
	"github.com/owenrumney/ade/internal/transcripts"
)

func minutes(ns ...int) []time.Duration {
	out := make([]time.Duration, len(ns))
	for i, n := range ns {
		out[i] = time.Duration(n) * time.Minute
	}
	return out
}

func TestUnderThreeRunsThereIsNoRange(t *testing.T) {
	_, ok := Spread(nil)
	assert.False(t, ok)
	_, ok = Spread(minutes(10, 20))
	assert.False(t, ok, "two runs is a made-up number")
}

func TestThreeRunsGiveTheMiddleHalf(t *testing.T) {
	got, ok := Spread(minutes(40, 10, 20))
	require.True(t, ok)
	assert.Equal(t, 15*time.Minute, got.Low)
	assert.Equal(t, 30*time.Minute, got.High)
	assert.Equal(t, 3, got.Runs)
}

func TestFiveRunsLandOnRanks(t *testing.T) {
	got, ok := Spread(minutes(50, 10, 30, 20, 40))
	require.True(t, ok)
	assert.Equal(t, 20*time.Minute, got.Low)
	assert.Equal(t, 40*time.Minute, got.High)
}

func TestFortyRunsIgnoreTheTails(t *testing.T) {
	var ns []int
	for i := 1; i <= 38; i++ {
		ns = append(ns, 30+i%5)
	}
	ns = append(ns, 1, 600)
	got, ok := Spread(minutes(ns...))
	require.True(t, ok)
	assert.Equal(t, 40, got.Runs)
	assert.GreaterOrEqual(t, got.Low, 30*time.Minute)
	assert.LessOrEqual(t, got.High, 35*time.Minute, "one runaway build does not stretch the range")
}

func build(repo, model string, words, items, mins int) Run {
	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	return Run{Kind: KindBuild, Repo: repo, Model: model, DocWords: words, ChecklistItems: items,
		Started: at, Finished: at.Add(time.Duration(mins) * time.Minute)}
}

func many(n int, r Run) []Run {
	out := make([]Run, n)
	for i := range out {
		out[i] = r
	}
	return out
}

func TestTheGroupWidensInItsStatedOrder(t *testing.T) {
	target := Target{Kind: KindBuild, Repo: "/code/ade", Model: "opus", DocWords: 700, ChecklistItems: 24}
	here := build("/code/ade", "opus", 720, 22, 30)
	elsewhere := build("/code/other", "opus", 700, 24, 40)
	coarser := build("/code/other", "opus", 300, 3, 50)
	otherModel := build("/code/other", "sonnet", 300, 3, 60)

	cases := []struct {
		history []Run
		label   string
		runs    int
	}{
		{many(5, here), "of a similar-sized design in this repository", 5},
		{append(many(3, here), many(2, elsewhere)...), "of a similar-sized design", 5},
		{append(many(3, here), many(2, coarser)...), "of a design about this size", 5},
		{append(many(3, here), many(2, otherModel)...), "of a design about this size, on any model", 5},
		{many(2, here), "of a similar-sized design in this repository", 2},
	}
	for _, c := range cases {
		g := Similar(c.history, target)
		assert.Equal(t, c.label, g.Label)
		assert.Len(t, g.Runs, c.runs)
	}
}

func TestAModelAliasMatchesTheFullIDsItNames(t *testing.T) {
	run := build("/r", "claude-opus-5", 700, 24, 30)
	assert.True(t, sameModel(Target{Model: "opus"}, run))
	assert.True(t, sameModel(Target{Model: "claude-opus-5[1m]"}, run))
	assert.True(t, sameModel(Target{Model: "Claude-Opus-5"}, run))
	assert.True(t, sameModel(Target{}, run), "no model is any model")
	assert.False(t, sameModel(Target{Model: "sonnet"}, run))
	assert.False(t, sameModel(Target{Model: "claude-opus-5-5[1m]"}, run))
}

func TestEachLevelHoldsEveryRunTheNarrowerOneDid(t *testing.T) {
	sizes := []int{1, 249, 250, 999, 1000, 2500, 2999, 3000, 3500, 8000, 40000}
	items := []int{0, 4, 5, 24, 200}
	for _, tw := range sizes {
		for _, rw := range sizes {
			for _, ti := range items {
				for _, ri := range items {
					target := Target{Kind: KindBuild, Repo: "/r", Model: "m", DocWords: tw, ChecklistItems: ti}
					run := build("/r", "m", rw, ri, 30)
					for i := 1; i < len(levels); i++ {
						if levels[i-1].match(target, run) {
							assert.Truef(t, levels[i].match(target, run),
								"%d words/%d items against %d/%d: level %d drops a level %d match", rw, ri, tw, ti, i, i-1)
						}
					}
				}
			}
		}
	}
}

func TestAWiderLevelThatMatchesLessDoesNotWinTheGroup(t *testing.T) {
	// Four exact matches are an estimate, not a reason to widen past them.
	target := Target{Kind: KindBuild, Repo: "/code/ade", Model: "opus", DocWords: 2500}
	history := many(4, build("/code/ade", "opus", 3500, 0, 30))
	g := Similar(history, target)
	assert.Len(t, g.Runs, 4)
	assert.Equal(t, "of a similar-sized design in this repository", g.Label, "a tie keeps the narrowest")
	_, ok := For(history, target, nil)
	assert.True(t, ok)
}

func TestAFailedRunIsNotHowLongARunTakes(t *testing.T) {
	stopped := build("/r", "m", 700, 24, 4)
	stopped.Failed = true
	history := append(many(3, build("/r", "m", 700, 24, 40)), many(3, stopped)...)
	g := Similar(history, Target{Kind: KindBuild, Repo: "/r", DocWords: 700, ChecklistItems: 24})
	assert.Len(t, g.Runs, 3)

	backfilled := stopped
	backfilled.Failed = false
	assert.Len(t, Merge([]Run{stopped}, []Run{backfilled}), 1, "the transcript's copy is dropped")
}

func TestAddingARunKeepsOneStartedJustBeforeIt(t *testing.T) {
	first := build("/r", "m", 700, 24, 30)
	second := first
	second.Started = first.Started.Add(time.Minute)
	h := &History{at: time.Now()}
	h.Add(first)
	h.Add(second)
	assert.Len(t, h.runs, 2)
}

func TestTheEstimateNamesTheGroupItSettledOn(t *testing.T) {
	target := Target{Kind: KindBuild, Repo: "/code/ade", Model: "opus", DocWords: 700, ChecklistItems: 24}
	history := append(many(3, build("/code/ade", "opus", 700, 24, 30)), many(2, build("/code/x", "opus", 700, 24, 40))...)
	got, ok := For(history, target, nil)
	require.True(t, ok)
	assert.Equal(t, "of a similar-sized design", got.Group)
	assert.Equal(t, 5, got.Runs)
	assert.Nil(t, got.Window)
}

func TestNoEstimateFromTooFewRuns(t *testing.T) {
	_, ok := For(many(2, build("/code/ade", "opus", 700, 24, 30)), Target{Kind: KindBuild, DocWords: 700}, nil)
	assert.False(t, ok)
}

func priced(r Run, cost float64) Run {
	r.Usage = transcripts.Usage{CostUSD: cost, CostKnown: true}
	return r
}

func TestTheWindowShareIsCalibratedOnThisAccount(t *testing.T) {
	history := []Run{
		priced(build("/r", "m", 700, 24, 30), 1),
		priced(build("/r", "m", 700, 24, 30), 2),
		priced(build("/r", "m", 700, 24, 30), 3),
	}
	cal := &Calibration{Name: "5h", UsedPct: 60, UnitsSinceReset: 20}
	got, ok := For(history, Target{Kind: KindBuild, DocWords: 700, ChecklistItems: 24}, cal)
	require.True(t, ok)
	require.NotNil(t, got.Window)
	assert.InDelta(t, 6, got.Window.About, 1e-9, "the median run at 3% a unit")
	assert.InDelta(t, 7.5, got.Window.High, 1e-9)
	assert.False(t, got.Window.Cap)

	cal.UsedPct = 95
	got, _ = For(history, Target{Kind: KindBuild, DocWords: 700, ChecklistItems: 24}, cal)
	require.NotNil(t, got.Window)
	assert.True(t, got.Window.Cap, "the upper end goes past what is left")
}

func TestTheCalibrationSurvivesAWindowReset(t *testing.T) {
	history := many(3, priced(build("/r", "m", 700, 24, 30), 2))
	before := Calibration{Name: "5h", UsedPct: 80, UnitsSinceReset: 40}
	after := Calibration{Name: "5h", UsedPct: 80, UnitsSinceReset: 0}

	rate, ok := before.PerUnit()
	require.True(t, ok)
	assert.InDelta(t, 2, rate, 1e-9)
	_, ok = after.PerUnit()
	assert.False(t, ok, "nothing spent since the reset is no calibration, not infinity")

	got, ok := For(history, Target{Kind: KindBuild, DocWords: 700, ChecklistItems: 24}, &after)
	require.True(t, ok, "the time estimate stands without the window")
	assert.Nil(t, got.Window)
	assert.Equal(t, 30*60, got.HighSeconds)
}

func TestPastTheUpperEndTheLineAppearsOnce(t *testing.T) {
	history := []Run{
		build("/r", "m", 700, 24, 10), build("/r", "m", 700, 24, 20),
		build("/r", "m", 700, 24, 30), build("/r", "m", 700, 24, 40),
		build("/r", "m", 700, 24, 50),
	}
	watch, ok := NewOverrun(history, Target{Kind: KindBuild, DocWords: 700, ChecklistItems: 24})
	require.True(t, ok)
	assert.Equal(t, 40*time.Minute, watch.High())

	var said []string
	for elapsed := time.Duration(0); elapsed < 2*time.Hour; elapsed += 30 * time.Second {
		if line, ok := watch.Check(elapsed); ok {
			said = append(said, line)
		}
	}
	require.Len(t, said, 1, "one line, not one a tick")
	assert.Equal(t, "running longer than 4 of 5 past builds", said[0])
}

func TestARunIsRecordedAndReadBack(t *testing.T) {
	dir := t.TempDir()
	run := priced(build("/r", "m", 700, 24, 30), 2)
	require.NoError(t, Record(dir, run))
	require.NoError(t, Record(dir, run))
	got, err := Recorded(dir)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, 30*time.Minute, got[0].Duration())
	assert.InDelta(t, 2, got[0].Units(), 1e-9)

	none, err := Recorded(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestARecordedBuildIsNotCountedTwice(t *testing.T) {
	live := build("/r", "m", 700, 24, 30)
	backfilled := live
	backfilled.Started = live.Started.Add(3 * time.Second)
	older := build("/r", "m", 700, 24, 30)
	older.Started = older.Started.Add(-24 * time.Hour)
	assert.Len(t, Merge([]Run{live}, []Run{backfilled, older}), 2)
}

func realTempDir(t *testing.T) string {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func TestASymlinkedCheckoutIsTheSameRepository(t *testing.T) {
	root := realTempDir(t)
	main := filepath.Join(root, "code", "ade")
	require.NoError(t, os.MkdirAll(filepath.Join(main, ".git"), 0o750))
	require.NoError(t, os.Symlink(filepath.Join(root, "code"), filepath.Join(root, "src")))

	assert.Equal(t, main, Repo(filepath.Join(root, "src", "ade")))
}

func TestAWorktreeBelongsToItsMainCheckout(t *testing.T) {
	root := realTempDir(t)
	main := filepath.Join(root, "ade")
	tree := filepath.Join(root, "ade-feature")
	require.NoError(t, os.MkdirAll(filepath.Join(main, ".git", "worktrees", "ade-feature"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(tree, "sub"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(tree, ".git"),
		[]byte("gitdir: "+filepath.Join(main, ".git", "worktrees", "ade-feature")+"\n"), 0o600))

	assert.Equal(t, main, Repo(main))
	assert.Equal(t, main, Repo(filepath.Join(tree, "sub")))
}

const assistantLine = `{"type":"assistant","timestamp":"%s","cwd":"%s","requestId":"%s","message":{"id":"%s","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":"working"}],"usage":{"input_tokens":1000,"output_tokens":200,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`

func TestABuildPromptNamesAPathWithASpaceInIt(t *testing.T) {
	dir := filepath.Join(realTempDir(t), "My Code")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	path := filepath.Join(dir, "thing.md")
	require.NoError(t, os.WriteFile(path, []byte("# Thing\n"), 0o600))

	collapsed := strings.Join(strings.Fields(design.BuildPrompt(path)), " ")
	assert.Equal(t, "# Thing\n", docOf(collapsed, dir))
	assert.Empty(t, docOf(design.BuildOpening+dir+". - Read it.", dir), "a directory is not a design")
}

func TestABackfilledBuildMatchesOneRecordedLive(t *testing.T) {
	home := realTempDir(t)
	t.Setenv("HOME", home)
	cwd := filepath.Join(home, "work", "ade")
	require.NoError(t, os.MkdirAll(filepath.Join(cwd, ".git", "ade", "docs"), 0o750))
	doc := "# Thing\n\n- one\n- two\n- three\n"
	require.NoError(t, os.WriteFile(filepath.Join(cwd, ".git", "ade", "docs", "thing.md"), []byte(doc), 0o600))

	prompt, err := jsonString(design.BuildPrompt(".git/ade/docs/thing.md"))
	require.NoError(t, err)
	lines := []string{
		fmt.Sprintf(`{"type":"user","timestamp":"2026-09-01T10:00:00Z","cwd":"%s","message":{"role":"user","content":%s}}`, cwd, prompt),
		fmt.Sprintf(assistantLine, "2026-09-01T10:05:00Z", cwd, "r1", "m1"),
		fmt.Sprintf(assistantLine, "2026-09-01T10:31:30Z", cwd, "r2", "m2"),
		fmt.Sprintf(`{"type":"user","timestamp":"2026-09-01T11:00:00Z","cwd":"%s","message":{"role":"user","content":"thanks, now tidy up"}}`, cwd),
		fmt.Sprintf(assistantLine, "2026-09-01T11:02:00Z", cwd, "r3", "m3"),
	}
	path := filepath.Join(home, ".claude", "projects", "-work-ade", "b.jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))

	runs := Backfill(transcripts.Scan())
	require.Len(t, runs, 1)
	got := runs[0]

	started := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	live := Run{Kind: KindBuild, Started: started, Finished: started.Add(31*time.Minute + 30*time.Second)}
	assert.Equal(t, live.Duration(), got.Duration(), "the follow-up turn is not the build")
	assert.Equal(t, "claude-sonnet-4-5", got.Model)
	assert.Equal(t, cwd, got.Repo)
	words, items := Size(doc)
	assert.Equal(t, words, got.DocWords)
	assert.Equal(t, items, got.ChecklistItems)
	assert.Equal(t, 2000, got.Usage.Input)
	assert.Positive(t, got.Units())
}

func jsonString(s string) (string, error) {
	out, err := json.Marshal(s)
	return string(out), err
}
