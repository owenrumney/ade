package ghpr

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realPayload mirrors owenrumney/mdview#6: two root comments and one reply.
const realPayload = `[
  {"id":3267658436,"in_reply_to_id":null,"path":"internal/render/page.html.tmpl","line":59,"original_line":59,
   "side":"RIGHT","body":"The inline script assigns string values without quoting.","created_at":"2026-08-01T10:00:00Z",
   "user":{"login":"Copilot"}},
  {"id":3267658567,"in_reply_to_id":null,"path":"README.md","line":52,"original_line":52,
   "side":"RIGHT","body":"README lists -v as a shorthand.","created_at":"2026-08-01T10:01:00Z",
   "user":{"login":"Copilot"}},
  {"id":3267923909,"in_reply_to_id":3267658436,"path":"internal/render/page.html.tmpl","line":59,"original_line":59,
   "side":"RIGHT","body":"false positive - go templates is sorting it","created_at":"2026-08-01T11:00:00Z",
   "user":{"login":"owenrumney"}}
]`

func TestParseComments(t *testing.T) {
	got, err := ParseComments(realPayload)
	require.NoError(t, err)
	require.Len(t, got, 3)

	assert.Equal(t, int64(3267658436), got[0].ID)
	assert.Equal(t, "internal/render/page.html.tmpl", got[0].Path)
	assert.Equal(t, 59, got[0].Line)
	assert.Equal(t, SideRight, got[0].Side, "GitHub's RIGHT is normalised")
	assert.Equal(t, "Copilot", got[0].Author)
	assert.False(t, got[0].Outdated)

	assert.Equal(t, int64(3267658436), got[2].ReplyTo)
	assert.Equal(t, "owenrumney", got[2].Author)
}

func TestParseCommentsEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		count   int
		check   func(t *testing.T, got []ReviewComment)
		isErr   bool
	}{
		{
			name:    "empty array",
			payload: `[]`,
			count:   0,
		},
		{
			name:    "LEFT side is normalised",
			payload: `[{"id":1,"path":"a.go","line":3,"side":"LEFT","user":{"login":"x"}}]`,
			count:   1,
			check: func(t *testing.T, got []ReviewComment) {
				assert.Equal(t, SideLeft, got[0].Side)
			},
		},
		{
			name:    "a null line falls back to the original and is marked outdated",
			payload: `[{"id":1,"path":"a.go","line":null,"original_line":42,"side":"RIGHT","user":{"login":"x"}}]`,
			count:   1,
			check: func(t *testing.T, got []ReviewComment) {
				assert.Equal(t, 42, got[0].Line)
				assert.True(t, got[0].Outdated, "the code has moved since this was written")
			},
		},
		{
			name:    "a comment with no line at all is skipped",
			payload: `[{"id":1,"path":"a.go","line":null,"original_line":null,"side":"RIGHT","user":{"login":"x"}}]`,
			count:   0,
		},
		{
			name:    "a comment with no path is skipped",
			payload: `[{"id":1,"path":"","line":3,"side":"RIGHT","user":{"login":"x"}}]`,
			count:   0,
		},
		{
			name:    "paginated output is several concatenated arrays",
			payload: `[{"id":1,"path":"a.go","line":1,"side":"RIGHT","user":{"login":"x"}}][{"id":2,"path":"b.go","line":2,"side":"RIGHT","user":{"login":"y"}}]`,
			count:   2,
			check: func(t *testing.T, got []ReviewComment) {
				assert.Equal(t, int64(2), got[1].ID)
			},
		},
		{
			name:    "malformed payload",
			payload: `[{"id":`,
			isErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseComments(tt.payload)
			if tt.isErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, tt.count)
			if tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}

