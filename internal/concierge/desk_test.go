package concierge

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type deskFile struct {
	Folders  []folder        `json:"folders"`
	Settings json.RawMessage `json:"settings"`
	Extra    json.RawMessage `json:"extensions"`
}

func parsed(t *testing.T, raw []byte) deskFile {
	t.Helper()
	var out deskFile
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func TestTheDeskIsTheFirstFolderAndTheCheckoutFollows(t *testing.T) {
	raw, err := withDesk(nil, "/home/o/.ade", "/home/o/.ade/concierge", "/code/ade")
	require.NoError(t, err)
	got := parsed(t, raw)
	assert.Equal(t, []folder{
		{Path: "/home/o/.ade/concierge", Name: DeskName},
		{Path: "/code/ade"},
	}, got.Folders)
	assert.JSONEq(t, `{}`, string(got.Settings))
}

func TestAnExistingWorkspaceKeepsItsFoldersAndSettings(t *testing.T) {
	was := `{
	  "folders": [{"path": "../code/lazytrivy"}, {"path": "concierge", "name": "Desk"}],
	  "settings": {"editor.fontSize": 13},
	  "extensions": {"recommendations": ["x"]}
	}`
	raw, err := withDesk([]byte(was), "/home/o/.ade", "/home/o/.ade/concierge", "/code/ade")
	require.NoError(t, err)
	got := parsed(t, raw)
	assert.Equal(t, []folder{
		{Path: "/home/o/.ade/concierge", Name: DeskName},
		{Path: "../code/lazytrivy"},
		{Path: "/code/ade"},
	}, got.Folders)
	assert.JSONEq(t, `{"editor.fontSize": 13}`, string(got.Settings))
	assert.JSONEq(t, `{"recommendations": ["x"]}`, string(got.Extra))
}

func TestACheckoutAlreadyHeldIsNotAddedAgain(t *testing.T) {
	tests := []struct {
		name string
		was  string
		dir  string
		want int
	}{
		{name: "held by a relative path", was: `{"folders": [{"path": "../code/ade"}]}`, dir: "/home/o/code/ade", want: 2},
		{name: "the desk itself", was: `{"folders": []}`, dir: "/home/o/.ade/concierge", want: 1},
		{name: "nothing asked for", was: `{"folders": [{"path": "/code/x"}]}`, dir: "", want: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := withDesk([]byte(test.was), "/home/o/.ade", "/home/o/.ade/concierge", test.dir)
			require.NoError(t, err)
			assert.Len(t, parsed(t, raw).Folders, test.want)
		})
	}
}

func TestAWorkspaceThatDoesNotParseIsNotOverwritten(t *testing.T) {
	_, err := withDesk([]byte(`{"folders": `), "/home/o/.ade", "/home/o/.ade/concierge", "/code/ade")
	require.Error(t, err)
}
