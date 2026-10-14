package launcher

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/agentbus"
	"github.com/owenrumney/ade/internal/busstate"
	"github.com/owenrumney/ade/internal/settings"
)

func askFrame(id, question string, options ...string) agentbus.Frame {
	body, _ := json.Marshal(agentbus.AskBody{Question: question, Options: options})
	return agentbus.Frame{Type: agentbus.TypeAsk, ID: id, Body: body, CreatedAt: "2026-10-02T09:00:00Z",
		From: &agentbus.From{
			User: "liam@infracost.io", Node: "liams-mbp",
			Instance: &agentbus.Instance{ID: "liam@infracost.io/crit-1", Harness: "crit"},
			Session:  &agentbus.Session{ID: "s1", Agent: "claude-code", Model: "claude-opus-5"},
		}}
}

func reviewFrame(id, pr, note string) agentbus.Frame {
	body, _ := json.Marshal(agentbus.ReviewRequestBody{PR: pr, Note: note})
	return agentbus.Frame{Type: agentbus.TypeReviewRequest, ID: id, Body: body, CreatedAt: "2026-10-02T09:01:00Z",
		From: &agentbus.From{User: "liam@infracost.io", Node: "liams-mbp"}}
}

func stateFrame(id string, message, reply agentbus.Frame) agentbus.Frame {
	return agentbus.Frame{Type: agentbus.TypeState, ID: id, State: "answered",
		Message: &message, Replies: []agentbus.Frame{reply}}
}

func onlineRow(user, id, presence string) agentbus.Online {
	return agentbus.Online{
		User: user, Node: "mbp", Presence: presence, Status: "on main", Connected: true,
		Instance: agentbus.Instance{ID: id, Harness: "crit", Label: "repo on main"},
		Accepts:  &agentbus.Accepts{Ask: true, Review: &agentbus.ReviewAccepts{}},
	}
}

func presenceFrame(online bool, rows ...agentbus.Online) agentbus.Frame {
	return agentbus.Frame{Type: agentbus.TypePresence, Online: &online, Instances: rows}
}

func TestAnOfferedAskReachesTheState(t *testing.T) {
	s := newTestLauncher(t)
	s.onBusFrame(context.Background(), askFrame("msg_1", "Is dev RDS safe to resize?", "yes", "no"))

	state := s.currentBus()
	require.NotNil(t, state)
	require.Len(t, state.Offers, 1)

	offer := state.Offers[0]
	assert.Equal(t, "ask", offer.Kind)
	assert.Equal(t, "msg_1", offer.ID)
	assert.Equal(t, "liam@infracost.io", offer.From)
	assert.Equal(t, "crit", offer.Harness)
	assert.Equal(t, "claude-code", offer.Agent)
	assert.Equal(t, "claude-opus-5", offer.Model)
	assert.Equal(t, []string{"yes", "no"}, offer.Options)
}

func TestAnOfferRepeatedAfterAReconnectIsNotListedTwice(t *testing.T) {
	s := newTestLauncher(t)
	for range 3 {
		s.onBusFrame(context.Background(), askFrame("msg_1", "again?"))
	}
	require.Len(t, s.currentBus().Offers, 1)
}

func TestCancelTakesTheOfferOffTheList(t *testing.T) {
	s := newTestLauncher(t)
	s.onBusFrame(context.Background(), askFrame("msg_1", "safe?"))
	s.onBusFrame(context.Background(), askFrame("msg_2", "and this?"))

	s.onBusFrame(context.Background(), agentbus.Frame{Type: agentbus.TypeCancel, ReplyTo: "msg_1"})
	state := s.currentBus()
	require.Len(t, state.Offers, 1)
	assert.Equal(t, "msg_2", state.Offers[0].ID)
}

func TestAReviewRequestIsReadAsItsRepoAndNumber(t *testing.T) {
	s := newTestLauncher(t)
	s.onBusFrame(context.Background(), reviewFrame("msg_3", "infracost/infra#123", "touches billing"))

	offer := s.currentBus().Offers[0]
	assert.Equal(t, "review", offer.Kind)
	assert.Equal(t, "infracost/infra", offer.Repo)
	assert.Equal(t, 123, offer.Number)
	assert.Equal(t, "https://github.com/infracost/infra/pull/123", offer.PR)
	assert.Equal(t, "touches billing", offer.Note)
}

