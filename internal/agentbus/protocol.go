// Package agentbus speaks the agentbus wire protocol: JSON frames over one
// WebSocket, so ade can take questions and review requests from the team.
package agentbus

import (
	"encoding/json"
	"fmt"
)

// Frame is every message on the wire. Which fields are set depends on Type.
type Frame struct {
	Type string `json:"type"`
	Ref  string `json:"ref,omitempty"`

	Instance  *Instance `json:"instance,omitempty"`
	Session   *Session  `json:"session,omitempty"`
	Accepts   *Accepts  `json:"accepts,omitempty"`
	Presence  string    `json:"presence,omitempty"`
	Status    *string   `json:"status,omitempty"`
	PID       int       `json:"pid,omitempty"`
	Ancestors []int     `json:"ancestors,omitempty"`

	ID        string          `json:"id,omitempty"`
	ReplyTo   string          `json:"reply_to,omitempty"`
	To        *Target         `json:"to,omitempty"`
	From      *From           `json:"from,omitempty"`
	TTL       string          `json:"ttl,omitempty"`
	Body      json.RawMessage `json:"body,omitempty"`
	CreatedAt string          `json:"created_at,omitempty"`

	Filter    *WhoFilter `json:"filter,omitempty"`
	Instances []Online   `json:"instances,omitempty"`

	State   string  `json:"state,omitempty"`
	Message *Frame  `json:"message,omitempty"`
	Replies []Frame `json:"replies,omitempty"`

	You           *From  `json:"you,omitempty"`
	ServerVersion string `json:"server_version,omitempty"`

	Code  string `json:"code,omitempty"`
	Error string `json:"error,omitempty"`

	Online *bool `json:"online,omitempty"`
}

// Instance is one running harness: one ade server, one checkout.
type Instance struct {
	ID      string `json:"id"`
	Harness string `json:"harness,omitempty"`
	Version string `json:"version,omitempty"`
	Label   string `json:"label,omitempty"`
}

// Session is one agent conversation inside an instance.
type Session struct {
	ID           string `json:"id"`
	Agent        string `json:"agent,omitempty"`
	AgentVersion string `json:"agent_version,omitempty"`
	Model        string `json:"model,omitempty"`
}

// From identifies the sender. User and Node are set by the server, from the
// tailnet, and a client cannot forge them.
type From struct {
	User     string    `json:"user"`
	Node     string    `json:"node"`
	Instance *Instance `json:"instance,omitempty"`
	Session  *Session  `json:"session,omitempty"`
}

// Accepts says which requests this instance will be offered.
type Accepts struct {
	Ask    bool           `json:"ask,omitempty"`
	Review *ReviewAccepts `json:"review,omitempty"`
}

// ReviewAccepts limits reviews to repos matching a glob. No repos means all.
type ReviewAccepts struct {
	Repos []string `json:"repos,omitempty"`
}

// Target is who a request is for. Anyone and All are the two string forms.
type Target struct {
	User     string `json:"user,omitempty"`
	Instance string `json:"instance,omitempty"`
	Group    string `json:"group,omitempty"`
	Anyone   bool   `json:"-"`
	All      bool   `json:"-"`
}

type WhoFilter struct {
	User     string `json:"user,omitempty"`
	Accepts  string `json:"accepts,omitempty"`
	Presence string `json:"presence,omitempty"`
}

// Online is one row of a who result.
type Online struct {
	User      string    `json:"user"`
	Node      string    `json:"node"`
	Instance  Instance  `json:"instance"`
	Accepts   *Accepts  `json:"accepts,omitempty"`
	Presence  string    `json:"presence"`
	Status    string    `json:"status,omitempty"`
	Sessions  []Session `json:"sessions,omitempty"`
	Connected bool      `json:"connected"`
}

type AskBody struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
	// AnswerBy is when the asker stops waiting, RFC 3339.
	AnswerBy string `json:"answer_by,omitempty"`
}

type AnswerBody struct {
	Text   string `json:"text,omitempty"`
	Option string `json:"option,omitempty"`
	By     string `json:"by,omitempty"`
}

type ReviewRequestBody struct {
	PR   string `json:"pr"`
	Note string `json:"note,omitempty"`
}

type ReviewResultBody struct {
	State   string `json:"state"`
	Summary string `json:"summary,omitempty"`
	URL     string `json:"url,omitempty"`
}

type CancelBody struct {
	Reason string `json:"reason,omitempty"`
}

// Frame types sent by clients.
const (
	TypeHello         = "hello"
	TypeStatus        = "status"
	TypeWho           = "who"
	TypeGet           = "get"
	TypeAsk           = "ask"
	TypeAnswer        = "answer"
	TypeReviewRequest = "review.request"
	TypeReviewClaim   = "review.claim"
	TypeReviewResult  = "review.result"
	TypeCancel        = "cancel"
)

// Frame types sent by the server.
const (
	TypeWelcome   = "welcome"
	TypeAck       = "ack"
	TypeError     = "error"
	TypePresence  = "presence"
	TypeWhoResult = "who.result"
	TypeState     = "state"
)

const (
	PresenceActive = "active"
	PresenceIdle   = "idle"
	PresenceDND    = "dnd"
)

const (
	ReviewApproved         = "approved"
	ReviewChangesRequested = "changes_requested"
	ReviewCommented        = "commented"
)

const (
	AnsweredByHuman = "human"
	AnsweredByAgent = "agent"
)

// Error codes.
const (
	ErrBadRequest = "bad_request"
	ErrNotFound   = "not_found"
	ErrForbidden  = "forbidden"
	ErrConflict   = "conflict"
	ErrNoHello    = "no_hello"
)

func (t Target) MarshalJSON() ([]byte, error) {
	switch {
	case t.All:
		return []byte(`"*"`), nil
	case t.Anyone:
		return []byte(`"anyone"`), nil
	}
	type plain Target
	return json.Marshal(plain(t))
}

func (t *Target) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		switch s {
		case "anyone", "":
			*t = Target{Anyone: true}
		case "*":
			*t = Target{All: true}
		default:
			return fmt.Errorf("target: unknown value %q", s)
		}
		return nil
	}
	type plain Target
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	n := 0
	for _, v := range []string{p.User, p.Instance, p.Group} {
		if v != "" {
			n++
		}
	}
	if n != 1 {
		return fmt.Errorf("target: set exactly one of user, instance, group")
	}
	*t = Target(p)
	return nil
}

func (t Target) String() string {
	switch {
	case t.All:
		return "*"
	case t.Anyone:
		return "anyone"
	case t.User != "":
		return t.User
	case t.Instance != "":
		return t.Instance
	}
	return t.Group
}
