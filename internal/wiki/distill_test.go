package wiki

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/transcripts"
)

const distillRepo = "owner/repo"

func distillReply(t *testing.T, log string, pages ...DistillPage) string {
	t.Helper()
	data, err := json.Marshal(DistillReply{Pages: pages, Log: log})
	require.NoError(t, err)
	return "Here it is.\n\n```ade-wiki\n" + string(data) + "\n```\n"
}

// saidIn writes a Claude transcript of user turns, dated an hour ago so the
// quiet period does not hold it back.
func saidIn(t *testing.T, dir, id, branch string, turns ...string) *transcripts.Session {
	t.Helper()
	path := filepath.Join(dir, id+".jsonl")
	appendTurns(t, path, turns...)
	return &transcripts.Session{Agent: transcripts.AgentClaude, ID: id, Path: path, Branch: branch}
}

func appendTurns(t *testing.T, path string, turns ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	for _, turn := range turns {
		line, err := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": turn}})
		require.NoError(t, err)
		_, err = f.Write(append(line, '\n'))
		require.NoError(t, err)
	}
	require.NoError(t, f.Close())
	old := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))
}

type fakeModel struct {
	prompts []string
	replies []string
	before  func()
}

func (m *fakeModel) run(_ context.Context, prompt string) (string, float64, error) {
	m.prompts = append(m.prompts, prompt)
	if m.before != nil {
		m.before()
	}
	if len(m.replies) == 0 {
		return "", 0, errors.New("no reply")
	}
	reply := m.replies[0]
	if len(m.replies) > 1 {
		m.replies = m.replies[1:]
	}
	return reply, 0.01, nil
}

func remotePage(t *testing.T, remote, name string) string {
	t.Helper()
	work := t.TempDir()
	run(t, "", "git", "clone", remote, work)
	data, err := os.ReadFile(filepath.Join(work, filepath.FromSlash(name)))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	require.NoError(t, err)
	return string(data)
}

func marks(t *testing.T, store *Store) distillState {
	t.Helper()
	state, err := store.distillState()
	require.NoError(t, err)
	return state
}

func TestParseDistillRefusesTheWholeReplyForOneBadPage(t *testing.T) {
	good := DistillPage{Path: "repos/owner/repo/topic.md", Body: "# Topic\n"}
	shown := map[string]int{"general/first.md": len("first\n")}
	exists := func(p string) bool { return p == "general/first.md" || p == "general/hidden.md" }

	for name, bad := range map[string]DistillPage{
		"escapes the root":        {Path: "../x.md", Body: "x"},
		"dot segment":             {Path: "general/../../x.md", Body: "x"},
		"another repository":      {Path: "repos/other/repo/x.md", Body: "x"},
		"not markdown":            {Path: "general/x.txt", Body: "x"},
		"the root index":          {Path: "index.md", Body: "x"},
		"the host's log":          {Path: "repos/owner/repo/log.md", Body: "x"},
		"a page it was not shown": {Path: "general/hidden.md", Body: "x"},
		"over the size cap":       {Path: "general/big.md", Body: strings.Repeat("x", maxDistillPage+1)},
		"hidden file":             {Path: "general/.x.md", Body: "x"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseDistill(distillReply(t, "", good, bad), distillRepo, shown, exists)
			assert.Error(t, err)
		})
	}

	t.Run("too many pages", func(t *testing.T) {
		var pages []DistillPage
		for i := range maxDistillPages + 1 {
			pages = append(pages, DistillPage{Path: "general/p" + string(rune('a'+i)) + ".md", Body: "x"})
		}
		_, err := ParseDistill(distillReply(t, "", pages...), distillRepo, shown, exists)
		assert.Error(t, err)
	})

	t.Run("no block", func(t *testing.T) {
		_, err := ParseDistill("nothing to add", distillRepo, shown, exists)
		assert.Error(t, err)
	})

	t.Run("a shown page and a new one", func(t *testing.T) {
		reply, err := ParseDistill(distillReply(t, "2026-09-24: noted", good, DistillPage{Path: "general/first.md", Body: "new\n"}), distillRepo, shown, exists)
		require.NoError(t, err)
		assert.Len(t, reply.Pages, 2)
	})
}

