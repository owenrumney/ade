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
	"github.com/owenrumney/ade/internal/bushistory"
)

func historyWith(t *testing.T, s *Server, who string) []bushistory.Entry {
	t.Helper()
	rec := do(t, s, http.MethodGet, "/api/bus/history?with="+who, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got struct {
		Said []bushistory.Entry `json:"said"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	return got.Said
}

func TestAQuestionPutToYouIsKept(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)
	s.onBusFrame(context.Background(), askFrame("msg_1", "Is dev RDS safe to resize?", "yes", "no"))

	said := historyWith(t, s, "liam@infracost.io")
	require.Len(t, said, 1)
	assert.Equal(t, "Is dev RDS safe to resize?", said[0].Question)
	assert.Equal(t, []string{"yes", "no"}, said[0].Options)
	assert.False(t, said[0].Mine)
	assert.True(t, said[0].Agent, "liam's agent asked, not liam")
}

// A question to anyone is asked of nobody in particular, so it is filed with
// whoever answers: their history reads as asked and answered.
func TestAnAnswerIsFiledWithWhoeverGaveIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)
	s.bus.mu.Lock()
	s.bus.asked["msg_7"] = askedBy{Kind: "ask", To: "anyone", Question: "can you hear me?", At: time.Now()}
	s.bus.mu.Unlock()

	asked, _ := json.Marshal(agentbus.AskBody{Question: "can you hear me?"})
	answer, _ := json.Marshal(agentbus.AnswerBody{Option: "yes", Text: "loud and clear"})
	s.onBusFrame(context.Background(), stateFrame("msg_7",
		agentbus.Frame{Type: agentbus.TypeAsk, Body: asked},
		agentbus.Frame{Type: agentbus.TypeAnswer, Body: answer, From: &agentbus.From{User: "liam@infracost.io"}}))

	said := historyWith(t, s, "liam@infracost.io")
	require.Len(t, said, 2)
	assert.Equal(t, bushistory.KindAsk, said[0].Kind)
	assert.True(t, said[0].Mine)
	assert.Equal(t, "can you hear me?", said[0].Question)
	assert.Equal(t, bushistory.KindAnswer, said[1].Kind)
	assert.Equal(t, "loud and clear", said[1].Text)

	// The bus sends the same state again on a reconnect.
	s.onBusFrame(context.Background(), stateFrame("msg_7",
		agentbus.Frame{Type: agentbus.TypeAsk, Body: asked},
		agentbus.Frame{Type: agentbus.TypeAnswer, Body: answer, From: &agentbus.From{User: "liam@infracost.io"}}))
	assert.Len(t, historyWith(t, s, "liam@infracost.io"), 2)
}

// Dismiss clears the notification. The conversation is not the notification.
func TestDismissingAnAnswerKeepsTheConversation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)
	s.onBusFrame(context.Background(), askFrame("msg_1", "Is dev RDS safe to resize?", "yes", "no"))

	rec := do(t, s, http.MethodPost, "/api/bus/dismiss", `{"id":"msg_1"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	if state := s.currentBus(); state != nil {
		assert.Empty(t, state.Offers, "the notification is gone")
	}
	assert.Len(t, historyWith(t, s, "liam@infracost.io"), 1)
}

func TestHistoryHasToSayWho(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)
	for _, who := range []string{"", "..", "*"} {
		rec := do(t, s, http.MethodGet, "/api/bus/history?with="+who, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, who)
	}
	assert.Empty(t, historyWith(t, s, "nobody@x.io"))
}
