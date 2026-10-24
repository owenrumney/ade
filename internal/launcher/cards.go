package launcher

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/concierge"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
)

// What a card would do if it were pressed.
const (
	CardSay   = "say"
	CardStart = "start"
)

// maxCards caps what can be waiting at once. The desk proposes; a pile of
// proposals nobody pressed is a pile to read, which is the thing to avoid.
const maxCards = 8

// cardLife is how long an unpressed card stands. A proposal about what was
// running an hour ago is not a proposal any more.
const cardLife = time.Hour

// maxDrafts is how many times one card may be redrafted. An edit asks the desk
// to try again; a card that can be edited forever is a conversation in a box.
const maxDrafts = 3

// redraftWait bounds the rewrite. It is one cheap turn with no tools, and the
// person is watching the card while it runs.
const redraftWait = 30 * time.Second

// Card is something the desk proposes and only the person may authorise. The
// text is what is sent, exactly as shown: what is pressed is what was read.
type Card struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`

	// Session is which conversation a say goes to; Dir is the checkout a start
	// happens in. Name is what to call either in the card.
	Session string `json:"session,omitempty"`
	Dir     string `json:"dir,omitempty"`
	Name    string `json:"name,omitempty"`

	// Design starts the work from a document rather than a prompt.
	Design bool `json:"design,omitempty"`

	Text   string    `json:"text"`
	At     time.Time `json:"at"`
	Drafts int       `json:"drafts"`
}

// draw puts a card up and tells every open window. It does not act.
func (s *Server) draw(card Card) (Card, error) {
	card.Text = strings.TrimSpace(card.Text)
	if card.Text == "" {
		return Card{}, errors.New("a card needs something to say")
	}
	s.cardMu.Lock()
	if s.cards == nil {
		s.cards = map[string]Card{}
	}
	s.sweepCards(time.Now())
	if len(s.cards) >= maxCards {
		s.cardMu.Unlock()
		return Card{}, fmt.Errorf("%d cards are already waiting to be pressed", len(s.cards))
	}
	card.ID = newCardID()
	card.At = time.Now()
	s.cards[card.ID] = card
	s.cardMu.Unlock()

	s.hub.Broadcast("cards", nil)
	return card, nil
}

// sweepCards drops what has stood too long. Called under the lock.
func (s *Server) sweepCards(now time.Time) {
	for id, card := range s.cards {
		if now.Sub(card.At) > cardLife {
			delete(s.cards, id)
		}
	}
}

func (s *Server) cardList() []Card {
	s.cardMu.Lock()
	defer s.cardMu.Unlock()
	s.sweepCards(time.Now())
	out := make([]Card, 0, len(s.cards))
	for _, card := range s.cards {
		out = append(out, card)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].At.Before(out[b].At) })
	return out
}

func (s *Server) takeCard(id string) (Card, bool) {
	s.cardMu.Lock()
	defer s.cardMu.Unlock()
	card, ok := s.cards[id]
	if ok {
		delete(s.cards, id)
	}
	return card, ok
}

func (s *Server) handleCards(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"cards": s.cardList()})
}

// handleCard is the press. An edit is an instruction, not replacement text:
// the card goes back to the desk to be written again. See the design doc.
func (s *Server) handleCard(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Action string `json:"action"`
		Said   string `json:"said"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	switch req.Action {
	case "cancel":
		if _, ok := s.takeCard(id); !ok {
			writeError(w, http.StatusNotFound, "that card is no longer waiting")
			return
		}
		s.hub.Broadcast("cards", nil)
		writeJSON(w, http.StatusOK, map[string]bool{"cancelled": true})
	case "edit":
		card, err := s.redraftCard(r.Context(), id, req.Said)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.hub.Broadcast("cards", nil)
		writeJSON(w, http.StatusOK, card)
	case "start":
		card, ok := s.takeCard(id)
		if !ok {
			writeError(w, http.StatusNotFound, "that card is no longer waiting")
			return
		}
		s.hub.Broadcast("cards", nil)
		out, err := s.pressCard(s.ctx, card)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, out)
	default:
		writeError(w, http.StatusBadRequest, "a card is started, cancelled or edited")
	}
}