func TestDistillPublishesPagesAndTheLogThenMovesTheWatermark(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	session := saidIn(t, t.TempDir(), "s1", "feat/jobs", "the launcher owns the clock because the web server exits")
	model := &fakeModel{replies: []string{distillReply(t, "recorded who owns the job clock",
		DistillPage{Path: "repos/owner/repo/jobs.md", Body: "# Jobs\n\nThe launcher owns the clock."},
		DistillPage{Path: "repos/owner/repo/status/feat/jobs.md", Body: "# feat/jobs\n\n- pause button still missing"},
	)}}

	out, err := store.Distill(context.Background(), DistillRun{Repo: distillRepo, Sessions: []*transcripts.Session{session}, Model: model.run})
	require.NoError(t, err)
	assert.Equal(t, "published", out.State)
	assert.NotEmpty(t, out.Commit)
	require.Len(t, model.prompts, 1)
	assert.Contains(t, model.prompts[0], "the launcher owns the clock")
	assert.Contains(t, model.prompts[0], "branch feat/jobs: repos/owner/repo/status/feat/jobs.md")
	assert.Contains(t, model.prompts[0], `<page path="general/first.md">`)

	assert.Equal(t, "# Jobs\n\nThe launcher owns the clock.\n", remotePage(t, remote, "repos/owner/repo/jobs.md"))
	assert.Contains(t, remotePage(t, remote, "repos/owner/repo/log.md"), ": recorded who owns the job clock\n")
	assert.NoDirExists(t, store.DraftRoot(distillSession(distillRepo)))

	again, err := store.Distill(context.Background(), DistillRun{Repo: distillRepo, Sessions: []*transcripts.Session{session}, Model: model.run})
	require.NoError(t, err)
	assert.Equal(t, DistillNothing, again.State)
	assert.Len(t, model.prompts, 1, "nothing new is not a run")
}

func TestARejectedReplyLeavesTheWatermarkAndThreeGiveUpOnIt(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	session := saidIn(t, t.TempDir(), "s1", "main", "something durable")
	model := &fakeModel{replies: []string{distillReply(t, "", DistillPage{Path: "repos/other/repo/x.md", Body: "x"})}}
	distill := func(manual bool) DistillOutcome {
		out, _ := store.Distill(context.Background(), DistillRun{Repo: distillRepo, Sessions: []*transcripts.Session{session}, Model: model.run, Manual: manual})
		return out
	}

	// The watermark stays put while there is still hope for this prose, so the
	// same input is offered again.
	for range maxDistillFailures - 1 {
		assert.Equal(t, "rejected", distill(false).State)
		assert.Zero(t, marks(t, store).Read[session.Path].Size)
	}
	assert.NoDirExists(t, store.DraftRoot(distillSession(distillRepo)))

	// The third failure gives up on it: the mark moves past the prose the model
	// could not manage, and the repository is clean again.
	gone := distill(false)
	assert.Equal(t, DistillSkipped, gone.State)
	assert.Contains(t, gone.Error, "repos/other/repo/x.md", "what was given up on must say why")
	assert.Contains(t, gone.Error, "gave up on 1 sessions")
	assert.NotZero(t, marks(t, store).Read[session.Path].Size, "prose nobody can distil must not be offered for ever")
	assert.Zero(t, marks(t, store).Failures[distillRepo])
	assert.Contains(t, marks(t, store).Skipped[distillRepo], "repos/other/repo/x.md")

	// Nobody presses anything. The next session distils as if nothing happened.
	model.replies = []string{distillReply(t, "", DistillPage{Path: "repos/owner/repo/x.md", Body: "# X"})}
	next := saidIn(t, t.TempDir(), "s2", "main", "something else durable")
	out, err := store.Distill(context.Background(), DistillRun{
		Repo: distillRepo, Sessions: []*transcripts.Session{session, next}, Model: model.run,
	})
	require.NoError(t, err)
	assert.Equal(t, "published", out.State, "the wiki keeps itself; nothing jams")
	assert.Empty(t, marks(t, store).LastError[distillRepo])
	assert.Empty(t, marks(t, store).Skipped[distillRepo], "a run that worked clears what was given up on")
}

