// Package busstate is ade's own view of agentbus: the rows a list draws and
// the pure reads that turn a wire frame into one.
package busstate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/owenrumney/ade/internal/agentbus"
)

// Offer is one request from somebody else, waiting on you. Kind is "ask"
// or "review"; the fields of the other kind are empty.
type Offer struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	From    string `json:"from"`
	Node    string `json:"node,omitempty"`
	Harness string `json:"harness,omitempty"`
	Agent   string `json:"agent,omitempty"`
	Model   string `json:"model,omitempty"`

	Question string   `json:"question,omitempty"`
	Options  []string `json:"options,omitempty"`
	AnswerBy string   `json:"answerBy,omitempty"`

	PR     string `json:"pr,omitempty"`
	Repo   string `json:"repo,omitempty"`
	Number int    `json:"number,omitempty"`
	Note   string `json:"note,omitempty"`

	CreatedAt string `json:"createdAt,omitempty"`
	Claimed   bool   `json:"claimed,omitempty"`

	// To is who the sender addressed it to: a user, a group, "anyone" or "*".
	// Without it a request aimed at you reads the same as one offered around.
	To string `json:"to,omitempty"`
}

// Reply is an answer to something asked from here, pushed by the server
// rather than waited for. Kind is "answer" or "review".
type Reply struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	From     string `json:"from"`
	Question string `json:"question,omitempty"`
	PR       string `json:"pr,omitempty"`
	Text     string `json:"text,omitempty"`
	Option   string `json:"option,omitempty"`
	Verdict  string `json:"verdict,omitempty"`
	ByAgent  bool   `json:"byAgent,omitempty"`
	At       string `json:"at,omitempty"`

	// Dir is the checkout whose session asked, so an answer arriving at the
	// launcher can be handed back to the one waiting on it.
	Dir string `json:"dir,omitempty"`

	// ForAgent is whether a run asked rather than you. An agent reads its
	// answer in the conversation; yours is a notification and nothing else.
	ForAgent bool `json:"forAgent,omitempty"`
}

// Member is somebody else's harness, as the bus last described it.
type Member struct {
	User     string `json:"user"`
	Node     string `json:"node,omitempty"`
	Instance string `json:"instance"`
	Harness  string `json:"harness,omitempty"`
	Label    string `json:"label,omitempty"`
	Presence string `json:"presence"`
	Status   string `json:"status,omitempty"`
	Agents   int    `json:"agents,omitempty"`
	TakesAsk bool   `json:"takesAsk,omitempty"`
	TakesPR  bool   `json:"takesPr,omitempty"`
	You      bool   `json:"you,omitempty"`
}

// Sent is a request this machine put to somebody and has no answer to yet.
type Sent struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	To       string `json:"to,omitempty"`
	Question string `json:"question,omitempty"`
	PR       string `json:"pr,omitempty"`
	At       string `json:"at,omitempty"`

	// ForAgent is whether a run asked rather than you.
	ForAgent bool `json:"forAgent,omitempty"`
}

// State is this machine's standing on the bus, for the page.
type State struct {
	Online bool `json:"online"`
	// Why there is no connection, when there is none.
	Why      string   `json:"why,omitempty"`
	User     string   `json:"user,omitempty"`
	Instance string   `json:"instance,omitempty"`
	Offers   []Offer  `json:"offers,omitempty"`
	Replies  []Reply  `json:"replies,omitempty"`
	Sent     []Sent   `json:"sent,omitempty"`
	Team     []Member `json:"team,omitempty"`
}

func MemberOf(row agentbus.Online, me string) Member {
	member := Member{
		User: row.User, Node: row.Node, Instance: row.Instance.ID,
		Harness: row.Instance.Harness, Label: row.Instance.Label,
		Presence: row.Presence, Status: row.Status, Agents: len(row.Sessions),
		You: row.Instance.ID == me,
	}
	if row.Accepts != nil {
		member.TakesAsk = row.Accepts.Ask
		member.TakesPR = row.Accepts.Review != nil
	}
	return member
}

