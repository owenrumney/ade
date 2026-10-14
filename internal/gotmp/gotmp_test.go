package gotmp

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApplySetsGoTmpDirOnlyWhereItIsNeeded(t *testing.T) {
	ok := func() (string, error) { return "/cache/ade/gobuild", nil }

	tests := []struct {
		name string
		env  []string
		goos string
		dir  func() (string, error)
		want []string
	}{
		{
			name: "darwin gets one",
			env:  []string{"PATH=/bin"},
			goos: "darwin",
			dir:  ok,
			want: []string{"PATH=/bin", "GOTMPDIR=/cache/ade/gobuild"},
		},
		{
			name: "linux is left alone",
			env:  []string{"PATH=/bin"},
			goos: "linux",
			dir:  ok,
			want: []string{"PATH=/bin"},
		},
		{
			name: "yours is kept",
			env:  []string{"GOTMPDIR=/somewhere/else"},
			goos: "darwin",
			dir:  ok,
			want: []string{"GOTMPDIR=/somewhere/else"},
		},
		{
			name: "an empty one is still yours",
			env:  []string{"GOTMPDIR="},
			goos: "darwin",
			dir:  ok,
			want: []string{"GOTMPDIR="},
		},
		{
			name: "no cache directory, no change",
			env:  []string{"PATH=/bin"},
			goos: "darwin",
			dir:  func() (string, error) { return "", errors.New("no home") },
			want: []string{"PATH=/bin"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, apply(tt.env, tt.goos, tt.dir))
		})
	}
}

func TestApplyDoesNotAliasTheEnvironmentItIsGiven(t *testing.T) {
	env := make([]string, 1, 4)
	env[0] = "PATH=/bin"
	out := apply(env, "darwin", func() (string, error) { return "/cache", nil })

	apply(env, "darwin", func() (string, error) { return "/other", nil })
	assert.Equal(t, "GOTMPDIR=/cache", out[1], "a second call wrote over the first result's backing array")
}