// pressCard does the thing. By here the person has read the text and pressed,
// which is the only authority either of these actions has.
func (s *Server) pressCard(ctx context.Context, card Card) (map[string]any, error) {
	switch card.Kind {
	case CardSay:
		url, err := s.sessionURL(ctx, card.Session)
		if err != nil {
			return nil, err
		}
		// /api/chat queues behind a running turn rather than refusing, which
		// is why the desk never has to interrupt anything.
		body := fmt.Sprintf(`{"message":%q}`, card.Text)
		if err := sendFirst(ctx, url, body); err != nil {
			return nil, fmt.Errorf("carry that to the session: %w", err)
		}
		return map[string]any{"said": true, "session": card.Session}, nil
	case CardStart:
		url, err := s.openOn(ctx, card.Dir, "", "", sessions.None)
		if err != nil {
			return nil, err
		}
		if card.Design {
			sendBriefMessage(ctx, url, &Brief{Name: card.Name, Body: card.Text})
			return map[string]any{"started": true, "dir": card.Dir, "design": true}, nil
		}
		sendFirstMessage(ctx, url, card.Text, nil)
		return map[string]any{"started": true, "dir": card.Dir}, nil
	}
	return nil, fmt.Errorf("a card cannot be a %q", card.Kind)
}

// sendBriefMessage writes the document into a new session. A failure is
// logged, not returned: the session is open either way.
func sendBriefMessage(ctx context.Context, url string, brief *Brief) {
	if err := sendBrief(ctx, url, brief); err != nil {
		slog.Warn("could not hand the session its brief", "err", err)
	}
}

// sessionURL is where one of this machine's conversations is served.
func (s *Server) sessionURL(ctx context.Context, id string) (string, error) {
	for _, row := range s.localSessions(ctx) {
		if row.ID != id {
			continue
		}
		rec, ok := session.Read(session.Path(sessions.GitDir(row.Dir)))
		if !ok || !session.Running(rec.PID) {
			return "", fmt.Errorf("the session in %s is no longer running", row.Name)
		}
		return rec.URL, nil
	}
	return "", fmt.Errorf("%q is not a conversation running on this machine", id)
}

// redraftCard asks the desk to write the card again. One redraft per edit:
// a card that rewrites itself while you read it is one you stop trusting.
func (s *Server) redraftCard(ctx context.Context, id, said string) (Card, error) {
	said = strings.TrimSpace(said)
	if said == "" {
		return Card{}, errors.New("an edit has to say what to change")
	}
	s.cardMu.Lock()
	card, ok := s.cards[id]
	s.cardMu.Unlock()
	if !ok {
		return Card{}, errors.New("that card is no longer waiting")
	}
	if card.Drafts >= maxDrafts {
		return Card{}, fmt.Errorf("that card has been redrafted %d times; start it or cancel it", card.Drafts)
	}

	text, err := s.redraft(ctx, card, said)
	if err != nil {
		return Card{}, err
	}
	s.cardMu.Lock()
	defer s.cardMu.Unlock()
	held, ok := s.cards[id]
	if !ok {
		return Card{}, errors.New("that card is no longer waiting")
	}
	held.Text, held.Drafts = text, held.Drafts+1
	s.cards[id] = held
	return held, nil
}

// redrafter is the cheap turn that rewrites a card. Tests stand one in.
var redrafter = func(ctx context.Context, prompt string) (string, error) {
	ask, cancel := context.WithTimeout(ctx, redraftWait)
	defer cancel()
	turn, err := bridge.OneShot(ask, bridge.OneShotOpts{
		Model:                concierge.Model,
		Prompt:               prompt,
		Tools:                []string{},
		AllowedTools:         []string{},
		NoMCP:                true,
		NoSessionPersistence: true,
	})
	if turn == nil {
		return "", err
	}
	return turn.Text, err
}

func (s *Server) redraft(ctx context.Context, card Card, said string) (string, error) {
	text, err := redrafter(ctx, redraftPrompt(card, said))
	if err != nil {
		return "", fmt.Errorf("the desk could not rewrite that: %w", err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("the desk rewrote that as nothing")
	}
	return text, nil
}

func redraftPrompt(card Card, said string) string {
	what := "the first turn of a new session in " + card.Name
	if card.Kind == CardSay {
		what = "a message to queue for the session in " + card.Name
	}
	return "You wrote " + what + ":\n\n" + card.Text +
		"\n\nThe person corrected it: " + said +
		"\n\nWrite it again, following the correction. Answer with the new text and nothing else: " +
		"no preamble, no quotes around it, no explanation."
}

func newCardID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("c%d", time.Now().UnixNano())
	}
	return "c" + hex.EncodeToString(b[:])
}
