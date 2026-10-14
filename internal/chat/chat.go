// Package chat stores the session conversation, so restarting ade does not cost
// you the reasoning behind the work.
//
// The conversation is everything the agent said and did in a session: your
// messages, its replies, the tools it called and ade's own notes about runs.
// That is append-only and can run to thousands of entries, which is why it is
// a key-value store rather than a JSON file rewritten on every turn.
package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/owenrumney/ade/internal/statedir"
	bolt "go.etcd.io/bbolt"
	berrors "go.etcd.io/bbolt/errors"
)

// Message kinds. A message is one of these, and the browser draws each
// differently: yours on the right, the agent's as prose, a tool call folded to
// one line, and a note as a quiet line between them.
const (
	KindYou   = "you"
	KindAgent = "agent"
	KindTool  = "tool"
	KindNote  = "note"
)

// Message is one entry in a conversation.
type Message struct {
	// ID orders the conversation and names an entry so it can be updated or
	// taken back out. A queued prompt is shown the moment you send it, and
	// dropping it from the queue has to remove the message too.
	ID   string `json:"id"`
	Kind string `json:"kind"`

	// Author is who spoke, when it was not you or the main agent: a named
	// agent called into the conversation. Empty for the two regulars.
	Author string `json:"author,omitempty"`

	// Model is what produced an agent's reply, when one was named, so a
	// conversation that switched models reads correctly afterwards.
	Model string `json:"model,omitempty"`

	// For is the agent a named agent was answering, when another agent
	// called it rather than you: the reply belongs in the asker's thread.
	For string `json:"for,omitempty"`

	// Body is what was said, and HTML its rendered form for an agent reply, so
	// the browser never has to parse markdown itself.
	Body string `json:"body,omitempty"`
	HTML string `json:"html,omitempty"`

	// Context is what you attached to a message: text highlighted in a design
	// document or a source file, and where it came from.
	Context *Context `json:"context,omitempty"`

	// Files are what you attached to the message, written beside the
	// conversation and named by path so the agent can read them.
	Files []File `json:"files,omitempty"`

	// Images are pictures pasted into a message before attachments were
	// files on disk. Read for those conversations, never written.
	Images []Image `json:"images,omitempty"`

	// The tool call, for KindTool. Input is the call's arguments as JSON and
	// Output what came back, both kept whole so the folded row can be opened.
	Tool   string `json:"tool,omitempty"`
	ToolID string `json:"toolId,omitempty"`
	Target string `json:"target,omitempty"`
	Input  string `json:"input,omitempty"`
	// Doing and Steps are a sub-agent's progress while the call that made
	// it is open: what it is on, and how many tool calls so far.
	Doing  string `json:"doing,omitempty"`
	Steps  int    `json:"steps,omitempty"`
	Output string `json:"output,omitempty"`

	// Failed marks a tool call that errored, or a note that reports one.
	Failed bool `json:"failed,omitempty"`

	// Rule is what decided a tool call without a card, so a rule too broad is
	// seen at work rather than found out later.
	Rule string `json:"rule,omitempty"`

	// Run is the kind of run this belongs to, so a review's chatter can be
	// told apart from the conversation.
	Run string `json:"run,omitempty"`

	// Review is a named agent's reply to a review of the diff, whose output
	// is the findings it filed rather than prose to fold into a document.
	Review bool `json:"review,omitempty"`

	// Undo names the snapshot of the working tree taken before your message
	// ran, and Undone the one taken when it was undone, which is the way back.
	Undo   string `json:"undo,omitempty"`
	Undone string `json:"undone,omitempty"`

	// FoldedInto names the prompt this one was sent with, as one turn.
	FoldedInto string `json:"foldedInto,omitempty"`

	At time.Time `json:"at"`
}

// Context is text attached to a message and where it was highlighted.
type Context struct {
	From string `json:"from"`
	Text string `json:"text"`
}

// File is something attached to a message: the name you dropped, where it
// was written, and whether it is a picture.
type File struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Image bool   `json:"image,omitempty"`
}

// Image is a picture attached to a message: its media type and the bytes,
// base64 encoded, which is how the agent takes them too.
type Image struct {
	MediaType string `json:"mediaType"`
	Data      string `json:"data"`
}

// Store is a checkout's conversations, one per agent session.
type Store struct {
	db *bolt.DB
}

// Pending is the conversation of a session that has not named itself yet. A
// fresh agent only reports its id once it is running, and what you said to
// start it is written here until then.
const Pending = ""

// maxOutput bounds a tool result. A Bash call can print megabytes, and the
// folded row exists to be opened occasionally, not to hold all of it.
const maxOutput = 8 * 1024

// openTimeout is how long to wait on the file lock. bbolt takes an exclusive
// lock, and the only thing that could hold it is another ade in this checkout,
// which one-server-per-checkout already rules out.
const openTimeout = 2 * time.Second

var (
	conversations = []byte("conversations")
	// state holds what a server needs back after a restart and that is not
	// a conversation: the queue of turns waiting for the agent.
	state = []byte("state")
)

