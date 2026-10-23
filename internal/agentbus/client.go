package agentbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// maxFrame is the server's limit, and ours: a frame bigger than this is not
// one it sent.
const maxFrame = 1 << 20

// Options is one connection's shape.
type Options struct {
	URL      string
	Instance Instance
	Session  *Session
	Accepts  *Accepts
	Presence string
	Status   string
	Header   http.Header

	// Ancestors are the parent process ids of a session connection, nearest
	// first. The server joins the session to the instance whose pid is in it.
	Ancestors []int

	// OnFrame takes the frames the server pushes on its own: offers, cancels,
	// presence and state. Replies to this client's own calls never reach it.
	OnFrame func(Frame)

	// OnTrouble is why the bus cannot be joined, for the page to say.
	OnTrouble func(error)
}

// Client is one connection. It does not reconnect; Run is the loop that does.
type Client struct {
	opts Options
	ws   *websocket.Conn
	you  From

	ref     atomic.Uint64
	mu      sync.Mutex
	waiting map[string]chan Frame
	done    chan struct{}
	err     error
}

// Error is an error frame from the server. Code is what to branch on:
// "conflict" means somebody else got there first.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// IsConflict reports whether err is the server saying someone else answered or
// claimed first, which is an ordinary outcome and not a fault.
func IsConflict(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == ErrConflict
}

// Dial connects and sends hello.
func Dial(ctx context.Context, opts Options) (*Client, error) {
	if opts.URL == "" {
		return nil, errors.New("agentbus: no URL")
	}
	ws, _, err := websocket.Dial(ctx, opts.URL, &websocket.DialOptions{HTTPHeader: opts.Header})
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", opts.URL, err)
	}
	ws.SetReadLimit(maxFrame)
	c := &Client{
		opts:    opts,
		ws:      ws,
		waiting: map[string]chan Frame{},
		done:    make(chan struct{}),
	}
	go c.read()

	hello := Frame{Type: TypeHello, Instance: &opts.Instance, Session: opts.Session, Presence: opts.Presence}
	if opts.Session != nil {
		hello.Ancestors = opts.Ancestors
	}
	if opts.Session == nil {
		hello.Accepts = opts.Accepts
		hello.PID = os.Getpid()
	}
	if opts.Status != "" {
		hello.Status = &opts.Status
	}
	welcome, err := c.call(ctx, hello)
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	if welcome.You != nil {
		c.you = *welcome.You
	}
	return c, nil
}

// You is the identity the server gave this connection: the tailnet's word for
// who and where we are, plus the instance id it namespaced.
func (c *Client) You() From { return c.you }

// Done is closed when the connection ends.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err is why the connection ended.
func (c *Client) Err() error {
	<-c.done
	return c.err
}

func (c *Client) Close() error { return c.ws.Close(websocket.StatusNormalClosure, "") }

// SetStatus updates presence, status text, label or accepts. An empty value
// leaves that field as it was.
func (c *Client) SetStatus(ctx context.Context, presence string, status *string, label string, accepts *Accepts) error {
	f := Frame{Type: TypeStatus, Presence: presence, Status: status, Accepts: accepts}
	if label != "" {
		f.Instance = &Instance{ID: c.opts.Instance.ID, Label: label}
	}
	_, err := c.call(ctx, f)
	return err
}

// Answer replies to an ask that was offered to this instance.
func (c *Client) Answer(ctx context.Context, id string, body AnswerBody) error {
	return c.reply(ctx, TypeAnswer, id, body)
}

// ClaimReview takes a review request. A conflict means another harness took it.
func (c *Client) ClaimReview(ctx context.Context, id string) error {
	return c.reply(ctx, TypeReviewClaim, id, CancelBody{})
}

// ReviewResult reports the verdict on a review this instance claimed.
func (c *Client) ReviewResult(ctx context.Context, id string, body ReviewResultBody) error {
	return c.reply(ctx, TypeReviewResult, id, body)
}

// Ask puts a question to a person, and answers the new message id.
func (c *Client) Ask(ctx context.Context, to Target, ttl time.Duration, body AskBody) (string, error) {
	return c.request(ctx, TypeAsk, to, ttl, body)
}

// RequestReview asks for a review of a pull request, and answers the new
// message id.
func (c *Client) RequestReview(ctx context.Context, to Target, ttl time.Duration, body ReviewRequestBody) (string, error) {
	return c.request(ctx, TypeReviewRequest, to, ttl, body)
}

// Cancel withdraws a request this client sent.
func (c *Client) Cancel(ctx context.Context, id string) error {
	_, err := c.call(ctx, Frame{Type: TypeCancel, ID: id})
	return err
}

// Who lists the instances the server can see.
func (c *Client) Who(ctx context.Context, filter *WhoFilter) ([]Online, error) {
	f, err := c.call(ctx, Frame{Type: TypeWho, Filter: filter})
	if err != nil {
		return nil, err
	}
	return f.Instances, nil
}

// Get reads a request and its replies.
func (c *Client) Get(ctx context.Context, id string) (Frame, error) {
	return c.call(ctx, Frame{Type: TypeGet, ID: id})
}

func (c *Client) reply(ctx context.Context, kind, id string, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, err = c.call(ctx, Frame{Type: kind, ReplyTo: id, Body: raw})
	return err
}

func (c *Client) request(ctx context.Context, kind string, to Target, ttl time.Duration, body any) (string, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	f := Frame{Type: kind, To: &to, Body: raw}
	if ttl > 0 {
		f.TTL = ttl.String()
	}
	ack, err := c.call(ctx, f)
	if err != nil {
		return "", err
	}
	return ack.ID, nil
}

func (c *Client) read() {
	defer close(c.done)
	for {
		_, b, err := c.ws.Read(context.Background())
		if err != nil {
			c.err = err
			return
		}
		var f Frame
		if err := json.Unmarshal(b, &f); err != nil {
			continue
		}
		c.mu.Lock()
		ch, waiting := c.waiting[f.Ref]
		if waiting && f.Ref != "" {
			delete(c.waiting, f.Ref)
		}
		c.mu.Unlock()
		if waiting && f.Ref != "" {
			ch <- f
			continue
		}
		if c.opts.OnFrame != nil {
			c.opts.OnFrame(f)
		}
	}
}

// call sends f and waits for the frame carrying its ref.
func (c *Client) call(ctx context.Context, f Frame) (Frame, error) {
	f.Ref = strconv.FormatUint(c.ref.Add(1), 10)
	ch := make(chan Frame, 1)
	c.mu.Lock()
	c.waiting[f.Ref] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.waiting, f.Ref)
		c.mu.Unlock()
	}()

	b, err := json.Marshal(f)
	if err != nil {
		return Frame{}, err
	}
	if err := c.ws.Write(ctx, websocket.MessageText, b); err != nil {
		return Frame{}, err
	}
	select {
	case r := <-ch:
		if r.Type == TypeError {
			return r, &Error{Code: r.Code, Message: r.Error}
		}
		return r, nil
	case <-c.done:
		return Frame{}, fmt.Errorf("agentbus: connection closed: %w", c.err)
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	}
}
