package review

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noteBlock(quote, note string) string {
	return "```ade-note\n{\"quote\": \"" + quote + "\", \"note\": \"" + note + "\"}\n```\n"
}

func TestNotesArriveAsTheyAreWritten(t *testing.T) {
	s := NewNoteStreamer()
	assert.Empty(t, s.Push("I have three points.\n\n"))

	got := s.Push(noteBlock("the sidecar", "keyed on what?"))
	require.Len(t, got, 1)
	assert.Equal(t, "the sidecar", got[0].Quote)
	assert.Equal(t, "keyed on what?", got[0].Note)
}

func TestANoteBlockSplitAcrossEventsStillArrives(t *testing.T) {
	s := NewNoteStreamer()
	assert.Empty(t, s.Push("```ade-note\n{\"quote\": \"the sidecar\", "))
	got := s.Push("\"note\": \"keyed on what?\"}\n```\n")
	require.Len(t, got, 1)
	assert.Equal(t, "keyed on what?", got[0].Note)
}

func TestTheSamePointIsFiledOnce(t *testing.T) {
	s := NewNoteStreamer()
	require.Len(t, s.Push(noteBlock("the sidecar", "keyed on what?")), 1)
	// The whole buffer is rescanned on every chunk, so without the seen set
	// every block files again on the next event.
	assert.Empty(t, s.Push("and that is all.\n"))
	assert.Empty(t, s.Push(noteBlock("the sidecar", "keyed on what?")))
}

func TestANoteWithNothingToSayOrNothingToSitOnIsDropped(t *testing.T) {
	s := NewNoteStreamer()
	assert.Empty(t, s.Push(noteBlock("", "keyed on what?")))
	assert.Empty(t, s.Push(noteBlock("the sidecar", "  ")))
	assert.Empty(t, s.Push("```ade-note\nnot json at all\n```\n"))
}

func TestAFindingIsNotANote(t *testing.T) {
	s := NewNoteStreamer()
	assert.Empty(t, s.Push("```ade-finding\n{\"file\":\"a.go\",\"line\":1,\"headline\":\"x\"}\n```\n"))

	// And the other way round: a note block is not a finding.
	assert.Empty(t, NewStreamer().Push(noteBlock("the sidecar", "keyed on what?")))
}

func TestBothKindsOfBlockLeaveTheReply(t *testing.T) {
	reply := "Three points.\n\n" + noteBlock("the sidecar", "keyed on what?") +
		"\n```ade-finding\n{\"file\":\"a.go\",\"line\":1,\"headline\":\"x\"}\n```\n"
	assert.Equal(t, "Three points.", StripStream(reply))
}
