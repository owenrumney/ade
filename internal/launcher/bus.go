package launcher

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/agentbus"
	"github.com/owenrumney/ade/internal/busstate"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/statedir"
)

// busPulse is how often presence and status are pushed.
const busPulse = 10 * time.Second

// busTTL is how long ade's own requests stay open.
const busTTL = 4 * time.Hour

// focusGrace is how long an unfocused editor still counts as you being here.
// The launcher re-sends on every pulse, so this only has to outlast a blink.
const focusGrace = 90 * time.Second

// askedBy is where a request came from. Agent is a run asking, which reads its
// answer in the conversation; otherwise you asked, and a notification is all.
type askedBy struct {
	Dir   string
	Agent bool

	// What it was, so the page can show what is still unanswered.
	Kind     string
	To       string
	Question string
	PR       string
	At       time.Time
}

// checkoutState is what one checkout's ade last said about itself. The
// launcher holds the connection, so presence is the sum of these.
type checkoutState struct {
	Label      string
	Focused    bool
	Asking     int
	Running    bool
	Queued     int
	QueueSince time.Time
	At         time.Time
}

type machineOperations struct {
	Live      int
	Queued    int
	QueueWait time.Duration
	Dirs      []string
}

// busnode is the machine's one connection to agentbus, and what it has been
// offered. One per launcher, which is what makes a person one row.
type busnode struct {
	mu     sync.Mutex
	client *agentbus.Client
	you    agentbus.From
	offers map[string]*busstate.Offer
	// dismissed is the offers you cleared, so a replay does not bring one back.
	dismissed map[string]bool

	// claimed maps repo#number to the review request this machine claimed, so
	// a verdict from any checkout reports back on the same connection.
	claimed map[string]string

	// replies are answers to what was asked from here, keyed by request id.
	replies map[string]*busstate.Reply

	// asked maps a request id to who sent it and from where, so an answer goes
	// back to the one waiting in the form that one reads.
	asked map[string]askedBy

	// checkouts is what each ade on this machine last reported, keyed by dir.
	checkouts map[string]checkoutState

	sentPresence string
	sentStatus   string

	// override is the presence you set by hand, empty to follow the window.
	// Per launcher and not saved: it is a mood, not a setting.
	override string

	team map[string]busstate.Member
}

func newBusnode() *busnode {
	return &busnode{
		offers:    map[string]*busstate.Offer{},
		dismissed: map[string]bool{},
		claimed:   map[string]string{},
		replies:   map[string]*busstate.Reply{},
		asked:     map[string]askedBy{},
		checkouts: map[string]checkoutState{},
		team:      map[string]busstate.Member{},
	}
}

// watchBus joins the bus and keeps the connection up until ctx ends. Quiet
// when there is no bus to join: not every machine is on the tailnet.
func (s *Server) watchBus(ctx context.Context) {
	cfg, err := settings.Load()
	if err != nil {
		slog.Debug("read settings for agentbus", "err", err)
		return
	}
	if cfg.Agentbus.Off {
		return
	}
	url := cfg.Agentbus.URL
	if url == "" {
		if url, err = agentbus.ResolveURL(ctx); err != nil {
			slog.Debug("no agentbus to join", "err", err)
			return
		}
	}

	opts := agentbus.Options{
		URL: url,
		Instance: agentbus.Instance{
			ID:      agentbus.InstanceID(statedir.Home(s.home)),
			Harness: "ade",
			Version: s.version,
			Label:   s.busLabel(),
		},
		Accepts:  busAccepts(cfg.Agentbus),
		Presence: s.busPresence(),
		Status:   s.busStatus(),
		OnFrame:  func(f agentbus.Frame) { s.onBusFrame(ctx, f) },
	}
	s.recallAsked()
	go s.pulseBus(ctx)
	agentbus.Run(ctx, opts, func(c *agentbus.Client) {
		s.bus.attach(c)
		if c != nil {
			go s.seedTeam(ctx)
			go s.reconcileAsked(ctx)
		}
	})
}

// busAccepts is what this machine will be offered. nil means it only sends.
func busAccepts(cfg settings.Agentbus) *agentbus.Accepts {
	accepts := &agentbus.Accepts{Ask: !cfg.NoAsk}
	if !cfg.NoReview {
		accepts.Review = &agentbus.ReviewAccepts{Repos: cfg.Repos}
	}
	if !accepts.Ask && accepts.Review == nil {
		return nil
	}
	return accepts
}

func (b *busnode) attach(c *agentbus.Client) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.client = c
	b.sentPresence, b.sentStatus = "", ""
	if c == nil {
		b.you = agentbus.From{}
		clear(b.team)
		return
	}
	b.you = c.You()
}

func (b *busnode) live() (*agentbus.Client, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.client, b.client != nil
}

