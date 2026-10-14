package agentbus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBus is agentbusd enough to answer a client: it welcomes a hello, acks
// what it is sent, and can push a frame of its own.
type fakeBus struct {
	url   string
	seen  chan Frame
	push  chan Frame
	reply func(Frame) Frame
}

func newFakeBus(t *testing.T, reply func(Frame) Frame) *fakeBus {
	t.Helper()
	bus := &fakeBus{seen: make(chan Frame, 32), push: make(chan Frame, 8), reply: reply}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ctx := r.Context()

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case f := <-bus.push:
					b, _ := json.Marshal(f)
					if conn.Write(ctx, websocket.MessageText, b) != nil {
						return
					}
				}
			}
		}()

		for {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var in Frame
			if json.Unmarshal(raw, &in) != nil {
				continue
			}
			bus.seen <- in
			out := bus.answer(in)
			out.Ref = in.Ref
			b, _ := json.Marshal(out)
			if conn.Write(ctx, websocket.MessageText, b) != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	bus.url = "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/ws"
	return bus
}

func (b *fakeBus) answer(in Frame) Frame {
	if b.reply != nil {
		if out := b.reply(in); out.Type != "" {
			return out
		}
	}
	if in.Type == TypeHello {
		return Frame{Type: TypeWelcome, ServerVersion: "test", You: &From{
			User: "owen@example.com", Node: "mbp",
			Instance: &Instance{ID: "owen@example.com/" + in.Instance.ID, Harness: in.Instance.Harness},
		}}
	}
	return Frame{Type: TypeAck, ID: "msg_1"}
}

func (b *fakeBus) next(t *testing.T) Frame {
	t.Helper()
	select {
	case f := <-b.seen:
		return f
	case <-time.After(3 * time.Second):
		t.Fatal("the bus was sent nothing")
		return Frame{}
	}
}

