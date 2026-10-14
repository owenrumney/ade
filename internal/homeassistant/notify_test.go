package homeassistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTargetsAreTheNotifyServicesOnly(t *testing.T) {
	// A Home Assistant exposes hundreds of services; the settings page wants
	// the handful you can send a phone notification to.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/services", r.URL.Path)
		assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`[
			{"domain":"light","services":{"turn_on":{}}},
			{"domain":"notify","services":{"mobile_app_phone":{},"persistent_notification":{}}}
		]`))
	}))
	defer server.Close()

	got, err := Client{URL: server.URL, Token: "secret"}.Targets(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"notify.mobile_app_phone", "notify.persistent_notification"}, got,
		"sorted, so the list does not reshuffle between readings")
}

func TestNotifyPostsToTheNamedService(t *testing.T) {
	var body map[string]string
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := Client{URL: server.URL, Token: "secret"}.
		Notify(context.Background(), "notify.mobile_app_phone", "ade · main", "is waiting for you")
	require.NoError(t, err)
	assert.Equal(t, "/api/services/notify/mobile_app_phone", path)
	assert.Equal(t, "ade · main", body["title"])
	assert.Equal(t, "is waiting for you", body["message"])
}

func TestSomethingThatIsNotANotifyServiceIsRefused(t *testing.T) {
	// The target goes into a URL path, so it is checked rather than trusted:
	// "light.desk" would post to a service that is not a notification.
	for _, target := range []string{
		"light.desk", "", "notify.", "mobile_app_phone",
		"notify.../lock/unlock", "notify.a/b", "notify.mobile app",
	} {
		err := Client{URL: "http://ha.example", Token: "secret"}.
			Notify(context.Background(), target, "t", "m")
		assert.Errorf(t, err, "%q was accepted", target)
	}
}

func TestNotifyingWithoutATokenSaysSo(t *testing.T) {
	err := Client{URL: "http://ha.example"}.Notify(context.Background(), "notify.a", "t", "m")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "long-lived access token")
}
