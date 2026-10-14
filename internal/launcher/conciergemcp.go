package launcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/gitdiff"

	"github.com/owenrumney/ade/internal/concierge"
	"github.com/owenrumney/ade/internal/todo"
)

// deskTool is one thing the desk can do. The two that act draw a card instead:
// a press cannot come from a tool call.
type deskTool struct {
	Name   string
	About  string
	Fields map[string]any
}

var deskTools = []deskTool{
	{
		Name:  "todo_list",
		About: "The todo list: this machine's, and a checkout's too when one is named.",
		Fields: map[string]any{
			"repo": map[string]any{"type": "string", "description": "a checkout path, for that repository's own todos"},
		},
	},
	{
		Name:  "todo_add",
		About: "File a todo. The text is all that is needed; a date can follow later.",
		Fields: map[string]any{
			"text": map[string]any{"type": "string", "description": "what to get to, in one line"},
			"repo": map[string]any{"type": "string", "description": "a checkout path, to file it against that repository"},
			"due":  map[string]any{"type": "string", "description": "RFC3339, or left out"},
		},
	},
	{
		Name:  "todo_done",
		About: "Mark a todo finished, or open again.",
		Fields: map[string]any{
			"id":   map[string]any{"type": "string", "description": "the todo's id, from todo_list"},
			"repo": map[string]any{"type": "string", "description": "the checkout it was filed against, if it was"},
			"done": map[string]any{"type": "boolean", "description": "false reopens it; the default is true"},
		},
	},
	{
		Name:  "todo_drop",
		About: "Delete a todo. Use it when the thing is not worth doing, not when it is done.",
		Fields: map[string]any{
			"id":   map[string]any{"type": "string", "description": "the todo's id, from todo_list"},
			"repo": map[string]any{"type": "string", "description": "the checkout it was filed against, if it was"},
		},
	},
	{
		Name:   "sessions",
		About:  "Every conversation running on this machine: where it is, what branch, and whether its agent is busy.",
		Fields: map[string]any{},
	},
	{
		Name: "say",
		About: "Propose a message for another session. It draws a card; nothing is sent until the person presses Start. " +
			"Say so plainly rather than reporting it as done.",
		Fields: map[string]any{
			"session": map[string]any{"type": "string", "description": "the session's id, from sessions"},
			"message": map[string]any{"type": "string", "description": "the turn to queue, written as the person would say it"},
		},
	},
	{
		Name: "start",
		About: "Propose starting work in a checkout. It draws a card; nothing starts until the person presses Start. " +
			"The text is the new session's first turn, so write it as an instruction.",
		Fields: map[string]any{
			"repo":   map[string]any{"type": "string", "description": "a checkout path, from sessions"},
			"text":   map[string]any{"type": "string", "description": "the first turn, or the design brief when design is true"},
			"design": map[string]any{"type": "boolean", "description": "start from a design document rather than a prompt"},
			"name":   map[string]any{"type": "string", "description": "what to call the design, when design is true"},
		},
	},
	{
		Name:   "waiting",
		About:  "What is waiting on the person: sessions that stopped on a question, and reviews asked of them.",
		Fields: map[string]any{},
	},
}

func deskToolFor(name string) (deskTool, bool) {
	for _, tool := range deskTools {
		if tool.Name == name {
			return tool, true
		}
	}
	return deskTool{}, false
}

// handleConciergeMCP serves the desk's tools over the JSON-RPC the bus speaks.
// The launcher answers them itself: it is the process holding the sessions.
func (s *Server) handleConciergeMCP(w http.ResponseWriter, r *http.Request) {
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
			"serverInfo":      map[string]any{"name": concierge.ServerName, "version": "1"},
		})
	case "ping":
		writeRPCResult(w, call.ID, map[string]any{})
	case "tools/list":
		writeRPCResult(w, call.ID, map[string]any{"tools": deskToolList()})
	case "tools/call":
		tool, ok := deskToolFor(call.Params.Name)
		if !ok {
			writeRPCError(w, call.ID, -32602, "unknown tool "+call.Params.Name)
			return
		}
		writeRPCResult(w, call.ID, s.callDeskTool(r, tool, call.Params.Arguments))
	default:
		writeRPCError(w, call.ID, -32601, "unknown method "+call.Method)
	}
}

