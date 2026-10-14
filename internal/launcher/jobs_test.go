package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/jobs"
	"github.com/owenrumney/ade/internal/limits"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/wiki"
)

// withJob stands a launcher up over a home holding one job definition, and a
// runner that answers instead of spawning claude.
func withJob(t *testing.T, name, body string, run jobs.Runner) *Server {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".ade", "jobs")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o600))

	s := newTestLauncher(t)
	s.jobs.run = run
	// No network from the spend gate: the reading is not what is under test.
	s.limits.Ask = func(context.Context) (limits.Limits, error) { return limits.Limits{}, nil }
	return s
}

func listJobs(t *testing.T, s *Server) []jobs.View {
	t.Helper()
	rec := do(t, s, http.MethodGet, "/api/jobs", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var out struct {
		Jobs    []jobs.View    `json:"jobs"`
		Skipped []jobs.Skipped `json:"skipped"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	return out.Jobs
}

func TestTheClockFiresAJobThatHasNeverFired(t *testing.T) {
	fired := make(chan string, 2)
	s := withJob(t, "nightly", "---\nschedule: \"0 2 * * *\"\n---\n\n/catchup\n",
		func(_ context.Context, req jobs.Request) (jobs.Result, error) {
			fired <- req.Prompt
			return jobs.Result{Text: "one check failed"}, nil
		})

	s.tickJobs(context.Background())
	s.jobs.inflight.Wait()

	select {
	case prompt := <-fired:
		assert.Contains(t, prompt, "/catchup")
	default:
		t.Fatal("the job never fired")
	}
}

func TestAJobFiresOnceAndThenWaitsForItsWindow(t *testing.T) {
	var runs int
	s := withJob(t, "nightly", "---\nschedule: \"0 2 * * *\"\n---\n\n/catchup\n",
		func(context.Context, jobs.Request) (jobs.Result, error) {
			runs++
			return jobs.Result{Text: "ok"}, nil
		})

	rec := do(t, s, http.MethodPost, "/api/jobs/run", `{"name":"nightly"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, runs)

	// The clock looks again with the window now in the future.
	s.tickJobs(context.Background())
	s.jobs.inflight.Wait()
	assert.Equal(t, 1, runs, "a fired job does not fire again until its next window")
}

func TestRunNowAnswersWithTheReport(t *testing.T) {
	s := withJob(t, "nightly", "---\nschedule: \"0 2 * * *\"\n---\n\n/catchup\n",
		func(context.Context, jobs.Request) (jobs.Result, error) {
			return jobs.Result{Text: "two PRs went green", Session: "sess-9"}, nil
		})

	rec := do(t, s, http.MethodPost, "/api/jobs/run", `{"name":"nightly"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var out struct{ Outcome jobs.Outcome }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, "two PRs went green", out.Outcome.Report)
	assert.Equal(t, "sess-9", out.Outcome.Session)

	rows := listJobs(t, s)
	require.Len(t, rows, 1)
	require.Len(t, rows[0].Outcomes, 1)
	assert.NotNil(t, rows[0].NextDue)
}

func TestAJobWhoseProjectIsNotHereSaysSoRatherThanCloning(t *testing.T) {
	s := withJob(t, "morning", "---\nschedule: \"0 8 * * *\"\nproject: github.com/nobody/nothing\n---\n\n/catchup\n",
		func(context.Context, jobs.Request) (jobs.Result, error) {
			t.Fatal("it should not have run")
			return jobs.Result{}, nil
		})

	rec := do(t, s, http.MethodPost, "/api/jobs/run", `{"name":"morning"}`)
	require.Equal(t, http.StatusOK, rec.Code)

	rows := listJobs(t, s)
	require.Len(t, rows, 1)
	require.Len(t, rows[0].Outcomes, 1)
	assert.Contains(t, rows[0].Outcomes[0].Error, "not checked out on this machine")
}

func TestPausingStopsTheClockAndTheListSaysSo(t *testing.T) {
	var runs int
	s := withJob(t, "nightly", "---\nschedule: \"* * * * *\"\n---\n\n/catchup\n",
		func(context.Context, jobs.Request) (jobs.Result, error) {
			runs++
			return jobs.Result{Text: "ok"}, nil
		})

	rec := do(t, s, http.MethodPost, "/api/jobs/pause", `{"name":"nightly","paused":true}`)
	require.Equal(t, http.StatusOK, rec.Code)

	s.tickJobs(context.Background())
	s.jobs.inflight.Wait()
	assert.Zero(t, runs)
	assert.True(t, listJobs(t, s)[0].Paused)
}

func TestAReportIsPutAwayBySeeingIt(t *testing.T) {
	s := withJob(t, "nightly", "---\nschedule: \"0 2 * * *\"\n---\n\n/catchup\n",
		func(context.Context, jobs.Request) (jobs.Result, error) {
			return jobs.Result{Text: "something happened"}, nil
		})
	require.Equal(t, http.StatusOK, do(t, s, http.MethodPost, "/api/jobs/run", `{"name":"nightly"}`).Code)

	rec := do(t, s, http.MethodPost, "/api/jobs/seen", `{"name":"nightly"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, listJobs(t, s)[0].Outcomes[0].Seen)
}

func TestAJobThatIsNotOnThisMachineIsRefused(t *testing.T) {
	s := withJob(t, "nightly", "---\nschedule: \"0 2 * * *\"\n---\n\n/catchup\n", nil)

	for _, route := range []string{"/api/jobs/run", "/api/jobs/pause", "/api/jobs/seen"} {
		rec := do(t, s, http.MethodPost, route, `{"name":"invented"}`)
		assert.Equalf(t, http.StatusBadRequest, rec.Code, "%s took a name it was never offered", route)
	}
}

func TestABrokenDefinitionIsListedRatherThanVanishing(t *testing.T) {
	s := withJob(t, "broken", "---\nschedule: \"0 99 * * *\"\n---\n\n/catchup\n", nil)

	rec := do(t, s, http.MethodGet, "/api/jobs", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var out struct {
		Jobs    []jobs.View    `json:"jobs"`
		Skipped []jobs.Skipped `json:"skipped"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Empty(t, out.Jobs)
	require.Len(t, out.Skipped, 1)
	assert.Contains(t, out.Skipped[0].Error, "outside 0-23")
}

func TestTheJobsRoutesNeedTheToken(t *testing.T) {
	s := withJob(t, "nightly", "---\nschedule: \"0 2 * * *\"\n---\n\n/catchup\n", nil)

	// A job fires a process on the subscription, so an unauthenticated page on
	// this machine must not be able to ask for one.
	for _, route := range [][2]string{
		{http.MethodGet, "/api/jobs"},
		{http.MethodPost, "/api/jobs/run"},
		{http.MethodPost, "/api/jobs/pause"},
		{http.MethodPost, "/api/jobs/seen"},
		{http.MethodPost, "/api/jobs/save"},
		{http.MethodPost, "/api/jobs/delete"},
	} {
		req := httptest.NewRequest(route[0], route[1], strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		s.routes().ServeHTTP(rec, req)
		assert.Equalf(t, http.StatusForbidden, rec.Code, "%s was open", route[1])
	}
}

func TestOneJobDoesNotRunTwiceAtOnce(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	s := withJob(t, "nightly", "---\nschedule: \"0 2 * * *\"\n---\n\n/catchup\n",
		func(context.Context, jobs.Request) (jobs.Result, error) {
			entered <- struct{}{}
			<-release
			return jobs.Result{Text: "ok"}, nil
		})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.fire(context.Background(), mustJob(t, s, "nightly"))
	}()
	<-entered

	_, err := s.fire(context.Background(), mustJob(t, s, "nightly"))
	assert.ErrorContains(t, err, "already running")

	close(release)
	<-done
}

func mustJob(t *testing.T, s *Server, name string) jobs.Job {
	t.Helper()
	job, ok := jobs.Find(mustList(s.jobsList()), name)
	require.True(t, ok)
	return job
}

func TestTheFormWritesAJobTheClockThenFinds(t *testing.T) {
	s := withJob(t, "nightly", "---\nschedule: \"0 2 * * *\"\n---\n\n/catchup\n", nil)

	rec := do(t, s, http.MethodPost, "/api/jobs/save",
		`{"mode":"create","name":"morning","schedule":"0 8 * * 1-5","prompt":"/catchup","description":"overnight"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var saved jobs.View
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &saved))
	assert.Equal(t, "fresh", saved.Continuity)
	assert.Equal(t, "reads", saved.Allow)
	// A job that has never fired is due, so a new one goes off on the next
	// tick rather than waiting for its first window.
	assert.Nil(t, saved.NextDue)
	assert.Nil(t, saved.LastFired)

	names := []string{}
	for _, view := range listJobs(t, s) {
		names = append(names, view.Name)
	}
	assert.Equal(t, []string{"morning", "nightly"}, names)
}

func TestASavedJobCarriesItsPromptBackToTheForm(t *testing.T) {
	s := withJob(t, "nightly", "---\nschedule: \"0 2 * * *\"\n---\n\n/catchup\n", nil)

	list := listJobs(t, s)
	require.Len(t, list, 1)
	assert.Equal(t, "/catchup", list[0].Prompt)
}

func TestASavedJobIsRefusedWhenTheLoaderWouldSkipIt(t *testing.T) {
	s := withJob(t, "nightly", "---\nschedule: \"0 2 * * *\"\n---\n\n/catchup\n", nil)

	rec := do(t, s, http.MethodPost, "/api/jobs/save", `{"mode":"create","name":"bad","schedule":"0 99 * * *","prompt":"/x"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "outside 0-23")
	assert.Len(t, listJobs(t, s), 1)
}

func TestDeletingAJobTakesItOffTheList(t *testing.T) {
	s := withJob(t, "nightly", "---\nschedule: \"0 2 * * *\"\n---\n\n/catchup\n", nil)

	rec := do(t, s, http.MethodPost, "/api/jobs/delete", `{"name":"nightly"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, listJobs(t, s))

	// And again: nothing to delete is an answer, not a silence.
	assert.Equal(t, http.StatusBadRequest,
		do(t, s, http.MethodPost, "/api/jobs/delete", `{"name":"nightly"}`).Code)
}

// A create landing on a name already taken destroyed the old definition, so
// the route has to carry which the form meant.
func TestSavingAJobSaysWhetherItIsNewOrAnEdit(t *testing.T) {
	s := withJob(t, "nightly", "---\nschedule: \"0 2 * * *\"\n---\n\n/catchup\n", nil)

	rec := do(t, s, http.MethodPost, "/api/jobs/save",
		`{"mode":"create","name":"nightly","schedule":"@every 1h","prompt":"/other"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "already a job called nightly")

	rec = do(t, s, http.MethodPost, "/api/jobs/save",
		`{"mode":"replace","name":"nightly","schedule":"@every 1h","prompt":"/other"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	list := listJobs(t, s)
	require.Len(t, list, 1)
	assert.Equal(t, "@every 1h", list[0].Schedule)
}

// A fire in flight writes its state back when it finishes, which would
// recreate what Remove deleted.
func TestAJobThatIsRunningIsNotDeletedUnderIt(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	s := withJob(t, "nightly", "---\nschedule: \"0 2 * * *\"\n---\n\n/catchup\n",
		func(context.Context, jobs.Request) (jobs.Result, error) {
			entered <- struct{}{}
			<-release
			return jobs.Result{Text: "ok"}, nil
		})

	// handleJobRun keeps the slot until it answers, so the request itself is
	// what to wait on rather than the tick's group.
	ran := make(chan struct{})
	go func() {
		defer close(ran)
		_ = do(t, s, http.MethodPost, "/api/jobs/run", `{"name":"nightly"}`)
	}()
	<-entered

	rec := do(t, s, http.MethodPost, "/api/jobs/delete", `{"name":"nightly"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "running")

	close(release)
	<-ran

	// And once it is done, it goes.
	rec = do(t, s, http.MethodPost, "/api/jobs/delete", `{"name":"nightly"}`)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, listJobs(t, s))
}

// A session distils itself now, so nothing schedules this. The launcher still
// runs a job of the kind, which is the only way back to a schedule.
func TestAWikiDistillJobStillRunsWhenOneIsWritten(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	remote := wikiRemote(t)
	require.NoError(t, settings.Save(settings.Settings{WikiRemote: remote}))
	s := newTestLauncher(t)
	s.limits.Ask = func(context.Context) (limits.Limits, error) { return limits.Limits{}, nil }
	_, found := jobs.Find(mustList(s.jobsList()), jobs.KindWikiDistill)
	assert.False(t, found, "the session owns its own distillation")

	scan := filepath.Join(home, "code")
	checkout := filepath.Join(scan, "repo")
	require.NoError(t, os.MkdirAll(checkout, 0o700))
	gitRun(t, checkout, "init", "--initial-branch", "main")
	gitRun(t, checkout, "remote", "add", "origin", "git@github.com:owner/repo.git")
	require.NoError(t, settings.Save(settings.Settings{ScanDirs: []string{scan}, WikiRemote: remote, WikiDistill: true}))
	s.wiki = wiki.New(filepath.Join(home, ".ade"), remote)
	// The run refreshes the wiki in the background; let it finish before cleanup.
	t.Cleanup(func() {
		assert.Eventually(t, func() bool { return !s.wikiSyncing.Load() }, 10*time.Second, 10*time.Millisecond)
	})
	var prompts []string
	s.jobs.distill = func(_ context.Context, prompt string) (string, float64, error) {
		prompts = append(prompts, prompt)
		return "```ade-wiki\n{\"pages\":[{\"path\":\"repos/owner/repo/topic.md\",\"body\":\"# Topic\"}],\"log\":\"\"}\n```", 0, nil
	}
	transcript := filepath.Join(home, ".claude", "projects", "-repo", "s1.jsonl")
	say := func(text string) {
		line, err := json.Marshal(map[string]any{"type": "user", "cwd": checkout, "message": map[string]any{"role": "user", "content": text}})
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Dir(transcript), 0o700))
		f, err := os.OpenFile(transcript, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		require.NoError(t, err)
		_, err = f.Write(append(line, '\n'))
		require.NoError(t, err)
		require.NoError(t, f.Close())
	}
	say("said before it was turned on")

	run := func() jobs.Outcome {
		out, err := s.fireJob(context.Background(), jobs.WikiDistillJob(), true, "")
		require.NoError(t, err)
		return out
	}
	assert.Contains(t, run().Report, "started")
	assert.Empty(t, prompts)

	say("the launcher owns the clock")
	out := run()
	assert.Empty(t, out.Error)
	assert.Contains(t, out.Report, "owner/repo: published repos/owner/repo/topic.md")
	require.Len(t, prompts, 1)
	assert.Contains(t, prompts[0], "the launcher owns the clock")
	assert.NotContains(t, prompts[0], "said before it was turned on")
}

// A writes job run by hand starts a session beside the checkout you name, and
// comes back with where it went rather than waiting on it.
func TestRunNowStartsAWritesJobBesideTheCheckoutYouName(t *testing.T) {
	var asked struct{ dir, name string }
	s := withJob(t, "bump", "---\nschedule: adhoc\nallow: writes\n---\n\nbump it\n",
		func(context.Context, jobs.Request) (jobs.Result, error) {
			t.Fatal("a writes job is not a cold run")
			return jobs.Result{}, nil
		})
	s.jobs.start = func(_ context.Context, dir, name, prompt string) (jobs.Started, error) {
		asked.dir, asked.name = dir, name
		assert.Contains(t, prompt, "bump it")
		return jobs.Started{URL: "http://127.0.0.1:1/?t=x", Path: filepath.Join(dir, "..", name)}, nil
	}
	repo := filepath.Join(t.TempDir(), "ade")
	require.NoError(t, exec.Command("git", "init", "-q", repo).Run())
	inside := filepath.Join(repo, "internal")
	require.NoError(t, os.MkdirAll(inside, 0o755))

	rec := do(t, s, http.MethodPost, "/api/jobs/run", `{"name":"bump","dir":"`+inside+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out struct{ Outcome jobs.Outcome }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, "http://127.0.0.1:1/?t=x", out.Outcome.URL)
	assert.Contains(t, out.Outcome.Report, asked.name)
	assert.Equal(t, repo, asked.dir, "the job works beside the checkout, not the directory inside it")
	assert.True(t, strings.HasPrefix(asked.name, "bump-20"), asked.name)

	rows := listJobs(t, s)
	require.Len(t, rows, 1)
	require.Len(t, rows[0].Outcomes, 1)
	assert.Equal(t, filepath.Join(repo, "..", asked.name), rows[0].Outcomes[0].Dir)
}

func TestAWritesJobRefusesADirThatIsNoCheckout(t *testing.T) {
	s := withJob(t, "bump", "---\nschedule: adhoc\nallow: writes\n---\n\nx\n", nil)
	s.jobs.start = func(context.Context, string, string, string) (jobs.Started, error) {
		t.Fatal("nothing should start")
		return jobs.Started{}, nil
	}
	rec := do(t, s, http.MethodPost, "/api/jobs/run", `{"name":"bump","dir":"`+t.TempDir()+`"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out struct{ Outcome jobs.Outcome }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Contains(t, out.Outcome.Error, "not a checkout")
}

// A repository's own jobs reach the list when a request names the checkout.
func TestListingJobsForACheckoutIncludesItsOwn(t *testing.T) {
	s := withJob(t, "nightly", "---\nschedule: \"0 2 * * *\"\n---\n\n/catchup\n", nil)
	repo := filepath.Join(t.TempDir(), "ade")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".ade", "jobs"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, ".ade", "jobs", "bump.md"),
		[]byte("---\nschedule: adhoc\nallow: writes\n---\n\nx\n"), 0o600))

	rec := do(t, s, http.MethodGet, "/api/jobs?dir="+repo, "")
	require.Equal(t, http.StatusOK, rec.Code)
	var out struct{ Jobs []jobs.View }
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	names := map[string]string{}
	for _, j := range out.Jobs {
		names[j.Name] = j.Scope
	}
	assert.Equal(t, "repo", names["bump"])
	assert.Equal(t, "user", names["nightly"])
}
