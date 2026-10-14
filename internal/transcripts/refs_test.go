package transcripts

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTicketKeys(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{"lower case is normalised", "look at linear fix-541 please", []string{"FIX-541"}},
		{"already upper", "TRK-410 is blocked", []string{"TRK-410"}},
		{"several", "fix-58 then FIX-493", []string{"FIX-58", "FIX-493"}},
		{"inside a branch name", "on feat/fix-448-observe-pr-creation", []string{"FIX-448"}},
		// The guards: these all look like keys but are not.
		{"aws region", "bucket in us-east-2 today", nil},
		{"model version", "running gpt-5.6-terra", nil},
		{"claude model", "claude-opus-4-8 is quick", nil},
		{"encoding", "it is utf-8 encoded", nil},
		{"a date", "shipped 2026-08-13", nil},
		{"cve", "patch CVE-2024 soon", nil},
		{"ipv6", "an ipv-6 address", nil},
		{"no digits", "just-words here", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ticketKeys(tc.body))
		})
	}
}

func TestClassifyURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantKind  RefKind
		wantLabel string
	}{
		{
			"linear issue", "https://linear.app/owenrumney/issue/FIX-541/some-title",
			RefLinear, "FIX-541",
		},
		{
			"github pr", "https://github.com/owenrumney/ade/pull/798",
			RefGitHub, "owenrumney/ade PR #798",
		},
		{
			"github issue", "https://github.com/owenrumney/ade/issues/12",
			RefGitHub, "owenrumney/ade#12",
		},
		{
			"github commit is shortened", "https://github.com/owenrumney/ade/commit/88f4dc28ab46e8bb1c5f4e7b7b0bcc787350d6ad",
			RefGitHub, "owenrumney/ade@88f4dc2",
		},
		{
			"github repo", "https://github.com/sindrets/diffview.nvim",
			RefGitHub, "github.com/sindrets/diffview.nvim",
		},
		{
			"notion drops the page id", "https://www.notion.so/owenrumney/Design-Notes-3b201c60607e803fa4d2e1a3f4c07b8e",
			RefNotion, "Design Notes",
		},
		{
			"notion drops the query", "https://notion.so/owenrumney/Design-Notes-3b201c60607e803fa4d2e1a3f4c07b8e?source=copy_link",
			RefNotion, "Design Notes",
		},
		{
			"anything else", "https://dashboard.example.com/org/q2/activity",
			RefLink, "dashboard.example.com/org/q2/activity",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, label := classifyURL(tc.url)
			assert.Equal(t, tc.wantKind, kind)
			assert.Equal(t, tc.wantLabel, label)
		})
	}
}

func TestPlausibleURL(t *testing.T) {
	// Escaped JSON in tool arguments leaves fragments behind.
	assert.False(t, plausibleURL("https://g"))
	assert.False(t, plausibleURL("https://agent:[REDACTED"))
	assert.True(t, plausibleURL("https://github.com/x/y"))
	assert.True(t, plausibleURL("http://localhost:8080/health"))
	assert.True(t, plausibleURL("http://127.0.0.1:4867/mcp"))
}

func TestReferencesDedupeAndCount(t *testing.T) {
	fakeHome(t)
	var claude *Session
	for _, s := range Scan() {
		if s.Agent == AgentClaude {
			claude = s
		}
	}
	require.NotNil(t, claude)
	require.NoError(t, Load(claude))

	// The fixture mentions no references at all.
	assert.Empty(t, claude.Refs)
}

// refsFixture is a transcript that references things from both sides of the
// conversation, on a ticket branch.
const refsFixture = `{"type":"user","timestamp":"2026-08-01T10:00:00Z","cwd":"/work/refs","gitBranch":"feat/fix-540-thing","message":{"role":"user","content":"look at fix-541 and https://github.com/owenrumney/ade/pull/798"}}
{"type":"assistant","timestamp":"2026-08-01T10:00:01Z","requestId":"r1","message":{"id":"m1","role":"assistant","model":"claude-opus-5","content":[{"type":"text","text":"FIX-590 too, see https://github.com/owenrumney/ade/pull/909 and gpt-5.6"}],"usage":{"input_tokens":1,"output_tokens":1}}}
{"type":"user","timestamp":"2026-08-01T10:00:02Z","isSidechain":true,"message":{"role":"user","content":"subagent: also handle FIX-777"}}
{"type":"assistant","timestamp":"2026-08-01T10:00:03Z","requestId":"r2","message":{"id":"m2","role":"assistant","model":"claude-opus-5","content":[{"type":"tool_use","name":"Bash","input":{"command":"ls rollout-2026-06-17 phase-1"}}],"usage":{"input_tokens":1,"output_tokens":1}}}
`