func (b *busnode) me() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.you.Instance == nil {
		return ""
	}
	return b.you.Instance.ID
}

// report takes one checkout's account of itself. Anything a checkout stops
// reporting ages out of presence through focusGrace rather than being removed.
func (b *busnode) report(dir string, state checkoutState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	state.At = time.Now()
	b.checkouts[dir] = state
}

func (b *busnode) running() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	count := 0
	for _, checkout := range b.checkouts {
		if checkout.Running && time.Since(checkout.At) < focusGrace {
			count++
		}
	}
	return count
}

func (b *busnode) operations(now time.Time) machineOperations {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out machineOperations
	for dir, checkout := range b.checkouts {
		if now.Sub(checkout.At) >= focusGrace {
			continue
		}
		out.Dirs = append(out.Dirs, dir)
		if checkout.Running {
			out.Live++
		}
		out.Queued += checkout.Queued
		if !checkout.QueueSince.IsZero() && now.Sub(checkout.QueueSince) > out.QueueWait {
			out.QueueWait = now.Sub(checkout.QueueSince)
		}
	}
	return out
}

// presence is the machine's, not one checkout's: at any of them is here.
func (b *busnode) presence() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	here := false
	for _, c := range b.checkouts {
		if c.Focused && time.Since(c.At) < focusGrace {
			here = true
		}
	}
	return busstate.PresenceFor(here, b.override)
}

// setOverride fixes presence until it is cleared. Empty follows the window
// again, which is the only way back.
func (b *busnode) setOverride(presence string) {
	b.mu.Lock()
	b.override = presence
	b.mu.Unlock()
}

// focus is the checkout to speak for: the focused one, else whichever has an
// agent running, else nothing. One line cannot name four checkouts.
func (b *busnode) focus() (checkoutState, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var best checkoutState
	var found bool
	for _, c := range b.checkouts {
		if time.Since(c.At) >= focusGrace {
			continue
		}
		switch {
		case c.Focused && (!found || !best.Focused):
			best, found = c, true
		case c.Running && !found:
			best, found = c, true
		case !found:
			best, found = c, true
		}
	}
	return best, found
}

func (s *Server) busPresence() string { return s.bus.presence() }

// busStatus is one line of what this machine is doing, spoken by whichever
// checkout is in front of you.
func (s *Server) busStatus() string {
	at, ok := s.bus.focus()
	if !ok {
		return ""
	}
	switch {
	case at.Asking > 0:
		return "waiting on an answer"
	case at.Running:
		return "an agent is working"
	}
	if n := s.bus.running(); n > 1 {
		return fmt.Sprintf("%d agents", n)
	}

	return "ade"
}

func (s *Server) busLabel() string {
	at, ok := s.bus.focus()
	if !ok {
		return ""
	}
	return at.Label
}

// pulseBus keeps presence and status current. The tick is what notices the
// things nothing reports: a run finishing, and focus ageing out.
func (s *Server) pulseBus(ctx context.Context) {
	ticker := time.NewTicker(busPulse)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		s.sendBusStatus(ctx)
	}
}

// sendBusStatus tells the bus where this machine stands, and says nothing
// when it already knows.
func (s *Server) sendBusStatus(ctx context.Context) {
	client, ok := s.bus.live()
	if !ok {
		return
	}
	presence, status := s.busPresence(), s.busStatus()
	s.bus.mu.Lock()
	same := presence == s.bus.sentPresence && status == s.bus.sentStatus
	s.bus.mu.Unlock()
	if same {
		return
	}
	if err := client.SetStatus(ctx, presence, &status, s.busLabel(), nil); err != nil {
		slog.Debug("agentbus status", "err", err)
		return
	}
	s.bus.mu.Lock()
	s.bus.sentPresence, s.bus.sentStatus = presence, status
	s.bus.mu.Unlock()
}

// seedTeam reads the roster once on connect. The pushes only carry changes.
func (s *Server) seedTeam(ctx context.Context) {
	client, ok := s.bus.live()
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	rows, err := client.Who(ctx, nil)
	if err != nil {
		slog.Debug("agentbus who", "err", err)
		return
	}
	me := s.bus.me()
	s.bus.mu.Lock()
	clear(s.bus.team)
	for _, row := range rows {
		s.bus.team[row.Instance.ID] = busstate.MemberOf(row, me)
	}
	s.bus.mu.Unlock()
	s.busChanged()
}

