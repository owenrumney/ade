package homeassistant

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shortHold keeps a test off the real two-second flash.
func shortHold(t *testing.T) {
	was := flashHold
	flashHold = time.Millisecond
	t.Cleanup(func() { flashHold = was })
}

func TestFlashRestoresThePriorLightState(t *testing.T) {
	shortHold(t)
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		switch r.Method {
		case http.MethodGet:
			assert.Equal(t, "/api/states/light.desk", r.URL.Path)
			_, err := w.Write([]byte(`{"state":"on","attributes":{"color_mode":"color_temp","color_temp":414,"brightness":138}}`))
			require.NoError(t, err)
		case http.MethodPost:
			assert.Equal(t, "/api/services/light/turn_on", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			bodies = append(bodies, string(body))
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	err := (Client{URL: server.URL, Token: "token"}).Flash(context.Background(), "light.desk", HueAsking)
	require.NoError(t, err)
	require.Len(t, bodies, 2)
	// Both calls go out at full brightness: turnOn sets it, so the flash is
	// seen from across the room and the light comes back up, not back dim.
	assert.JSONEq(t, `{"entity_id":"light.desk","hs_color":[0,100],"brightness":255}`, bodies[0])
	assert.JSONEq(t, `{"entity_id":"light.desk","color_temp":414,"brightness":255}`, bodies[1])
}

func TestFlashHoldsTheColourBeforeRestoring(t *testing.T) {
	was := flashHold
	flashHold = 300 * time.Millisecond
	t.Cleanup(func() { flashHold = was })

	var at []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, err := w.Write([]byte(`{"state":"on","attributes":{"color_mode":"hs","hs_color":[30,60],"brightness":138}}`))
			require.NoError(t, err)
			return
		}
		at = append(at, time.Now())
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	require.NoError(t, (Client{URL: server.URL, Token: "token"}).Flash(context.Background(), "light.desk", HueAsking))

	// Without this nothing proves the light is red for any length of time:
	// the round-trip test runs the hold down to a millisecond.
	require.Len(t, at, 2)
	assert.GreaterOrEqual(t, at[1].Sub(at[0]), flashHold)
}

func TestFlashSkipsAnUnrestorableLight(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		assert.Equal(t, http.MethodGet, r.Method)
		_, err := w.Write([]byte(`{"state":"off","attributes":{}}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	err := (Client{URL: server.URL, Token: "token"}).Flash(context.Background(), "light.desk", HueAsking)
	require.Error(t, err)
	assert.Equal(t, 1, requests, "the red signal is not sent")
}

func TestFlashRefusesAnUnknownEntity(t *testing.T) {
	err := (Client{URL: "https://assistant.example", Token: "token"}).Flash(context.Background(), "switch.desk", HueAsking)
	assert.Error(t, err)
}

func TestValidURL(t *testing.T) {
	got, err := ValidURL("https://assistant.example/")
	require.NoError(t, err)
	assert.Equal(t, "https://assistant.example", got)

	for _, raw := range []string{"assistant.example", "ftp://assistant.example", "https://user:pass@assistant.example", "https://assistant.example/?x=1"} {
		_, err := ValidURL(raw)
		assert.Error(t, err, raw)
	}
}

// Done and wants-you are the same two seconds of light, so the hue has to
// carry the difference on its own.
func TestTheHueIsWhatTheFlashSays(t *testing.T) {
	shortHold(t)
	var first string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, err := w.Write([]byte(`{"state":"on","attributes":{"color_mode":"hs","hs_color":[30,60],"brightness":138}}`))
			require.NoError(t, err)
			return
		}
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		if first == "" {
			first = string(body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	require.NoError(t, (Client{URL: server.URL, Token: "token"}).Flash(context.Background(), "light.desk", HueDone))
	assert.JSONEq(t, `{"entity_id":"light.desk","hs_color":[120,100],"brightness":255}`, first)
}