func TestAnOfferNobodyCanReadIsDropped(t *testing.T) {
	s := newTestLauncher(t)
	s.onBusFrame(context.Background(), agentbus.Frame{Type: agentbus.TypeAsk, ID: "msg_4",
		Body: json.RawMessage(`{"question":"  "}`)})
	s.onBusFrame(context.Background(), reviewFrame("msg_5", "not-a-pull-request", ""))
	assert.Nil(t, s.currentBus())
}

func TestTheOffersAreOldestFirst(t *testing.T) {
	s := newTestLauncher(t)
	s.onBusFrame(context.Background(), reviewFrame("msg_3", "infracost/infra#123", ""))
	s.onBusFrame(context.Background(), askFrame("msg_1", "safe?"))

	offers := s.currentBus().Offers
	require.Len(t, offers, 2)
	assert.Equal(t, "msg_1", offers[0].ID)
}

func TestAnsweringWithoutABusSaysSoRatherThanHanging(t *testing.T) {
	s := newTestLauncher(t)
	s.onBusFrame(context.Background(), askFrame("msg_1", "safe?"))

	rec := do(t, s, "POST", "/api/bus/answer", `{"id":"msg_1","option":"yes"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "not connected")
}

func TestAnAnswerNeedsWordsOrAnOption(t *testing.T) {
	s := newTestLauncher(t)
	rec := do(t, s, "POST", "/api/bus/answer", `{"id":"msg_1","text":"  "}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "an option or some words")
}

