package linear

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMyIssuesAsksWithTheKeyAndOrdersTheAnswer(t *testing.T) {
	var got struct {
		auth  string
		query string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.auth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		var q struct{ Query string }
		_ = json.Unmarshal(body, &q)
		got.query = q.Query
		_, _ = io.WriteString(w, `{"data":{"viewer":{"assignedIssues":{"nodes":[
			{"identifier":"FIX-1","title":"low todo","url":"u1","priority":4,"updatedAt":"2026-08-01T00:00:00Z","state":{"name":"Todo","type":"unstarted"}},
			{"identifier":"FIX-2","title":"none todo, newer","url":"u2","priority":0,"updatedAt":"2026-08-20T00:00:00Z","state":{"name":"Todo","type":"unstarted"}},
			{"identifier":"FIX-3","title":"in progress","url":"u3","priority":3,"branchName":"fix-3","updatedAt":"2026-07-01T00:00:00Z","state":{"name":"In Progress","type":"started"}},
			{"identifier":"FIX-4","title":"urgent todo","url":"u4","priority":1,"updatedAt":"2026-06-01T00:00:00Z","state":{"name":"Todo","type":"unstarted"}}
		]}}}}`)
	}))
	defer srv.Close()

	issues, err := Client{Key: "lin_api_x", BaseURL: srv.URL}.MyIssues(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "lin_api_x", got.auth)
	assert.Contains(t, got.query, `"unstarted", "started"`)

	ids := make([]string, 0, len(issues))
	for _, i := range issues {
		ids = append(ids, i.Identifier)
	}
	assert.Equal(t, []string{"FIX-3", "FIX-4", "FIX-1", "FIX-2"}, ids, "started, then urgent, then low, then none")
	assert.True(t, issues[0].Started)
	assert.Equal(t, "fix-3", issues[0].BranchName)
	assert.Equal(t, "In Progress", issues[0].State)
}

func TestMyIssuesReportsRefusalsAndErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"bad key", http.StatusUnauthorized, `{}`, "refused the API key"},
		{"graphql error", http.StatusOK, `{"errors":[{"message":"boom"}]}`, "boom"},
		{"outage", http.StatusBadGateway, ``, "502"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			_, err := Client{Key: "k", BaseURL: srv.URL}.MyIssues(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}

	_, err := Client{}.MyIssues(context.Background())
	assert.ErrorContains(t, err, "no Linear API key")
}

func TestSortIsStableForTies(t *testing.T) {
	now := time.Now()
	issues := []Issue{{Identifier: "a", Priority: 2, UpdatedAt: now}, {Identifier: "b", Priority: 2, UpdatedAt: now}}
	Sort(issues)
	assert.Equal(t, "a", issues[0].Identifier)
}
