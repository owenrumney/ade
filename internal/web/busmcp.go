package web

import (
	"encoding/json"
	"net/http"
)

// BusMCPPath is where ade serves the bus tools. The agentbus skills name the
// server "agentbus", so the config entry keeps that name whatever serves it.
const BusMCPPath = "/mcp/agentbus"

// busTool is one tool the agent can call, and the launcher route behind it.
type busTool struct {
	Name   string
	Path   string
	About  string
	Fields map[string]any
}

// Only what an agent can usefully do. Answering and claiming are the person's,
// and offering them here would let a run answer a question put to its human.
var busTools = []busTool{
	{
		Name:  "ask_human",
		Path:  "/api/bus/ask",
		About: "Ask a person a question over agentbus and get an id back. The answer arrives in this conversation as a note; it does not block.",
		Fields: map[string]any{
			"question": map[string]any{"type": "string", "description": "the question, with enough context to answer without this conversation"},
			"to":       map[string]any{"type": "string", "description": "anyone (default), *, an email, or a group"},
			"options":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "optional answer choices"},
		},
	},
	{
		Name:  "request_review",
		Path:  "/api/bus/review",
		About: "Ask the team to review a pull request. Whoever claims it gets it checked out in their own ade.",
		Fields: map[string]any{
			"pr":   map[string]any{"type": "string", "description": "a github.com pull request URL, or owner/repo#123"},
			"to":   map[string]any{"type": "string", "description": "anyone (default), *, an email, or a group"},
			"note": map[string]any{"type": "string", "description": "what to look at, or why it matters"},
		},
	},
	{
		Name:   "who_is_online",
		Path:   "/api/bus/who",
		About:  "List the people and harnesses on agentbus, with presence and what each accepts.",
		Fields: map[string]any{},
	},
}

func busToolFor(name string) (busTool, bool) {
	for _, tool := range busTools {
		if tool.Name == name {
			return tool, true
		}
	}
	return busTool{}, false
}

// handleBusMCP serves the bus tools over the same JSON-RPC the wiki uses. Each
// call forwards to the launcher, which is the one holding the connection.
func (s *Server) handleBusMCP(w http.ResponseWriter, r *http.Request) {
	// The agent's own token, not the browser's: these tools are reached from a
	// run, and they can message other people.
	if !s.delegateAuthorized(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var call struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
		writeRPCError(w, nil, -32700, "could not read that request")
		return
	}
	if len(call.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch call.Method {
	case "initialize":
		writeRPCResult(w, call.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "agentbus", "version": "1"},
		})
	case "ping":
		writeRPCResult(w, call.ID, map[string]any{})
	case "tools/list":
		writeRPCResult(w, call.ID, map[string]any{"tools": busToolList()})
	case "tools/call":
		tool, ok := busToolFor(call.Params.Name)
		if !ok {
			writeRPCError(w, call.ID, -32602, "unknown tool "+call.Params.Name)
			return
		}
		writeRPCResult(w, call.ID, s.callBusTool(r, tool, call.Params.Arguments))
	default:
		writeRPCError(w, call.ID, -32601, "unknown method "+call.Method)
	}
}

func busToolList() []any {
	out := make([]any, 0, len(busTools))
	for _, tool := range busTools {
		out = append(out, map[string]any{
			"name":        tool.Name,
			"description": tool.About,
			"inputSchema": map[string]any{"type": "object", "properties": tool.Fields},
		})
	}
	return out
}

// callBusTool hands the arguments to the launcher and gives the agent back
// whatever it said, refusals included.
func (s *Server) callBusTool(r *http.Request, tool busTool, args map[string]any) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	args["dir"] = s.startRoot
	args["from"] = "agent"

	method := http.MethodPost
	var body any = args
	if tool.Name == "who_is_online" {
		method, body = http.MethodGet, nil
	}
	var out json.RawMessage
	if err := busCall(r.Context(), method, tool.Path, body, &out); err != nil {
		return busToolResult(err.Error(), true)
	}
	return busToolResult(string(out), false)
}

func busToolResult(text string, failed bool) map[string]any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
		"isError": failed,
	}
}

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": id, "result": result,
	})
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": message},
	})
}