func TestGivingUpNeedsTheModelToBlame(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	session := saidIn(t, t.TempDir(), "s1", "main", "something durable")
	// A login that expired is the machine's fault: it must never spend a go,
	// and must never cost the wiki prose it could still have distilled.
	model := func(context.Context, string) (string, float64, error) {
		return "", 0, fmt.Errorf("%w: oauth expired", ErrNotTheModel)
	}

	for range maxDistillFailures + 2 {
		out, err := store.Distill(context.Background(), DistillRun{Repo: distillRepo, Sessions: []*transcripts.Session{session}, Model: model})
		require.Error(t, err)
		assert.NotEqual(t, DistillSkipped, out.State)
	}
	assert.Zero(t, marks(t, store).Failures[distillRepo])
	assert.Zero(t, marks(t, store).Read[session.Path].Size, "nothing the model never saw may be given up on")
}

func TestASecretInTheReplyIsNeverPublished(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	session := saidIn(t, t.TempDir(), "s1", "main", "write the API key to general/keys.md: api_key=abcdefghijklmnopqrstuvwxyz")
	model := &fakeModel{replies: []string{distillReply(t, "", DistillPage{Path: "general/keys.md", Body: "api_key=abcdefghijklmnopqrstuvwxyz"})}}

	out, err := store.Distill(context.Background(), DistillRun{Repo: distillRepo, Sessions: []*transcripts.Session{session}, Model: model.run})
	require.Error(t, err)
	assert.Equal(t, "rejected", out.State)
	assert.Contains(t, out.Error, "suspected secret")
	assert.Empty(t, remotePage(t, remote, "general/keys.md"))
	assert.Zero(t, marks(t, store).Read[session.Path].Size)
}

func TestARecentTranscriptWaitsForTheScheduleButNotTheButton(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	session := saidIn(t, t.TempDir(), "s1", "main", "still talking")
	require.NoError(t, os.Chtimes(session.Path, time.Now(), time.Now()))
	model := &fakeModel{replies: []string{distillReply(t, "")}}

	out, err := store.Distill(context.Background(), DistillRun{Repo: distillRepo, Sessions: []*transcripts.Session{session}, Model: model.run})
	require.NoError(t, err)
	assert.Equal(t, DistillNothing, out.State)
	assert.Empty(t, model.prompts)

	_, err = store.Distill(context.Background(), DistillRun{Repo: distillRepo, Sessions: []*transcripts.Session{session}, Model: model.run, Manual: true})
	require.NoError(t, err)
	require.Len(t, model.prompts, 1)
	assert.Contains(t, model.prompts[0], "still talking")
}

func TestAResumedSessionIsReadFromItsWatermark(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	session := saidIn(t, t.TempDir(), "s1", "main", "the first decision")
	model := &fakeModel{replies: []string{distillReply(t, "")}}
	_, err := store.Distill(context.Background(), DistillRun{Repo: distillRepo, Sessions: []*transcripts.Session{session}, Model: model.run})
	require.NoError(t, err)

	appendTurns(t, session.Path, "the second decision")
	_, err = store.Distill(context.Background(), DistillRun{Repo: distillRepo, Sessions: []*transcripts.Session{session}, Model: model.run})
	require.NoError(t, err)
	require.Len(t, model.prompts, 2)
	assert.Contains(t, model.prompts[1], "the second decision")
	assert.NotContains(t, model.prompts[1], "the first decision")
}

