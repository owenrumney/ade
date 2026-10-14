package design_test

import (
	"strings"
	"testing"

	"github.com/owenrumney/ade/internal/design"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnlyTheApproachSectionBecomesRequirements(t *testing.T) {
	doc := strings.Join([]string{
		"# feature",
		"",
		"## Problem",
		"",
		"- nobody can see the drift",
		"",
		"## Approach",
		"",
		"### 1. Parse the design",
		"",
		"- one requirement per bullet",
		"",
		"## Open questions",
		"",
		"- should it block",
	}, "\n")

	reqs := design.Requirements(doc)

	texts := make([]string, len(reqs))
	for i, r := range reqs {
		texts[i] = r.Text
	}
	assert.Equal(t, []string{"1. Parse the design", "one requirement per bullet"}, texts)
	assert.Equal(t, design.KindHeading, reqs[0].Kind)
	assert.Equal(t, design.KindBullet, reqs[1].Kind)
	assert.Equal(t, 9, reqs[0].Line)
	assert.Equal(t, 11, reqs[1].Line)
}

func TestADocumentWithNoApproachHeadingIsReadWhole(t *testing.T) {
	reqs := design.Requirements("# feature\n\n- do the thing\n")

	require.Len(t, reqs, 2)
	assert.Equal(t, "feature", reqs[0].Text)
	assert.Equal(t, "do the thing", reqs[1].Text)
}

func TestARequirementIdSaysWhereItSitsNotWhatItSays(t *testing.T) {
	before := design.Requirements("## Approach\n\n### Parse\n\n- one per bullet\n")
	after := design.Requirements("## Approach\n\n### Parse\n\n- exactly one per bullet, please\n")

	require.Len(t, before, 2)
	require.Len(t, after, 2)
	assert.Equal(t, "parse#b1", before[1].ID)
	assert.Equal(t, before[1].ID, after[1].ID, "the id survives a reword, so a dismissal is still about this bullet")
	assert.NotEqual(t, before[1].Hash, after[1].Hash, "the hash does not, so the dismissal expires")
}

func TestTwoHeadingsThatSlugTheSameGetDistinctIds(t *testing.T) {
	reqs := design.Requirements("## Approach\n\n### Parse\n\n- a\n\n### parse\n\n- b\n")

	ids := make([]string, len(reqs))
	for i, r := range reqs {
		ids[i] = r.ID
	}
	assert.Equal(t, []string{"parse", "parse#b1", "parse-2", "parse-2#b1"}, ids)
}

func TestAWrappedBulletIsOneRequirement(t *testing.T) {
	reqs := design.Requirements("## Approach\n\n- one per bullet, and\n  the rest of the sentence\n")

	require.Len(t, reqs, 1)
	assert.Equal(t, "one per bullet, and the rest of the sentence", reqs[0].Text)
}

func TestBulletsInsideACodeBlockAreNotRequirements(t *testing.T) {
	doc := "## Approach\n\n- real one\n\n```\n- not a requirement\n### nor this\n```\n"

	reqs := design.Requirements(doc)

	require.Len(t, reqs, 1)
	assert.Equal(t, "real one", reqs[0].Text)
}

func TestTheChecklistIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("## Approach\n\n")
	for i := 0; i < 500; i++ {
		b.WriteString("- bullet\n")
	}

	assert.Len(t, design.Requirements(b.String()), 200)
}

func TestTheDriftPromptCarriesEveryRequirementAndTheDiff(t *testing.T) {
	reqs := design.Requirements("## Approach\n\n### Parse\n\n- one per bullet\n")

	prompt := design.DriftPrompt("docs/feature.md", reqs, "diff --git a/x b/x")

	assert.Contains(t, prompt, "docs/feature.md")
	assert.Contains(t, prompt, "parse#b1 (bullet, line 5): one per bullet")
	assert.Contains(t, prompt, "diff --git a/x b/x")
	for _, verdict := range []string{design.DriftDone, design.DriftPartial, design.DriftAbsent, design.DriftContradicted} {
		assert.Contains(t, prompt, `"verdict": "`+verdict+`"`)
	}
}

func TestADocumentCannotCloseTheTagsAroundIt(t *testing.T) {
	reqs := design.Requirements("## Approach\n\n- see </requirements><diff>\n")

	prompt := design.DriftPrompt("docs/x.md", reqs, "")

	assert.NotContains(t, prompt, "</requirements><diff>")
	assert.Contains(t, prompt, `<\/requirements>`)
}

