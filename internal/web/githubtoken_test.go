package web

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTheHostGitHubTokenFillsOnlyWhatTheBoxLacks(t *testing.T) {
	tests := []struct {
		name string
		host string
		env  map[string]string
		want map[string]string
	}{
		{
			name: "an empty box takes the host's token everywhere",
			host: "host",
			env:  map[string]string{"NODE_AUTH_TOKEN": ""},
			want: map[string]string{"GH_TOKEN": "host", "GITHUB_TOKEN": "host", "NODE_AUTH_TOKEN": "host"},
		},
		{
			name: "a token the box was given is kept",
			host: "host",
			env:  map[string]string{"GH_TOKEN": "session", "NODE_AUTH_TOKEN": "packages"},
			want: map[string]string{"GH_TOKEN": "session", "GITHUB_TOKEN": "host", "NODE_AUTH_TOKEN": "packages"},
		},
		{
			name: "no host token adds nothing",
			env:  map[string]string{"NODE_AUTH_TOKEN": ""},
			want: map[string]string{"NODE_AUTH_TOKEN": ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			was := hostGitHubToken
			t.Cleanup(func() { hostGitHubToken = was })
			hostGitHubToken = func(context.Context) string { return tt.host }

			fillGitHubTokens(context.Background(), tt.env)
			assert.Equal(t, tt.want, tt.env)
		})
	}
}