func loadRefsFixture(t *testing.T) *Session {
	t.Helper()
	home := fakeHome(t)
	writeFixture(t, filepath.Join(home, ".claude", "projects", "-work-refs", "refs-0001.jsonl"), refsFixture)
	var target *Session
	for _, s := range Scan() {
		if s.ID == "refs-0001" {
			target = s
		}
	}
	require.NotNil(t, target)
	require.NoError(t, Load(target))
	return target
}

func TestReferencesComeOnlyFromWhatYouTyped(t *testing.T) {
	target := loadRefsFixture(t)

	labels := map[string]bool{}
	for _, ref := range target.Refs {
		labels[ref.Label] = true
	}

	// Yours: a ticket you named, on a branch that vouches for the prefix, and a
	// PR you pasted.
	assert.True(t, labels["FIX-541"])
	assert.True(t, labels["owenrumney/ade PR #798"])

	// Not yours. The agent listing tickets and PRs was what made this list
	// useless; a subagent brief is agent-authored too.
	assert.False(t, labels["FIX-590"], "an agent naming a ticket is not you referencing it")
	assert.False(t, labels["owenrumney/ade PR #909"], "nor a PR the agent found")
	assert.False(t, labels["FIX-777"], "nor a subagent brief")

	// Never: prose and code that merely looks like issue keys.
	for _, junk := range []string{"GPT-5", "ROLLOUT-2026", "PHASE-1"} {
		assert.False(t, labels[junk], "%s is not a ticket", junk)
	}
}

func TestReferencesCountedButNotDisplayed(t *testing.T) {
	target := loadRefsFixture(t)
	for _, ref := range target.Refs {
		assert.Positive(t, ref.Count, "counts are still tracked internally")
	}
}

func TestReferencesSortLinearFirst(t *testing.T) {
	refs := []Reference{
		{Kind: RefLink, Label: "example.com", Count: 9},
		{Kind: RefLinear, Label: "FIX-2", Count: 1},
		{Kind: RefLinear, Label: "FIX-1", Count: 5},
		{Kind: RefGitHub, Label: "a/b#1", Count: 1},
	}
	sortReferences(refs)
	assert.Equal(t, []string{"FIX-1", "FIX-2", "a/b#1", "example.com"},
		[]string{refs[0].Label, refs[1].Label, refs[2].Label, refs[3].Label})
}

func TestBareKeysNeedCorroboration(t *testing.T) {
	tests := []struct {
		name    string
		ref     Reference
		trusted map[string]bool
		want    bool
	}{
		{
			name: "a linear URL is its own evidence",
			ref:  Reference{Kind: RefLinear, Label: "FIX-1", URL: "https://linear.app/x/issue/FIX-1"},
			want: true,
		},
		{
			// Repetition is not evidence: rollout-2026 and phase-1 recur just as
			// readily as a real ticket.
			name: "repetition alone is not enough",
			ref:  Reference{Kind: RefLinear, Label: "FIX-588", Count: 8},
			want: false,
		},
		{
			name:    "a learned workspace prefix",
			ref:     Reference{Kind: RefLinear, Label: "TRK-680", Count: 1},
			trusted: map[string]bool{"trk": true},
			want:    true,
		},
		{
			name:    "prefix vouched for by the branch",
			ref:     Reference{Kind: RefLinear, Label: "FIX-1", Count: 1},
			trusted: map[string]bool{"fix": true},
			want:    true,
		},
		{
			name: "a one-off RFC number is not a ticket",
			ref:  Reference{Kind: RefLinear, Label: "JSON-6902", Count: 1},
			want: false,
		},
		{
			name: "nor a one-off unknown prefix",
			ref:  Reference{Kind: RefLinear, Label: "BARE-40", Count: 1},
			want: false,
		},
		{
			name: "non-linear kinds are always kept",
			ref:  Reference{Kind: RefLink, Label: "example.com", Count: 1},
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, corroborated(tc.ref, tc.trusted))
		})
	}
}