func TestGroupThreads(t *testing.T) {
	comments, err := ParseComments(realPayload)
	require.NoError(t, err)

	threads := GroupThreads(comments)
	require.Len(t, threads, 2, "three comments form two threads")

	byPath := map[string]Thread{}
	for _, thread := range threads {
		byPath[thread.Root.Path] = thread
	}

	t.Run("a reply is nested under its root", func(t *testing.T) {
		thread := byPath["internal/render/page.html.tmpl"]
		assert.Equal(t, "Copilot", thread.Root.Author)
		require.Len(t, thread.Replies, 1)
		assert.Equal(t, "owenrumney", thread.Replies[0].Author)
	})

	t.Run("a root with no replies stands alone", func(t *testing.T) {
		thread := byPath["README.md"]
		assert.Empty(t, thread.Replies)
	})
}

func TestGroupThreadsKeepsOrphanedReplies(t *testing.T) {
	// A reply whose root is outside the fetched page must still be shown.
	comments, err := ParseComments(
		`[{"id":9,"in_reply_to_id":1234,"path":"a.go","line":1,"side":"RIGHT","body":"orphan","user":{"login":"x"}}]`)
	require.NoError(t, err)

	threads := GroupThreads(comments)
	require.Len(t, threads, 1)
	assert.Equal(t, "orphan", threads[0].Root.Body)
}

func TestGroupThreadsOrdersRepliesOldestFirst(t *testing.T) {
	comments, err := ParseComments(`[
	  {"id":1,"path":"a.go","line":1,"side":"RIGHT","body":"root","created_at":"2026-08-01T10:00:00Z","user":{"login":"a"}},
	  {"id":3,"in_reply_to_id":1,"path":"a.go","line":1,"side":"RIGHT","body":"second","created_at":"2026-08-01T12:00:00Z","user":{"login":"c"}},
	  {"id":2,"in_reply_to_id":1,"path":"a.go","line":1,"side":"RIGHT","body":"first","created_at":"2026-08-01T11:00:00Z","user":{"login":"b"}}
	]`)
	require.NoError(t, err)

	threads := GroupThreads(comments)
	require.Len(t, threads, 1)
	require.Len(t, threads[0].Replies, 2)
	assert.Equal(t, "first", threads[0].Replies[0].Body)
	assert.Equal(t, "second", threads[0].Replies[1].Body)
}

func TestGroupThreadsEmpty(t *testing.T) {
	assert.Empty(t, GroupThreads(nil))
}

func TestCheckFailed(t *testing.T) {
	assert.True(t, Check{Bucket: "fail"}.Failed())
	for _, bucket := range []string{"pass", "pending", "skipping", "cancel", ""} {
		assert.False(t, Check{Bucket: bucket}.Failed(), bucket)
	}
}

func TestParsePR(t *testing.T) {
	pr, err := parsePR(`{"number":7,"title":"Cart","url":"https://x/pull/7","state":"OPEN",
		"isDraft":true,"body":"why","baseRefName":"main","headRefName":"cart",
		"headRefOid":"abc","author":{"login":"owen"}}`)
	require.NoError(t, err)
	require.NotNil(t, pr)

	assert.Equal(t, 7, pr.Number)
	assert.Equal(t, "owen", pr.Author)
	assert.Equal(t, "why", pr.Body)
	assert.True(t, pr.Draft)
	assert.Equal(t, "main", pr.BaseRef)
}

func TestParsePRTreatsNoPullRequestAsAbsence(t *testing.T) {
	// gh can print an empty object for a branch with no pull request; that is
	// an ordinary state, not a parse failure.
	pr, err := parsePR(`{}`)
	require.NoError(t, err)
	assert.Nil(t, pr)
}

func TestRankBucketPutsFailuresFirst(t *testing.T) {
	assert.Less(t, rankBucket("fail"), rankBucket("pending"))
	assert.Less(t, rankBucket("pending"), rankBucket("cancel"))
	assert.Less(t, rankBucket("skipping"), rankBucket("pass"))
}