// Open opens the store at path, creating it if need be.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create chat dir: %w", err)
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: openTimeout})
	if err != nil {
		return nil, fmt.Errorf("open chat store: %w", err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(conversations); err != nil {
			return err
		}
		_, err := tx.CreateBucketIfNotExists(state)
		return err
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("prepare chat store: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the file. Nothing else can open it until then.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Path is where this store is written.
func (s *Store) Path() string {
	if s == nil || s.db == nil {
		return ""
	}
	return s.db.Path()
}

// Append adds a message to a session's conversation and returns it with its
// id and time filled in.
func (s *Store) Append(session string, msg Message) (Message, error) {
	if s == nil {
		return msg, errors.New("no chat store")
	}
	if msg.At.IsZero() {
		msg.At = time.Now()
	}
	msg.Output = clip(msg.Output)
	err := s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.Bucket(conversations).CreateBucketIfNotExists(key(session))
		if err != nil {
			return err
		}
		seq, err := b.NextSequence()
		if err != nil {
			return err
		}
		msg.ID = fmt.Sprintf("%016x", seq)
		return put(b, msg)
	})
	if err != nil {
		return msg, fmt.Errorf("append chat: %w", err)
	}
	return msg, nil
}

// Update replaces a message in place, for a tool call that has since reported
// its result.
func (s *Store) Update(session string, msg Message) error {
	if s == nil {
		return errors.New("no chat store")
	}
	msg.Output = clip(msg.Output)
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(conversations).Bucket(key(session))
		if b == nil || b.Get([]byte(msg.ID)) == nil {
			return fmt.Errorf("message %s is not in the conversation", msg.ID)
		}
		return put(b, msg)
	})
	if err != nil {
		return fmt.Errorf("update chat: %w", err)
	}
	return nil
}

// Remove takes a message back out, for a queued prompt you decided against.
// Removing what is not there is not an error.
func (s *Store) Remove(session, id string) error {
	if s == nil || id == "" {
		return nil
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(conversations).Bucket(key(session))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(id))
	})
	if err != nil {
		return fmt.Errorf("remove chat message: %w", err)
	}
	return nil
}

// List returns a session's conversation in order.
func (s *Store) List(session string) []Message {
	if s == nil {
		return nil
	}
	var out []Message
	_ = s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(conversations).Bucket(key(session))
		if b == nil {
			return nil
		}
		return b.ForEach(func(_, v []byte) error {
			var msg Message
			if err := json.Unmarshal(v, &msg); err == nil {
				out = append(out, msg)
			}
			return nil
		})
	})
	return out
}

// Len is how many messages a session holds.
func (s *Store) Len(session string) int {
	if s == nil {
		return 0
	}
	n := 0
	_ = s.db.View(func(tx *bolt.Tx) error {
		if b := tx.Bucket(conversations).Bucket(key(session)); b != nil {
			n = b.Stats().KeyN
		}
		return nil
	})
	return n
}

// Rename moves a conversation to another session id. A fresh agent starts
// under Pending and names itself on its first event; a resumed one can fork to
// a new id. Either way the conversation follows. Anything already under the
// new id comes first, since it was said earlier.
func (s *Store) Rename(from, to string) error {
	if s == nil || from == to {
		return nil
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		root := tx.Bucket(conversations)
		src := root.Bucket(key(from))
		if src == nil {
			return nil
		}
		dst, err := root.CreateBucketIfNotExists(key(to))
		if err != nil {
			return err
		}
		err = src.ForEach(func(_, v []byte) error {
			var msg Message
			if err := json.Unmarshal(v, &msg); err != nil {
				return nil
			}
			seq, err := dst.NextSequence()
			if err != nil {
				return err
			}
			msg.ID = fmt.Sprintf("%016x", seq)
			return put(dst, msg)
		})
		if err != nil {
			return err
		}
		return root.DeleteBucket(key(from))
	})
	if err != nil {
		return fmt.Errorf("rename chat: %w", err)
	}
	return nil
}

// PutState keeps a blob of server state under name; empty removes it.
func (s *Store) PutState(name string, data []byte) error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(state)
		if len(data) == 0 {
			return b.Delete([]byte(name))
		}
		return b.Put([]byte(name), data)
	})
}

// State reads a blob kept by PutState, or nil.
func (s *Store) State(name string) []byte {
	if s == nil || s.db == nil {
		return nil
	}
	var out []byte
	_ = s.db.View(func(tx *bolt.Tx) error {
		if v := tx.Bucket(state).Get([]byte(name)); v != nil {
			out = append([]byte(nil), v...)
		}
		return nil
	})
	return out
}

// Clear forgets a session's conversation.
func (s *Store) Clear(session string) error {
	if s == nil {
		return nil
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		err := tx.Bucket(conversations).DeleteBucket(key(session))
		if errors.Is(err, berrors.ErrBucketNotFound) {
			return nil
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("clear chat: %w", err)
	}
	return nil
}

// Sessions lists the session ids that have a conversation here.
func (s *Store) Sessions() []string {
	if s == nil {
		return nil
	}
	var out []string
	_ = s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(conversations).ForEachBucket(func(k []byte) error {
			if id := string(k); id != pendingKey {
				out = append(out, id)
			}
			return nil
		})
	})
	return out
}

// Path is where a checkout's conversations live. A worktree has its own git
// dir, so its conversations are its own; every session in a checkout shares
// the one file, keyed by session id inside it.
func Path(gitDir string) string {
	return filepath.Join(statedir.Git(gitDir), "chat.db")
}

// pendingKey is the bucket a session that has not named itself writes to. A
// bbolt bucket cannot have an empty name.
const pendingKey = "\x00pending"

func key(session string) []byte {
	if session == Pending {
		return []byte(pendingKey)
	}
	return []byte(session)
}

func put(b *bolt.Bucket, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return b.Put([]byte(msg.ID), data)
}

func clip(s string) string {
	if len(s) <= maxOutput {
		return s
	}
	return s[:maxOutput] + "\n… (truncated)"
}
