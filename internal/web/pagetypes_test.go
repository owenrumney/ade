package web

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/linear"
)

// The window's hand-written mirrors of what this server answers. A field
// renamed here and not there reads undefined for ever, and says nothing.
const (
	windowChanges = "../../app/renderer/changes.ts"
	windowRail    = "../../app/renderer/real.ts"
)

// One direction only: every field the window names must exist on the Go
// struct with that exact JSON tag. What it ignores is its business.
func TestTheWindowNamesFieldsTheServerActuallySends(t *testing.T) {
	for _, c := range []struct {
		iface string
		file  string
		of    any
	}{
		{"Diff", windowChanges, gitdiff.Diff{}},
		{"Change", windowChanges, gitdiff.File{}},
		{"Thread", windowChanges, comments.Thread{}},
		{"Listed", windowRail, ghpr.Listed{}},
		{"Issue", windowRail, linear.Issue{}},
	} {
		t.Run(c.iface, func(t *testing.T) {
			data, err := os.ReadFile(c.file) // #nosec G304 -- a path in this repository
			require.NoError(t, err, "the window's own file is missing")
			sent := tagsOf(reflect.TypeOf(c.of))
			for _, named := range fieldsIn(t, string(data), c.iface) {
				assert.Contains(t, sent, named,
					"%s.%s is not a field %T sends", c.iface, named, c.of)
			}
		})
	}
}

// fieldsIn is the property names of one exported interface, which is enough
// parsing for a file written to one shape and checked by this test.
func fieldsIn(t *testing.T, src, name string) []string {
	t.Helper()
	// An interface may extend another, and the fields it inherits are checked
	// under that one's own name: the opening line is matched up to the brace.
	start := strings.Index(src, "export interface "+name+" ")
	require.GreaterOrEqual(t, start, 0, "no interface %s in the window", name)
	body := src[start:]
	body = body[:strings.Index(body, "\n}")]
	brace := strings.Index(body, "{")
	require.GreaterOrEqual(t, brace, 0, "interface %s has no body", name)
	body = body[brace:]

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
func tagsOf(typ reflect.Type) []string {
	var out []string
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous && field.Type.Kind() == reflect.Struct && field.Tag.Get("json") == "" {
			out = append(out, tagsOf(field.Type)...)
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			out = append(out, name)
		}
	}
	return out
}
