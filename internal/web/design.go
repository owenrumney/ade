package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/render"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/design"
	"github.com/owenrumney/ade/internal/docs"
	"github.com/owenrumney/ade/internal/docsave"
	"github.com/owenrumney/ade/internal/ghpr"
)

// DesignState is the design side of the world, as the browser needs it.
type DesignState struct {
	Dir   string             `json:"dir"`
	File  string             `json:"file,omitempty"`
	Files []render.FileEntry `json:"files"`

	// Worktree is where a build would be isolated, so the choice can name it.
	Worktree *Worktree `json:"worktree,omitempty"`

	// Version fingerprints the open document's contents. The agent rewrites the
	// file underneath the browser, and without this the page would sit showing
	// what it fetched when the run started.
	Version string `json:"version,omitempty"`
}

type documentResponse struct {
	File    string             `json:"file"`
	Title   string             `json:"title"`
	Body    string             `json:"body"`
	TOC     []render.TOCEntry  `json:"toc"`
	Files   []render.FileEntry `json:"files"`
	Raw     string             `json:"raw"`
	Version string             `json:"version,omitempty"`
}

// handleDesignDoc renders one document, or the file list alone when the design
// directory is empty.
func (s *Server) handleDesignDoc(w http.ResponseWriter, r *http.Request) {
	file := strings.TrimSpace(r.URL.Query().Get("file"))
	if file == "" {
		file = s.currentDoc()
	}

	files, err := docs.Tree(s.docsDir, file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if file == "" {
		writeJSON(w, http.StatusOK, documentResponse{Files: files})
		return
	}

	doc, err := docs.Render(s.docsDir, file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	raw, err := docs.Read(s.docsDir, file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.setDoc(file)
	writeJSON(w, http.StatusOK, documentResponse{
		File: file, Title: doc.Title, Body: s.docImages(string(doc.Body)), TOC: doc.TOC,
		Files: files, Raw: raw, Version: docs.Version(s.docsDir, file),
	})
}

// handleDesignClose puts the open document away, so another can be opened
// or the work can carry on without one. The file stays where it is; the
// conversation just stops being told about it.
func (s *Server) handleDesignClose(w http.ResponseWriter, _ *http.Request) {
	if s.currentDoc() == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if s.buildingDoc(s.currentDoc()) {
		writeError(w, http.StatusConflict, "the agent is building this document; wait for it or stop it first")
		return
	}
	s.closeDoc()
	s.hub.Broadcast("state", nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleDesignDelete removes a document from disk. Closing puts one away and
// leaves it there; this is the other thing you sometimes want, and until now it
// was only on the command line as `ade rm`.
//
// Only the directory the page lists from, so the only documents reachable are
// the ones it showed you. docs.Delete refuses a name that climbs out of it.
func (s *Server) handleDesignDelete(w http.ResponseWriter, r *http.Request) {
	file := strings.TrimSpace(r.URL.Query().Get("file"))
	if file == "" {
		writeError(w, http.StatusBadRequest, "name a document to delete")
		return
	}
	dir, open := s.designPlace()
	if dir == "" {
		writeError(w, http.StatusBadRequest, "there is nowhere design documents live for this checkout")
		return
	}
	if s.buildingDoc(file) {
		writeError(w, http.StatusConflict, "the agent is building this document; wait for it or stop it first")
		return
	}
	if _, err := docs.Delete([]string{dir}, file); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// A name that no longer exists is not something to hold open.
	if sameDoc(file, open) {
		s.setDoc("")
	}
	s.hub.Broadcast("state", nil)
	w.WriteHeader(http.StatusNoContent)
}

// sameDoc compares two document names allowing for a missing .md, which is what
// docs.Filename tolerates everywhere else, and for case, since a document's name
// reaches the server from several places and the filesystem here is usually
// case-insensitive.
func sameDoc(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(docs.Filename(a), docs.Filename(b))
}

// buildingDoc reports a document a running build is reading as its brief. One
// helper, because every route that deletes or overwrites one asks it, and a
// rule written out per route is a rule three routes stop sharing.
func (s *Server) buildingDoc(name string) bool {
	return s.agent.State().Running && s.agent.kind() == "build" && sameDoc(name, s.currentDoc())
}

func (s *Server) handleDesignCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Title  string `json:"title"`
		Ticket string `json:"ticket"`
	}
	if !decode(w, r, &req) {
		return
	}

	ticket := strings.TrimSpace(req.Ticket)
	name := strings.TrimSpace(req.Name)
	if name == "" {
		// The ticket names the document, the same way ade create --linear does.
		name = ticket
	}

	file, err := docs.Create(s.docsDir, name, req.Title)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.setDoc(file)
	// Given a ticket there is something to write from, so the agent starts on a
	// draft rather than handing you an empty document.
	if ticket != "" {
		s.draftFromTicket(ticket)
	}
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{"file": file, "drafting": ticket != ""})
}

func (s *Server) handleDesignSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File string `json:"file"`
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := docs.Save(s.docsDir, req.File, req.Body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.Broadcast("state", nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleDelegateDoc is how an agent puts a document back: .git is a path its
// harness refuses to write. It replaces an existing document and creates none.
func (s *Server) handleDelegateDoc(w http.ResponseWriter, r *http.Request) {
	if !s.delegateAuthorized(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req struct {
		Name string `json:"name"`
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		writeError(w, http.StatusBadRequest, "refusing to write an empty document")
		return
	}
	dir, _ := s.designPlace()
	if dir == "" {
		writeError(w, http.StatusBadRequest, "there is nowhere design documents live for this checkout")
		return
	}
	// As delete and push: a build reads its brief as it runs.
	if s.buildingDoc(req.Name) {
		writeError(w, http.StatusConflict, "the agent is building this document; wait for it or stop it first")
		return
	}
	if err := docs.Save(dir, req.Name, req.Body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.Broadcast("state", nil)
	file := docs.Filename(req.Name)
	writeJSON(w, http.StatusOK, docsave.Answer{File: file, Path: filepath.Join(dir, file)})
}

// handleDesignPush receives a document from another machine and opens it. It is
// where a build sent to a worker lands, before the build itself is asked for.
func (s *Server) handleDesignPush(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		writeError(w, http.StatusBadRequest, "a pushed document needs a body")
		return
	}
	// As delete: a build reads its brief as it runs.
	if s.buildingDoc(req.Name) {
		writeError(w, http.StatusConflict, "the agent is building this document; wait for it or stop it first")
		return
	}
	file, err := docs.Put(s.docsDir, req.Name, req.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.setDoc(file)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, map[string]any{"file": file})
}

// handleDesignSeed opens the conversation from what was just written. It reports
// started=false rather than erroring when there is nothing to react to yet, so
// saving an untouched template does not spend an agent run or raise an alarm.
func (s *Server) handleDesignSeed(w http.ResponseWriter, _ *http.Request) {
	file := s.currentDoc()
	if file == "" {
		writeError(w, http.StatusBadRequest, "there is no design document open")
		return
	}
	body, err := docs.Read(s.docsDir, file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !hasProse(body) {
		writeJSON(w, http.StatusOK, map[string]any{"started": false, "reason": "the document is still just headings"})
		return
	}
	runCtx, ok := s.agent.begin(s.ctx, kindSeed)
	if !ok {
		s.queueBehind(w, queued{kind: kindSeed})
		return
	}

	s.saySeed(file)
	s.spawn(func() { s.runTurn(runCtx, design.SeedPrompt(s.docPath(file)), fmt.Sprintf("reading %s", file)) })
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true})
}

// condenserName is the agent that consolidates a finished design document.
const condenserName = "condenser"

// handleDesignCondense hands the open document to the condenser. It runs
// beside the conversation and writes the rewrite back over the doc capability.
func (s *Server) handleDesignCondense(w http.ResponseWriter, _ *http.Request) {
	file := s.currentDoc()
	if file == "" {
		writeError(w, http.StatusBadRequest, "there is no design document open")
		return
	}
	attached := &chat.Context{From: file, Text: "the whole document"}
	message := "Condense " + file + ", losing nothing."
	s.say(ChatMessage{Kind: chat.KindYou, Body: "@" + condenserName + " " + message, Context: attached})
	called := s.callSubagents([]string{condenserName}, message, attached, nil)
	if called == nil {
		writeError(w, http.StatusBadRequest, "there is no condenser agent in this checkout")
		return
	}
	if len(called) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"started": false, "reason": "too many agents are already running"})
		return
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true})
}

