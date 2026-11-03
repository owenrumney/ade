package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/owenrumney/ade/internal/agents"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/design"
	"github.com/owenrumney/ade/internal/docnotes"
	"github.com/owenrumney/ade/internal/docs"
	"github.com/owenrumney/ade/internal/review"
)

// noteStore is the open document's notes, swapped when another is opened. Keyed
// on the common git dir: a design belongs to the repository, not the worktree.
func (s *Server) noteStore() (*docnotes.Store, string, error) {
	file := s.currentDoc()
	if file == "" || s.docsDir == "" {
		return nil, "", errors.New("there is no design document open")
	}

	s.notesMu.Lock()
	defer s.notesMu.Unlock()
	if s.notes != nil && s.notesDoc == file {
		return s.notes, file, nil
	}
	store, err := docnotes.Open(s.commonGitDir, file)
	if err != nil {
		return nil, "", err
	}
	s.notes, s.notesDoc = store, file
	return store, file, nil
}

// notesNow reanchors against what the document says this moment, because the
// agent rewrites it underneath the page.
func (s *Server) notesNow() ([]docnotes.Note, error) {
	store, file, err := s.noteStore()
	if err != nil {
		return nil, err
	}
	body, err := docs.Read(s.docsDir, file)
	if err != nil {
		return nil, err
	}
	return store.Reanchor(body), nil
}

func (s *Server) handleNotes(w http.ResponseWriter, _ *http.Request) {
	notes, err := s.notesNow()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": notes})
}

// handleNoteCreate files a note on quoted text. Nth says which occurrence of
// the quote the page means, so the same sentence twice is still two places.
func (s *Server) handleNoteCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Quote string `json:"quote"`
		Nth   int    `json:"nth"`
		Body  string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}

	store, file, err := s.noteStore()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body, err := docs.Read(s.docsDir, file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	anchor, ok := docnotes.At(body, req.Quote, req.Nth)
	if !ok {
		writeError(w, http.StatusBadRequest, "that text is not in the document any more")
		return
	}

	note, err := store.Add(docnotes.Note{
		Anchor:   anchor,
		Comments: []docnotes.Comment{{Author: docnotes.AuthorYou, Body: req.Body}},
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.Broadcast("notes", nil)
	writeJSON(w, http.StatusOK, note)
}

func (s *Server) handleNoteReply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Body string `json:"body"`
	}
	if !decode(w, r, &req) {
		return
	}
	store, _, err := s.noteStore()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	note, err := store.Reply(r.PathValue("id"), docnotes.AuthorYou, req.Body)
	if err != nil {
		writeNoteError(w, err)
		return
	}
	s.hub.Broadcast("notes", nil)
	writeJSON(w, http.StatusOK, note)
}

func (s *Server) handleNoteResolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Resolved bool `json:"resolved"`
	}
	if !decode(w, r, &req) {
		return
	}
	store, _, err := s.noteStore()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	note, err := store.SetResolved(r.PathValue("id"), req.Resolved)
	if err != nil {
		writeNoteError(w, err)
		return
	}
	s.hub.Broadcast("notes", nil)
	writeJSON(w, http.StatusOK, note)
}

func (s *Server) handleNoteDelete(w http.ResponseWriter, r *http.Request) {
	store, _, err := s.noteStore()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := store.Delete(r.PathValue("id")); err != nil {
		writeNoteError(w, err)
		return
	}
	s.hub.Broadcast("notes", nil)
	w.WriteHeader(http.StatusNoContent)
}

// fileNotes anchors each reviewed point on the open document. One whose quote
// is not there comes back lost, to travel as text rather than vanish.
func (s *Server) fileNotes(found []review.Note, author string) (filed []string, lost []review.Note) {
	store, file, err := s.noteStore()
	if err != nil {
		return nil, found
	}
	body, err := docs.Read(s.docsDir, file)
	if err != nil {
		return nil, found
	}
	for _, point := range found {
		anchor, ok := docnotes.First(body, point.Quote)
		if !ok {
			lost = append(lost, point)
			continue
		}
		note, err := store.Add(docnotes.Note{
			Anchor:   anchor,
			Comments: []docnotes.Comment{{Author: author, Body: point.Note}},
		})
		if err != nil {
			lost = append(lost, point)
			continue
		}
		filed = append(filed, headline(note.Comments[0].Body))
	}
	return filed, lost
}