func TestReadOnly(t *testing.T) {
	reads := [][]string{
		{"pr", "view", "7", "--json", "body"},
		{"pr", "diff", "7"},
		{"pr", "checks", "7", "--json", "name"},
		{"pr", "list"},
		{"api", "--paginate", "repos/o/r/pulls/7/comments"},
		{"issue", "view", "412", "--json", "body"},
		{"issue", "list"},
	}
	for _, args := range reads {
		assert.Truef(t, readOnly(args), "gh %v should be allowed", args)
	}

	// ade drafts replies for you to send; it never sends them. These are the
	// shapes that would break that, and they must stay refused.
	writes := [][]string{
		{"pr", "comment", "7", "--body", "hi"},
		{"pr", "review", "7", "--approve"},
		{"pr", "merge", "7"},
		{"pr", "close", "7"},
		{"pr", "create", "--draft"},
		{"pr", "edit", "7", "--body", "x"},
		{"api", "--method", "POST", "repos/o/r/pulls/7/comments"},
		{"api", "-X", "POST", "repos/o/r/issues/7/comments"},
		{"api", "repos/o/r/issues/7/comments", "-f", "body=hi"},
		{"api", "repos/o/r/issues/7/comments", "--raw-field", "body=hi"},
		{"api", "repos/o/r/issues/7/comments", "--input", "-"},
		{"issue", "comment", "7"},
		{"issue", "close", "7"},
		{"issue", "edit", "7", "--body", "x"},
		{"issue", "create", "--title", "x"},
		{"issue"},
		{},
		{"pr"},
	}
	for _, args := range writes {
		assert.Falsef(t, readOnly(args), "gh %v must be refused", args)
	}
}

func TestRunRefusesAWrite(t *testing.T) {
	_, err := run(context.Background(), t.TempDir(), "pr", "comment", "7", "--body", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "will not run a gh command that writes")
}

func TestBothWaysOfReadingAPRAskForTheSameThing(t *testing.T) {
	// Fetch took a shorter list than ForBranch, so a pull request opened by
	// number arrived with no description and never marked draft.
	for _, field := range []string{"body", "isDraft", "number", "title", "author", "state", "headRefOid"} {
		assert.Containsf(t, prFields, field, "prFields is missing %s", field)
	}
}

func TestWritableIsOnlyTheWritesAdeMakes(t *testing.T) {
	allowed := [][]string{
		{"pr", "edit", "7", "--title", "t", "--body", "b"},
		{"pr", "merge", "7", "--rebase"},
		{"pr", "merge", "7", "--merge"},
		// An approval is a verdict you gave by pressing the button, and there
		// is nothing in it written by an agent.
		{"pr", "review", "7", "--approve"},
		{"pr", "review", "7", "--approve", "--body", "looks right to me"},
		{"pr", "review", "7", "--request-changes", "--body", "the retry loop never exits"},
		// A clone writes only to your disk.
		{"repo", "clone", "acme/other", "/tmp/acme/other"},
		// A reply goes out from the button on the thread, which shows you the
		// words first. PostReply builds exactly this.
		{"api", "--method", "POST", "repos/o/r/pulls/7/comments/42/replies", "--input", "-"},
	}
	for _, args := range allowed {
		assert.Truef(t, writable(args), "gh %v should be allowed", args)
	}

	// What stays refused is prose in your name: a review that is words without
	// a verdict is something an agent drafts and you send yourself.
	refused := [][]string{
		{"pr", "comment", "7", "--body", "hi"},
		{"pr", "review", "7", "--comment", "--body", "thoughts"},
		{"pr", "review", "7", "--approve", "--request-changes"},
		{"pr", "review", "7", "--body", "no verdict, just words"},
		{"pr", "review", "7"},
		{"pr", "close", "7"},
		{"pr", "reopen", "7"},
		{"pr", "create", "--draft"},
		{"pr", "ready", "7"},
		{"issue", "comment", "7", "--body", "hi"},
		{"repo", "delete", "acme/other"},
		{"repo", "clone"},
		{"api", "--method", "POST", "repos/o/r/pulls/7/comments"},
		{"pr", "edit"},
		{"pr"},
		{},
	}
	for _, args := range refused {
		assert.Falsef(t, writable(args), "gh %v must be refused", args)
	}
}