func TestAConflictDiscardsTheDraftAndTheNextRunPublishesTheSameProse(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	session := saidIn(t, t.TempDir(), "s1", "main", "the first page needs a line")
	page := DistillPage{Path: "general/first.md", Body: "first\n\nfrom the distiller"}
	model := &fakeModel{replies: []string{distillReply(t, "", page)}}
	model.before = func() { pushPage(t, remote, "general/first.md", "someone else's edit") }

	out, err := store.Distill(context.Background(), DistillRun{Repo: distillRepo, Sessions: []*transcripts.Session{session}, Model: model.run})
	require.Error(t, err)
	assert.Equal(t, "conflict", out.State)
	assert.NoDirExists(t, store.DraftRoot(distillSession(distillRepo)))
	assert.Empty(t, store.draftRecords())
	assert.Zero(t, marks(t, store).Read[session.Path].Size)

	model.before = nil
	out, err = store.Distill(context.Background(), DistillRun{Repo: distillRepo, Sessions: []*transcripts.Session{session}, Model: model.run})
	require.NoError(t, err)
	assert.Equal(t, "published", out.State)
	assert.Contains(t, model.prompts[1], "the first page needs a line")
	assert.Contains(t, model.prompts[1], "someone else's edit", "the next run sees the new base")
}

func TestTwoLaptopsOnDifferentBranchesBothPublish(t *testing.T) {
	remote := newRemote(t)
	laptopA, laptopB := New(t.TempDir(), remote), New(t.TempDir(), remote)
	sessionA := saidIn(t, t.TempDir(), "a", "feat/a", "working on a")
	sessionB := saidIn(t, t.TempDir(), "b", "feat/b", "working on b")
	modelA := &fakeModel{replies: []string{distillReply(t, "", DistillPage{Path: StatusPath(distillRepo, "feat/a"), Body: "# feat/a"})}}
	modelB := &fakeModel{replies: []string{distillReply(t, "", DistillPage{Path: StatusPath(distillRepo, "feat/b"), Body: "# feat/b"})}}
	// A publishes while B is waiting on its model, so B's base is behind.
	modelB.before = func() {
		out, err := laptopA.Distill(context.Background(), DistillRun{Repo: distillRepo, Sessions: []*transcripts.Session{sessionA}, Model: modelA.run})
		require.NoError(t, err)
		require.Equal(t, "published", out.State)
	}

	out, err := laptopB.Distill(context.Background(), DistillRun{Repo: distillRepo, Sessions: []*transcripts.Session{sessionB}, Model: modelB.run})
	require.NoError(t, err)
	assert.Equal(t, "published", out.State)
	assert.Equal(t, "# feat/a\n", remotePage(t, remote, "repos/owner/repo/status/feat/a.md"))
	assert.Equal(t, "# feat/b\n", remotePage(t, remote, "repos/owner/repo/status/feat/b.md"))
}

