package launcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
)

const (
	convHere  = "11111111-1111-4111-8111-111111111111"
	convLoose = "22222222-2222-4222-8222-222222222222"
)

var notInProjectName = regexp.MustCompile(`[^A-Za-z0-9]`)

// importer is a launcher that starts nothing, and what it was asked to start.
type importer struct {
	*Server
	started [][3]string
	moved   []string
	refuse  error
}

func newImporter(t *testing.T) *importer {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	it := &importer{Server: newTestLauncher(t)}
	it.open = func(_ context.Context, dir, agent, _, id string) (string, error) {
		it.started = append(it.started, [3]string{dir, agent, id})
		return "http://127.0.0.1:1/?t=x", nil
	}
	wasMove, wasWait := moveAgent, announcedWait
	moveAgent = func(_ context.Context, _, id string) error {
		it.moved = append(it.moved, id)
		return it.refuse
	}
	announcedWait = 10 * time.Millisecond
	t.Cleanup(func() { moveAgent, announcedWait = wasMove, wasWait })
	return it
}

func TestClaudesConversationsAreListedWithHowTheyStand(t *testing.T) {
	it := newImporter(t)
	checkout, loose := checkoutDir(t), t.TempDir()
	held(t, checkout, convHere, "the one ade is on")
	old := held(t, loose, convLoose, "started outside a checkout")
	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(old, past, past))
	it.served[convHere] = Serving{ID: convHere, Dir: checkout, Port: 1}

	rec := do(t, it.Server, http.MethodGet, "/api/conversations", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var got struct {
		Conversations []Importable `json:"conversations"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Conversations, 2)
	first, second := got.Conversations[0], got.Conversations[1]
	assert.Equal(t, convHere, first.ID)
	assert.Equal(t, "the one ade is on", first.Title)
	assert.Equal(t, checkout, first.Dir)
	assert.True(t, first.Here)
	assert.True(t, first.Checkout)
	assert.False(t, second.Here)
	assert.False(t, second.Checkout, "a session needs a checkout")
}

func TestImportingStartsASessionWhereTheConversationWasHad(t *testing.T) {
	it := newImporter(t)
	checkout := checkoutDir(t)
	held(t, checkout, convHere, "hello")

	rec := do(t, it.Server, http.MethodPost, "/api/conversations/import", fmt.Sprintf(`{"id":%q}`, convHere))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, fmt.Sprintf(`{"session":%q,"dir":%q}`, convHere, checkout), rec.Body.String())
	assert.Equal(t, [][3]string{{checkout, "claude", convHere}}, it.started)
	assert.Equal(t, []string{convHere}, it.moved, "an ade already there is told which conversation")
}

func TestWhatCannotBeImportedIsRefusedInWords(t *testing.T) {
	it := newImporter(t)
	checkout, loose := checkoutDir(t), t.TempDir()
	held(t, checkout, convHere, "hello")
	held(t, loose, convLoose, "hello")
	ask := func(id string) (int, string) {
		rec := do(t, it.Server, http.MethodPost, "/api/conversations/import", fmt.Sprintf(`{"id":%q}`, id))
		return rec.Code, rec.Body.String()
	}

	code, _ := ask("33333333-3333-4333-8333-333333333333")
	assert.Equal(t, http.StatusBadRequest, code, "claude holds nothing by that id")
	code, _ = ask("../../etc/passwd")
	assert.Equal(t, http.StatusBadRequest, code)
	code, said := ask(convLoose)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, said, "not the top of a git checkout")

	openElsewhere(t, convHere)
	code, said = ask(convHere)
	assert.Equal(t, http.StatusConflict, code)
	assert.Contains(t, said, "open in another terminal")
	assert.Empty(t, it.started, "nothing was started for any of them")

	// With an ade on the checkout, the claude that has it open may be ade's own.
	require.NoError(t, session.Write(session.Path(sessions.GitDir(checkout)), session.Record{
		PID: os.Getpid(), URL: "http://127.0.0.1:1/?t=x", Root: checkout, StartedAt: time.Now(),
	}))
	it.probe = func(context.Context, *session.Record) (session.Health, bool) { return session.Health{}, true }
	code, _ = ask(convHere)
	assert.Equal(t, http.StatusOK, code)

	it.refuse = errors.New("its agent is running in its terminal")
	code, said = ask(convHere)
	assert.Equal(t, http.StatusConflict, code)
	assert.Contains(t, said, "its agent is running in its terminal")
}

// checkoutDir is a folder git would call a checkout.
func checkoutDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o750))
	return dir
}

// held writes the transcript claude would keep for a conversation had in dir.
func held(t *testing.T, dir, id, asked string) string {
	t.Helper()
	project := notInProjectName.ReplaceAllString(dir, "-")
	path := filepath.Join(os.Getenv("HOME"), ".claude", "projects", project, id+".jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	line := fmt.Sprintf(`{"type":"user","cwd":%q,"message":{"content":%q}}`, dir, asked)
	require.NoError(t, os.WriteFile(path, []byte(line+"\n"), 0o600))
	return path
}

// openElsewhere is claude's record of a living process on a conversation.
func openElsewhere(t *testing.T, id string) {
	t.Helper()
	path := filepath.Join(os.Getenv("HOME"), ".claude", "sessions", "1.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	body := fmt.Sprintf(`{"pid":%d,"sessionId":%q}`, os.Getpid(), id)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
}