func TestApproveBlocked(t *testing.T) {
	tests := []struct {
		name   string
		pr     *PR
		viewer string
		want   string
	}{
		{"nothing there", nil, "owen", "there is no pull request"},
		{"merged", &PR{State: "MERGED"}, "owen", "this pull request is merged"},
		{"your own", &PR{State: "OPEN", Author: "owen"}, "owen", "your own pull request"},
		{"your own, other case", &PR{State: "OPEN", Author: "Owen"}, "owen", "your own pull request"},
		{"already approved", &PR{State: "OPEN", Author: "someone",
			Reviews: []Review{{Author: "owen", State: "APPROVED"}}}, "owen", "already approved"},
		{"you asked for changes", &PR{State: "OPEN", Author: "someone",
			Reviews: []Review{{Author: "owen", State: "CHANGES_REQUESTED"}}}, "owen", ""},
		{"somebody else's", &PR{State: "OPEN", Author: "someone"}, "owen", ""},
		// Not knowing who you are is not a reason to refuse; GitHub will say.
		{"unknown viewer", &PR{State: "OPEN", Author: "someone"}, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.pr.ApproveBlocked(tc.viewer)
			if tc.want == "" {
				assert.Empty(t, got)
				return
			}
			assert.Contains(t, got, tc.want)
		})
	}
}

func TestParsePRReadsTheReviews(t *testing.T) {
	pr, err := parsePR(`{"number":4,"state":"OPEN","reviewDecision":"APPROVED",
		"latestReviews":[{"state":"APPROVED","author":{"login":"alice"}},
		                 {"state":"COMMENTED","author":{"login":"bob"}}]}`)
	require.NoError(t, err)
	require.NotNil(t, pr)

	assert.Equal(t, "APPROVED", pr.ReviewDecision)
	assert.Equal(t, "APPROVED", pr.ReviewBy("alice"))
	assert.Equal(t, "COMMENTED", pr.ReviewBy("bob"))
	assert.Empty(t, pr.ReviewBy("carol"))
	assert.Empty(t, pr.ReviewBy(""))

	for _, field := range []string{"reviewDecision", "latestReviews"} {
		assert.Containsf(t, prFields, field, "prFields is missing %s", field)
	}
}

