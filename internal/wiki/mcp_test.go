package wiki

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPRefusesACallWithoutTheDraftID(t *testing.T) {
	store := New(t.TempDir(), newRemote(t))
	server := MCP{Store: store, Draft: "session-mcp"}

	for _, header := range []string{"", "Bearer wrong", "Bearer "} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, MCPPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		if header != "" {
			r.Header.Set("Authorization", header)
		}
		server.ServeHTTP(w, r)
		assert.Equal(t, http.StatusForbidden, w.Code, header)
	}
}

func TestMCPOffersOnlyWikiSubmit(t *testing.T) {
	store := New(t.TempDir(), newRemote(t))
	body := callMCP(t, MCP{Store: store, Draft: "session-mcp"}, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)

	var reply struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(body, &reply))
	require.Len(t, reply.Result.Tools, 1)
	assert.Equal(t, SubmitTool, reply.Result.Tools[0].Name)
}

func TestMCPSubmitPublishesTheCallersOwnDraft(t *testing.T) {
	remote := newRemote(t)
	store := New(t.TempDir(), remote)
	draft, err := store.Draft(context.Background(), "session-mcp", checkoutWithOrigin(t))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(draft.Root, "general", "first.md"), []byte("from mcp\n"), 0o600))

	body := callMCP(t, MCP{Store: store, Draft: "session-mcp"}, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"wiki_submit"}}`)
	assert.Contains(t, string(body), "published as")
	assert.NotContains(t, string(body), `"isError":true`)

	work := t.TempDir()
	run(t, "", "git", "clone", remote, work)
	data, err := os.ReadFile(filepath.Join(work, "general", "first.md"))
	require.NoError(t, err)
	assert.Equal(t, "from mcp\n", string(data))
}

func TestMCPSubmitReportsARejectionRatherThanFailing(t *testing.T) {
	store := New(t.TempDir(), newRemote(t))
	draft, err := store.Draft(context.Background(), "session-mcp", checkoutWithOrigin(t))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(draft.Root, "general", "leak.md"), []byte("AKIAIOSFODNN7EXAMPLE\n"), 0o600))

	body := callMCP(t, MCP{Store: store, Draft: "session-mcp"}, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"wiki_submit"}}`)
	assert.Contains(t, string(body), "aws-access-key")
	assert.Contains(t, string(body), `"isError":true`)

	drafts, err := store.Drafts()
	require.NoError(t, err)
	require.Len(t, drafts, 1)
	assert.Equal(t, "rejected", drafts[0].State)
}

func TestServerConfigCarriesTheDraftAsItsBearer(t *testing.T) {
	config := ServerConfig("http://127.0.0.1:9999", "session-mcp")
	require.Contains(t, config, "ade_wiki")
	assert.JSONEq(t,
		`{"type":"http","url":"http://127.0.0.1:9999/mcp/wiki","headers":{"Authorization":"Bearer session-mcp"}}`,
		string(config["ade_wiki"]))
}

func callMCP(t *testing.T, server MCP, body string) []byte {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, MCPPath, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+server.Draft)
	server.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	return w.Body.Bytes()
}

func TestMCPRefusesEverythingButAPost(t *testing.T) {
	// A browser can only reach this endpoint with a preflight, and the mux
	// answers one with 405. An OPTIONS handler added later would open it.
	store := New(t.TempDir(), newRemote(t))
	server := MCP{Store: store, Draft: "session-mcp"}
	mux := http.NewServeMux()
	mux.Handle("POST "+MCPPath, server)

	for _, method := range []string{http.MethodGet, http.MethodOptions, http.MethodPut} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, MCPPath, strings.NewReader("{}")))
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code, method)
	}
}

func TestTheWikiToolSaysWhyItRefused(t *testing.T) {
	ask := func(m MCP, token string) string {
		req := httptest.NewRequest(http.MethodPost, MCPPath, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, req)
		return strings.TrimSpace(rec.Body.String())
	}

	store := New(t.TempDir(), "git@github.com:owner/wiki.git")
	assert.Contains(t, ask(MCP{Store: store}, "draft-1"), "no wiki draft")
	assert.Contains(t, ask(MCP{Store: store, Draft: "draft-1"}, ""), "no bearer token")
	assert.Contains(t, ask(MCP{Store: store, Draft: "draft-1"}, "draft-2"), "not this session's wiki draft")
	assert.NotContains(t, ask(MCP{Store: store, Draft: "draft-1"}, "draft-1"), "forbidden")
}