// onBusFrame takes what the server pushes: offers, answers and presence.
func (s *Server) onBusFrame(ctx context.Context, f agentbus.Frame) {
	switch f.Type {
	case agentbus.TypeAsk, agentbus.TypeReviewRequest:
		offer := busstate.OfferOf(f)
		if offer == nil {
			return
		}
		s.bus.mu.Lock()
		if s.bus.dismissed[offer.ID] {
			s.bus.mu.Unlock()
			return
		}
		s.bus.offers[offer.ID] = offer
		s.bus.mu.Unlock()
		slog.Info("agentbus request", "kind", offer.Kind, "from", offer.From, "id", offer.ID)
		s.busChanged()
	case agentbus.TypeState:
		reply := busstate.ReplyOf(f)
		if reply == nil {
			return
		}
		s.bus.mu.Lock()
		seen := s.bus.replies[reply.ID]
		from := s.bus.asked[reply.ID]
		reply.Dir, reply.ForAgent = from.Dir, from.Agent
		s.bus.replies[reply.ID] = reply
		s.bus.mu.Unlock()
		if seen != nil && *seen == *reply {
			return
		}
		slog.Info("agentbus reply", "kind", reply.Kind, "from", reply.From, "id", reply.ID)
		s.busChanged()
	case agentbus.TypePresence:
		if len(f.Instances) == 0 {
			return
		}
		online := f.Online == nil || *f.Online
		me := s.bus.me()
		s.bus.mu.Lock()
		for _, row := range f.Instances {
			if !online {
				delete(s.bus.team, row.Instance.ID)
				continue
			}
			s.bus.team[row.Instance.ID] = busstate.MemberOf(row, me)
		}
		s.bus.mu.Unlock()
		s.busChanged()
	case agentbus.TypeCancel:
		// A cancel names the request it withdraws, not itself.
		s.bus.mu.Lock()
		delete(s.bus.offers, f.ReplyTo)
		s.bus.mu.Unlock()
		s.busChanged()
	}
}

// ours corrects our own row. The server pushes a presence change to everyone
// else, never back to us, so the roster's copy of us never moves.
func ours(state *busstate.State, presence, status string) {
	for i, member := range state.Team {
		if member.Instance == state.Instance {
			state.Team[i].Presence, state.Team[i].Status = presence, status
		}
	}
}

func (s *Server) busChanged() { s.hub.Broadcast("bus", nil) }

// currentBus is the machine's standing, for a checkout's page to draw.
func (s *Server) currentBus() *busstate.State {
	s.bus.mu.Lock()
	state := &busstate.State{Online: s.bus.client != nil, User: s.bus.you.User}
	if s.bus.you.Instance != nil {
		state.Instance = s.bus.you.Instance.ID
	}
	for _, offer := range s.bus.offers {
		state.Offers = append(state.Offers, *offer)
	}
	for _, reply := range s.bus.replies {
		state.Replies = append(state.Replies, *reply)
	}
	// Still out: asked, not yet answered. The bus drops a request at busTTL,
	// so one that old is never coming back.
	dropped := false
	for id, sent := range s.bus.asked {
		if s.bus.replies[id] != nil {
			continue
		}
		if !sent.At.IsZero() && time.Since(sent.At) > busTTL {
			delete(s.bus.asked, id)
			dropped = true
			continue
		}
		at := ""
		if !sent.At.IsZero() {
			at = sent.At.UTC().Format(time.RFC3339)
		}
		state.Sent = append(state.Sent, busstate.Sent{
			ID: id, Kind: sent.Kind, To: sent.To, Question: sent.Question,
			PR: sent.PR, At: at, ForAgent: sent.Agent,
		})
	}
	for _, member := range s.bus.team {
		state.Team = append(state.Team, member)
	}
	s.bus.mu.Unlock()
	if dropped {
		go s.rememberAsked()
	}
	ours(state, s.busPresence(), s.busStatus())

	sort.Slice(state.Replies, func(i, j int) bool { return state.Replies[i].At < state.Replies[j].At })
	sort.Slice(state.Sent, func(i, j int) bool { return state.Sent[i].At < state.Sent[j].At })
	sort.Slice(state.Team, func(i, j int) bool {
		a, b := state.Team[i], state.Team[j]
		if busstate.RankPresence(a.Presence) != busstate.RankPresence(b.Presence) {
			return busstate.RankPresence(a.Presence) < busstate.RankPresence(b.Presence)
		}
		if a.User != b.User {
			return a.User < b.User
		}
		return a.Instance < b.Instance
	})
	sort.Slice(state.Offers, func(i, j int) bool {
		if state.Offers[i].CreatedAt == state.Offers[j].CreatedAt {
			return state.Offers[i].ID < state.Offers[j].ID
		}
		return state.Offers[i].CreatedAt < state.Offers[j].CreatedAt
	})
	if !state.Online && len(state.Offers) == 0 && len(state.Replies) == 0 &&
		len(state.Sent) == 0 && len(state.Team) == 0 {
		return nil
	}
	return state
}