// handleDesignFold puts what a named agent said into the open document. Any
// of them: which reply is worth keeping is the user's call, not a list here.
func (s *Server) handleDesignFold(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad request")
		return
	}
	file := s.currentDoc()
	if file == "" {
		writeError(w, http.StatusBadRequest, "there is no design document open")
		return
	}
	said, ok := s.namedReply(req.ID)
	if !ok {
		writeError(w, http.StatusBadRequest, "that reply is not in this conversation")
		return
	}
	runCtx, started := s.agent.begin(s.ctx, kindFold)
	if !started {
		s.queueBehind(w, queued{kind: kindFold, reply: req.ID})
		return
	}

	s.sayFold(said.Author, file)
	s.spawn(func() {
		s.runTurn(runCtx, design.FoldPrompt(said.Author, s.docPath(file), said.Body), "updating "+file)
	})
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true})
}

func (s *Server) saySeed(file string) {
	s.say(ChatMessage{
		Kind: chat.KindYou, Body: "Here is the first draft. What have I missed?",
		Context: &chat.Context{From: file, Text: "the whole document"},
	})
}

func (s *Server) sayFold(author, file string) {
	s.say(ChatMessage{Kind: chat.KindYou, Body: fmt.Sprintf(
		"Review and fold what @%s said into %s where the review considers it applicable and relevant.", author, file)})
}

