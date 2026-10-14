package review

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func verdictBlock(body string) string {
	return "```" + VerdictFence + "\n" + body + "\n```"
}

func TestParseVerdictsReadsEachJudgement(t *testing.T) {
	text := "Judging.\n" +
		verdictBlock(`{"ref":"F1","verdict":"agree"}`) + "\n" +
		verdictBlock(`{"ref":" f2 ","verdict":"DISPUTE","reason":"the nil is impossible here"}`) + "\n" +
		verdictBlock(`{"ref":"F3","verdict":"duplicate-of","of":"f1"}`)

	got := ParseVerdicts(text)
	require.Len(t, got, 3)
	assert.Equal(t, Verdict{Ref: "F1", Verdict: VerdictAgree}, got[0])
	assert.Equal(t, Verdict{Ref: "F2", Verdict: VerdictDispute, Reason: "the nil is impossible here"}, got[1], "case and space are normalised")
	assert.Equal(t, Verdict{Ref: "F3", Verdict: VerdictDuplicate, Of: "F1"}, got[2])
}

func TestParseVerdictsDropsWhatItCannotUse(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"no ref", `{"verdict":"agree"}`},
		{"unknown verdict", `{"ref":"F1","verdict":"escalate"}`},
		{"a dispute with no reason", `{"ref":"F1","verdict":"dispute"}`},
		{"a duplicate of nothing", `{"ref":"F1","verdict":"duplicate-of"}`},
		{"a duplicate of itself", `{"ref":"F1","verdict":"duplicate-of","of":"F1"}`},
		{"not JSON", `nope`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Empty(t, ParseVerdicts(verdictBlock(tc.body)))
		})
	}
}

func TestParseVerdictsKeepsTheFirstJudgementOfAFinding(t *testing.T) {
	got := ParseVerdicts(
		verdictBlock(`{"ref":"F1","verdict":"agree"}`) + "\n" +
			verdictBlock(`{"ref":"F1","verdict":"dispute","reason":"changed my mind"}`))
	require.Len(t, got, 1)
	assert.Equal(t, VerdictAgree, got[0].Verdict)
}

func TestParseVerdictsIgnoresAnUnfencedReply(t *testing.T) {
	assert.Empty(t, ParseVerdicts(`I agree with F1 and dispute F2.`))
}
