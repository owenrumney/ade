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
	"github.com/owenrumney/ade/internal/models"
	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/wiki"
)

// The extension's mirror of what this launcher answers. The checkout server
// has the same test against its own client; between them every field the
// extension names is one something actually sends.
const vscodeLauncher = "../../vscode/src/launcher.ts"

func TestTheExtensionNamesFieldsTheLauncherActuallySends(t *testing.T) {
	data, err := os.ReadFile(vscodeLauncher) // #nosec G304 -- a path in this repository
	require.NoError(t, err, "the extension's launcher client is missing")
	src := string(data)

	// A row stitched from two routes names the second as a nested interface,
	// which is checked on its own line rather than as a field of the first.
	nested := map[string]string{"Host.load": "Load"}

	for _, c := range []struct {
		iface string
		of    []any
	}{
		{"Load", []any{loadRow{}}},
		{"Session", []any{sessions.Session{}}},
		{"Checkout", []any{sessions.Dir{}}},
		{"Repo", []any{sessions.Repo{}}},
		{"Host", []any{hostView{}}},
		{"Waiting", []any{sessions.Row{}}},
		{"Job", []any{jobs.View{}}},
		{"Skipped", []any{jobs.Skipped{}}},
		{"Hit", []any{wiki.Hit{}}},
		{"Model", []any{models.Model{}}},
		{"Review", []any{dash.Review{}}},
	} {
		t.Run(c.iface, func(t *testing.T) {
			var sent []string
			for _, from := range c.of {
				sent = append(sent, jsonTags(reflect.TypeOf(from))...)
			}
			for _, named := range tsFields(t, src, c.iface, vscodeLauncher) {
				if _, ok := nested[c.iface+"."+named]; ok {
					continue
				}
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