func TestADryRunPublishesNothingAndMovesNothing(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	session := saidIn(t, t.TempDir(), "s1", "main", "a decision")
	model := &fakeModel{replies: []string{distillReply(t, "", DistillPage{Path: "general/new.md", Body: "# New"})}}

	out, err := store.Distill(context.Background(), DistillRun{Repo: distillRepo, Sessions: []*transcripts.Session{session}, Model: model.run, DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, DistillDry, out.State)
	assert.Equal(t, []string{"general/new.md"}, out.Pages)
	assert.NotEmpty(t, out.Prompt)
	assert.Empty(t, remotePage(t, remote, "general/new.md"))
	assert.Zero(t, marks(t, store).Read[session.Path].Size)
	assert.NoDirExists(t, store.DraftRoot(distillSession(distillRepo)))
}

func TestStartingSkipsWhatWasAlreadySaid(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	session := saidIn(t, t.TempDir(), "s1", "main", "said before it was turned on")
	assert.False(t, store.DistillStarted())
	require.NoError(t, store.StartDistilling([]string{session.Path}))
	assert.True(t, store.DistillStarted())

	model := &fakeModel{}
	out, err := store.Distill(context.Background(), DistillRun{Repo: distillRepo, Sessions: []*transcripts.Session{session}, Model: model.run})
	require.NoError(t, err)
	assert.Equal(t, DistillNothing, out.State)
	assert.Empty(t, model.prompts)
}

func TestAWatermarkFileThatWillNotParseStopsTheRun(t *testing.T) {
	store := New(t.TempDir(), newRemote(t))
	session := saidIn(t, t.TempDir(), "s1", "main", "a decision")
	require.NoError(t, store.StartDistilling([]string{session.Path}))
	require.NoError(t, os.WriteFile(store.distillPath(), []byte(`{"read":`), 0o600))

	assert.True(t, store.DistillStarted())
	model := &fakeModel{}
	_, err := store.Distill(context.Background(), DistillRun{Repo: distillRepo, Sessions: []*transcripts.Session{session}, Model: model.run})
	require.Error(t, err, "an empty watermark would read every transcript again")
	assert.Empty(t, model.prompts)
}

func TestTurningDistillationOffForgetsTheMarks(t *testing.T) {
	store := New(t.TempDir(), newRemote(t))
	session := saidIn(t, t.TempDir(), "s1", "main", "a decision")
	require.NoError(t, store.StartDistilling([]string{session.Path}))
	require.NoError(t, store.StopDistilling())
	assert.False(t, store.DistillStarted(), "turning it on again starts from the current end")
	require.NoError(t, store.StopDistilling(), "forgetting twice is not a failure")
}

func TestARewriteMayNotDropMostOfAShownPage(t *testing.T) {
	body := strings.Repeat("a durable line\n", 100)
	shown := map[string]int{"general/first.md": len(body)}
	exists := func(p string) bool { return p == "general/first.md" }

	_, err := ParseDistill(distillReply(t, "", DistillPage{Path: "general/first.md", Body: "# Index\n"}), distillRepo, shown, exists)
	assert.Error(t, err, "a page replaced by a line is a delete in all but name")

	_, err = ParseDistill(distillReply(t, "", DistillPage{Path: "general/first.md", Body: body + "one more\n"}), distillRepo, shown, exists)
	assert.NoError(t, err)
}

func TestAStatusPageMayShrinkAsItsWorkLands(t *testing.T) {
	status := StatusPath(distillRepo, "main")
	body := strings.Repeat("in flight, still\n", 100)
	shown := map[string]int{status: len(body)}
	exists := func(p string) bool { return p == status }

	_, err := ParseDistill(distillReply(t, "", DistillPage{Path: status, Body: "# main\n\nNothing in flight.\n"}), distillRepo, shown, exists)
	assert.NoError(t, err, "work landing is the page getting shorter, not a page being dropped")

	_, err = ParseDistill(distillReply(t, "", DistillPage{Path: status, Body: "  \n"}), distillRepo, shown, exists)
	assert.Error(t, err, "empty is still empty")
}

func TestAPageCopiedBackKeepsItsClosingTags(t *testing.T) {
	shown := map[string]int{"general/first.md": 200}
	reply := distillReply(t, "", DistillPage{Path: "general/first.md", Body: strings.Repeat("x", 150) + `<details>a<\/details>`})
	parsed, err := ParseDistill(reply, distillRepo, shown, func(string) bool { return true })
	require.NoError(t, err)
	assert.Contains(t, parsed.Pages[0].Body, "</details>")
}

func TestTheLogIsNewestFirstUnderItsHeading(t *testing.T) {
	now := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	got := prependLog("# Log\n\n- 2026-09-20: older\n", "newer", now)
	assert.Equal(t, "# Log\n\n- 2026-09-24: newer\n- 2026-09-20: older\n", got)
	assert.Equal(t, "# Log\n\n- 2026-09-24: first\n", prependLog("", "2026-09-24: first", now))
}

func TestAHeldReasonIsCutOnACharacterNotAByte(t *testing.T) {
	why := strings.Repeat("a", 198) + "—— and on it goes"
	got := firstLine(why)
	assert.True(t, utf8.ValidString(got), "half a rune is written out as a replacement mark")
	assert.LessOrEqual(t, len(got), 203+len("…"))
	assert.Equal(t, "one line", firstLine(" one line\nand another\n"))
}

func TestALoginThatExpiredIsNotTheModelsFault(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	session := saidIn(t, t.TempDir(), "s1", "main", "something durable")
	model := func(context.Context, string) (string, float64, error) {
		return "", 0, fmt.Errorf("%w: claude failed to authenticate", ErrNotTheModel)
	}
	for range maxDistillFailures + 1 {
		out, err := store.Distill(context.Background(), DistillRun{
			Repo: distillRepo, Sessions: []*transcripts.Session{session}, Model: model})
		require.Error(t, err)
		assert.Equal(t, "failed", out.State, "a night of expired tokens must not hold the job for days")
	}
	assert.Zero(t, marks(t, store).Failures[distillRepo])
}

func TestABlamedRunAlwaysRecordsAReason(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	session := saidIn(t, t.TempDir(), "s1", "main", "something durable")
	// A model that answers nothing at all: no error to quote, and a state the
	// brake still counts against the repository.
	model := &fakeModel{replies: []string{""}}

	out, _ := store.Distill(context.Background(), DistillRun{Repo: distillRepo, Sessions: []*transcripts.Session{session}, Model: model.run})
	require.NotEqual(t, "published", out.State)
	assert.NotEmpty(t, out.Error)
	assert.Equal(t, 1, marks(t, store).Failures[distillRepo])
	assert.NotEmpty(t, marks(t, store).LastError[distillRepo],
		"a failure counted against the repository must say why")
	assert.NotContains(t, heldWhy(marks(t, store).LastError[distillRepo]), "no reason recorded")
}

func TestStatusReportsWhatIsFailingWithoutARun(t *testing.T) {
	store := New(t.TempDir(), newRemote(t))
	require.NoError(t, store.withDistillState(func(state *distillState) {
		state.Failures[distillRepo] = 2
		state.LastError = map[string]string{distillRepo: "repos/other/repo/x.md: outside scope"}
		state.Skipped = map[string]string{"owner/older": "gave up on 3 sessions"}
	}))

	got, err := store.DistillStatus()
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "owner/older", got[0].Repo)
	assert.Equal(t, "gave up on 3 sessions", got[0].Skipped)
	assert.Equal(t, distillRepo, got[1].Repo)
	assert.Equal(t, 2, got[1].Failures)
	assert.Contains(t, got[1].LastError, "outside scope")
}

