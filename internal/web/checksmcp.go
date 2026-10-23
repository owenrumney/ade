package web

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/verify"
)

// ChecksMCPPath is where ade serves the repository's own checks. The box
// carries no toolchain, so this is how a run builds and tests at all.
const ChecksMCPPath = "/mcp/checks"

// ChecksServerName is what the agent sees them as.
const ChecksServerName = "checks"

// checksAtOnce bounds what runs here at once: the box's CPU limit does not
// apply out here, and several sessions would each take the machine.
const checksAtOnce = 2

// checksGate bounds what every session on this machine is running at once.
var checksGate = make(chan struct{}, checksAtOnce) //nolint:gochecknoglobals // a machine-wide limit

// checksRunning counts what is going, for the state the page draws.
var checksRunning sync.WaitGroup //nolint:gochecknoglobals // paired with checksGate

// handleChecksMCP serves the checkout's own checks as tools. Each runs on
// this machine, not in the box: the version comes from the checkout's pins.
func (s *Server) handleChecksMCP(w http.ResponseWriter, r *http.Request) {
	// Claude sends the header; Codex only knows how to send a bearer token.
	if !s.delegateAuthorized(r) && !s.delegateBearer(r) {
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
			"serverInfo":      map[string]any{"name": ChecksServerName, "version": "1"},
		})
	case "ping":
		writeRPCResult(w, call.ID, map[string]any{})
	case "tools/list":
		writeRPCResult(w, call.ID, map[string]any{"tools": s.checkToolList()})
	case "tools/call":
		writeRPCResult(w, call.ID, s.callCheckTool(r.Context(), call.Params.Name, call.Params.Arguments))
	default:
		writeRPCError(w, call.ID, -32601, "unknown method "+call.Method)
	}
}

// checkToolName is the tool for a check, prefixed so it cannot collide with
// another server's and reads as what it is.
func checkToolName(name string) string {
	return "run_" + strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			return r
		case r >= 'A' && r <= 'Z':
			return r + 32
		default:
			return '_'
		}
	}, name)
}

// offered is the checks a run may start: the ones marked as tools.
func (s *Server) offered() []verify.Check {
	config, err := verify.Load(verify.Path(s.commonGitDir))
	if err != nil {
		return nil
	}
	var out []verify.Check
	for _, check := range config.Checks {
		if check.Tool {
			out = append(out, check)
		}
	}
	return out
}

func (s *Server) checkToolList() []any {
	checks := s.offered()
	out := make([]any, 0, len(checks))
	for _, check := range checks {
		fields := map[string]any{
			"paths": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "packages or files to run against, inside this checkout. Omit for all of them.",
			},
		}
		if check.Filter != "" {
			fields["filter"] = map[string]any{
				"type":        "string",
				"description": "a test-name pattern",
			}
		}
		about := check.About
		if about == "" {
			about = fmt.Sprintf("Run this repository's %q check: %s", check.Name, check.Run)
		}
		out = append(out, map[string]any{
			"name":        checkToolName(check.Name),
			"description": about + " It runs on the host, on the versions this checkout pins.",
			"inputSchema": map[string]any{"type": "object", "properties": fields},
		})
	}
	return out
}

// callCheckTool runs one check. A name it does not know says what it has,
// because a box with no toolchain and no tools is a dead end.
func (s *Server) callCheckTool(ctx context.Context, name string, args map[string]any) map[string]any {
	checks := s.offered()
	var found *verify.Check
	for _, check := range checks {
		if checkToolName(check.Name) == name {
			found = &check
			break
		}
	}
	if found == nil {
		return busToolResult(unknownCheck(name, checks), true)
	}
	narrowed := verify.Args{Paths: strs(args["paths"]), Filter: str(args["filter"])}

	select {
	case checksGate <- struct{}{}:
	case <-ctx.Done():
		return busToolResult("the machine is busy with other checks; try again", true)
	}
	checksRunning.Add(1)
	defer func() {
		<-checksGate
		checksRunning.Done()
	}()

	// Its own deadline and not the post-turn pass's cancel: that one belongs
	// to the turn, and cancelling across the two kills the wrong run.
	each, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	result, err := verify.One(each, s.space().homeRoot, *found, narrowed)
	if err != nil {
		return busToolResult(err.Error(), true)
	}
	return busToolResult(saidCheck(*found, result), !result.Passed)
}

// saidCheck is what the agent reads back: the verdict, and the tail of the
// output when it failed. A pass says so in one line and no more.
func saidCheck(check verify.Check, result verify.Result) string {
	took := fmt.Sprintf("%.1fs", float64(result.Millis)/1000)
	if result.Passed {
		return fmt.Sprintf("%s passed (%s)", check.Name, took)
	}
	what := "failed"
	if result.TimedOut {
		what = "timed out"
	}
	return fmt.Sprintf("%s %s (%s)\n\n%s", check.Name, what, took, result.Output)
}

func unknownCheck(name string, checks []verify.Check) string {
	if len(checks) == 0 {
		return "this checkout offers no checks. Add one to .git/ade/checks.json, " +
			"or ask the person to: nothing here can run a build or a test otherwise."
	}
	var have []string
	for _, check := range checks {
		have = append(have, checkToolName(check.Name))
	}
	return fmt.Sprintf("no check called %s. This checkout has: %s", name, strings.Join(have, ", "))
}

// delegateBearer is the same token as an Authorization header.
func (s *Server) delegateBearer(r *http.Request) bool {
	given, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && s.delegateToken != "" &&
		subtle.ConstantTimeCompare([]byte(strings.TrimSpace(given)), []byte(s.delegateToken)) == 1
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func strs(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, each := range list {
		if s := str(each); s != "" {
			out = append(out, s)
		}
	}
	return out
}