func TestWriteRefusesAnythingElse(t *testing.T) {
	_, err := write(context.Background(), t.TempDir(), "pr", "comment", "7", "--body", "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "will not run this gh command")
}

func TestMergeRejectsAnUnknownMethod(t *testing.T) {
	err := Merge(context.Background(), t.TempDir(), 7, "squash")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown merge method")
}

func TestEditNeedsATitle(t *testing.T) {
	err := Edit(context.Background(), t.TempDir(), 7, "  ", "body")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs a title")
}

func TestMergeBlocked(t *testing.T) {
	tests := []struct {
		name string
		pr   *PR
		want string
	}{
		{"nothing there", nil, "there is no pull request"},
		{"already merged", &PR{State: "MERGED"}, "this pull request is merged"},
		{"closed", &PR{State: "CLOSED"}, "this pull request is closed"},
		{"draft", &PR{State: "OPEN", Draft: true}, "still a draft"},
		{"protected", &PR{State: "OPEN", MergeState: "BLOCKED"}, "branch protection"},
		{"behind", &PR{State: "OPEN", BaseRef: "main", MergeState: "BEHIND"}, "behind main"},
		{"conflicting", &PR{State: "OPEN", BaseRef: "main", Mergeable: "CONFLICTING"}, "conflicts with main"},
		{"ready", &PR{State: "OPEN", Mergeable: "MERGEABLE", MergeState: "CLEAN"}, ""},
		// Checks are failing but nothing requires them to pass. GitHub would
		// take the merge, so ade must not be the one refusing it.
		{"unstable", &PR{State: "OPEN", Mergeable: "MERGEABLE", MergeState: "UNSTABLE"}, ""},
		// Still being worked out upstream; let the attempt find out.
		{"unknown", &PR{State: "OPEN", Mergeable: "UNKNOWN", MergeState: "UNKNOWN"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.pr.MergeBlocked()
			if tc.want == "" {
				assert.Empty(t, got)
				return
			}
			assert.Contains(t, got, tc.want)
		})
	}
}

func TestParsePRReadsMergeState(t *testing.T) {
	pr, err := parsePR(`{"number":4,"state":"OPEN","mergeable":"MERGEABLE","mergeStateStatus":"CLEAN"}`)
	require.NoError(t, err)
	require.NotNil(t, pr)
	assert.Equal(t, "MERGEABLE", pr.Mergeable)
	assert.Equal(t, "CLEAN", pr.MergeState)
	// Both ways of reading a pull request must carry it, or the merge button
	// disagrees with itself depending on how you opened the thing.
	assert.Contains(t, prFields, "mergeable")
	assert.Contains(t, prFields, "mergeStateStatus")
}

func TestAReplyGoesToTheThreadItAnswers(t *testing.T) {
	assert.True(t, writable([]string{
		"api", "--method", "POST", "repos/owner/repo/pulls/7/comments/3979692152/replies", "--input", "-",
	}))
	assert.True(t, writable([]string{
		"api", "-X", "POST", "repos/owner/repo/pulls/7/comments/42/replies", "--input", "-",
	}))

	refused := [][]string{
		// A new comment on a line is not a reply, and has no such button.
		{"api", "--method", "POST", "repos/o/r/pulls/7/comments", "--input", "-"},
		// Paths that only look like the one.
		{"api", "--method", "POST", "repos/o/r/pulls/7/comments/42/replies/1", "--input", "-"},
		{"api", "--method", "POST", "repos/o/r/pulls/7/comments/abc/replies", "--input", "-"},
		{"api", "--method", "POST", "repos/o/r/issues/7/comments/42/replies", "--input", "-"},
		{"api", "--method", "POST", "evil.example/repos/o/r/pulls/7/comments/42/replies", "--input", "-"},
		// Not a POST, or not one whole document.
		{"api", "--method", "DELETE", "repos/o/r/pulls/7/comments/42/replies", "--input", "-"},
		{"api", "--method", "POST", "repos/o/r/pulls/7/comments/42/replies", "-f", "body=hi"},
		{"api", "--method", "POST", "repos/o/r/pulls/7/comments/42/replies"},
	}
	for _, args := range refused {
		assert.Falsef(t, writable(args), "gh %v must be refused", args)
	}
}

func TestTheOnlyAPIWritesAreAReviewAndAReply(t *testing.T) {
	// The whole payload goes in as one document you have read, to one path.
	assert.True(t, writable([]string{
		"api", "--method", "POST", "repos/owner/repo/pulls/7/reviews", "--input", "-",
	}))
	assert.True(t, writable([]string{
		"api", "-X", "POST", "repos/owner/repo/pulls/7/reviews", "--input", "-",
	}))

	refused := [][]string{
		// Every other way of talking to someone through the API.
		{"api", "--method", "POST", "repos/o/r/issues/7/comments", "--input", "-"},
		{"api", "--method", "POST", "repos/o/r/pulls/7/comments", "--input", "-"},
		{"api", "--method", "POST", "repos/o/r/pulls/7/reviews/1/events", "--input", "-"},
		{"api", "--method", "PUT", "repos/o/r/pulls/7/reviews/1/dismissals", "--input", "-"},
		{"api", "--method", "DELETE", "repos/o/r/pulls/7/reviews", "--input", "-"},
		// A path that only looks like the one.
		{"api", "--method", "POST", "repos/o/r/pulls/7/reviews/extra", "--input", "-"},
		{"api", "--method", "POST", "repos/o/r/pulls/abc/reviews", "--input", "-"},
		{"api", "--method", "POST", "evil.example/repos/o/r/pulls/7/reviews", "--input", "-"},
		// A body assembled field by field is harder to see and harder to bound.
		{"api", "--method", "POST", "repos/o/r/pulls/7/reviews", "-f", "body=hi"},
		{"api", "--method", "POST", "repos/o/r/pulls/7/reviews", "--raw-field", "body=hi"},
		// No body at all, or no method.
		{"api", "--method", "POST", "repos/o/r/pulls/7/reviews"},
		{"api", "repos/o/r/pulls/7/reviews", "--input", "-"},
		{"api"},
	}
	for _, args := range refused {
		assert.Falsef(t, writable(args), "gh %v must be refused", args)
	}
}

func TestPostReviewRefusesAnUnsendableComment(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	require.ErrorContains(t, PostReview(ctx, dir, "o", "r", 7, "", nil), "nothing to send")

	// A comment with no anchor would either 422 or land somewhere surprising.
	for _, bad := range []InlineComment{
		{Line: 3, Body: "text"},
		{Path: "a.go", Body: "text"},
		{Path: "a.go", Line: 0, Body: "text"},
		{Path: "a.go", Line: 3, Body: "  "},
	} {
		err := PostReview(ctx, dir, "o", "r", 7, "", []InlineComment{bad})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no file, line or text")
	}
}

func TestParseListedMarksWhatIsMineAndWhatIsWaitingOnMe(t *testing.T) {
	out := `[
	  {"number":7,"title":"mine","author":{"login":"owen"},"headRefName":"a","isDraft":false,"url":"u7","reviewRequests":[]},
	  {"number":8,"title":"asked","author":{"login":"sam"},"headRefName":"b","isDraft":true,"url":"u8",
	   "reviewRequests":[{"__typename":"User","login":"owen"},{"__typename":"Team","name":"core","slug":"core"}]},
	  {"number":9,"title":"team only","author":{"login":"sam"},"headRefName":"c","isDraft":false,"url":"u9",
	   "reviewRequests":[{"__typename":"Team","name":"core","slug":"core"}]}
	]`

	listed, err := parseListed(out, "owen")
	require.NoError(t, err)
	require.Len(t, listed, 3)

	assert.True(t, listed[0].Mine)
	assert.False(t, listed[0].ReviewRequested)

	assert.False(t, listed[1].Mine)
	assert.True(t, listed[1].ReviewRequested, "asked by login")
	assert.True(t, listed[1].Draft)

	// A team request cannot be told from one to a team the viewer is not on.
	assert.False(t, listed[2].ReviewRequested)

	// With no viewer nothing is mine and nothing is asked of me.
	anon, err := parseListed(out, "")
	require.NoError(t, err)
	for _, pr := range anon {
		assert.False(t, pr.Mine)
		assert.False(t, pr.ReviewRequested)
	}

	_, err = parseListed("{not json", "owen")
	assert.Error(t, err)
}

func TestParseListedSaysWhereEachPullRequestStands(t *testing.T) {
	out := `[
	  {"number":1,"author":{"login":"sam"},"updatedAt":"2026-08-29T09:41:28Z","reviewDecision":"APPROVED",
	   "statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"},{"__typename":"StatusContext","state":"SUCCESS"}]},
	  {"number":2,"author":{"login":"sam"},"reviewDecision":"CHANGES_REQUESTED",
	   "statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"},{"__typename":"CheckRun","status":"COMPLETED","conclusion":"FAILURE"},{"__typename":"CheckRun","status":"IN_PROGRESS","conclusion":""}]},
	  {"number":3,"author":{"login":"sam"},"reviewDecision":"REVIEW_REQUIRED",
	   "statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"},{"__typename":"CheckRun","status":"QUEUED","conclusion":""}]},
	  {"number":4,"author":{"login":"sam"},"statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SKIPPED"}]},
	  {"number":5,"author":{"login":"sam"},"statusCheckRollup":[]}
	]`
	listed, err := parseListed(out, "owen")
	require.NoError(t, err)
	require.Len(t, listed, 5)

	assert.Equal(t, "2026-08-29T09:41:28Z", listed[0].UpdatedAt)
	assert.Equal(t, "APPROVED", listed[0].ReviewDecision)
	assert.Equal(t, "pass", listed[0].Checks, "a check run and a commit status, both green")
	assert.Equal(t, "fail", listed[1].Checks, "one failure is a failure, whatever else is running")
	assert.Equal(t, "CHANGES_REQUESTED", listed[1].ReviewDecision)
	assert.Equal(t, "pending", listed[2].Checks, "green so far, one still to come")
	assert.Empty(t, listed[3].Checks, "skipped counts for nothing, as on github's own tick")
	assert.Empty(t, listed[4].Checks, "nothing reported")
	assert.Empty(t, listed[4].UpdatedAt)
}

func TestParseRequestsReadsTheRepositoryAndTheChecksEachIsIn(t *testing.T) {
	out := `{"data":{"search":{"nodes":[
	  {"number":3,"title":"here","author":{"login":"sam"},"isDraft":false,"url":"u3",
	   "repository":{"nameWithOwner":"owenrumney/ade"},
	   "commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}},
	  {"number":9,"title":"elsewhere","author":{"login":"kim"},"isDraft":true,"url":"u9",
	   "updatedAt":"2026-08-28T10:00:00Z","reviewDecision":"CHANGES_REQUESTED",
	   "repository":{"nameWithOwner":"acme/other"},
	   "commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE"}}}]}},
	  {"number":11,"title":"running","author":{"login":"kim"},"url":"u11",
	   "repository":{"nameWithOwner":"acme/other"},
	   "commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"PENDING"}}}]}},
	  {"number":12,"title":"quiet","author":{"login":"kim"},"url":"u12",
	   "repository":{"nameWithOwner":"acme/other"},
	   "commits":{"nodes":[{"commit":{"statusCheckRollup":null}}]}},
	  {}
	]}}}`

	asked, err := parseRequests(out)
	require.NoError(t, err)
	require.Len(t, asked, 4, "the node that is not a pull request is dropped")

	assert.Equal(t, "owenrumney/ade", asked[0].Repo)
	assert.Empty(t, asked[0].UpdatedAt)
	assert.Equal(t, 3, asked[0].Number)
	assert.Equal(t, "pass", asked[0].Checks)
	assert.Empty(t, asked[0].ReviewDecision)

	assert.Equal(t, "acme/other", asked[1].Repo)
	assert.Equal(t, "2026-08-28T10:00:00Z", asked[1].UpdatedAt)
	assert.True(t, asked[1].Draft)
	assert.Equal(t, "kim", asked[1].Author)
	assert.Equal(t, "fail", asked[1].Checks)
	assert.Equal(t, "CHANGES_REQUESTED", asked[1].ReviewDecision)

	assert.Equal(t, "pending", asked[2].Checks)
	assert.Empty(t, asked[3].Checks, "nothing reported is nothing to draw")

	_, err = parseRequests("nope")
	assert.Error(t, err)
}

func TestTheReviewSearchIsAQueryAndNeverAMutation(t *testing.T) {
	// The query is a constant here, so this guards the file against itself:
	// pasting a mutation into it must not become a mutation ade runs.
	for _, query := range []string{waitingOnMe, mineOpen} {
		assert.NotContains(t, strings.ToLower(prSearch(query)), "mutation")
	}

	_, err := graphqlRead(context.Background(), t.TempDir(), "mutation { addComment { id } }")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "will not run a GraphQL mutation")
}

