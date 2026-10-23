package launcher

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/todo"
)

// todoStore opens the store a request named: the machine's, or a checkout's.
// A dir that is no checkout is refused — the file lands in its git dir.
func (s *Server) todoStore(ctx context.Context, dir string) (*todo.Store, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return todo.Global()
	}
	gitDir, err := gitdiff.GitDir(ctx, dir)
	if err != nil {
		return nil, err
	}
	return todo.Repo(gitDir, filepath.Base(dir))
}

func (s *Server) todoScope(ctx context.Context, dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", nil
	}
	if _, err := gitdiff.GitDir(ctx, dir); err != nil {
		return "", err
	}
	return filepath.Base(dir), nil
}

// handleTodos answers the machine's list, and the checkout's too when one is
// named. Two stores, so a repository's todos travel with the checkout.
func (s *Server) handleTodos(w http.ResponseWriter, r *http.Request) {
	if s.todos != nil && s.todos.Enabled() {
		items, err := s.todos.List(r.Context(), "")
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		if dir := r.URL.Query().Get("dir"); strings.TrimSpace(dir) != "" {
			scope, err := s.todoScope(r.Context(), dir)
			if err != nil {
				writeError(w, http.StatusBadRequest, "that is not a checkout ade can keep todos for")
				return
			}
			repo, err := s.todos.List(r.Context(), scope)
			if err != nil {
				writeError(w, http.StatusBadGateway, err.Error())
				return
			}
			for i := range repo {
				repo[i].Repo = scope
			}
			items = append(items, repo...)
		}
		todo.Sort(items)
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	items := []todo.Item{}
	global, err := todo.Global()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items = append(items, global.List()...)

	if dir := r.URL.Query().Get("dir"); strings.TrimSpace(dir) != "" {
		store, err := s.todoStore(r.Context(), dir)
		if err != nil {
			writeError(w, http.StatusBadRequest, "that is not a checkout ade can keep todos for")
			return
		}
		items = append(items, store.List()...)
	}
	todo.Sort(items)
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) handleAddTodo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir  string `json:"dir"`
		Text string `json:"text"`
		Note string `json:"note"`
		Due  string `json:"due"`
		// From and Session say who filed it. A row names its source, which is
		// what makes an agent's todo landing in the list safe to allow.
		From    string `json:"from"`
		Session string `json:"session"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if s.todos != nil && s.todos.Enabled() {
		scope, err := s.todoScope(r.Context(), req.Dir)
		if err != nil {
			writeError(w, http.StatusBadRequest, "that is not a checkout ade can keep todos for")
			return
		}
		due, err := parseDue(req.Due)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		item, err := s.todos.Add(r.Context(), scope, todo.Item{Text: req.Text, Note: req.Note, Due: due, From: req.From, Session: req.Session})
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		s.hub.Broadcast("todos", nil)
		writeJSON(w, http.StatusOK, item)
		return
	}
	store, err := s.todoStore(r.Context(), req.Dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, "that is not a checkout ade can keep todos for")
		return
	}
	due, err := parseDue(req.Due)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := store.Add(todo.Item{
		Text: req.Text, Note: req.Note, Due: due,
		From: req.From, Session: req.Session,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.Broadcast("todos", nil)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleEditTodo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir     string  `json:"dir"`
		Text    *string `json:"text"`
		Note    *string `json:"note"`
		Done    *bool   `json:"done"`
		Started *string `json:"started"`
		// Due is a time, or "" to take the date off again.
		Due *string `json:"due"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if s.todos != nil && s.todos.Enabled() {
		scope, err := s.todoScope(r.Context(), req.Dir)
		if err != nil {
			writeError(w, http.StatusBadRequest, "that is not a checkout ade can keep todos for")
			return
		}
		patch, err := todoPatch(req.Text, req.Note, req.Done, req.Started, req.Due)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		item, err := s.todos.Update(r.Context(), scope, r.PathValue("id"), patch)
		if err != nil {
			writeError(w, todoStatus(err), err.Error())
			return
		}
		s.hub.Broadcast("todos", nil)
		writeJSON(w, http.StatusOK, item)
		return
	}
	store, err := s.todoStore(r.Context(), req.Dir)
	if err != nil {
		writeError(w, http.StatusBadRequest, "that is not a checkout ade can keep todos for")
		return
	}
	patch, err := todoPatch(req.Text, req.Note, req.Done, req.Started, req.Due)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := store.Update(r.PathValue("id"), patch)
	if err != nil {
		writeError(w, todoStatus(err), err.Error())
		return
	}
	s.hub.Broadcast("todos", nil)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleDeleteTodo(w http.ResponseWriter, r *http.Request) {
	if s.todos != nil && s.todos.Enabled() {
		scope, err := s.todoScope(r.Context(), r.URL.Query().Get("dir"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "that is not a checkout ade can keep todos for")
			return
		}
		if err := s.todos.Delete(r.Context(), scope, r.PathValue("id")); err != nil {
			writeError(w, todoStatus(err), err.Error())
			return
		}
		s.hub.Broadcast("todos", nil)
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
		return
	}
	store, err := s.todoStore(r.Context(), r.URL.Query().Get("dir"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "that is not a checkout ade can keep todos for")
		return
	}
	if err := store.Delete(r.PathValue("id")); err != nil {
		writeError(w, todoStatus(err), err.Error())
		return
	}
	s.hub.Broadcast("todos", nil)
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func todoPatch(text, note *string, done *bool, started *string, raw *string) (todo.Patch, error) {
	patch := todo.Patch{Text: text, Note: note, Done: done, Started: started}
	if raw == nil {
		return patch, nil
	}
	due, err := parseDue(*raw)
	if err != nil {
		return todo.Patch{}, err
	}
	if due == nil {
		due = new(time.Time)
	}
	patch.Due = due
	return patch, nil
}

func todoStatus(err error) int {
	if errors.Is(err, todo.ErrNotFound) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

// parseDue reads a due date off the wire. Empty is no date rather than an
// error: a capture that never had one is the normal case.
func parseDue(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	when, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, errBadDue
	}
	return &when, nil
}

// errBadDue is what an unreadable date says back. One message: the page sends
// RFC3339 or nothing at all.
var errBadDue = errors.New("a due date has to be a time, like 2026-10-09T09:00:00Z")