// The job gave up on owenrumney/ade with "status/main.md: 16416 bytes, 16384 at
// most" — 32 over a limit the prompt never mentioned. A cap the model cannot
// read is a cap it cannot keep to, and three failures skip the repository.
func TestThePromptStatesThePageSizeItRefuses(t *testing.T) {
	got := distillPrompt("owenrumney/ade", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		nil, nil, []Spoken{{Session: "s1", Branch: "main", Text: "we moved the clock"}})

	assert.Contains(t, got, strconv.Itoa(maxDistillPage),
		"the model has to be told the byte cap its reply is measured against")
	assert.Contains(t, got, strconv.Itoa(maxStatusPage),
		"and what a status page is meant to stay under")
	assert.Contains(t, got, "refused whole",
		"and that one page over the cap loses the other pages with it")
}

// The numbers in the prompt are the ones the check uses, or the prompt is
// telling the model a limit nothing enforces.
func TestThePromptsCapIsTheOneEnforced(t *testing.T) {
	body := strings.Repeat("x", maxDistillPage+1)
	reply := "```" + distillFence + `
{"pages":[{"path":"repos/o/r/topic.md","body":"` + body + `","why":"y"}],"log":"l"}
` + "```"

	_, err := ParseDistill(reply, "o/r", map[string]int{}, func(string) bool { return false })
	require.Error(t, err)
	assert.Contains(t, err.Error(), strconv.Itoa(maxDistillPage))
}
