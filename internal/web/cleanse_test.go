package web

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/cleanse"
	"github.com/owenrumney/ade/internal/logs"
	"github.com/owenrumney/ade/internal/settings"
)

// longReply is prose the gate sends to the model, with a block it must keep.
var longReply = strings.Repeat("The retry loop resets its counter on success, which the old test never covered. ", 4) +
	"\n\n```sh\ngo test ./internal/retry/\n```"

const shortReply = "Counter resets on success. The old test missed it.\n\n```sh\ngo test ./internal/retry/\n```"

func TestALongReplyIsRewrittenShortBesideTheOriginal(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	calls := sharpenAnswers(t, func(opts bridge.OneShotOpts) (*bridge.Turn, error) {
		assert.Equal(t, cleanse.Model, opts.Model)
		assert.Equal(t, cleanse.Role(), opts.SystemPrompt)
		assert.Equal(t, []string{}, opts.Tools, "a cleanser with tools is an agent")
		assert.True(t, opts.NoMCP)
		assert.True(t, opts.NoSessionPersistence)
		assert.Contains(t, opts.Prompt, "resets its counter on success")
		return &bridge.Turn{Text: shortReply}, nil
	})

	s.writeEvent(kindChat, bridge.Event{Text: longReply})
	s.cleansing.Wait()

	msgs := messagesOfKind(s.conversation(), chat.KindAgent)
	require.Len(t, msgs, 1)
	assert.Equal(t, strings.TrimSpace(longReply), msgs[0].Body, "the body is the truth and is never rewritten")
	assert.Equal(t, shortReply, msgs[0].Short)
	assert.Contains(t, msgs[0].ShortHTML, "<code")
	assert.Equal(t, 1, *calls)
}

func TestANamedAgentsReplyIsCleansedAndAReviewsIsNot(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	calls := sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		return &bridge.Turn{Text: shortReply}, nil
	})

	reply := s.say(ChatMessage{Kind: chat.KindAgent, Author: "architect", Run: runSubagent, Body: longReply, HTML: s.replyHTML(longReply)})
	s.cleanse(reply)
	s.cleansing.Wait()
	assert.Equal(t, 1, *calls)

	review := s.say(ChatMessage{Kind: chat.KindAgent, Author: "security", Run: runSubagent, Review: true, Body: longReply})
	s.cleanse(review)
	s.cleansing.Wait()
	assert.Equal(t, 1, *calls, "a review's prose is the argument for its findings")

	var shorts []string
	for _, msg := range messagesOfKind(s.conversation(), chat.KindAgent) {
		shorts = append(shorts, msg.Short)
	}
	assert.Equal(t, []string{shortReply, ""}, shorts)
}

// A short reply is the one that started this: "cheapRunner is not boxed" is
// 45 characters and unreadable. A run's own output is still never rewritten.
func TestAShortReplyIsRewrittenAndARunsOwnOutputIsNot(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	calls := sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		return &bridge.Turn{Text: "The shortening call no longer starts a container."}, nil
	})

	s.writeEvent(kindChat, bridge.Event{Text: "cheapRunner is not boxed, cold slots untouched"})
	s.cleansing.Wait()
	assert.Equal(t, 1, *calls, "the gate has no length floor; this is the reply it exists for")

	s.writeEvent(kindCommitMessage, bridge.Event{Text: longReply})
	s.writeEvent(kindQuiz, bridge.Event{Text: longReply})
	s.cleansing.Wait()
	assert.Equal(t, 1, *calls)

	var shorts []string
	for _, msg := range messagesOfKind(s.conversation(), chat.KindAgent) {
		shorts = append(shorts, msg.Short)
	}
	assert.Equal(t, []string{"The shortening call no longer starts a container.", "", ""}, shorts)
}

func TestTwoRewritesInFlightAtOnceBothLand(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	// Each run waits for the other, so anything serialising them deadlocks.
	var started atomic.Int32
	both := make(chan struct{})
	old := runOneShot
	runOneShot = func(context.Context, bridge.OneShotOpts) (*bridge.Turn, error) {
		if started.Add(1) == 2 {
			close(both)
		}
		select {
		case <-both:
			return &bridge.Turn{Text: shortReply}, nil
		case <-time.After(5 * time.Second):
			return nil, errors.New("the other rewrite never got to run")
		}
	}
	t.Cleanup(func() { runOneShot = old })

	s.writeEvent(kindChat, bridge.Event{Text: longReply})
	s.writeEvent(kindChat, bridge.Event{Text: longReply})
	s.cleansing.Wait()

	msgs := messagesOfKind(s.conversation(), chat.KindAgent)
	require.Len(t, msgs, 2)
	for _, msg := range msgs {
		assert.Equal(t, shortReply, msg.Short)
	}
	assert.Equal(t, int32(2), started.Load())
}

func TestADroppedRewriteSaysWhichGateTurnedItDown(t *testing.T) {
	tests := []struct {
		name   string
		answer func(bridge.OneShotOpts) (*bridge.Turn, error)
		why    string
		lost   string
	}{
		{"unfaithful", func(bridge.OneShotOpts) (*bridge.Turn, error) {
			return &bridge.Turn{Text: "Counter resets on success. Run the tests."}, nil
		}, cleanse.WhyUnfaithful, "go test ./internal/retry/"},
		{"a timeout", func(bridge.OneShotOpts) (*bridge.Turn, error) {
			return nil, context.DeadlineExceeded
		}, cleanse.WhyTimeout, ""},
		{"a failed run", func(bridge.OneShotOpts) (*bridge.Turn, error) {
			return nil, errors.New("claude is not logged in")
		}, cleanse.WhyFailed, ""},
		{"nothing back", func(bridge.OneShotOpts) (*bridge.Turn, error) {
			return &bridge.Turn{Text: ""}, nil
		}, cleanse.WhyEmpty, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t)
			t.Setenv("HOME", t.TempDir())
			ring := pinDefaultLog(t)
			sharpenAnswers(t, tt.answer)

			s.writeEvent(kindChat, bridge.Event{Text: longReply})
			s.cleansing.Wait()

			line := dropLine(t, ring)
			assert.Equal(t, tt.why, line["why"])
			assert.Equal(t, tt.lost, line["lost"])
			assert.NotEmpty(t, line["id"])
			assert.NotEmpty(t, line["ms"], "how long it took is what separates a timeout from a refusal")
		})
	}
}

