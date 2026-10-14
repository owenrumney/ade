package web

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThePermissionsPageIsSelfContained(t *testing.T) {
	page := string(rulesPage)
	// Served whole from the binary, so it must not reach for the app's bundle:
	// it opens in an editor panel that has no Vite behind it.
	require.NotEmpty(t, page)
	assert.NotContains(t, page, "/assets/")
	assert.NotContains(t, page, "<script src")
	assert.Contains(t, page, RulesAPI, "the page has no api to read")
	assert.Contains(t, page, "X-Ade-Token", "the page would not be let in")
}

func TestThePermissionsPageCanBeFiltered(t *testing.T) {
	page := string(rulesPage)
	assert.Contains(t, page, `id="find"`, "the page has no filter box")
	for _, list := range []string{"shown.session", "shown.allow", "shown.deny"} {
		assert.Contains(t, page, "matching("+list+")", list+" is drawn unfiltered")
	}
}

func TestThePermissionsPageIsNeverCached(t *testing.T) {
	assert.Contains(t, read(t, "rules.go"), `"Cache-Control", "no-store"`,
		"an editor webview would keep drawing the page the old binary embedded")
}

func TestThePermissionsPageSortsEachGroup(t *testing.T) {
	// Every group goes through matching(), so the sort belongs there.
	assert.Contains(t, string(rulesPage), ".sort(",
		"the groups are drawn in whatever order the rules were granted")
}

func TestThePermissionsRoutesAreMountedWhereTheExtensionLooks(t *testing.T) {
	source := read(t, "web.go")
	for _, route := range []string{
		`mux.HandleFunc("GET ` + RulesPath + `"`,
		`mux.HandleFunc("GET ` + RulesAPI + `"`,
		`mux.HandleFunc("POST ` + RulesAPI + `"`,
		`mux.HandleFunc("POST ` + HookPath + `"`,
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