// seedNow is what a queued seed would read when its turn comes. The document
// may have been closed or emptied while it waited.
func (s *Server) seedNow() (file, prompt string, ok bool) {
	file = s.currentDoc()
	if file == "" {
		return "", "", false
	}
	body, err := docs.Read(s.docsDir, file)
	if err != nil || !hasProse(body) {
		return "", "", false
	}
	return file, design.SeedPrompt(s.docPath(file)), true
}

// foldNow is the same for a fold: the reply has to still be in the
// conversation and a document has to still be open to fold it into.
func (s *Server) foldNow(id string) (file, author, prompt string, ok bool) {
	file = s.currentDoc()
	if file == "" {
		return "", "", "", false
	}
	said, found := s.namedReply(id)
	if !found {
		return "", "", "", false
	}
	return file, said.Author, design.FoldPrompt(said.Author, s.docPath(file), said.Body), true
}

// namedReply is a named agent's message in this conversation. Only a named
// one: the main agent's own words are already the document's author.
func (s *Server) namedReply(id string) (chat.Message, bool) {
	if strings.TrimSpace(id) == "" {
		return chat.Message{}, false
	}
	for _, msg := range s.chatStore().List(s.agent.Current()) {
		if msg.ID != id {
			continue
		}
		if msg.Kind != chat.KindAgent || msg.Author == "" || strings.TrimSpace(msg.Body) == "" {
			return chat.Message{}, false
		}
		return msg, true
	}
	return chat.Message{}, false
}

// runTurn is a chat turn whose prompt was written by ade rather than typed:
// the seed, the ticket draft. It reads like a turn and fails like one.
func (s *Server) runTurn(ctx context.Context, prompt, step string) {
	defer s.recoverRun(kindChat)

	s.agent.step(step)
	err := s.sendTurn(ctx, prompt)
	s.endRun(err)
	if err != nil {
		slog.Warn("turn failed", "err", err)
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
}

// hasProse reports whether a document says anything beyond its headings, so the
// scaffold alone is not mistaken for a written brief. Front matter is metadata,
// not content, so the whole block is skipped rather than just its fences.
func hasProse(body string) bool {
	lines := strings.Split(body, "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		lines = lines[1:]
		for len(lines) > 0 {
			closed := strings.TrimSpace(lines[0]) == "---"
			lines = lines[1:]
			if closed {
				break
			}
		}
	}

	inComment := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// The scaffold's guidance lives in comments, so it must not read as a
		// brief the user has actually written.
		if inComment {
			inComment = !strings.Contains(line, "-->")
			continue
		}
		if strings.HasPrefix(line, "<!--") {
			inComment = !strings.Contains(line, "-->")
			continue
		}
		if line == "" || line == "---" || strings.HasPrefix(line, "#") {
			continue
		}
		return true
	}
	return false
}

