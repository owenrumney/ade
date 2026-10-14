package models

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const answer = `{"items":[
 {"id":"anthropic:claude-opus-5","display_name":"Claude Opus 5","provider":{"id":"anthropic"},
  "context_window":1000000,"latest":true,"prices":{"input_tokens_per_1000000":"10","output_tokens_per_1000000":"50"}},
 {"id":"openai:gpt-5.6-terra","display_name":"GPT-5.6 Terra","provider":{"id":"openai"},
  "context_window":1050000,"prices":{"input_tokens_per_1000000":"2","output_tokens_per_1000000":"12"}},
 {"id":"google:gemini","display_name":"Gemini","provider":{"id":"google"},"context_window":1000000}
]}`

func TestTheCatalogueKeepsOnlyAgentsAdeRuns(t *testing.T) {
	got, err := decode(strings.NewReader(answer))
	require.NoError(t, err)

	// A provider ade has no bridge for is not a model you can start on.
	require.Len(t, got, 2)
	byID := map[string]Model{}
	for _, one := range got {
		byID[one.ID] = one
	}

	// The provider prefix is cost.dev's key, not what the CLI is given.
	opus := byID["claude-opus-5"]
	assert.Equal(t, "claude", opus.Agent)
	assert.Equal(t, 1_000_000, opus.Context)
	assert.Equal(t, "10", opus.In)
	assert.True(t, opus.Latest)

	assert.Equal(t, "codex", byID["gpt-5.6-terra"].Agent)
}

func TestTheCuratedAliasesSurviveASync(t *testing.T) {
	// Claude Code resolves "opus" itself and no price list carries it, so a
	// sync must add to the built-in list rather than replace it.
	fetched, err := decode(strings.NewReader(answer))
	require.NoError(t, err)
	merged := merge(builtIn, fetched)

	ids := map[string]int{}
	for _, one := range merged {
		ids[one.ID]++
	}
	for _, alias := range []string{"opus", "sonnet", "haiku", "claude-opus-5[1m]"} {
		assert.Equalf(t, 1, ids[alias], "%s was lost or doubled", alias)
	}
	assert.Equal(t, 1, ids["claude-opus-5"], "a fetched model was not added")
	// The built-in gpt-5.6-terra and the fetched one are the same model.
	assert.Equal(t, 1, ids["gpt-5.6-terra"], "a fetched duplicate was kept")
}

func TestABuiltCatalogueIsWhatAnUnsyncedMachineOffers(t *testing.T) {
	built := Built()
	assert.Nil(t, built.SyncedAt, "the built-in list claims no sync")
	require.NotEmpty(t, built.Models)
	for _, one := range built.Models {
		assert.Contains(t, []string{"claude", "codex"}, one.Agent, one.ID)
	}
}
