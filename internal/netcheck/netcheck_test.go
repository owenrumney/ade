package netcheck

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
)

func TestAnAnswerIsReachableWhateverItSays(t *testing.T) {
	// 401 is reachable: the question is whether packets get there and back.
	// It is reported as refused rather than ok, which is a different problem.
	refuse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer refuse.Close()

	got := Probe(context.Background(), nil, Target{Name: "linear", URL: refuse.URL})
	assert.True(t, got.Reachable)
	assert.True(t, got.Refused)
	assert.Contains(t, got.Status, "401")
	assert.Empty(t, got.Err)
}

func TestAHangSaysHowFarItGot(t *testing.T) {
	release := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release
	}))
	defer hang.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	got := Probe(ctx, nil, Target{Name: "linear", URL: hang.URL})

	assert.False(t, got.Reachable)
	assert.Contains(t, got.Reached, "sent", "the request went out")
	assert.NotContains(t, got.Reached, "firstbyte", "and nothing came back")
	assert.Contains(t, got.Line(), "UNREACHABLE")
}

func TestOneHangingTargetDoesNotDelayTheRest(t *testing.T) {
	release := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer hang.Close()
	defer close(release)
	quick := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer quick.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	found := ProbeAll(ctx, nil, []Target{
		{Name: "slow", URL: hang.URL},
		{Name: "fast", URL: quick.URL},
	})

	require.Len(t, found, 2)
	assert.Equal(t, "slow", found[0].Name, "results keep the order they were asked in")
	assert.False(t, found[0].Reachable)
	assert.True(t, found[1].Reachable, "the fast one is not held up by the slow one")
}

func TestAnUndialableTargetReachedNothing(t *testing.T) {
	got := Probe(context.Background(), &http.Client{Transport: dead{}}, Target{Name: "x", URL: "https://example.invalid/"})
	assert.False(t, got.Reachable)
	assert.Equal(t, "nothing", got.Reached)
	assert.NotEmpty(t, got.Err)
}

func TestPhasesReadInOrder(t *testing.T) {
	p := New()
	assert.Equal(t, "nothing", p.Reached())
	p.mark("dns")
	p.mark("connect")
	got := p.Reached()
	assert.Less(t, strings.Index(got, "dns"), strings.Index(got, "connect"))
}

type dead struct{}

func (dead) RoundTrip(*http.Request) (*http.Response, error) { return nil, errDead }

var errDead = &refused{}

type refused struct{}

func (*refused) Error() string { return "proxyconnect tcp: connection refused" }

func TestAProbeSeparatesRefusedFromUnreachable(t *testing.T) {
	var sawAuth string
	guarded := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if sawAuth != "good-key" {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer guarded.Close()

	bad := Probe(context.Background(), nil, Target{Name: "linear", URL: guarded.URL, Auth: "bad-key"})
	assert.True(t, bad.Reachable, "it answered, so the network is fine")
	assert.True(t, bad.Refused, "and it would not take the key")
	assert.Contains(t, bad.Line(), "refused the credential")

	good := Probe(context.Background(), nil, Target{Name: "linear", URL: guarded.URL, Auth: "good-key"})
	assert.True(t, good.Reachable)
	assert.False(t, good.Refused)
	assert.Equal(t, "good-key", sawAuth, "the credential is sent as Authorization")
}

func TestACredentialIsNeverInTheResult(t *testing.T) {
	// The result is logged and returned to the page; the key must not be.
	ok := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer ok.Close()

	got := Probe(context.Background(), nil, Target{Name: "linear", URL: ok.URL, Auth: "secret-key"})
	blob, err := json.Marshal(got)
	require.NoError(t, err)
	assert.NotContains(t, string(blob), "secret-key")
	assert.NotContains(t, got.Line(), "secret-key")
}

// TestAFailedPhaseIsNotReportedAsASuccess is the bug this caught: httptrace
// fires ConnectDone whether the connect worked or not, and a trace that
// ignores the error reports a refused connect as "connected".
func TestAFailedPhaseIsNotReportedAsASuccess(t *testing.T) {
	// Port 1 on loopback: nothing listens, so the connect is refused fast.
	got := Probe(context.Background(), nil, Target{Name: "dead", URL: "http://127.0.0.1:1/"})

	require.False(t, got.Reachable)
	assert.Contains(t, got.Reached, "connect-failed", "a refused connect says so")
	assert.NotContains(t, got.Reached, "connected 127.0.0.1:1")
}

func TestASuccessfulPhaseStillReadsAsOne(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer ok.Close()

	got := Probe(context.Background(), nil, Target{Name: "ok", URL: ok.URL})
	require.True(t, got.Reachable)
	assert.Contains(t, got.Reached, "connected ")
	assert.NotContains(t, got.Reached, "failed")
	assert.Contains(t, got.Reached, "firstbyte")
}

func TestATargetCanBePinnedToOneAddressFamily(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer ok.Close()

	// httptest listens on 127.0.0.1, so v4 reaches it and v6 cannot.
	v4 := Probe(context.Background(), nil, Target{Name: "v4", URL: ok.URL, Network: "tcp4"})
	assert.True(t, v4.Reachable, "the family it is actually on")

	v6 := Probe(context.Background(), nil, Target{Name: "v6", URL: ok.URL, Network: "tcp6"})
	assert.False(t, v6.Reachable, "and not the other")
	assert.Contains(t, v6.Reached, "failed")
}

func TestPinningDoesNotRewriteTheSharedClient(t *testing.T) {
	shared := &http.Client{}
	ok := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer ok.Close()

	Probe(context.Background(), shared, Target{Name: "v4", URL: ok.URL, Network: "tcp4"})
	assert.Nil(t, shared.Transport, "the caller's client is left as it was")
}
