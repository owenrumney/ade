package web

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/permissions"
)

// denyServer is a server over a home of its own, with the deny list open.
func denyServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	s.agent.setHook("http://127.0.0.1:1", "hook")
	require.NotNil(t, s.agent.rulesStore())
	return s
}

func denied(t *testing.T, body []byte) rulesView {
	t.Helper()
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &raw))
	assert.NotContains(t, raw, "allow", "the allow list has no page left")
	assert.NotContains(t, raw, "free")
	var view rulesView
	require.NoError(t, json.Unmarshal(body, &view))
	return view
}

func TestThePermissionsRouteAnswersTheDenyListAndItsFile(t *testing.T) {
	s := denyServer(t)

	rec := do(t, s, "GET", RulesAPI, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	view := denied(t, rec.Body.Bytes())
	assert.NotNil(t, view.Deny, "the page reads a list, never null")
	assert.True(t, strings.HasSuffix(view.File, "permissions.json"), view.File)
}

func TestADenyRuleIsAddedAndWithdrawn(t *testing.T) {
	s := denyServer(t)
	rule := permissions.Rule("Bash(git push --force:*)")

	rec := do(t, s, "POST", RulesAPI, `{"rule":"Bash(git push --force:*)"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, denied(t, rec.Body.Bytes()).Deny, rule)

	file, _ := s.agent.rulesStore().Rules()
	assert.Contains(t, file.Deny, rule)
	assert.NotContains(t, file.Allow, rule, "a rule added here is only ever a deny")

	rec = do(t, s, "POST", RulesAPI, `{"rule":"Bash(git push --force:*)","forget":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, denied(t, rec.Body.Bytes()).Deny, rule)
}

func TestSomethingThatIsNotARuleIsRefused(t *testing.T) {
	s := denyServer(t)

	rec := do(t, s, "POST", RulesAPI, `{"rule":""}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

func TestThePermissionsRoutesAreMountedWhereTheWindowLooks(t *testing.T) {
	source := read(t, "web.go")
	for _, route := range []string{
		`mux.HandleFunc("GET ` + RulesAPI + `"`,
		`mux.HandleFunc("POST ` + RulesAPI + `"`,
	} {
		assert.Contains(t, source, route,
			"the mount table has to spell the path out: the route guard reads it as text")
	}
}

func read(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	require.NoError(t, err)
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}
