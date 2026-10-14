package web

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/dash"
	"github.com/owenrumney/ade/internal/estimate"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/lineage"
	"github.com/owenrumney/ade/internal/linear"
	"github.com/owenrumney/ade/internal/overlap"
	"github.com/owenrumney/ade/internal/worktrees"
)

// vscodeClient is the extension's hand-written mirror of what /api/state
// answers. A field renamed here and not there is a surface that silently
// reads undefined for ever: `branchPr` was typed `branchPR` and every pull
// request command said the branch had none.
const vscodeClient = "../../vscode/src/client.ts"

// The extension decodes only part of the state, so this checks one direction:
// every field the extension names must exist on the Go struct with that exact
// JSON tag. What it ignores is its business.
func TestTheExtensionNamesFieldsTheServerActuallySends(t *testing.T) {
	src := readClient(t)

	for _, c := range []struct {
		iface string
		of    any
	}{
		{"State", stateResponse{}},
		{"Meta", meta{}},
		{"RunState", RunState{}},
		{"BranchPR", BranchPR{}},
		{"CommitDraft", CommitDraft{}},
		{"DesignState", DesignState{}},
		{"Estimate", estimate.Estimate{}},
		{"Window", estimate.Window{}},
		{"Diff", gitdiff.Diff{}},
		{"DiffFile", gitdiff.File{}},
		{"Worktree", gitdiff.Status{}},
		{"Change", gitdiff.Change{}},
		{"Thread", comments.Thread{}},
		{"Comment", comments.Comment{}},
		{"Tree", worktrees.Worktree{}},
		{"Rev", gitdiff.Rev{}},
		{"OverlapNode", overlap.Node{}},
		{"OverlapEdge", overlap.Edge{}},
		{"Overlap", overlap.Graph{}},
		{"Turn", lineage.Turn{}},
		{"Issue", linear.Issue{}},
		{"Tickets", dash.Tickets{}},
		{"Listed", ghpr.Listed{}},
		{"Check", ghpr.Check{}},
	} {
		t.Run(c.iface, func(t *testing.T) {
			sent := tagsOf(reflect.TypeOf(c.of))
			for _, named := range fieldsIn(t, src, c.iface) {
				assert.Contains(t, sent, named,
					"%s.%s is not a field %T sends", c.iface, named, c.of)
			}
		})
	}
}

func readClient(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(vscodeClient) // #nosec G304 -- a path in this repository
	require.NoError(t, err, "the extension's client is missing")
	return string(data)
}

// fieldsIn is the property names of one exported interface, which is enough
// parsing for a file written to one shape and checked by this test.
func fieldsIn(t *testing.T, src, name string) []string {
	t.Helper()
	// An interface may extend another, and the fields it inherits are checked
	// under that one's own name: the opening line is matched up to the brace.
	start := strings.Index(src, "export interface "+name+" ")
	require.GreaterOrEqual(t, start, 0, "no interface %s in %s", name, vscodeClient)
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
