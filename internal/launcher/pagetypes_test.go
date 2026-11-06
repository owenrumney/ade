package launcher

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/dash"
	"github.com/owenrumney/ade/internal/jobs"
	"github.com/owenrumney/ade/internal/sessions"
)

// The window's mirrors of what this launcher answers: every field one of
// them names is one something here actually sends.
const (
	windowRail = "../../app/renderer/real.ts"
	windowJobs = "../../app/renderer/myday.ts"
)

func TestTheWindowNamesFieldsTheLauncherActuallySends(t *testing.T) {
	for _, c := range []struct {
		iface string
		file  string
		of    any
	}{
		{"Session", windowRail, sessions.Session{}},
		{"Checkout", windowRail, sessions.Dir{}},
		{"Repo", windowRail, sessions.Repo{}},
		{"Review", windowRail, dash.Review{}},
		{"Job", windowJobs, jobs.View{}},
	} {
		t.Run(c.iface, func(t *testing.T) {
			data, err := os.ReadFile(c.file) // #nosec G304 -- a path in this repository
			require.NoError(t, err, "the window's own file is missing")
			sent := jsonTags(reflect.TypeOf(c.of))
			for _, named := range tsFields(t, string(data), c.iface, c.file) {
				assert.Contains(t, sent, named,
					"%s.%s is not a field anything the launcher sends has", c.iface, named)
			}
		})
	}
}

// tsFields is the property names of one exported interface. Enough parsing
// for a file written to one shape and checked by this test.
func tsFields(t *testing.T, src, name, where string) []string {
	t.Helper()
	start := strings.Index(src, "export interface "+name+" {")
	require.GreaterOrEqual(t, start, 0, "no interface %s in %s", name, where)
	body := src[start:]
	body = body[:strings.Index(body, "\n}")]

	var out []string
	for _, line := range strings.Split(body, "\n")[1:] {
		line = strings.TrimSpace(line)
		cut := strings.IndexAny(line, "?:")
		if cut <= 0 || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "*") {
			continue
		}
		field := strings.TrimSpace(line[:cut])
		if field == "" || strings.ContainsAny(field, " {}()[]") {
			continue
		}
		out = append(out, field)
	}
	require.NotEmpty(t, out, "read no fields out of %s", name)
	return out
}

// An embedded struct is inlined by encoding/json, so its tags are sent under
// the outer name and count here too.
func jsonTags(typ reflect.Type) []string {
	var out []string
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct && field.Tag.Get("json") == "" {
			out = append(out, jsonTags(field.Type)...)
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	return out
}