func TestSearchIsReadOnlyAndCheckoutIsAWrite(t *testing.T) {
	assert.True(t, readOnly([]string{"search", "prs", "--review-requested=@me"}))
	assert.True(t, writable([]string{"pr", "checkout", "9"}))
	assert.False(t, readOnly([]string{"pr", "checkout", "9"}))
}

func TestRepoViewIsReadOnlyAndNothingElseOnRepoIs(t *testing.T) {
	assert.True(t, readOnly([]string{"repo", "view", "--json", "owner,name"}))
	assert.False(t, readOnly([]string{"repo", "clone", "owenrumney/ade", "/tmp/x"}))
	assert.False(t, readOnly([]string{"repo", "delete", "owenrumney/ade"}))
	assert.False(t, readOnly([]string{"repo"}))
}

func TestPostReplyRefusesWhatItCannotSend(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	require.ErrorContains(t, PostReply(ctx, dir, "o", "r", 7, 100, ""), "nothing to send")
	require.ErrorContains(t, PostReply(ctx, dir, "o", "r", 7, 100, "   "), "nothing to send")
	require.ErrorContains(t, PostReply(ctx, dir, "o", "r", 7, 0, "text"), "no comment to reply to")
	require.ErrorContains(t, PostReply(ctx, dir, "o", "r", 7, -1, "text"), "no comment to reply to")
}

