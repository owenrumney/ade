package remote

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/settings"
)

// sentWritten is the stdin of the call that wrote to a path. ssh collapses the
// whole command into one argument, so both halves are matched in that.
func sentWritten(t *testing.T, rec *recorder, path string) []byte {
	t.Helper()
	for i, call := range rec.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "cat >") && strings.Contains(joined, path) {
			return rec.input[i]
		}
	}
	t.Fatalf("nothing was written to %s", path)
	return nil
}

func TestTheWorkerRunsUnderThisMachinesPrompt(t *testing.T) {
	rec := &recorder{reply: says("")}
	host := testHost(t, rec)

	err := host.PreparePrompt(context.Background(), Prompt{
		RoleName: "oss-dev", Role: "be brief", Instructions: "prefer brevity",
	})
	require.NoError(t, err)

	assert.Equal(t, "be brief", string(sentWritten(t, rec, ".ade/roles/oss-dev.md")))

	var cfg map[string]any
	require.NoError(t, json.Unmarshal(sentWritten(t, rec, ".ade/settings.json"), &cfg))
	assert.Equal(t, "oss-dev", cfg["default_role"])
	assert.Equal(t, "prefer brevity", cfg["instructions"])
}

// A setting changed over there is not this machine's to throw away; only the
// two keys that say how to behave are.
func TestSendingThePromptKeepsTheWorkersOwnSettings(t *testing.T) {
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		joined := strings.Join(argv, " ")
		if strings.Contains(joined, "-f") && strings.Contains(joined, ".ade/settings.json") &&
			!strings.Contains(joined, "cat >") {
			return []byte(sentinel + `{"sandbox":{"paths":[]},"default_role":"stale"}`), nil, nil
		}
		return []byte(sentinel), nil, nil
	}}
	host := testHost(t, rec)

	require.NoError(t, host.PreparePrompt(context.Background(), Prompt{RoleName: "infracost", Role: "x"}))

	var cfg map[string]any
	require.NoError(t, json.Unmarshal(sentWritten(t, rec, ".ade/settings.json"), &cfg))
	assert.Equal(t, "infracost", cfg["default_role"])
	assert.Contains(t, cfg, "sandbox", "a setting made over there was thrown away")
}

// A rename would otherwise leave the old file beside the new one, and the
// worker would have two roles where this machine has one.
func TestTheWorkerKeepsNoRoleButTheSelectedOne(t *testing.T) {
	rec := &recorder{reply: says("")}
	host := testHost(t, rec)

	require.NoError(t, host.PreparePrompt(context.Background(), Prompt{RoleName: "a", Role: "x"}))

	var cleared bool
	for _, call := range rec.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "rm -f") && strings.Contains(joined, "roles/*.md") {
			cleared = true
		}
	}
	assert.True(t, cleared, "the roles directory was not cleared first")
}

// An empty read is "no file yet" or "something went wrong", and those must not
// be the same answer: one starts from scratch, the other throws settings away.
func TestAnUnreadableSettingsFileIsNotTreatedAsAnEmptyOne(t *testing.T) {
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		joined := strings.Join(argv, " ")
		if strings.Contains(joined, "-f") && strings.Contains(joined, ".ade/settings.json") &&
			!strings.Contains(joined, "cat >") {
			return []byte(sentinel + "not json at all"), nil, nil
		}
		return []byte(sentinel), nil, nil
	}}
	host := testHost(t, rec)

	err := host.PreparePrompt(context.Background(), Prompt{RoleName: "a", Role: "x"})
	require.Error(t, err, "it overwrote settings it could not read")

	for _, call := range rec.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "cat >") {
			assert.NotContains(t, joined, "settings.json", "settings were written anyway")
		}
	}
}

func TestNoRoleSelectedSendsNoRole(t *testing.T) {
	rec := &recorder{reply: says("")}
	host := testHost(t, rec)

	require.NoError(t, host.PreparePrompt(context.Background(), Prompt{Instructions: "only this"}))

	for _, call := range rec.calls {
		joined := strings.Join(call, " ")
		if strings.Contains(joined, "rm -f") {
			continue
		}
		assert.NotContains(t, joined, ".md", "a role was sent when none is selected")
	}
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(sentWritten(t, rec, ".ade/settings.json"), &cfg))
	assert.Equal(t, "", cfg["default_role"])
	assert.Equal(t, "only this", cfg["instructions"])
}

func TestTheWorkerReportsToTheSameKioskUnderItsOwnName(t *testing.T) {
	rec := &recorder{reply: says("")}
	host := testHost(t, rec)

	err := host.PreparePrompt(context.Background(), Prompt{Dashboard: settings.Dashboard{
		URL: "http://pi.local:8080", Token: "secret", Host: "mac-mini", Claude: "owen",
	}})
	require.NoError(t, err)

	var cfg struct {
		Dashboard settings.Dashboard `json:"dashboard"`
	}
	require.NoError(t, json.Unmarshal(sentWritten(t, rec, ".ade/settings.json"), &cfg))
	assert.Equal(t, "mac-mini", cfg.Dashboard.Host)
	assert.Equal(t, "http://pi.local:8080", cfg.Dashboard.URL)
	assert.Equal(t, "owen", cfg.Dashboard.Claude)
}

// Turning the kiosk off here has to turn it off there, or the worker keeps
// reporting to a screen this machine has stopped using.
func TestAWorkerToldToStopReportingStops(t *testing.T) {
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		joined := strings.Join(argv, " ")
		if strings.Contains(joined, "-f") && strings.Contains(joined, ".ade/settings.json") &&
			!strings.Contains(joined, "cat >") {
			return []byte(sentinel + `{"dashboard":{"url":"http://pi.local:8080","token":"old"}}`), nil, nil
		}
		return []byte(sentinel), nil, nil
	}}
	host := testHost(t, rec)

	require.NoError(t, host.PreparePrompt(context.Background(), Prompt{}))

	var cfg map[string]any
	require.NoError(t, json.Unmarshal(sentWritten(t, rec, ".ade/settings.json"), &cfg))
	assert.NotContains(t, cfg, "dashboard")
}
