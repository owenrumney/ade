package editor

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func onPath(have ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, h := range have {
			if h == name {
				return "/usr/local/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestFindingTheEditor(t *testing.T) {
	tests := []struct {
		name    string
		named   string
		have    []string
		want    string
		wantErr string
	}{
		{name: "ADE before anything else", have: []string{"code", "ade-editor"}, want: "/usr/local/bin/ade-editor"},
		{name: "code when there is no ADE", have: []string{"cursor", "code"}, want: "/usr/local/bin/code"},
		{name: "the next one when code is missing", have: []string{"cursor"}, want: "/usr/local/bin/cursor"},
		{name: "the one that is named", named: "cursor", have: []string{"code", "cursor"}, want: "/usr/local/bin/cursor"},
		{name: "a named one that is missing is not replaced", named: "zed", have: []string{"code"}, wantErr: "ADE_EDITOR names zed"},
		{name: "none is none", named: "none", have: []string{"code"}, wantErr: ErrNone.Error()},
		{name: "nothing on PATH", wantErr: ErrNone.Error()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := find(tt.named, onPath(tt.have...))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestOpeningWithNoEditorSaysSo(t *testing.T) {
	t.Setenv("ADE_EDITOR", "none")
	assert.ErrorIs(t, Open(t.TempDir()), ErrNone)
}