func TestRequestChangesBlocked(t *testing.T) {
	tests := []struct {
		name   string
		pr     *PR
		viewer string
		want   string
	}{
		{"nothing there", nil, "owen", "there is no pull request"},
		{"closed", &PR{State: "CLOSED"}, "owen", "this pull request is closed"},
		{"your own", &PR{State: "OPEN", Author: "owen"}, "owen", "your own pull request"},
		{"already asked", &PR{State: "OPEN", Author: "someone",
			Reviews: []Review{{Author: "owen", State: "CHANGES_REQUESTED"}}}, "owen", "already requested changes"},
		// Changing your mind after approving is the whole point of the button.
		{"you approved it", &PR{State: "OPEN", Author: "someone",
			Reviews: []Review{{Author: "owen", State: "APPROVED"}}}, "owen", ""},
		{"somebody else's", &PR{State: "OPEN", Author: "someone"}, "owen", ""},
		{"unknown viewer", &PR{State: "OPEN", Author: "someone"}, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.pr.RequestChangesBlocked(tc.viewer)
			if tc.want == "" {
				assert.Empty(t, got)
				return
			}
			assert.Contains(t, got, tc.want)
		})
	}
}

func TestRequestChangesNeedsAReason(t *testing.T) {
	err := RequestChanges(context.Background(), t.TempDir(), 7, "   ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs a reason")
}

