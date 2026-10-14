package settings

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestADashboardIsOffUntilItHasBothHalves(t *testing.T) {
	tests := []struct {
		name string
		at   Dashboard
		on   bool
	}{
		{"unset", Dashboard{}, false},
		{"url only", Dashboard{URL: "http://pi.local:8080"}, false},
		{"token only", Dashboard{Token: "secret"}, false},
		{"both", Dashboard{URL: "http://pi.local:8080", Token: "secret"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.on, tt.at.On())
		})
	}
}

func TestTheHostnameIsTheMachinesUnlessOneIsSet(t *testing.T) {
	assert.Equal(t, "pi-shed", Dashboard{Host: "pi-shed"}.Machine())

	name, err := os.Hostname()
	require.NoError(t, err)
	assert.Equal(t, name, Dashboard{}.Machine())
}

func TestADashboardURLIsCheckedAndTidied(t *testing.T) {
	tests := []struct {
		name string
		in   Dashboard
		want string
		bad  bool
	}{
		{"unset is the off state", Dashboard{}, "", false},
		{"trailing slash goes", Dashboard{URL: "http://pi.local:8080/"}, "http://pi.local:8080", false},
		{"spaces go", Dashboard{URL: "  https://pi.local  "}, "https://pi.local", false},
		{"no scheme", Dashboard{URL: "pi.local:8080"}, "", true},
		{"not a url", Dashboard{URL: "http://"}, "", true},
		{"wrong scheme", Dashboard{URL: "ftp://pi.local"}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CheckDashboard(tt.in)
			if tt.bad {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "dashboard url")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.URL)
		})
	}
}

func TestTheAccountLabelsAreTrimmed(t *testing.T) {
	got, err := CheckDashboard(Dashboard{Claude: "  personal ", Codex: " work ", Host: " mbp "})
	require.NoError(t, err)
	assert.Equal(t, "personal", got.Claude)
	assert.Equal(t, "work", got.Codex)
	assert.Equal(t, "mbp", got.Host)
}

func TestWhatADashboardWritesIntoTheSettingsFile(t *testing.T) {
	// omitempty does nothing to a struct field, so an unset dashboard is an
	// empty object — the same as home_assistant beside it. No secret in it.
	data, err := json.Marshal(Settings{})
	require.NoError(t, err)
	assert.Contains(t, string(data), `"dashboard":{}`)

	data, err = json.Marshal(Settings{Dashboard: Dashboard{
		URL: "http://pi.local", Token: "secret", Claude: "personal",
	}})
	require.NoError(t, err)
	assert.Contains(t, string(data), `"url":"http://pi.local"`)
	assert.Contains(t, string(data), `"claude_account":"personal"`)
}