func deskToolList() []any {
	out := make([]any, 0, len(deskTools))
	for _, tool := range deskTools {
		out = append(out, map[string]any{
			"name":        tool.Name,
			"description": tool.About,
			"inputSchema": map[string]any{"type": "object", "properties": tool.Fields},
		})
	}
	return out
}

func (s *Server) callDeskTool(r *http.Request, tool deskTool, args map[string]any) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	out, err := s.runDeskTool(r, tool.Name, args)
	if err != nil {
		return deskResult(err.Error(), true)
	}
	body, err := json.Marshal(out)
	if err != nil {
		return deskResult(err.Error(), true)
	}
	return deskResult(string(body), false)
}

func (s *Server) runDeskTool(r *http.Request, name string, args map[string]any) (any, error) {
	switch name {
	case "sessions":
		return s.activeSessions(r.Context()), nil
	case "waiting":
		return s.waiting(r.Context()), nil
	case "say":
		return s.drawCard(r.Context(), Card{
			Kind: CardSay, Session: text(args, "session"), Text: text(args, "message"),
		})
	case "start":
		return s.drawCard(r.Context(), Card{
			Kind: CardStart, Dir: text(args, "repo"), Text: text(args, "text"),
			Name: text(args, "name"), Design: args["design"] == true,
		})
	}

	store, err := s.todoStore(r.Context(), text(args, "repo"))
	if err != nil {
		return nil, fmt.Errorf("%q is not a checkout ade can keep todos for", text(args, "repo"))
	}
	switch name {
	case "todo_list":
		return store.List(), nil
	case "todo_add":
		due, err := parseDue(text(args, "due"))
		if err != nil {
			return nil, err
		}
		return store.Add(todo.Item{Text: text(args, "text"), Due: due, From: todo.FromConcierge})
	case "todo_done":
		done := true
		if said, ok := args["done"].(bool); ok {
			done = said
		}
		return store.Update(text(args, "id"), todo.Patch{Done: &done})
	case "todo_drop":
		if err := store.Delete(text(args, "id")); err != nil {
			return nil, err
		}
		return map[string]any{"deleted": true, "at": time.Now()}, nil
	}
	return nil, fmt.Errorf("unknown tool %s", name)
}

func text(args map[string]any, key string) string {
	said, _ := args[key].(string)
	return strings.TrimSpace(said)
}

func deskResult(said string, failed bool) map[string]any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": said}},
		"isError": failed,
	}
}

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": code, "message": message},
	})
}

// drawCard puts a proposal up and tells the desk it is waiting. A tool call
// is not a press, so this is the furthest either of these tools gets.
func (s *Server) drawCard(ctx context.Context, card Card) (any, error) {
	switch card.Kind {
	case CardSay:
		// Resolved now, so a card is never drawn about a session that is not
		// there. It is resolved again on the press: an hour may pass between.
		if _, err := s.sessionURL(ctx, card.Session); err != nil {
			return nil, err
		}
		card.Name = sessionName(s.localSessions(ctx), card.Session)
	case CardStart:
		if _, err := gitdiff.GitDir(ctx, card.Dir); err != nil {
			return nil, fmt.Errorf("%q is not a checkout on this machine", card.Dir)
		}
		if card.Name == "" {
			card.Name = filepath.Base(card.Dir)
		}
	}
	drawn, err := s.draw(card)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"card":    drawn,
		"waiting": "the card is drawn. Nothing happens until they press Start.",
	}, nil
}

func sessionName(rows []ActiveSession, id string) string {
	for _, row := range rows {
		if row.ID == id {
			return row.Name
		}
	}
	return id
}