func TestParseIssue(t *testing.T) {
	issue, err := parseIssue(`{"number":412,"title":"Cache the token","body":"It is read on every request.","url":"https://github.com/o/r/issues/412","state":"OPEN"}`)
	require.NoError(t, err)
	assert.Equal(t, 412, issue.Number)
	assert.Equal(t, "Cache the token", issue.Title)
	assert.Equal(t, "It is read on every request.", issue.Body)
	assert.Equal(t, "open", issue.State, "lowercased, the way the PR state is")
}

func TestParseIssueRefusesWhatIsNotAnIssue(t *testing.T) {
	// gh answers {} for a number that is a pull request or nothing at all, and
	// drafting a design from that would produce an empty document with no reason.
	_, err := parseIssue(`{}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an issue")

	_, err = parseIssue("not json")
	require.Error(t, err)
}

func TestFetchIssueNeedsANumber(t *testing.T) {
	_, err := FetchIssue(context.Background(), t.TempDir(), 0)
	require.Error(t, err)
}

func TestParseIssueRefusesAPullRequest(t *testing.T) {
	// gh issue view 1 answers with pull request 1 rather than failing.
	_, err := parseIssue(`{"number":1,"title":"lots of refactoring","url":"https://github.com/o/r/pull/1","state":"MERGED"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pull request, not an issue")
}

func TestABranchWithNoPullRequestIsNotAFailureWorthWarningAbout(t *testing.T) {
	// It is polled every 30s, and at Warn it buries every other line.
	assert.True(t, noPullRequest(`no pull requests found for branch "main"`))
	assert.True(t, noPullRequest("GraphQL: no pull requests found\n"))
	assert.False(t, noPullRequest("gh: command not found"))
	assert.False(t, noPullRequest(""))
}

func TestAnOriginWithNoRepositoryBehindItIsToldApartFromARealFailure(t *testing.T) {
	assert.True(t, NoRepository(errors.New(
		"gh pr list: exit status 1: GraphQL: Could not resolve to a Repository with the name 'o/r'. (repository)")))
	assert.True(t, NoRepository(errors.New("gh pr list: exit status 1: no git remotes found")))
	assert.False(t, NoRepository(errors.New("gh pr list: exit status 1: HTTP 502")))
	assert.False(t, NoRepository(nil))
}