// handleDesignGo hands the finished design to the agent to build: here, in a
// worktree checked out beside this one, or on another machine entirely.
func (s *Server) handleDesignGo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Worktree bool   `json:"worktree"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Machine  string `json:"machine"`
	}
	if r.ContentLength > 0 && !decode(w, r, &req) {
		return
	}

	file := s.currentDoc()
	if file == "" {
		writeError(w, http.StatusBadRequest, "there is no design document to build")
		return
	}
	if s.space().snapshot {
		writeError(w, http.StatusBadRequest, "this is a fetched snapshot, so there is nothing local to build in")
		return
	}
	// Before the agent is claimed: a build over there leaves this session alone,
	// so it is not this one being busy that would refuse it.
	if machine := strings.TrimSpace(req.Machine); machine != "" {
		at, path, err := s.buildOnMachine(r.Context(), machine, file, req.Provider, req.Model)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		s.say(ChatMessage{Kind: chat.KindNote, Body: fmt.Sprintf("Building %s on %s.", file, machine)})
		// path is the checkout over there, which is what a window opens on.
		writeJSON(w, http.StatusAccepted, map[string]any{"url": at, "machine": machine, "path": path})
		return
	}
	// Claimed before the worktree is made, so relocation cannot race a run and
	// so a failure here leaves nothing half-done.
	runCtx, ok := s.agent.begin(s.ctx, "build")
	if !ok {
		writeError(w, http.StatusConflict, "the agent is already running")
		return
	}

	var tree *Worktree
	if req.Worktree {
		var err error
		if tree, err = s.intoWorktree(runCtx); err != nil {
			s.agent.finish(err)
			s.hub.Broadcast("agent", nil)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	// After the move, so a worktree built in before does not resume what it
	// left. The window the design was argued out in is not the build's brief.
	s.clearForBuild(strings.TrimSpace(req.Provider), strings.TrimSpace(req.Model))

	if tree != nil {
		s.agent.step(fmt.Sprintf("building in %s on branch %s", tree.Path, tree.Branch))
	}
	s.agent.step("this is a fresh agent: the design document is its brief")
	s.say(ChatMessage{Kind: chat.KindYou, Body: fmt.Sprintf("Build what %s describes.", file)})
	s.spawn(func() {
		s.runBuild(runCtx, design.BuildPrompt(s.docPath(file)), fmt.Sprintf("building what %s describes", file))
	})
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	// The session moved, so the window has to follow: without the path the
	// editor stays on the checkout the design was argued out in.
	out := map[string]any{"running": true}
	if tree != nil {
		out["path"] = tree.Path
	}
	writeJSON(w, http.StatusAccepted, out)
}

func (s *Server) runBuild(ctx context.Context, prompt, step string) {
	defer s.recoverRun("build")

	started := time.Now()
	target := s.buildTarget(s.agent.modelName())
	stopWatch := s.watchOverrun(ctx, target, started)
	s.agent.step(step)
	err := s.sendTurn(ctx, prompt)
	stopWatch()
	if err == nil {
		s.agent.step("build finished — check the diff")
	}
	// Read now: the goroutine may run after the next session has begun.
	from := buildFrom{root: s.space().homeRoot, provider: s.agent.State().Provider, session: s.agent.Current()}
	s.spawn(func() { s.recordBuild(target, from, started, time.Now(), err != nil) })
	s.endRun(err)
	if err != nil {
		slog.Warn("build failed", "err", err)
	}
	if refreshErr := s.refresh(s.ctx); refreshErr != nil {
		slog.Debug("refresh after build", "err", refreshErr)
	}
	s.settleTurn()
	s.spawn(func() { s.capturePreview() })
	s.spawn(func() { s.attachOutbox() })
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
}

// adoptDesign looks for a document belonging to this branch and opens it if
// there is one, answering its name. Only ever opens: clearing one is
// rematchDesign's job, on a bind, where the branch has actually changed.
//
// Called from the state poll, so it runs while there is no document and stops
// as soon as there is one.
func (s *Server) adoptDesign(was string) (string, string) {
	ws := s.space()
	dirs := docs.SearchDirs(ws.homeRoot, s.commonGitDir, "")
	dir, file, ok := docs.Find(dirs, s.commonGitDir, ws.branch, s.agent.Current())
	// No name to match, so the design is whatever was written during the
	// conversation — until something is recorded for the branch.
	if !ok && designName(ws.branch) == "" {
		if _, recorded := docs.Opened(s.commonGitDir, ws.branch, s.agent.Current()); !recorded {
			dir, file, ok = docs.Newest(dirs, s.started)
		}
	}
	if !ok || s.declined(file) {
		return was, ""
	}
	s.mu.Lock()
	// Another poll may have got there first, and its answer is as good as this
	// one; either way what comes back is what is now open.
	if s.docFile == "" {
		s.docsDir, s.docFile = dir, file
	}
	dir, file = s.docsDir, s.docFile
	s.mu.Unlock()

	s.rememberDoc(ws.branch, file)
	return dir, file
}

// designDoc is where the design lives, for the turn prompt: the open document,
// or — when none is open — where a new one belongs.
//
// The second half is the point. Without it the agent is never told ade keeps
// design documents, so being asked to start one produced a file in whatever
// scratch place looked reasonable, which the Design tab never reads.
func (s *Server) designDoc() design.Doc {
	if file := s.currentDoc(); file != "" {
		return design.Doc{Path: s.docPath(file)}
	}
	if dir, _ := s.designPlace(); dir == "" {
		return design.Doc{}
	}
	if name := designName(s.space().branch); name != "" {
		return design.Doc{New: s.docPath(docs.Filename(name))}
	}
	return design.Doc{Dir: s.docPath("")}
}

// defaultBranches name no work, so every design done on one would be main.md.
var defaultBranches = map[string]bool{
	"main": true, "master": true, "trunk": true, "develop": true,
}

// designName is the name a new document takes, chosen so docs.Find will match
// it against this branch: the ticket the branch names, or its last segment.
// Empty on a default branch, where there is nothing to match against.
func designName(branch string) string {
	if ticket := docs.Ticket(branch); ticket != "" {
		return ticket
	}
	if i := strings.LastIndex(branch, "/"); i >= 0 {
		branch = branch[i+1:]
	}
	if defaultBranches[strings.ToLower(branch)] {
		return ""
	}
	return branch
}

// docPath is what the agent is told to open. It is repo-relative when the design
// directory sits inside the repo, and absolute otherwise.
func (s *Server) docPath(file string) string {
	full := filepath.Join(s.docsDir, filepath.FromSlash(file))
	if rel, err := filepath.Rel(s.space().homeRoot, full); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return full
}

func markdownHTML(body string) string {
	html, err := render.Markdown([]byte(body), render.Options{Theme: render.ThemeDark})
	if err != nil {
		return ""
	}
	return string(html)
}

// replyHTML is an agent reply as markdown. A picture in one is routed at the
// docs directory, the same as in the document: chat has nowhere else to serve.
func (s *Server) replyHTML(body string) string {
	return s.docImages(markdownHTML(body))
}

var docImageSrc = regexp.MustCompile(`(<img[^>]*?\ssrc=")(?:\./)?([^/"?#:][^"?#:]*)"`)

// docImages routes a relative picture at the docs directory; the token rides
// in the query because a browser sends no header for an <img>.
func (s *Server) docImages(html string) string {
	return docImageSrc.ReplaceAllString(html, `${1}design-images/${2}?t=`+url.QueryEscape(s.token)+`"`)
}

func (s *Server) currentDoc() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.docFile
}

