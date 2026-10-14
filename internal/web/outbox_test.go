package web

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/chat"
)

// onePNG is the smallest thing http.DetectContentType calls a PNG.
var onePNG = []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 24))

// drop writes a file into the server's outbox.
func drop(t *testing.T, s *Server, name string, data []byte) string {
	t.Helper()
	dir := s.outboxPath()
	require.NotEmpty(t, dir)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

// replied is a conversation with a turn to hang pictures off.
func replied(t *testing.T, s *Server) {
	t.Helper()
	s.say(ChatMessage{Kind: chat.KindYou, Body: "show me"})
	s.say(ChatMessage{Kind: chat.KindAgent, Body: "here"})
}

func TestAPictureInTheOutboxLandsOnTheReply(t *testing.T) {
	s := newTestServer(t)
	replied(t, s)
	drop(t, s, "shot.png", onePNG)

	s.attachOutbox()

	messages := s.conversation()
	require.Len(t, messages, 2)
	require.Len(t, messages[1].Images, 1)
	assert.Equal(t, "image/png", messages[1].Images[0].MediaType)
	assert.Equal(t, base64.StdEncoding.EncodeToString(onePNG), messages[1].Images[0].Data)

	// Taken means gone: a file left behind would arrive again next turn.
	left, err := os.ReadDir(s.outboxPath())
	require.NoError(t, err)
	assert.Empty(t, left)
}

func TestTheOutboxTrustsTheBytesAndNotTheName(t *testing.T) {
	s := newTestServer(t)
	replied(t, s)
	drop(t, s, "a-truthful-png.txt", onePNG)
	drop(t, s, "b-lying-text.png", []byte("this is not a picture at all"))

	s.attachOutbox()

	messages := s.conversation()
	require.Len(t, messages[1].Images, 1, "the text file named .png was taken")
	assert.Equal(t, "image/png", messages[1].Images[0].MediaType)

	left, err := os.ReadDir(s.outboxPath())
	require.NoError(t, err)
	assert.Empty(t, left, "a rejected file has to be cleared too, or it wedges the outbox")
}

func TestTheOutboxDropsWhatItCannotUse(t *testing.T) {
	s := newTestServer(t)
	replied(t, s)
	drop(t, s, "a-empty.png", nil)
	drop(t, s, "b-huge.png", append(onePNG, make([]byte, maxImageBytes)...))
	drop(t, s, "c-fine.png", onePNG)

	s.attachOutbox()

	require.Len(t, s.conversation()[1].Images, 1, "only the usable one")
	left, err := os.ReadDir(s.outboxPath())
	require.NoError(t, err)
	assert.Empty(t, left)
}

func TestTheOutboxTakesFourAtMostAndInNameOrder(t *testing.T) {
	s := newTestServer(t)
	replied(t, s)
	// Distinguishable by length, so the order they arrive in can be asserted.
	for i, name := range []string{"1.png", "2.png", "3.png", "4.png", "5.png"} {
		drop(t, s, name, append(onePNG, make([]byte, i)...))
	}

	s.attachOutbox()

	images := s.conversation()[1].Images
	require.Len(t, images, maxImages)
	for i, img := range images {
		want := base64.StdEncoding.EncodeToString(append(onePNG, make([]byte, i)...))
		assert.Equalf(t, want, img.Data, "picture %d is out of order", i)
	}

	left, err := os.ReadDir(s.outboxPath())
	require.NoError(t, err)
	assert.Empty(t, left, "the fifth is dropped, not held over")
}

// The cap is silent everywhere else, and a full outbox showing the same four
// pictures every turn is what that silence looks like from the chat.
func TestTheOutboxSaysWhatItCouldNotShow(t *testing.T) {
	s := newTestServer(t)
	replied(t, s)
	for _, name := range []string{"1.png", "2.png", "3.png", "4.png", "5.png", "6.png"} {
		drop(t, s, name, onePNG)
	}

	s.attachOutbox()

	var notes []string
	for _, message := range s.conversation() {
		if message.Kind == chat.KindNote {
			notes = append(notes, message.Body)
		}
	}
	require.Len(t, notes, 1, "one note, naming what went")
	assert.Contains(t, notes[0], "2 more")
	assert.Contains(t, notes[0], "5.png, 6.png")
}

func TestAnOutboxUnderTheCapSaysNothing(t *testing.T) {
	s := newTestServer(t)
	replied(t, s)
	drop(t, s, "shot.png", onePNG)

	s.attachOutbox()

	for _, message := range s.conversation() {
		assert.NotEqual(t, chat.KindNote, message.Kind, "nothing was dropped")
	}
}

func TestAnEmptyOutboxChangesNothing(t *testing.T) {
	s := newTestServer(t)
	replied(t, s)

	// No directory at all, which is the ordinary case.
	s.attachOutbox()
	assert.Empty(t, s.conversation()[1].Images)

	// And one that exists but holds nothing.
	require.NoError(t, os.MkdirAll(s.outboxPath(), 0o750))
	s.attachOutbox()
	assert.Empty(t, s.conversation()[1].Images)
}

func TestADirectoryInTheOutboxIsLeftAlone(t *testing.T) {
	s := newTestServer(t)
	replied(t, s)
	require.NoError(t, os.MkdirAll(filepath.Join(s.outboxPath(), "nested"), 0o750))
	drop(t, s, "shot.png", onePNG)

	s.attachOutbox()

	require.Len(t, s.conversation()[1].Images, 1)
	assert.DirExists(t, filepath.Join(s.outboxPath(), "nested"))
}

func TestTheOutboxGoesToTheMainAgentsOwnReply(t *testing.T) {
	s := newTestServer(t)
	s.say(ChatMessage{Kind: chat.KindYou, Body: "show me"})
	s.say(ChatMessage{Kind: chat.KindAgent, Body: "the main agent"})
	// A named agent's message is newer, and is not what the turn replied.
	s.say(ChatMessage{Kind: chat.KindAgent, Author: "security", Body: "beside it"})
	drop(t, s, "shot.png", onePNG)

	s.attachOutbox()

	messages := s.conversation()
	require.Len(t, messages, 3)
	assert.Len(t, messages[1].Images, 1, "the main agent's reply carries it")
	assert.Empty(t, messages[2].Images)
}

func TestTheOutboxAndThePreviewBothLandOnOneReply(t *testing.T) {
	s := newTestServer(t)
	replied(t, s)

	// Both writers read the message, append, and write it back. Serialised, or
	// whichever finished second would drop the other's picture.
	s.attachImages(s.agent.Current(), []chat.Image{{MediaType: "image/png", Data: "YQ=="}})
	s.attachImages(s.agent.Current(), []chat.Image{{MediaType: "image/png", Data: "Yg=="}})

	images := s.conversation()[1].Images
	require.Len(t, images, 2)
	assert.Equal(t, "YQ==", images[0].Data)
	assert.Equal(t, "Yg==", images[1].Data)
}

func TestASandboxedSessionStillGetsItsPictures(t *testing.T) {
	s := newTestServer(t)
	replied(t, s)
	s.agent.setSandbox(true)
	require.True(t, s.agent.State().Sandbox)
	drop(t, s, "shot.png", onePNG)

	// The preview capture skips a container because its dev server is not
	// reachable from here. A file on shared disk has no such problem, and this
	// is the session that most needs the outbox.
	s.attachOutbox()

	require.Len(t, s.conversation()[1].Images, 1)
}

func TestPicturesForASessionThatMovedOnAreDropped(t *testing.T) {
	s := newTestServer(t)
	replied(t, s)

	s.attachImages("a-session-that-is-no-longer-current", []chat.Image{
		{MediaType: "image/png", Data: base64.StdEncoding.EncodeToString(onePNG)},
	})

	assert.Empty(t, s.conversation()[1].Images, "they belonged to another conversation")
}

// The rule in the system prompt tells the agent to write into $ADE_OUTBOX, so
// the variable has to be set and the directory has to exist before it looks.
func TestTheAgentIsToldWhereTheOutboxIs(t *testing.T) {
	s := newTestServer(t)
	dir := s.outboxEnv()
	require.Equal(t, s.outboxPath(), dir)
	assert.DirExists(t, dir)
	assert.Equal(t, dir, s.agent.outboxDir())
	assert.Contains(t, bridge.MainSystemPrompt(""), bridge.OutboxEnv)
}

// A sandboxed agent writes into $ADE_OUTBOX, and the path has to be the same
// string on both sides: the directory holding it is mounted where it already is.
func TestASandboxedRunIsGivenTheOutbox(t *testing.T) {
	s := newTestServer(t)
	// The spec writes a container home, so it must not be the user's own.
	t.Setenv("HOME", t.TempDir())
	dir := s.outboxEnv()
	require.NotEmpty(t, dir)

	spec, _, err := s.agent.specFor(context.Background(), boxInputs{
		provider: "codex", dir: s.space().homeRoot, gitDir: s.space().gitDir,
	})
	require.NoError(t, err)
	assert.Equal(t, dir, spec.Env[bridge.OutboxEnv])

	mounted := false
	for _, mount := range spec.Mounts {
		if mount.Host == mount.Guest && mount.RW && strings.HasPrefix(dir, mount.Host) {
			mounted = true
		}
	}
	assert.True(t, mounted, "the outbox has to be writable at its own path inside the container")
}

// A runner nobody wired an outbox onto answers nothing rather than panicking:
// the one-shots build their own.
func TestARunnerWithNoOutboxSaysSo(t *testing.T) {
	assert.Empty(t, (&agentRunner{}).outboxDir())
}
