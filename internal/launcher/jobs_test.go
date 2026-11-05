package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/jobs"
)

func jobsLauncher(t *testing.T) *Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	return newTestLauncher(t)
}

func listJobs(t *testing.T, s *Server) []jobs.View {
	t.Helper()
	rec := do(t, s, http.MethodGet, "/api/jobs", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var out struct {
		Jobs    []jobs.View     `json:"jobs"`
		Skipped json.RawMessage `json:"skipped"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.JSONEq(t, `[]`, string(out.Skipped), "the page reads a list, never null")
	return out.Jobs
}

func TestMyDayIsTheOnlyJobAndNothingFiresItOnAClock(t *testing.T) {
	rows := listJobs(t, jobsLauncher(t))

	require.Len(t, rows, 1)
	assert.Equal(t, jobs.KindMyDay, rows[0].Name)
	assert.Equal(t, jobs.ScopeBuiltin, rows[0].Scope)
	assert.Equal(t, jobs.Manual, rows[0].Schedule)
	assert.Nil(t, rows[0].NextDue)
	assert.Empty(t, rows[0].Outcomes)
}

func TestAReportIsPutAwayBySeeingIt(t *testing.T) {
	s := jobsLauncher(t)
	_, err := jobs.Record(jobs.MyDayJob(), jobs.Outcome{FiredAt: time.Now(), Report: "two reviews are waiting"})
	require.NoError(t, err)
	require.False(t, listJobs(t, s)[0].Outcomes[0].Seen)

	rec := do(t, s, http.MethodPost, "/api/jobs/seen", `{"name":"my-day"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, listJobs(t, s)[0].Outcomes[0].Seen)
}

func TestAJobThatIsNotOnThisMachineIsRefused(t *testing.T) {
	s := jobsLauncher(t)

	for _, route := range []string{"/api/jobs/run", "/api/jobs/seen"} {
		rec := do(t, s, http.MethodPost, route, `{"name":"invented"}`)
		assert.Equalf(t, http.StatusBadRequest, rec.Code, "%s took a name it was never offered", route)
	}
}

func TestTheJobsRoutesNeedTheToken(t *testing.T) {
	s := jobsLauncher(t)

	// A job fires a process on the subscription, so an unauthenticated page on
	// this machine must not be able to ask for one.
	for _, route := range [][2]string{
		{http.MethodGet, "/api/jobs"},
		{http.MethodPost, "/api/jobs/run"},
		{http.MethodPost, "/api/jobs/seen"},
	} {
		req := httptest.NewRequest(route[0], route[1], strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		s.routes().ServeHTTP(rec, req)
		assert.Equalf(t, http.StatusForbidden, rec.Code, "%s was open", route[1])
	}
}

func TestAJobYouWroteHasNoRouteLeft(t *testing.T) {
	s := jobsLauncher(t)

	for _, route := range []string{"/api/jobs/pause", "/api/jobs/save", "/api/jobs/delete"} {
		rec := do(t, s, http.MethodPost, route, `{"name":"my-day"}`)
		assert.Containsf(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed}, rec.Code, "%s still answers", route)
	}
}

func TestOneJobDoesNotRunTwiceAtOnce(t *testing.T) {
	s := jobsLauncher(t)
	require.NoError(t, s.jobs.claim(jobs.KindMyDay))
	assert.True(t, s.jobs.busy(), "a fire in flight holds the launcher up")
	assert.True(t, listJobs(t, s)[0].Running)

	_, err := s.fireJob(context.Background(), jobs.MyDayJob())
	require.ErrorContains(t, err, "already running")
	rec := do(t, s, http.MethodPost, "/api/jobs/run", `{"name":"my-day"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)

	s.jobs.release(jobs.KindMyDay)
	assert.False(t, s.jobs.busy())
}
