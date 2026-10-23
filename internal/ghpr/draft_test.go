package ghpr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// graphql() runs outside the `writable` allowlist, as graphqlRead does, so
// this check is the whole guard on what a mutation may do.
func TestOnlyTheThreeReviewMutationsMayRun(t *testing.T) {
	for _, query := range []string{queryPullRequestID, queryPendingReview, mutationStartDraft, mutationAddThread, mutationSubmit} {
		assert.NoError(t, allowedDocument(query))
	}

	refused := map[string]string{
		"a mutation nobody allowed": `mutation{ deleteRepository(input:{repositoryId:"x"}){ clientMutationId } }`,
		"one allowed, one not":      `mutation{ submitPullRequestReview(input:{}){ id } mergePullRequest(input:{}){ id } }`,
		"a mutation with no field":  `mutation { }`,
		"a query that hides one":    `query{ x } mutation{ mergePullRequest(input:{}){ id } }`,
		"merge dressed as an add":   `mutation($x:ID!){ mergePullRequest(input:{pullRequestId:$x}){ clientMutationId } }`,
	}
	for why, query := range refused {
		assert.Error(t, allowedDocument(query), why)
	}
}

func TestAnInlineCommentNeedsSomewhereToGo(t *testing.T) {
	ok := InlineComment{Path: "a.go", Line: 3, Side: "RIGHT", Body: "x"}
	require.NoError(t, ok.ok())

	for why, c := range map[string]InlineComment{
		"no file": {Line: 3, Body: "x"},
		"no line": {Path: "a.go", Body: "x"},
		"no text": {Path: "a.go", Line: 3, Body: "  "},
	} {
		assert.Error(t, c.ok(), why)
	}
}

// The enum is GitHub's, and the store spells a side its own way.
func TestTheSideIsTheOneGitHubNames(t *testing.T) {
	assert.Equal(t, "LEFT", InlineComment{Side: SideLeft}.graphSide())
	assert.Equal(t, "LEFT", InlineComment{Side: "LEFT"}.graphSide())
	assert.Equal(t, "RIGHT", InlineComment{Side: "right"}.graphSide())
	assert.Equal(t, "RIGHT", InlineComment{Side: ""}.graphSide())
}

func TestOnlyAVerdictFinishesAReview(t *testing.T) {
	for _, event := range []string{EventApprove, EventRequestChanges, EventComment} {
		assert.NotContains(t, SubmitDraftReview(t.Context(), "", "", event, "").Error(),
			"unknown review event", event)
	}
	err := SubmitDraftReview(t.Context(), "", "r1", "DISMISS", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown review event")
}