func TestBranchTicketPrefix(t *testing.T) {
	assert.Equal(t, "fix", branchTicketPrefix("feat/fix-448-observe-pr-creation"))
	assert.Equal(t, "fix", branchTicketPrefix("fix-540-add-quotas"))
	assert.Empty(t, branchTicketPrefix("main"))
	assert.Empty(t, branchTicketPrefix(""))
}

func TestOneOffTicketSurvivesWhenTheBranchNamesIt(t *testing.T) {
	home := fakeHome(t)
	writeFixture(t, filepath.Join(home, ".claude", "projects", "-work-branchref", "br-0001.jsonl"),
		`{"type":"user","timestamp":"2026-08-01T10:00:00Z","cwd":"/work/branchref","gitBranch":"feat/fix-900-thing","message":{"role":"user","content":"start on fix-901 and note JSON-6902"}}
{"type":"assistant","timestamp":"2026-08-01T10:00:01Z","requestId":"r1","message":{"id":"m1","role":"assistant","model":"claude-opus-5","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}}
`)
	var target *Session
	for _, s := range Scan() {
		if s.ID == "br-0001" {
			target = s
		}
	}
	require.NotNil(t, target)
	require.NoError(t, Load(target))

	labels := map[string]bool{}
	for _, ref := range target.Refs {
		labels[ref.Label] = true
	}
	assert.True(t, labels["FIX-901"], "the branch vouches for the fix prefix, even mentioned once")
	assert.False(t, labels["JSON-6902"], "an unvouched one-off stays out")
}

func TestLearnTicketPrefixes(t *testing.T) {
	home := fakeHome(t)
	// Three linear links share FIX; ORG appears once and must not be learned.
	writeFixture(t, filepath.Join(home, ".claude", "projects", "-work-learn", "learn-0001.jsonl"),
		`{"type":"user","timestamp":"2026-08-01T10:00:00Z","cwd":"/work/learn","message":{"role":"user","content":"https://linear.app/ic/issue/FIX-1 https://linear.app/ic/issue/FIX-2 https://linear.app/ic/issue/FIX-3 https://linear.app/ic/issue/ORG-9"}}
`)
	sessions := Scan()
	LearnTicketPrefixes(sessions)

	prefixes := TicketPrefixes()
	assert.True(t, prefixes["fix"], "a prefix used repeatedly in linear links is a team key")
	assert.False(t, prefixes["org"], "a single use is below the threshold")
}

func TestLearnedPrefixAdmitsABareKey(t *testing.T) {
	home := fakeHome(t)
	writeFixture(t, filepath.Join(home, ".claude", "projects", "-work-lp", "lp-0001.jsonl"),
		`{"type":"user","timestamp":"2026-08-01T10:00:00Z","cwd":"/work/lp","message":{"role":"user","content":"https://linear.app/ic/issue/TRK-1 https://linear.app/ic/issue/TRK-2 https://linear.app/ic/issue/TRK-3"}}
`)
	writeFixture(t, filepath.Join(home, ".claude", "projects", "-work-lp2", "lp-0002.jsonl"),
		`{"type":"user","timestamp":"2026-08-02T10:00:00Z","cwd":"/work/lp2","message":{"role":"user","content":"now do trk-680 and also stage-2"}}
`)
	sessions := Scan()
	LearnTicketPrefixes(sessions)

	var target *Session
	for _, s := range sessions {
		if s.ID == "lp-0002" {
			target = s
		}
	}
	require.NotNil(t, target)
	require.NoError(t, Load(target))

	labels := map[string]bool{}
	for _, ref := range target.Refs {
		labels[ref.Label] = true
	}
	assert.True(t, labels["TRK-680"], "learned from links in another session")
	assert.False(t, labels["STAGE-2"], "an unlearned prefix stays out")
}

func TestPlaceholderReposAreNotReferences(t *testing.T) {
	for _, url := range []string{
		"https://github.com/org/repo",
		"https://github.com/acme/infra",
		"https://github.com/example/thing/pull/1",
	} {
		kind, label := classifyURL(url)
		assert.Empty(t, kind, "%s is an example, not a repository", url)
		assert.Empty(t, label)
	}
	kind, _ := classifyURL("https://github.com/owenrumney/ade")
	assert.Equal(t, RefGitHub, kind)
}

func TestLearnTicketPrefixesIsSafeWithNoSessions(t *testing.T) {
	LearnTicketPrefixes(nil)
	assert.Empty(t, TicketPrefixes())
}