func TestAVerdictHasToBeOneTheBusKnows(t *testing.T) {
	s := newTestLauncher(t)
	rec := do(t, s, "POST", "/api/bus/result", `{"id":"msg_3","state":"lgtm"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "approved, changes_requested or commented")
}

func TestRequestingAReviewRefusesSomethingThatIsNotAPullRequest(t *testing.T) {
	s := newTestLauncher(t)
	rec := do(t, s, "POST", "/api/bus/review", `{"pr":"over there"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "github.com PR URL")
}

func TestWhoIsAnEmptyAnswerRatherThanAFailureWhenThereIsNoBus(t *testing.T) {
	s := newTestLauncher(t)
	rec := do(t, s, "GET", "/api/bus/who", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"online":false}`, rec.Body.String())
}

func TestWhatThisAdeAcceptsFollowsTheSettings(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cfg   settings.Agentbus
		check func(*testing.T, *agentbus.Accepts)
	}{
		{"by default both", settings.Agentbus{}, func(t *testing.T, a *agentbus.Accepts) {
			require.NotNil(t, a)
			assert.True(t, a.Ask)
			require.NotNil(t, a.Review)
			assert.Empty(t, a.Review.Repos)
		}},
		{"repos limit reviews", settings.Agentbus{Repos: []string{"infracost/*"}}, func(t *testing.T, a *agentbus.Accepts) {
			assert.Equal(t, []string{"infracost/*"}, a.Review.Repos)
		}},
		{"no reviews", settings.Agentbus{NoReview: true}, func(t *testing.T, a *agentbus.Accepts) {
			assert.True(t, a.Ask)
			assert.Nil(t, a.Review)
		}},
		{"neither means send only", settings.Agentbus{NoAsk: true, NoReview: true}, func(t *testing.T, a *agentbus.Accepts) {
			assert.Nil(t, a)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) { tc.check(t, busAccepts(tc.cfg)) })
	}
}

func TestAnAnswerToSomethingAskedFromHereArrivesWithoutBeingAskedFor(t *testing.T) {
	s := newTestLauncher(t)
	asked, _ := json.Marshal(agentbus.AskBody{Question: "can you hear me?"})
	said, _ := json.Marshal(agentbus.AnswerBody{Option: "yes", Text: "loud and clear"})

	s.onBusFrame(context.Background(), stateFrame("msg_7",
		agentbus.Frame{Type: agentbus.TypeAsk, Body: asked},
		agentbus.Frame{Type: agentbus.TypeAnswer, Body: said, CreatedAt: "2026-10-02T09:05:00Z",
			From: &agentbus.From{User: "liam@infracost.io"}}))

	state := s.currentBus()
	require.NotNil(t, state)
	require.Len(t, state.Replies, 1)

	reply := state.Replies[0]
	assert.Equal(t, "answer", reply.Kind)
	assert.Equal(t, "liam@infracost.io", reply.From)
	assert.Equal(t, "yes", reply.Option)
	assert.Equal(t, "loud and clear", reply.Text)
	assert.Equal(t, "can you hear me?", reply.Question)
	assert.False(t, reply.ByAgent)
}

func TestAnAnswerAnAgentGaveForItsHumanSaysSo(t *testing.T) {
	s := newTestLauncher(t)
	asked, _ := json.Marshal(agentbus.AskBody{Question: "safe?"})
	said, _ := json.Marshal(agentbus.AnswerBody{Text: "probably", By: agentbus.AnsweredByAgent})

	s.onBusFrame(context.Background(), stateFrame("msg_8",
		agentbus.Frame{Type: agentbus.TypeAsk, Body: asked},
		agentbus.Frame{Type: agentbus.TypeAnswer, Body: said}))

	assert.True(t, s.currentBus().Replies[0].ByAgent)
}

func TestAReviewVerdictComesBackAsOne(t *testing.T) {
	s := newTestLauncher(t)
	asked, _ := json.Marshal(agentbus.ReviewRequestBody{PR: "https://github.com/infracost/infra/pull/9"})
	verdict, _ := json.Marshal(agentbus.ReviewResultBody{State: "approved", Summary: "LGTM"})

	s.onBusFrame(context.Background(), stateFrame("msg_9",
		agentbus.Frame{Type: agentbus.TypeReviewRequest, Body: asked},
		agentbus.Frame{Type: agentbus.TypeReviewResult, Body: verdict}))

	reply := s.currentBus().Replies[0]
	assert.Equal(t, "review", reply.Kind)
	assert.Equal(t, "approved", reply.Verdict)
	assert.Equal(t, "LGTM", reply.Text)
	assert.Equal(t, "https://github.com/infracost/infra/pull/9", reply.PR)
}

func TestAStateWithNothingInItIsNotAnAnswer(t *testing.T) {
	s := newTestLauncher(t)
	s.onBusFrame(context.Background(), agentbus.Frame{Type: agentbus.TypeState, ID: "msg_7", State: "pending"})
	assert.Nil(t, s.currentBus())
}

func TestAnAnswerIsClearedOnceItHasBeenRead(t *testing.T) {
	s := newTestLauncher(t)
	asked, _ := json.Marshal(agentbus.AskBody{Question: "safe?"})
	said, _ := json.Marshal(agentbus.AnswerBody{Option: "yes"})
	s.onBusFrame(context.Background(), stateFrame("msg_7",
		agentbus.Frame{Type: agentbus.TypeAsk, Body: asked},
		agentbus.Frame{Type: agentbus.TypeAnswer, Body: said}))

	rec := do(t, s, "POST", "/api/bus/dismiss", `{"id":"msg_7"}`)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Nil(t, s.currentBus())
}

func TestTheBusIsNotToldWhatItAlreadyKnows(t *testing.T) {
	s := newTestLauncher(t)
	// No connection, so nothing is sent and nothing is recorded as sent.
	s.sendBusStatus(context.Background())
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	assert.Empty(t, s.bus.sentPresence)
}

func TestSomebodyComingOnlineJoinsTheTeamWithoutAnythingAsking(t *testing.T) {
	s := newTestLauncher(t)
	s.onBusFrame(context.Background(),
		presenceFrame(true, onlineRow("liam@infracost.io", "liam@infracost.io/crit-1", "active")))

	team := s.currentBus().Team
	require.Len(t, team, 1)
	assert.Equal(t, "liam@infracost.io", team[0].User)
	assert.Equal(t, "active", team[0].Presence)
	assert.True(t, team[0].TakesAsk)
	assert.True(t, team[0].TakesPR)
}

func TestGoingOfflineTakesThemOffTheList(t *testing.T) {
	s := newTestLauncher(t)
	row := onlineRow("liam@infracost.io", "liam@infracost.io/crit-1", "active")
	s.onBusFrame(context.Background(), presenceFrame(true, row))
	require.Len(t, s.currentBus().Team, 1)

	s.onBusFrame(context.Background(), presenceFrame(false, row))
	// Nobody left, nothing waiting, not connected: there is no bus to show.
	assert.Nil(t, s.currentBus())
}

func TestAPresenceChangeReplacesTheRowRatherThanAddingOne(t *testing.T) {
	s := newTestLauncher(t)
	id := "liam@infracost.io/crit-1"
	s.onBusFrame(context.Background(), presenceFrame(true, onlineRow("liam@infracost.io", id, "active")))
	s.onBusFrame(context.Background(), presenceFrame(true, onlineRow("liam@infracost.io", id, "dnd")))

	team := s.currentBus().Team
	require.Len(t, team, 1)
	assert.Equal(t, "dnd", team[0].Presence)
}

func TestWhoIsMostLikelyToAnswerComesFirst(t *testing.T) {
	s := newTestLauncher(t)
	s.onBusFrame(context.Background(), presenceFrame(true,
		onlineRow("sam@infracost.io", "sam@infracost.io/crit-1", "dnd"),
		onlineRow("vadim@infracost.io", "vadim@infracost.io/crit-1", "idle"),
		onlineRow("liam@infracost.io", "liam@infracost.io/crit-1", "active")))

	team := s.currentBus().Team
	assert.Equal(t, []string{"active", "idle", "dnd"},
		[]string{team[0].Presence, team[1].Presence, team[2].Presence})
}

func TestAPresenceFrameWithNobodyInItChangesNothing(t *testing.T) {
	s := newTestLauncher(t)
	s.onBusFrame(context.Background(), presenceFrame(true))
	assert.Nil(t, s.currentBus())
}
func TestPresenceFollowsWhetherAnyoneIsWatching(t *testing.T) {
	s := newTestLauncher(t)
	assert.Equal(t, agentbus.PresenceIdle, s.busPresence())
}

func TestRunningCountsFreshCheckoutsOnThisMachine(t *testing.T) {
	bus := newBusnode()
	bus.report("/work/one", checkoutState{Running: true})
	bus.report("/work/idle", checkoutState{})
	bus.mu.Lock()
	bus.checkouts["/work/stale"] = checkoutState{Running: true, At: time.Now().Add(-focusGrace)}
	bus.mu.Unlock()

	assert.Equal(t, 1, bus.running())
}

func TestOperationsCombineFreshCheckoutQueues(t *testing.T) {
	now := time.Now()
	bus := newBusnode()
	bus.checkouts["/work/one"] = checkoutState{Running: true, Queued: 2, QueueSince: now.Add(-45 * time.Second), At: now}
	bus.checkouts["/work/two"] = checkoutState{Queued: 1, QueueSince: now.Add(-10 * time.Second), At: now}
	bus.checkouts["/work/stale"] = checkoutState{Running: true, Queued: 9, At: now.Add(-focusGrace)}

	got := bus.operations(now)
	assert.Equal(t, 1, got.Live)
	assert.Equal(t, 3, got.Queued)
	assert.Equal(t, 45*time.Second, got.QueueWait)
	assert.ElementsMatch(t, []string{"/work/one", "/work/two"}, got.Dirs)
}

// The bus pushes a presence change to every instance but the one that made it,
// so the roster's copy of us is stale from the moment we set it.
func TestOurOwnRowSaysWhatWeKnow(t *testing.T) {
	state := &busstate.State{
		Instance: "owen@infracost.io/ade-1",
		Team: []busstate.Member{
			{Instance: "liam@infracost.io/crit-1", Presence: "active", Status: "on main"},
			{Instance: "owen@infracost.io/ade-1", Presence: "idle", You: true},
		},
	}
	ours(state, "active", "an agent is working in ade")

	assert.Equal(t, "active", state.Team[1].Presence, "ours is what we know, not what we were told")
	assert.Equal(t, "an agent is working in ade", state.Team[1].Status)
	assert.Equal(t, "active", state.Team[0].Presence, "somebody else's row is theirs to report")
	assert.Equal(t, "on main", state.Team[0].Status)
}

// Before the welcome there is no instance to match, and an empty one must not
// claim the first row with an empty instance.
func TestNobodysRowIsTakenBeforeWeKnowOurOwn(t *testing.T) {
	state := &busstate.State{Team: []busstate.Member{{Instance: "liam@infracost.io/crit-1", Presence: "active"}}}
	ours(state, "idle", "")
	assert.Equal(t, "active", state.Team[0].Presence)
}

func TestAnAskWithNoAnswerYetIsListedAsStillOut(t *testing.T) {
	s := newTestLauncher(t)
	s.bus.asked["msg_out"] = askedBy{
		Kind: "ask", To: "liam@infracost.io", Question: "Is dev RDS safe to resize?",
		At: time.Now(),
	}

	state := s.currentBus()
	require.Len(t, state.Sent, 1)
	assert.Equal(t, "msg_out", state.Sent[0].ID)
	assert.Equal(t, "ask", state.Sent[0].Kind)
	assert.Equal(t, "liam@infracost.io", state.Sent[0].To)
	assert.Equal(t, "Is dev RDS safe to resize?", state.Sent[0].Question)
}

func TestAnAnsweredAskIsNoLongerStillOut(t *testing.T) {
	s := newTestLauncher(t)
	s.bus.asked["msg_out"] = askedBy{Kind: "ask", Question: "ready?", At: time.Now()}
	s.bus.replies["msg_out"] = &busstate.Reply{ID: "msg_out", Kind: "answer", Text: "yes"}

	assert.Empty(t, s.currentBus().Sent)
}

func TestAnAskPastItsTTLIsForgottenRatherThanShown(t *testing.T) {
	s := newTestLauncher(t)
	s.bus.asked["msg_old"] = askedBy{
		Kind: "ask", Question: "still there?", At: time.Now().Add(-busTTL - time.Minute),
	}

	// Nothing left to show, so there is no state at all: the point is that the
	// entry is gone from the book, not that an empty list came back.
	if state := s.currentBus(); state != nil {
		assert.Empty(t, state.Sent)
	}
	assert.NotContains(t, s.bus.asked, "msg_old", "an expired ask should be dropped, not kept")
}

func TestRefreshingTheRosterWithoutABusSaysSoRatherThanHanging(t *testing.T) {
	s := newTestLauncher(t)
	rec := do(t, s, "POST", "/api/bus/refresh", "{}")
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestWhatWasAskedSurvivesARestart(t *testing.T) {
	s := newTestLauncher(t)
	s.bus.asked["msg_keep"] = askedBy{
		Kind: "ask", To: "liam@infracost.io", Question: "safe to resize?", At: time.Now(),
	}
	s.rememberAsked()

	// A second launcher over the same state directory is what a restart is.
	next := newTestLauncher(t)
	next.home = s.home
	next.recallAsked()

	require.Contains(t, next.bus.asked, "msg_keep")
	assert.Equal(t, "safe to resize?", next.bus.asked["msg_keep"].Question)
	assert.Equal(t, "liam@infracost.io", next.bus.asked["msg_keep"].To)
}

func TestAnAskPastItsTTLIsNotReadBackIn(t *testing.T) {
	s := newTestLauncher(t)
	s.bus.asked["msg_old"] = askedBy{Kind: "ask", At: time.Now().Add(-busTTL - time.Minute)}
	s.rememberAsked()

	next := newTestLauncher(t)
	next.home = s.home
	next.recallAsked()

	assert.NotContains(t, next.bus.asked, "msg_old")
}

func TestDismissingAnAnswerForgetsTheAskOnDisk(t *testing.T) {
	s := newTestLauncher(t)
	s.bus.asked["msg_1"] = askedBy{Kind: "ask", Question: "ready?", At: time.Now()}
	s.rememberAsked()

	rec := do(t, s, "POST", "/api/bus/dismiss", `{"id":"msg_1"}`)
	require.Equal(t, http.StatusOK, rec.Code)

	next := newTestLauncher(t)
	next.home = s.home
	next.recallAsked()
	assert.NotContains(t, next.bus.asked, "msg_1", "a dismissed ask should not come back")
}

func TestDismissingAReviewRequestDropsItAndKeepsItGone(t *testing.T) {
	s := newTestLauncher(t)
	frame := reviewFrame("msg_r", "https://github.com/infracost/coast/pull/1178", "")
	s.onBusFrame(context.Background(), frame)
	require.Contains(t, s.bus.offers, "msg_r")

	rec := do(t, s, "POST", "/api/bus/dismiss", `{"id":"msg_r"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, s.bus.offers, "msg_r")

	s.onBusFrame(context.Background(), frame)
	assert.NotContains(t, s.bus.offers, "msg_r", "a replay must not bring a dismissed request back")
}
