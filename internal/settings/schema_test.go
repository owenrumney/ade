package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The editor edits ~/.ade/settings.json as a file, with this schema for
// completion and validation. A field the schema does not know is a field the
// editor marks as an error in a file ade wrote itself.
func TestTheEditorSchemaKnowsEverySetting(t *testing.T) {
	schema := readSchema(t)
	known := properties(t, schema)

	for _, name := range tags(reflect.TypeOf(Settings{})) {
		assert.Contains(t, known, name,
			"vscode/schema/settings.json does not know %q; add it there too", name)
	}
}

// And the other way: a property the struct dropped would sit in the schema
// offering completion for a setting nothing reads.
func TestTheEditorSchemaHasNothingSettingsDoesNot(t *testing.T) {
	schema := readSchema(t)
	fields := map[string]bool{}
	for _, name := range tags(reflect.TypeOf(Settings{})) {
		fields[name] = true
	}

	for name := range properties(t, schema) {
		assert.True(t, fields[name], "the schema offers %q, which Settings does not have", name)
	}
}

// additionalProperties must stay false, or a typo in the file is a setting
// silently ignored rather than an error the editor shows.
func TestTheEditorSchemaRefusesAnythingItDoesNotKnow(t *testing.T) {
	var schema map[string]any
	require.NoError(t, json.Unmarshal(readSchema(t), &schema))
	assert.Equal(t, false, schema["additionalProperties"])
}

func readSchema(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "vscode", "schema", "settings.json")
	data, err := os.ReadFile(path) // #nosec G304 -- a path in this repository
	require.NoError(t, err, "the extension's settings schema is missing")
	return data
}

func properties(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var schema struct {
		Properties map[string]any `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(data, &schema))
	return schema.Properties
}

// tags is the json name of every exported field, skipping the ones marked "-".
func tags(typ reflect.Type) []string {
	var out []string
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		out = append(out, name)
	}
	return out
}
