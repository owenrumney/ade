package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forgetContained drops what a deliberate panic recorded, so TestMain's check
// only reports the ones nothing was expecting.
func forgetContained(t *testing.T) {
	t.Cleanup(func() {
		containedMu.Lock()
		containedPanics = nil
		containedMu.Unlock()
	})
}

func TestAPanickingGoroutineDoesNotTakeTheSessionWithIt(t *testing.T) {
	forgetContained(t)
	s := &Server{}
	done := make(chan struct{})
	s.spawn(func() {
		defer close(done)
		panic("the poll fell over")
	})
	<-done
	require.True(t, s.settle(closeWait), "the goroutine is still counted off")
	require.Len(t, Contained(), 1)
	assert.Contains(t, Contained()[0], "the poll fell over")
}

func TestAPanickingHandlerAnswers500AndIsRecorded(t *testing.T) {
	forgetContained(t)
	s := &Server{token: "t"}
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/diff", nil)
	r.Header.Set("X-Ade-Token", "t")
	s.guard(func(http.ResponseWriter, *http.Request) { panic("no diff to speak of") })(rec, r)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Len(t, Contained(), 1)
	assert.Contains(t, Contained()[0], "GET /api/diff")
}

// The window shows it: a session that broke and carried on says so rather
// than looking well.
func TestAContainedPanicIsReportedInTheState(t *testing.T) {
	forgetContained(t)
	assert.Nil(t, currentFault(), "nothing has fallen over")

	s := &Server{}
	done := make(chan struct{})
	s.spawn(func() {
		defer close(done)
		panic("the poll fell over")
	})
	<-done
	require.True(t, s.settle(closeWait))

	fault := currentFault()
	require.NotNil(t, fault)
	assert.Equal(t, 1, fault.Count)
	assert.Contains(t, fault.Latest, "the poll fell over")
}

func TestTheRecordNamesTheGoroutineThatFell(t *testing.T) {
	assert.Equal(t, "web.pollingFunc", goroutineName(pollingFunc))
	assert.NotEmpty(t, goroutineName(func() {}))
}

func pollingFunc() {}
