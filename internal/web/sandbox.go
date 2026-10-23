package web

import (
	"net/http"
	"os"
	"strings"

	"github.com/owenrumney/ade/internal/sandbox"
)

// sandboxView is the repository's sandbox as the page edits it.
type sandboxView struct {
	On      bool              `json:"on"`
	Paths   []sandbox.Path    `json:"paths"`
	Env     map[string]string `json:"env"`
	Path    string            `json:"path"`
	Runtime string            `json:"runtime,omitempty"`
	// Fixed is why the sandbox cannot be turned off here, or empty when it can.
	Fixed string `json:"fixed,omitempty"`
	// Always is what the container gets whether or not the file names it:
	// without these the page showed an empty list for a mounted checkout.
	Always []alwaysMount `json:"always,omitempty"`
}

// alwaysMount is a mount ade makes itself, with the reason it makes it.
// Not editable: removing one would be a container the agent cannot work in.
type alwaysMount struct {
	Path string `json:"path"`
	Why  string `json:"why"`
	RW   bool   `json:"rw,omitempty"`
}

// always is what the spec mounts before the workspace file is read: the
// checkout, a worktree's git directory, and the design documents.
func (s *Server) always() []alwaysMount {
	out := []alwaysMount{}
	if dir := s.agent.dir; dir != "" {
		out = append(out, alwaysMount{Path: dir, Why: "the checkout", RW: true})
	}
	if git := s.agent.gitDir; git != "" && !under(git, s.agent.dir) {
		out = append(out, alwaysMount{Path: git, Why: "this worktree's git directory", RW: true})
	}
	if s.agent.docsDir != nil {
		if docs := s.agent.docsDir(); docs != "" && !under(docs, s.agent.dir) {
			out = append(out, alwaysMount{Path: docs, Why: "the design documents", RW: true})
		}
	}
	return out
}

// under is whether a path is the directory or inside it, so a mount the
// checkout already covers is not listed twice.
func under(path, dir string) bool {
	return dir != "" && (path == dir || strings.HasPrefix(path, dir+string(os.PathSeparator)))
}

func (s *Server) handleSandbox(w http.ResponseWriter, _ *http.Request) {
	ws, err := sandbox.LoadWorkspace(s.agent.workspace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.sandboxView(ws))
}

// handleSaveSandbox writes the repository's sandbox file. A container that
// is running was started with the old mounts, so it is dropped; the next
// turn starts one with the new. The conversation carries on.
func (s *Server) handleSaveSandbox(w http.ResponseWriter, r *http.Request) {
	var req sandbox.Workspace
	if !decode(w, r, &req) {
		return
	}
	// Without what is stored there is nothing behind the markers the page sent
	// back, and saving them would write bullets over every secret.
	was, err := sandbox.LoadWorkspace(s.agent.workspace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	env, err := sandbox.RestoreEnv(req.Env, was.Env)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Env = env
	home, _ := os.UserHomeDir()
	ws, err := sandbox.SaveWorkspace(s.agent.workspace, home, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.snapshotRoots(r.Context())
	if s.agent.restartSandbox() {
		s.agent.step("the sandbox changed, so its container went; the conversation resumes in a new one on the next turn")
	}
	writeJSON(w, http.StatusOK, s.sandboxView(ws))
}

// sandboxFixed is why the sandbox cannot be turned off, or "". On a worker it
// cannot: the worker image carries ade and no agent, so the host it would fall
// back to has no claude or codex on it.
func sandboxFixed() string {
	if strings.TrimSpace(os.Getenv("ADE_WORKER_NAME")) == "" {
		return ""
	}
	return "this session runs on a worker, which has no agent outside the container"
}

func (s *Server) sandboxView(ws sandbox.Workspace) sandboxView {
	view := sandboxView{On: s.agent.State().Sandbox, Paths: ws.Paths, Env: sandbox.RedactEnv(ws.Env),
		Path: s.agent.workspace, Fixed: sandboxFixed(), Always: s.always()}
	s.agent.mu.Lock()
	if s.agent.runtime != nil {
		view.Runtime = s.agent.runtime.Name()
	}
	s.agent.mu.Unlock()
	return view
}
