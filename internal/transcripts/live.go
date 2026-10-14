package transcripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func filepathBase(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	return filepath.Base(fields[0])
}

// LiveWindow is how fresh a transcript write must be to count as live. The only
// signal that catches a new session: argv carries an id only when resuming.
const LiveWindow = 3 * time.Minute

// maxCWDLookups bounds how many processes we interrogate per refresh.
const maxCWDLookups = 12

// agentByBinary maps a process name to the agent that owns its transcripts.
var agentByBinary = map[string]Agent{
	"claude":     AgentClaude,
	"codex":      AgentCodex,
	"pi":         AgentPi,
	"watchtower": AgentWatchtower,
}

// sessionIDFlags precede a session id in an agent's argv.
var sessionIDFlags = map[string]bool{
	"--resume":     true,
	"--session":    true,
	"--session-id": true,
	"-r":           true,
}

// headlessMarkers mark processes nobody is sitting in. Without these the mdview
// sidecars, which resume real session ids, would all read as live.
var headlessMarkers = map[string]bool{
	"-p":              true,
	"--print":         true,
	"--input-format":  true,
	"--output-format": true,
	"--bg-pty-host":   true,
	"--mcp-server":    true,
}

// Liveness reports which sessions look attached to a running agent, keyed by
// path. A map, not a tag, so refreshing cannot race a background parse.
func Liveness(sessions []*Session) map[string]bool {
	live := make(map[string]bool, len(sessions))

	// A transcript written moments ago means work is happening now.
	for _, s := range sessions {
		if unix, ok := modTime(s.Path); ok {
			if time.Since(time.Unix(unix, 0)) < LiveWindow {
				live[s.Path] = true
			}
		}
	}

	// A resumed session may sit untouched while it waits for you to type, so
	// also believe the process table.
	ids, dirs := runningAgents()
	for _, s := range sessions {
		if s.ID != "" && ids[s.ID] {
			live[s.Path] = true
		}
	}
	// A session started fresh names no id anywhere, so fall back to matching an
	// agent's working directory to the newest session run there.
	for _, dir := range dirs {
		if s := newestSessionUnder(sessions, dir); s != nil {
			live[s.Path] = true
		}
	}
	return live
}

// agentDir is an interactive agent process that never named a session id.
type agentDir struct {
	agent Agent
	cwd   string
}

// newestSessionUnder picks the session a process is most likely sitting in: the
// same agent, in or under that directory, used most recently.
//
// Ambiguous if two fresh sessions share a directory; the newest wins.
func newestSessionUnder(sessions []*Session, dir agentDir) *Session {
	var best *Session
	for _, s := range sessions {
		if s.Agent != dir.agent || s.CWD == "" || !underDir(dir.cwd, s.CWD) {
			continue
		}
		if best == nil || s.LastUsed.After(best.LastUsed) {
			best = s
		}
	}
	return best
}

// underDir reports whether path is root or sits inside it. Agents wander into
// subdirectories, so an exact match is not enough.
func underDir(path, root string) bool {
	if path == root {
		return true
	}
	root = strings.TrimSuffix(root, string(filepath.Separator))
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

// processCWD reads a process's working directory. /proc where it exists, lsof
// otherwise; best effort either way.
func processCWD(pid string) string {
	if link, err := os.Readlink(filepath.Join("/proc", pid, "cwd")); err == nil {
		return link
	}
	out, err := exec.Command("lsof", "-a", "-p", pid, "-d", "cwd", "-Fn").Output()
	if err != nil {
		return ""
	}
	for line := range strings.Lines(string(out)) {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "n/"); ok {
			return "/" + after
		}
	}
	return ""
}

// runningAgents inspects the process table for interactive agents, returning the
// session ids they name and, for those naming none, their working directories.
//
// Best effort: if ps is unavailable we simply learn nothing.
func runningAgents() (map[string]bool, []agentDir) {
	out, err := exec.Command("ps", "-Axo", "pid=,command=").Output()
	if err != nil {
		return nil, nil
	}
	ids := map[string]bool{}
	var dirs []agentDir
	for line := range strings.Lines(string(out)) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, argv := fields[0], fields[1:]
		agent, interactive := agentOfCommand(argv)
		if !interactive {
			continue
		}
		found := sessionIDsInCommand(argv)
		for _, id := range found {
			ids[id] = true
		}
		// Looking up a cwd costs a syscall or an lsof, so only do it when the
		// argv told us nothing and we have not hit the cap.
		if len(found) == 0 && len(dirs) < maxCWDLookups {
			if cwd := processCWD(pid); cwd != "" {
				dirs = append(dirs, agentDir{agent: agent, cwd: cwd})
			}
		}
	}
	return ids, dirs
}

// agentOfCommand identifies an interactive agent process, or reports false for
// anything headless or unrelated.
func agentOfCommand(fields []string) (Agent, bool) {
	if len(fields) == 0 {
		return "", false
	}
	agent, ok := agentByBinary[filepath.Base(fields[0])]
	if !ok {
		return "", false
	}
	for _, field := range fields {
		if headlessMarkers[field] || field == "daemon" {
			return "", false
		}
	}
	return agent, true
}

// sessionIDsInCommand extracts the session ids an interactive agent command
// refers to, or nothing if the command is headless or not an agent at all.
func sessionIDsInCommand(fields []string) []string {
	if _, ok := agentByBinary[filepathBase(fields)]; !ok {
		return nil
	}

	var ids []string
	forked := false
	for i, field := range fields {
		switch {
		case headlessMarkers[field]:
			return nil
		case field == "daemon":
			return nil
		case field == "--fork-session":
			// The resumed transcript is the source, not the live session; the
			// live one is whatever --session-id names.
			forked = true
		case sessionIDFlags[field] && i+1 < len(fields):
			if id := normaliseSessionID(fields[i+1]); id != "" {
				ids = append(ids, id)
			}
		case (field == "resume" || field == "open") && i+1 < len(fields):
			// codex resume <id>, watchtower open <id>.
			if id := normaliseSessionID(fields[i+1]); id != "" {
				ids = append(ids, id)
			}
		}
	}
	if forked {
		return forkedSessionID(fields)
	}
	return ids
}

// forkedSessionID returns only the --session-id value, used when --fork-session
// means the --resume target is a different session from the running one.
func forkedSessionID(fields []string) []string {
	for i, field := range fields {
		if field == "--session-id" && i+1 < len(fields) {
			if id := normaliseSessionID(fields[i+1]); id != "" {
				return []string{id}
			}
		}
	}
	return nil
}

// normaliseSessionID accepts either a bare id or a transcript path, since
// claude resumes by either.
func normaliseSessionID(value string) string {
	if value == "" || strings.HasPrefix(value, "-") {
		return ""
	}
	if !strings.HasSuffix(value, ".jsonl") {
		return value
	}
	name := strings.TrimSuffix(filepath.Base(value), ".jsonl")
	// pi names files "<timestamp>_<uuid>".
	if i := strings.LastIndex(name, "_"); i >= 0 {
		return name[i+1:]
	}
	return name
}