// ReplyOf reads the newest reply out of a state push. The server sends
// these only to whoever sent the request, so anything here is an answer to us.
func ReplyOf(f agentbus.Frame) *Reply {
	if len(f.Replies) == 0 || f.Message == nil {
		return nil
	}
	last := f.Replies[len(f.Replies)-1]
	reply := &Reply{ID: f.ID, At: last.CreatedAt}
	if last.From != nil {
		reply.From = last.From.User
	}
	switch f.Message.Type {
	case agentbus.TypeAsk:
		var asked agentbus.AskBody
		_ = json.Unmarshal(f.Message.Body, &asked)
		reply.Question = asked.Question
	case agentbus.TypeReviewRequest:
		var asked agentbus.ReviewRequestBody
		_ = json.Unmarshal(f.Message.Body, &asked)
		reply.PR = asked.PR
	}
	switch last.Type {
	case agentbus.TypeAnswer:
		var body agentbus.AnswerBody
		if json.Unmarshal(last.Body, &body) != nil {
			return nil
		}
		reply.Kind, reply.Text, reply.Option = "answer", body.Text, body.Option
		reply.ByAgent = body.By == agentbus.AnsweredByAgent
	case agentbus.TypeReviewResult:
		var body agentbus.ReviewResultBody
		if json.Unmarshal(last.Body, &body) != nil {
			return nil
		}
		reply.Kind, reply.Verdict, reply.Text = "review", body.State, body.Summary
	default:
		return nil
	}
	return reply
}

// OfferOf reads one offered frame, or nothing when its body will not parse.
func OfferOf(f agentbus.Frame) *Offer {
	offer := &Offer{ID: f.ID, CreatedAt: f.CreatedAt}
	if f.To != nil {
		offer.To = f.To.String()
	}
	if f.From != nil {
		offer.From, offer.Node = f.From.User, f.From.Node
		if f.From.Instance != nil {
			offer.Harness = f.From.Instance.Harness
		}
		if f.From.Session != nil {
			offer.Agent, offer.Model = f.From.Session.Agent, f.From.Session.Model
		}
	}
	switch f.Type {
	case agentbus.TypeAsk:
		var body agentbus.AskBody
		if json.Unmarshal(f.Body, &body) != nil || strings.TrimSpace(body.Question) == "" {
			return nil
		}
		offer.Kind, offer.Question, offer.Options, offer.AnswerBy = "ask", body.Question, body.Options, body.AnswerBy
	case agentbus.TypeReviewRequest:
		var body agentbus.ReviewRequestBody
		if json.Unmarshal(f.Body, &body) != nil {
			return nil
		}
		url, repo, err := agentbus.ParsePR(body.PR)
		if err != nil {
			return nil
		}
		offer.Kind, offer.PR, offer.Repo, offer.Note = "review", url, repo, body.Note
		offer.Number = PRNumberOf(url)
	default:
		return nil
	}
	return offer
}

// RankPresence is who is most likely to answer, first.
func RankPresence(p string) int {
	switch p {
	case agentbus.PresenceActive:
		return 0
	case agentbus.PresenceIdle:
		return 1
	}
	return 2
}

// PresenceFor is the rule, apart from the server so it can be read and tested
// on its own. Override is what you set by hand, and it wins.
func PresenceFor(here bool, override string) string {
	switch override {
	case agentbus.PresenceActive, agentbus.PresenceIdle, agentbus.PresenceDND:
		return override
	}
	if here {
		return agentbus.PresenceActive
	}
	return agentbus.PresenceIdle
}

func ReviewKey(repo string, number int) string { return fmt.Sprintf("%s#%d", repo, number) }

// PRNumberOf reads the number off a canonical pull request URL.
func PRNumberOf(url string) int {
	at := strings.LastIndex(url, "/")
	if at < 0 {
		return 0
	}
	n := 0
	for _, r := range url[at+1:] {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
