package linear

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAFailedCallSaysHowFarItGot(t *testing.T) {
	t.Run("it never dialled", func(t *testing.T) {
		// A client that cannot make a connection at all — a dead proxy looks
		// like this, and so does a transport that refuses before DNS.
		c := Client{Key: "k", BaseURL: "https://api.linear.app/graphql", HTTP: &http.Client{
			Transport: refuse{},
		}}
		_, err := c.MyIssues(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "got as far as: nothing")
	})

	t.Run("it reached the server and the server hung", func(t *testing.T) {
		// Released by the test rather than by the request being cancelled:
		// httptest.Close waits on the handler, and a handler that waits on the
		// connection closing deadlocks against it.
		release := make(chan struct{})
		hang := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			<-release
		}))
		defer hang.Close()
		defer close(release)

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		_, err := Client{Key: "k", BaseURL: hang.URL}.MyIssues(ctx)
		require.Error(t, err)

		msg := err.Error()
		assert.Contains(t, msg, "got as far as:")
		assert.Contains(t, msg, "sent", "the request went out")
		assert.NotContains(t, msg, "firstbyte", "and nothing came back")
	})
}

func TestASuccessfulCallSaysNothingAboutPhases(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"viewer":{"assignedIssues":{"nodes":[]}}}}`))
	}))
	defer ok.Close()

	issues, err := Client{Key: "k", BaseURL: ok.URL}.MyIssues(context.Background())
	require.NoError(t, err)
	assert.Empty(t, issues)
}

// refuse is a transport that fails before anything is traced.
type refuse struct{}

func (refuse) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, assertErr
}

var assertErr = &net404{}

type net404 struct{}

func (*net404) Error() string { return "proxyconnect tcp: connection refused" }