func dialTest(t *testing.T, bus *fakeBus, opts Options) *Client {
	t.Helper()
	opts.URL = bus.url
	if opts.Instance.ID == "" {
		opts.Instance = Instance{ID: "ade-1", Harness: "ade", Version: "test"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	c, err := Dial(ctx, opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestHelloCarriesTheInstanceAndWhatItAccepts(t *testing.T) {
	bus := newFakeBus(t, nil)
	c := dialTest(t, bus, Options{Accepts: &Accepts{Ask: true, Review: &ReviewAccepts{Repos: []string{"infracost/*"}}}})

	hello := bus.next(t)
	assert.Equal(t, TypeHello, hello.Type)
	assert.Equal(t, "ade-1", hello.Instance.ID)
	assert.Equal(t, "ade", hello.Instance.Harness)
	assert.True(t, hello.Accepts.Ask)
	assert.Equal(t, []string{"infracost/*"}, hello.Accepts.Review.Repos)
	// The PID is what lets an agent started under ade find its harness.
	assert.NotZero(t, hello.PID)

	assert.Equal(t, "owen@example.com/ade-1", c.You().Instance.ID)
	assert.Equal(t, "owen@example.com", c.You().User)
}

func TestTheServerNamesTheInstanceAndTheClientUsesThatName(t *testing.T) {
	bus := newFakeBus(t, nil)
	c := dialTest(t, bus, Options{})
	bus.next(t)

	require.NoError(t, c.SetStatus(context.Background(), PresenceIdle, nil, "ade on main", nil))
	status := bus.next(t)
	assert.Equal(t, TypeStatus, status.Type)
	assert.Equal(t, PresenceIdle, status.Presence)
	assert.Equal(t, "ade on main", status.Instance.Label)
}

func TestAPushedOfferReachesOnFrameAndARefReplyDoesNot(t *testing.T) {
	seen := make(chan Frame, 4)
	bus := newFakeBus(t, nil)
	c := dialTest(t, bus, Options{OnFrame: func(f Frame) { seen <- f }})
	bus.next(t)

	body, _ := json.Marshal(AskBody{Question: "ship it?", Options: []string{"yes", "no"}})
	bus.push <- Frame{Type: TypeAsk, ID: "msg_9", Body: body,
		From: &From{User: "liam@example.com", Node: "liams-laptop"}}

	select {
	case f := <-seen:
		assert.Equal(t, TypeAsk, f.Type)
		assert.Equal(t, "msg_9", f.ID)
	case <-time.After(3 * time.Second):
		t.Fatal("the offer never arrived")
	}

	// A reply to our own call is correlated by ref and must not be handed to
	// OnFrame as if it were a push.
	require.NoError(t, c.Answer(context.Background(), "msg_9", AnswerBody{Option: "yes"}))
	select {
	case f := <-seen:
		t.Fatalf("OnFrame got a reply to our own call: %v", f.Type)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestAConflictIsReadableAsOne(t *testing.T) {
	bus := newFakeBus(t, func(in Frame) Frame {
		if in.Type == TypeAnswer {
			return Frame{Type: TypeError, Code: ErrConflict, Error: "already answered"}
		}
		return Frame{}
	})
	c := dialTest(t, bus, Options{})
	bus.next(t)

	err := c.Answer(context.Background(), "msg_9", AnswerBody{Text: "late"})
	require.Error(t, err)
	assert.True(t, IsConflict(err), "want a conflict, got %v", err)
}

func TestAskAndReviewRequestCarryTheirTargetAndTTL(t *testing.T) {
	bus := newFakeBus(t, nil)
	c := dialTest(t, bus, Options{})
	bus.next(t)

	id, err := c.RequestReview(context.Background(), Target{Group: "engineering"}, time.Hour,
		ReviewRequestBody{PR: "https://github.com/infracost/infra/pull/7", Note: "billing"})
	require.NoError(t, err)
	assert.Equal(t, "msg_1", id)

	sent := bus.next(t)
	assert.Equal(t, TypeReviewRequest, sent.Type)
	assert.Equal(t, "engineering", sent.To.Group)
	assert.Equal(t, "1h0m0s", sent.TTL)

	_, err = c.Ask(context.Background(), Target{Anyone: true}, 0, AskBody{Question: "safe?"})
	require.NoError(t, err)
	ask := bus.next(t)
	assert.Equal(t, TypeAsk, ask.Type)
	assert.True(t, ask.To.Anyone)
	assert.Empty(t, ask.TTL)
}

func TestTargetsSurviveTheirStringForms(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		json string
	}{
		{"", "anyone", `"anyone"`},
		{"anyone", "anyone", `"anyone"`},
		{"*", "*", `"*"`},
		{"Bob@x.io", "bob@x.io", `{"user":"bob@x.io"}`},
		{"bob@x.io/crit-1", "bob@x.io/crit-1", `{"instance":"bob@x.io/crit-1"}`},
		{"engineering", "engineering", `{"group":"engineering"}`},
	} {
		t.Run(tc.in, func(t *testing.T) {
			target, err := ParseTarget(tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.want, target.String())

			raw, err := json.Marshal(target)
			require.NoError(t, err)
			assert.JSONEq(t, tc.json, string(raw))

			var back Target
			require.NoError(t, json.Unmarshal(raw, &back))
			assert.Equal(t, tc.want, back.String())
		})
	}
}

func TestPRsAreReadInBothFormsAndRefusedOtherwise(t *testing.T) {
	url, repo, err := ParsePR("infracost/infra#123")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/infracost/infra/pull/123", url)
	assert.Equal(t, "infracost/infra", repo)

	url, repo, err = ParsePR("https://github.com/infracost/infra/pull/123/files")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/infracost/infra/pull/123", url)
	assert.Equal(t, "infracost/infra", repo)

	for _, bad := range []string{"", "infra#1", "https://gitlab.com/a/b/pull/1", "https://github.com/a/b/issues/1"} {
		_, _, err := ParsePR(bad)
		assert.Errorf(t, err, "%q should not read as a pull request", bad)
	}
}

func TestTheInstanceIDIsMadeOnceAndKept(t *testing.T) {
	dir := t.TempDir()
	first := InstanceID(dir)
	assert.True(t, strings.HasPrefix(first, "ade-"), "got %q", first)
	assert.Equal(t, first, InstanceID(dir), "a restart must find the same id")
}

func TestResolveURLPrefersTheEnvironment(t *testing.T) {
	t.Setenv(EnvURL, "ws://127.0.0.1:7171/v1/ws")
	url, err := ResolveURL(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "ws://127.0.0.1:7171/v1/ws", url)

	t.Setenv(EnvURL, "")
	t.Setenv(EnvHost, "bus.example.ts.net")
	url, err = ResolveURL(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "ws://bus.example.ts.net/v1/ws", url)
}

func TestRunRedialsAfterTheConnectionDrops(t *testing.T) {
	backoffFirst, backoffMax = 20*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { backoffFirst, backoffMax = 2*time.Second, 2*time.Minute })

	dials := make(chan struct{}, 8)
	bus := newFakeBus(t, func(in Frame) Frame {
		if in.Type == TypeHello {
			dials <- struct{}{}
		}
		return Frame{}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	up := make(chan *Client, 8)
	go Run(ctx, Options{URL: bus.url, Instance: Instance{ID: "ade-1"}}, func(c *Client) { up <- c })

	first := <-up
	require.NotNil(t, first)
	require.NoError(t, first.Close())

	select {
	case c := <-up:
		assert.Nil(t, c, "a dropped connection is reported as down first")
	case <-time.After(3 * time.Second):
		t.Fatal("the drop was never noticed")
	}
	select {
	case c := <-up:
		assert.NotNil(t, c, "Run should have redialled")
	case <-time.After(3 * time.Second):
		t.Fatal("Run never redialled")
	}
	assert.GreaterOrEqual(t, len(dials), 2)
}