// pinDefaultLog makes a ring the default logger for one test, so a Warn can be read back.
func pinDefaultLog(t *testing.T) *logs.Ring {
	t.Helper()
	was := slog.Default()
	ring := logs.New(nil)
	slog.SetDefault(slog.New(ring))
	t.Cleanup(func() { slog.SetDefault(was) })
	return ring
}

// dropLine is the cleanse's one Warn, read by attribute.
func dropLine(t *testing.T, ring *logs.Ring) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, rec := range ring.Since(0) {
		if rec.Msg != "cleanse dropped" {
			continue
		}
		assert.Equal(t, "WARN", rec.Level, "a drop below Warn is the day the feature looked switched off")
		for _, attr := range rec.Attrs {
			out[attr.Key] = attr.Value
		}
		return out
	}
	t.Fatal("the drop logged nothing, which is the bug this is here for")
	return out
}

func TestTheCleanseTurnedOffRewritesNothing(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, settings.Update(func(into *settings.Settings) error {
		into.CleanseOff = true
		return nil
	}))
	calls := sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		return &bridge.Turn{Text: shortReply}, nil
	})

	s.writeEvent(kindChat, bridge.Event{Text: longReply})
	s.cleansing.Wait()
	assert.Zero(t, *calls)
}

func TestAFailedOrEmptyCleanseLeavesTheReplyAsItWas(t *testing.T) {
	for name, answer := range map[string]func(bridge.OneShotOpts) (*bridge.Turn, error){
		"a failure": func(bridge.OneShotOpts) (*bridge.Turn, error) { return nil, errors.New("claude is not logged in") },
		"nothing":   func(bridge.OneShotOpts) (*bridge.Turn, error) { return &bridge.Turn{Text: ""}, nil },
		"a rewrite that dropped the block": func(bridge.OneShotOpts) (*bridge.Turn, error) {
			return &bridge.Turn{Text: "Counter resets on success. Run the tests."}, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := newTestServer(t)
			t.Setenv("HOME", t.TempDir())
			calls := sharpenAnswers(t, answer)

			s.writeEvent(kindChat, bridge.Event{Text: longReply})
			s.cleansing.Wait()

			msgs := messagesOfKind(s.conversation(), chat.KindAgent)
			require.Len(t, msgs, 1)
			assert.Equal(t, 1, *calls)
			assert.Equal(t, strings.TrimSpace(longReply), msgs[0].Body)
			assert.Empty(t, msgs[0].Short)
			assert.Empty(t, msgs[0].ShortHTML)
		})
	}
}

func TestTheShortFormKeepsWhatLandedOnTheReplyMeanwhile(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("HOME", t.TempDir())
	picture := chat.Image{MediaType: "image/png", Data: "iVBORw0KGgo="}
	sharpenAnswers(t, func(bridge.OneShotOpts) (*bridge.Turn, error) {
		s.attachImages(s.agent.Current(), []chat.Image{picture})
		return &bridge.Turn{Text: shortReply}, nil
	})

	s.writeEvent(kindChat, bridge.Event{Text: longReply})
	s.cleansing.Wait()

	msgs := messagesOfKind(s.conversation(), chat.KindAgent)
	require.Len(t, msgs, 1)
	assert.Equal(t, shortReply, msgs[0].Short)
	assert.Equal(t, []chat.Image{picture}, msgs[0].Images, "the cleanse wrote over a picture attached while it ran")
}

// shortForm is any read or write of a reply's short form in Go.
var shortForm = regexp.MustCompile(`\.Short(HTML)?\b`)

// shortFormOK is every Go file allowed to name it, and why: Body is the record,
// so a transcript or a wiki draft hands a reader what the agent wrote.
var shortFormOK = map[string]string{ //nolint:gochecknoglobals // a table, not state
	"internal/web/cleanse.go":   "writes it; this is the cleanse",
	"internal/gitdiff/blame.go": "a commit's short sha, not a reply",
	"internal/web/blame.go":     "a commit's short sha, not a reply",
	"cmd/ade/why.go":            "a commit's short sha, not a reply",
	"tools/seedchat/main.go":    "seeds a short form for the screenshots",
}

func TestNothingButTheChatPageNamesTheShortForm(t *testing.T) {
	seen := map[string]bool{}
	err := filepath.WalkDir("../..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path) // #nosec G304 -- a walk of this repository
		require.NoError(t, err)
		rel := filepath.ToSlash(strings.TrimPrefix(path, "../../"))
		for i, line := range strings.Split(string(body), "\n") {
			if !shortForm.MatchString(line) {
				continue
			}
			seen[rel] = true
			assert.Containsf(t, shortFormOK, rel,
				"%s:%d reads a reply's short form; Body is the record, so argue for this in the design doc first", rel, i+1)
		}
		return nil
	})
	require.NoError(t, err)
	assert.True(t, seen["internal/web/cleanse.go"], "the scan found no short form at all, so it proves nothing")
}
