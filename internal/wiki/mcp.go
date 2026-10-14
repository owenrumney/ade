package wiki

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// mcpProtocol is the revision this server answers initialize with.
const mcpProtocol = "2025-06-18"

// SubmitTool is the name the agent calls. It is the only tool ADE's own MCP
// server offers, and the only way a draft leaves the sandbox.
const SubmitTool = "wiki_submit"

// MCP serves wiki_submit to one session's agent over streamable HTTP. Git
// never runs in the sandbox; this endpoint is the whole boundary.
type MCP struct {
	Store *Store
	// Draft is the opaque id ADE allocated before the agent started. It names
	// the draft and authenticates the caller, and nothing else may use it.
	Draft string
	// Agent names the provider in the published commit's trailer.
	Agent func() string
}

// ToolDescription is what the agent reads before calling.
const ToolDescription = "Publish this session's wiki draft to the shared wiki. " +
	"Call it once, explicitly, after writing durable notes into the mounted draft directory. " +
	"Only Markdown under general/ and repos/<this repo>/ is published; deletes and renames are refused."

func (m MCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if why := m.refuse(r); why != "" {
		slog.Warn("the wiki tool refused a call", "why", why)
		http.Error(w, why, http.StatusForbidden)
		return
	}
	var call struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&call); err != nil {
		writeRPC(w, nil, nil, &rpcError{Code: -32700, Message: "parse error"})
		return
	}
	// A notification has no id and takes no reply.
	if len(call.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch call.Method {
	case "initialize":
		writeRPC(w, call.ID, map[string]any{
			"protocolVersion": mcpProtocol,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "ade-wiki", "version": "1"},
		}, nil)
	case "ping":
		writeRPC(w, call.ID, map[string]any{}, nil)
	case "tools/list":
		writeRPC(w, call.ID, map[string]any{"tools": []any{map[string]any{
			"name":        SubmitTool,
			"description": ToolDescription,
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		}}}, nil)
	case "tools/call":
		if call.Params.Name != SubmitTool {
			writeRPC(w, call.ID, nil, &rpcError{Code: -32602, Message: "unknown tool " + call.Params.Name})
			return
		}
		writeRPC(w, call.ID, m.submit(r), nil)
	default:
		writeRPC(w, call.ID, nil, &rpcError{Code: -32601, Message: "unknown method " + call.Method})
	}
}

func (m MCP) submit(r *http.Request) map[string]any {
	agent := ""
	if m.Agent != nil {
		agent = m.Agent()
	}
	draft, err := m.Store.draftOf(m.Draft)
	if err != nil {
		return toolResult(err.Error(), true)
	}
	out, err := m.Store.Submit(r.Context(), draft, agent)
	if err != nil {
		return toolResult(fmt.Sprintf("%s: %s", out.State, err.Error()), true)
	}
	if out.State == "unchanged" {
		return toolResult("nothing to publish: the draft matches the shared wiki", false)
	}
	return toolResult("published as "+out.Commit, false)
}

// refuse says why a call is not allowed, or "" when it is. Three different
// faults shared one "forbidden", which diagnosed none of them.
func (m MCP) refuse(r *http.Request) string {
	switch {
	case m.Store == nil:
		return "this session has no wiki store"
	case m.Draft == "":
		return "this session has no wiki draft: it started before a wiki remote was set, so nothing can be published from it"
	case bearer(r) == "":
		return "the wiki tool was called with no bearer token"
	case !m.authorized(r):
		return "that token is not this session's wiki draft: the mcp config was written by a different session, so restart this one"
	}
	return ""
}

// authorized accepts the draft id as a bearer token. It is per-session and
// never leaves the host beyond the one container it was written into.
func (m MCP) authorized(r *http.Request) bool {
	return subtle.ConstantTimeCompare([]byte(bearer(r)), []byte(m.Draft)) == 1
}

func bearer(r *http.Request) string {
	return strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
}

func toolResult(text string, failed bool) map[string]any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
		"isError": failed,
	}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func writeRPC(w http.ResponseWriter, id json.RawMessage, result any, fault *rpcError) {
	body := map[string]any{"jsonrpc": "2.0"}
	if len(id) > 0 {
		body["id"] = id
	}
	if fault != nil {
		body["error"] = fault
	} else {
		body["result"] = result
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

// MCPPath is where ADE's web server serves this session's wiki tool.
const MCPPath = "/mcp/wiki"

// ServerConfig is the MCP entry an agent reads to reach base's wiki tool.
// The draft id is the bearer token: it names the draft and authorises it.
func ServerConfig(base, draft string) map[string]json.RawMessage {
	entry, err := json.Marshal(map[string]any{
		"type":    "http",
		"url":     strings.TrimSuffix(base, "/") + MCPPath,
		"headers": map[string]string{"Authorization": "Bearer " + draft},
	})
	if err != nil {
		return nil
	}
	return map[string]json.RawMessage{"ade_wiki": entry}
}

// WriteServerConfig writes a --mcp-config file for a run on the host, where
// ADE owns no box to merge into.
func WriteServerConfig(path, base, draft string) error {
	data, err := json.MarshalIndent(map[string]any{"mcpServers": ServerConfig(base, draft)}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