// headline is a note's first line, for the one-line summary in the chat.
func headline(body string) string {
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(body), "\n", 2)[0])
	if len(line) > 120 {
		return line[:120] + "…"
	}
	return line
}

// docReviewers is who reviews a design document: the named agents asked for,
// each of which must be one that reviews.
func (s *Server) handleDesignReview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Agents []string `json:"agents"`
		Agent  string   `json:"agent"`
	}
	if !decode(w, r, &req) {
		return
	}
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
	if strings.TrimSpace(body) == "" {
		writeError(w, http.StatusBadRequest, "there is nothing in "+file+" to review")
		return
	}

	names := reviewers(req.Agents, req.Agent)
	if len(names) == 0 {
		names = []string{defaultDocReviewer}
	}
	// User scope only, for the reason a diff review is: a repository agent
	// shadows a user one, and the branch under design can ship either.
	list := s.userAgentsList()
	calls := make([]sideCall, 0, len(names))
	mentions := make([]string, 0, len(names))
	for _, name := range names {
		agent, ok := agents.Find(list, name)
		if !ok {
			writeError(w, http.StatusBadRequest, "there is no agent called @"+name)
			return
		}
		// Not agent.Reviews: that says whether findings land on a diff, and
		// @architect sets it false while being exactly who should read a design.
		if !mayNote(agent) {
			writeError(w, http.StatusBadRequest, "@"+name+" can edit files, so it is not a reviewer of designs")
			return
		}
		calls = append(calls, sideCall{
			agent:  agent,
			prompt: design.DocReviewPrompt(name, s.docPath(file), body),
			onDoc:  true,
		})
		mentions = append(mentions, "@"+name)
	}

	s.say(ChatMessage{Kind: chat.KindYou, Body: strings.Join(mentions, " ") + " review " + file})
	if !s.startSides(calls) {
		writeError(w, http.StatusConflict, fmt.Sprintf("no room for another agent; %d at most. Wait for one, or stop them", maxSides))
		return
	}
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"running": true, "agents": names})
}

// defaultDocReviewer is who reviews a design when nobody is named: the agent
// whose whole job is designing one.
const defaultDocReviewer = "architect"

// handleDesignAccept folds accepted recommendations into the document. The
// rows come from the page: it drew the table, so it knows what was ticked.
func (s *Server) handleDesignAccept(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Rows []struct {
			Ref            string `json:"ref"`
			Question       string `json:"question"`
			Recommendation string `json:"recommendation"`
		} `json:"rows"`
		// File is the document the rows were drawn from, which is not always
		// the one this server has open.
		File string `json:"file"`
	}
	if !decode(w, r, &req) {
		return
	}
	file := strings.TrimSpace(req.File)
	if file == "" {
		file = s.currentDoc()
	}
	if file == "" {
		writeError(w, http.StatusBadRequest, "there is no design document open")
		return
	}
	if _, err := docs.Resolve(s.docsDir, file); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	taken := make([]design.Accepted, 0, len(req.Rows))
	for _, row := range req.Rows {
		if strings.TrimSpace(row.Question) == "" || strings.TrimSpace(row.Recommendation) == "" {
			continue
		}
		taken = append(taken, design.Accepted{Ref: row.Ref, Question: row.Question, Recommendation: row.Recommendation})
	}
	if len(taken) == 0 {
		writeError(w, http.StatusBadRequest, "no recommendations were accepted")
		return
	}

	runCtx, started := s.agent.begin(s.ctx, kindFold)
	if !started {
		writeError(w, http.StatusConflict, "the agent is busy; wait for it or stop it first")
		return
	}
	s.say(ChatMessage{Kind: chat.KindYou, Body: fmt.Sprintf("I accepted %d recommendation(s). Fold them into %s.", len(taken), file)})
	s.spawn(func() {
		s.runTurn(runCtx, design.AcceptPrompt(s.docPath(file), taken), "updating "+file)
	})
	s.hub.Broadcast("agent", nil)
	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true})
}

func writeNoteError(w http.ResponseWriter, err error) {
	if errors.Is(err, docnotes.ErrNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeError(w, http.StatusBadRequest, err.Error())
}