// closeDoc puts the open document away and remembers it, so the poll's branch
// match does not find it on disk and open it straight back up. Closing means
// "not this one", not "for a moment".
func (s *Server) closeDoc() {
	s.declineDoc(s.currentDoc())
	s.setDoc("")
}

func (s *Server) setDoc(file string) {
	s.mu.Lock()
	// Opening one is the answer to having closed it.
	if file != "" && file == s.docDeclined {
		s.docDeclined = ""
	}
	s.docFile = file
	branch := s.ws.branch
	s.mu.Unlock()
	s.rememberDoc(branch, file)
}

// rememberDoc writes down this branch's document, so the next start reopens it
// without recognising its name. Failing to record only costs that.
func (s *Server) rememberDoc(branch, file string) {
	if err := docs.SetOpened(s.commonGitDir, branch, s.agent.Current(), file); err != nil {
		slog.Debug("record open document", "branch", branch, "file", file, "err", err)
	}
}

// currentDesign is the design state for /api/state. A tree read failure is not
// worth failing the whole request over, so it degrades to no file list.
//
// Everything below works from locals rather than the fields: adoptDesign can
// move the directory out from under a poll, and reading the field twice either
// side of that is how a tree gets listed from one place and versioned from
// another.
func (s *Server) currentDesign() *DesignState {
	dir, file := s.designPlace()

	// Nowhere to put a document — a fetched snapshot has no working tree to
	// design against — means no Design tab at all, which is what the client
	// keys off a nil here.
	if dir == "" {
		return nil
	}

	// Nothing open? Look again before saying so. A document can appear after
	// the server started — the agent was asked to start one, or you wrote it
	// yourself — and the branch match otherwise only runs at startup and on a
	// bind, so the Design tab stayed empty until a restart.
	if file == "" {
		dir, file = s.adoptDesign(dir)
	}

	files, err := docs.Tree(dir, file)
	if err != nil {
		slog.Debug("design tree", "err", err)
	}
	if files == nil {
		files = []render.FileEntry{}
	}
	if file == "" {
		// Somewhere to put one, but nothing open. The tab is where you start a
		// design, so it has to be there before there is one — and it carries
		// what already exists, since a document only opens by itself when it
		// matches the branch, and the one you want often does not.
		return &DesignState{Dir: dir, Files: files}
	}
	return &DesignState{
		Dir:      dir,
		File:     file,
		Files:    files,
		Worktree: s.proposedWorktree(),
		Version:  docs.Version(dir, file),
	}
}

