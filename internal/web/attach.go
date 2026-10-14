package web

import (
	"net/http"
	"strconv"
	"strings"
)

// maxAttachText bounds what an editor may put on the next message. A selection
// is context, not a file: anything larger belongs in the repository.
const maxAttachText = 16 * 1024

// maxAttachFrom bounds the label. It is a file and line, and it is drawn in a
// chip: a longer one is a mistake, so it is cut rather than refused.
const maxAttachFrom = 512

// Attachment is highlighted code waiting to go on the next message. It is held
// here rather than in the page so an editor can put it there.
type Attachment struct {
	// From is what the page shows above the text: a file and line, usually.
	From string `json:"from"`
	Text string `json:"text"`

	// Seq names this attachment. A client clears the one it took by number,
	// so a selection made while it was reading is not wiped by its delete.
	Seq int64 `json:"seq"`
}

// currentAttachment is what the page should show in its composer, if anything.
func (s *Server) currentAttachment() *Attachment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.attachment
}

// handleAttach puts a selection on the next message. Replacing, not appending:
// two selections glued together are neither of the things that were selected.
func (s *Server) handleAttach(w http.ResponseWriter, r *http.Request) {
	var req Attachment
	if !decode(w, r, &req) {
		return
	}
	req.From = strings.TrimSpace(req.From)
	if len(req.From) > maxAttachFrom {
		req.From = req.From[:maxAttachFrom]
	}
	req.Text = strings.TrimRight(req.Text, "\n")
	if strings.TrimSpace(req.Text) == "" {
		writeError(w, http.StatusBadRequest, "nothing was selected")
		return
	}
	if len(req.Text) > maxAttachText {
		writeError(w, http.StatusRequestEntityTooLarge, "that selection is too long to attach")
		return
	}

	s.mu.Lock()
	s.attachSeq++
	req.Seq = s.attachSeq
	s.attachment = &req
	s.mu.Unlock()

	s.hub.Broadcast("state", nil)
	writeJSON(w, http.StatusOK, req)
}

// handleUnattach clears the attachment a client took. By number: every page
// competes for the one, and a blind clear loses a newer selection.
func (s *Server) handleUnattach(w http.ResponseWriter, r *http.Request) {
	seq, err := strconv.ParseInt(r.URL.Query().Get("seq"), 10, 64)
	if err != nil {
		seq = 0
	}

	s.mu.Lock()
	cleared := s.attachment != nil && (seq == 0 || s.attachment.Seq == seq)
	if cleared {
		s.attachment = nil
	}
	s.mu.Unlock()

	// The others are still showing what has now gone.
	if cleared {
		s.hub.Broadcast("state", nil)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"cleared": cleared})
}