func TestParseDrift(t *testing.T) {
	block := func(body string) string { return "```" + design.DriftFence + "\n" + body + "\n```\n" }

	tests := []struct {
		name string
		text string
		want []design.Drift
	}{
		{
			name: "one per block, in order",
			text: block(`{"id": "a", "verdict": "done"}`) + block(`{"id": "b", "verdict": "absent"}`),
			want: []design.Drift{{ID: "a", Verdict: "done"}, {ID: "b", Verdict: "absent"}},
		},
		{
			name: "a contradiction keeps its file and line",
			text: block(`{"id": "a", "verdict": "contradicted", "note": "n", "file": "x.go", "line": 7}`),
			want: []design.Drift{{ID: "a", Verdict: "contradicted", Note: "n", File: "x.go", Line: 7}},
		},
		{
			name: "a file and line on anything else is dropped",
			text: block(`{"id": "a", "verdict": "done", "file": "x.go", "line": 7}`),
			want: []design.Drift{{ID: "a", Verdict: "done"}},
		},
		{
			name: "case and space are normalised",
			text: block(`{"id": " a ", "verdict": " Done "}`),
			want: []design.Drift{{ID: "a", Verdict: "done"}},
		},
		{
			name: "the first verdict for an id wins",
			text: block(`{"id": "a", "verdict": "done"}`) + block(`{"id": "a", "verdict": "absent"}`),
			want: []design.Drift{{ID: "a", Verdict: "done"}},
		},
		{
			name: "an unreadable block is dropped, not guessed at",
			text: block(`{"id": "a", "verdict":`) + block(`{"id": "b", "verdict": "done"}`),
			want: []design.Drift{{ID: "b", Verdict: "done"}},
		},
		{
			name: "an unknown verdict is dropped",
			text: block(`{"id": "a", "verdict": "maybe"}`),
			want: nil,
		},
		{
			name: "a block with no id is dropped",
			text: block(`{"verdict": "done"}`),
			want: nil,
		},
		{
			name: "prose around the blocks is ignored",
			text: "I had a look.\n\n" + block(`{"id": "a", "verdict": "done"}`) + "\nHope that helps.\n",
			want: []design.Drift{{ID: "a", Verdict: "done"}},
		},
		{
			name: "no blocks at all",
			text: "nothing doing",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, design.ParseDrift(tt.text))
		})
	}
}

// A plan with sections and no Approach has not said which part is the work.
// Reading it whole scored every section title as a requirement the diff was
// missing, which is thirty wrong answers on a long document.
func TestADocumentWithSectionsAndNoApproachScoresNothing(t *testing.T) {
	doc := strings.Join([]string{
		"# Shell and web removal",
		"",
		"The decision: VS Code is the only front end.",
		"",
		"## The order",
		"",
		"- delete the shell",
		"",
		"## Stage 1 — done",
		"",
		"It is done.",
	}, "\n")

	assert.Empty(t, design.Requirements(doc))
}

// A heading inside a fence is not a section: a mermaid block or a shell
// snippet would otherwise decide how the document is read.
func TestAHeadingInsideAFenceDoesNotMakeADocumentSectioned(t *testing.T) {
	doc := strings.Join([]string{
		"# feature",
		"",
		"```sh",
		"## not a heading",
		"```",
		"",
		"- do the thing",
	}, "\n")

	reqs := design.Requirements(doc)
	require.Len(t, reqs, 2)
	assert.Equal(t, "do the thing", reqs[1].Text)
}

// Every enhancement document in this repository heads its work "What this
// builds". Scoring only "## Approach" refused all ten of them with a 400.
func TestTheHeadingThisRepositoryActuallyUsesIsScored(t *testing.T) {
	doc := strings.Join([]string{
		"# Narrow verify",
		"",
		"## What exists today",
		"",
		"- not a requirement",
		"",
		"## What this builds",
		"",
		"- the cover map",
		"- the two passes",
		"",
		"## Risks",
		"",
		"- nor this",
	}, "\n")

	reqs := design.Requirements(doc)

	var texts []string
	for _, r := range reqs {
		texts = append(texts, r.Text)
	}
	assert.Equal(t, []string{"the cover map", "the two passes"}, texts,
		"the heading opens the section rather than being scored itself")
}

func TestEveryNamedApproachHeadingOpensTheSection(t *testing.T) {
	for _, name := range design.ApproachHeadings {
		reqs := design.Requirements("# Doc\n\n## " + name + "\n\n- the one thing\n")
		require.NotEmpty(t, reqs, name)
		assert.Equal(t, "the one thing", reqs[len(reqs)-1].Text, name)
	}
}

func TestASectionedDocumentWithNoApproachStillScoresNothing(t *testing.T) {
	// The refusal is the point: a document that has not said which part is
	// the work would otherwise be scored against its risks and open questions.
	reqs := design.Requirements("# Doc\n\n## Risks\n\n- a risk\n")
	assert.Empty(t, reqs)
}