// declineDoc remembers a document the user closed, so the poll does not open it
// again. Only the one they closed: a different document appearing afterwards —
// the agent was asked to start one — is still worth opening.
func (s *Server) declineDoc(file string) {
	if file == "" {
		return
	}
	s.mu.Lock()
	s.docDeclined = file
	s.mu.Unlock()
}

func (s *Server) declined(file string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// sameDoc rather than ==: the open document's name can reach us by more than
	// one route — the branch match, --doc, `ade create` — and an exact compare
	// meant a difference in case or a missing .md let the poll reopen the very
	// document that had just been closed.
	return sameDoc(file, s.docDeclined)
}

// designPlace is the design directory and the open document, read together so
// they cannot disagree.
func (s *Server) designPlace() (string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.docsDir, s.docFile
}

// handleMermaid serves the bundled mermaid build. It is deliberately outside the
// token guard, like the other static assets, and is only fetched the first time
// the Design tab is opened.
func (s *Server) handleMermaid(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if _, err := w.Write(render.MermaidJS); err != nil {
		slog.Debug("write mermaid", "err", err)
	}
}

// handleDesignAssets serves the markdown stylesheet and the copy/zoom behaviours
// that go with a rendered document, straight from the render package.
func (s *Server) handleDesignAssets(w http.ResponseWriter, r *http.Request) {
	var body, mime string
	switch r.PathValue("name") {
	case "markdown.css":
		body, mime = render.MarkdownCSS, "text/css; charset=utf-8"
	case "copy-buttons.js":
		body, mime = render.CopyJS, "application/javascript; charset=utf-8"
	case "zoom.js":
		body, mime = render.ZoomJS, "application/javascript; charset=utf-8"
	case "mermaid-init.js":
		body, mime = render.MermaidInit(render.ThemeDark, false), "application/javascript; charset=utf-8"
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	if _, err := w.Write([]byte(body)); err != nil {
		slog.Debug("write design asset", "err", err)
	}
}

// docImageTypes are what a document may show; the extension decides.
var docImageTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".svg": "image/svg+xml",
}

// maxDocImage is what one picture may be. A document shows diagrams, and
// anything larger is buffered per request for no one's benefit.
const maxDocImage = 16 << 20

// handleDocImage serves a picture beside a document, by base name only.
func (s *Server) handleDocImage(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(filepath.FromSlash(r.PathValue("name")))
	mime, ok := docImageTypes[strings.ToLower(filepath.Ext(name))]
	dir, _ := s.designPlace()
	if !ok || dir == "" || name == "." || name == string(filepath.Separator) {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(filepath.Join(dir, name)) // #nosec G304 -- base name only, under the docs directory
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = file.Close() }()
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() || info.Size() > maxDocImage {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	// An svg is served from ade's own origin, so it is both told not to load
	// anything and stopped from being sniffed into something else.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := io.Copy(w, file); err != nil {
		slog.Debug("write design image", "err", err)
	}
}

