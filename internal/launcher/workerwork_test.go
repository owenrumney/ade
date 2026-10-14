package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
)

// connected is a launcher with one bookmarked machine already dialled. The
// action under test never touches the connection, so a bare conn will do.
func connected(t *testing.T, name string) *Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, settings.Save(settings.Settings{Hosts: []settings.Host{{Name: name}}}))
	s, err := New(0, "test")
	require.NoError(t, err)
	s.hosts[name] = &conn{url: "http://127.0.0.1:1/?t=x"}
	return s
}

func rowFor(t *testing.T, s *Server, name string) hostView {
	t.Helper()
	rec := do(t, s, http.MethodGet, "/api/hosts", "")
	var got struct {
		Hosts []hostView `json:"hosts"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	for _, row := range got.Hosts {
		if row.Name == name {
			return row
		}
	}
	t.Fatalf("no row for %s", name)
	return hostView{}
}

func waitFor(t *testing.T, what string, until func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if until() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestAWorkerActionAnswersBeforeTheWorkIsDone(t *testing.T) {
	s := connected(t, "linux")
	let := make(chan struct{})
	rec := httptest.NewRecorder()
	s.start(rec, httptest.NewRequest(http.MethodPost, "/api/worker/update", nil), "linux",
		workerAction{Verb: workUpdating, Wait: time.Minute, Do: func(context.Context, *conn) error {
			<-let
			return nil
		}})

	assert.Equal(t, http.StatusAccepted, rec.Code, "the row is told later, not by this request")
	assert.Equal(t, workUpdating, rowFor(t, s, "linux").Working)

	close(let)
	waitFor(t, "the row to stop working", func() bool { return rowFor(t, s, "linux").Working == "" })
	assert.Empty(t, rowFor(t, s, "linux").Failed)
}

func TestASecondWorkerActionIsRefusedWhileOneRuns(t *testing.T) {
	s := connected(t, "linux")
	let := make(chan struct{})
	defer close(let)
	first := httptest.NewRecorder()
	s.start(first, httptest.NewRequest(http.MethodPost, "/api/worker/update", nil), "linux",
		workerAction{Verb: workUpdating, Wait: time.Minute, Do: func(context.Context, *conn) error {
			<-let
			return nil
		}})
	require.Equal(t, http.StatusAccepted, first.Code)

	second := httptest.NewRecorder()
	s.start(second, httptest.NewRequest(http.MethodPost, "/api/worker/stop", nil), "linux",
		workerAction{Verb: workStopping, Wait: time.Minute, Do: func(context.Context, *conn) error {
			t.Error("the second action ran while the first held the slot")
			return nil
		}})
	assert.Equal(t, http.StatusConflict, second.Code)
	assert.Contains(t, second.Body.String(), "already updating")
}

func TestAFailedWorkerActionIsOnTheRowAfterwards(t *testing.T) {
	s := connected(t, "linux")
	rec := httptest.NewRecorder()
	s.start(rec, httptest.NewRequest(http.MethodPost, "/api/worker/update", nil), "linux",
		workerAction{Verb: workUpdating, Wait: time.Minute, Do: func(context.Context, *conn) error {
			return errors.New("registry refused the pull")
		}})
	require.Equal(t, http.StatusAccepted, rec.Code)

	waitFor(t, "the failure to land", func() bool { return rowFor(t, s, "linux").Failed != "" })
	row := rowFor(t, s, "linux")
	assert.Contains(t, row.Failed, "registry refused the pull")
	assert.Empty(t, row.Working, "a failed action is not still running")
}

func TestANewAttemptClearsWhatTheLastOneSaid(t *testing.T) {
	s := connected(t, "linux")
	s.noteWork("linux", errors.New("registry refused the pull"))
	require.NotEmpty(t, rowFor(t, s, "linux").Failed)

	let := make(chan struct{})
	defer close(let)
	rec := httptest.NewRecorder()
	s.start(rec, httptest.NewRequest(http.MethodPost, "/api/worker/update", nil), "linux",
		workerAction{Verb: workUpdating, Wait: time.Minute, Do: func(context.Context, *conn) error {
			<-let
			return nil
		}})
	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Empty(t, rowFor(t, s, "linux").Failed, "the message was about the attempt that failed")
}

// The window that asks can be closed while a machine is mid-update. Ending the
// work with the request would leave the worker stopped and nothing put back.
func TestTheWorkOutlivesTheRequestThatAskedForIt(t *testing.T) {
	s := connected(t, "linux")
	ctx, cancel := context.WithCancel(context.Background())
	ran := make(chan error, 1)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/worker/update", nil).WithContext(ctx)
	s.start(rec, req, "linux",
		workerAction{Verb: workUpdating, Wait: time.Minute, Do: func(ctx context.Context, _ *conn) error {
			time.Sleep(20 * time.Millisecond)
			ran <- ctx.Err()
			return nil
		}})
	require.Equal(t, http.StatusAccepted, rec.Code)
	cancel()

	select {
	case err := <-ran:
		assert.NoError(t, err, "the work kept its own deadline, not the request's")
	case <-time.After(2 * time.Second):
		t.Fatal("the work never ran")
	}
}

func TestAWorkerActionNeedsTheMachineConnected(t *testing.T) {
	s := connected(t, "linux")
	rec := httptest.NewRecorder()
	s.start(rec, httptest.NewRequest(http.MethodPost, "/api/worker/stop", nil), "gpu",
		workerAction{Verb: workStopping, Wait: time.Minute, Do: func(context.Context, *conn) error {
			t.Error("an action ran against a machine that is not connected")
			return nil
		}})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.True(t, strings.Contains(rec.Body.String(), "not connected"))
}