// draftFromIssue does the same from a GitHub issue, which ade reads itself
// through gh rather than leaving to whatever tools the agent has.
func (s *Server) draftFromIssue(ctx context.Context, number int) {
	file := s.currentDoc()
	if file == "" || number <= 0 {
		return
	}
	// A second `create --github N` on a document that already says something is
	// resuming that work, and redrafting it would throw the draft away.
	if body, err := docs.Read(s.docsDir, file); err == nil && hasProse(body) {
		return
	}
	// gh can stall for as long as the process lives, and this holds up the note
	// that would otherwise say why the document is still empty.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	issue, err := ghpr.FetchIssue(ctx, s.space().homeRoot, number)
	if err != nil {
		// Said aloud: the alternative is a blank document and no reason given.
		s.say(ChatMessage{Kind: chat.KindNote, Body: fmt.Sprintf("Could not read issue #%d: %s", number, err)})
		return
	}
	runCtx, ok := s.agent.begin(s.ctx, kindChat)
	if !ok {
		return
	}

	ref := fmt.Sprintf("Issue #%d", issue.Number)
	s.say(ChatMessage{Kind: chat.KindYou, Body: fmt.Sprintf("Draft the design from %s.", ref)})
	prompt := design.IssuePrompt(s.docPath(file), ref, issue.Title, issue.Body)
	s.spawn(func() { s.runTurn(runCtx, prompt, fmt.Sprintf("reading issue #%d", issue.Number)) })
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
}

// draftFromTicket fills a freshly created document from its ticket, so starting
// work on one opens on a draft rather than an empty template.
//
// It is best effort: the tools to read a ticket come from whatever MCP servers
// the agent has, and a repository without them should still get its document.
func (s *Server) draftFromTicket(ticket string) {
	file := s.currentDoc()
	if file == "" || ticket == "" {
		return
	}
	runCtx, ok := s.agent.begin(s.ctx, kindChat)
	if !ok {
		return
	}

	s.say(ChatMessage{Kind: chat.KindYou, Body: fmt.Sprintf("Draft the design from %s.", ticket)})
	s.spawn(func() {
		s.runTurn(runCtx, design.TicketPrompt(s.docPath(file), ticket), fmt.Sprintf("reading %s", ticket))
	})
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
}

// buildWait covers the whole hand-off: the clone on the far machine, the
// session starting, and the document landing in it before the build is asked for.
const buildWait = 5 * time.Minute

// buildOnMachine starts the build somewhere else: a session over there with
// this document as its brief, not this session's agent run on another host.
func (s *Server) buildOnMachine(ctx context.Context, machine, file, provider, model string) (url, path string, err error) {
	project := s.project()
	if project == "" {
		return "", "", fmt.Errorf("this checkout has no origin remote, so %s has nothing to clone", machine)
	}
	// Not docPath: that is repo-relative for the agent's prompt, and a server
	// started below the root would resolve it against the wrong directory.
	body, err := docs.Read(s.docsDir, file)
	if err != nil {
		return "", "", err
	}
	ask, err := json.Marshal(map[string]any{
		"machine": machine,
		"dir":     project,
		"agent":   provider,
		"model":   model,
		"fresh":   true,
		"brief":   map[string]any{"name": file, "body": body, "build": true},
	})
	if err != nil {
		return "", "", err
	}
	answer, askErr := askLauncher(ctx, http.MethodPost, "/api/open", string(ask), buildWait)
	var opened struct {
		URL  string `json:"url"`
		Path string `json:"path"`
	}
	_ = json.Unmarshal(answer, &opened)
	if askErr != nil {
		// A hand-off that failed part way still left a session over there.
		if opened.URL != "" {
			return "", "", fmt.Errorf("build on %s: %w — the session is open at %s", machine, askErr, opened.URL)
		}
		return "", "", fmt.Errorf("build on %s: %w", machine, askErr)
	}
	if opened.URL == "" {
		return "", "", fmt.Errorf("%s did not say where the build is", machine)
	}
	return opened.URL, opened.Path, nil
}
