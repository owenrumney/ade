package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/docs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeDoc(t *testing.T, s *Server, name, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(s.docsDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(s.docsDir, name), []byte(body), 0o600))
}

// fakeGh puts a gh on the PATH that prints this and exits with code, so the
// issue path can be driven without the network.
func fakeGh(t *testing.T, out string, code int) {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\ncat <<'ADEEOF'\n%s\nADEEOF\nexit %d\n", out, code)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

const issueJSON = `{"number":412,"title":"Cache the token","body":"It is read on every request.","url":"https://github.com/o/r/issues/412","state":"OPEN"}`

func TestDraftingFromAnIssue(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "412-cache-the-token.md", "# Cache the token\n")
	s.setDoc("412-cache-the-token.md")
	fakeGh(t, issueJSON, 0)

	s.draftFromIssue(context.Background(), 412)
	defer s.agent.Cancel()

	said := s.conversation()
	require.Len(t, said, 1)
	assert.Equal(t, chat.KindYou, said[0].Kind)
	assert.Contains(t, said[0].Body, "Issue #412")
}

func TestAnIssueThatCannotBeReadSaysSoRatherThanDraftingNothing(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "412-cache-the-token.md", "# Cache the token\n")
	s.setDoc("412-cache-the-token.md")
	fakeGh(t, "gh: could not read it", 1)

	s.draftFromIssue(context.Background(), 412)

	said := s.conversation()
	require.Len(t, said, 1)
	assert.Equal(t, chat.KindNote, said[0].Kind)
	assert.Contains(t, said[0].Body, "#412")
	assert.False(t, s.agent.Cancel(), "a failed read does not spend a run")
}

func TestAnIssueDoesNotRedraftADocumentThatSaysSomething(t *testing.T) {
	// The second `ade create --github 412` is resuming that work, not asking
	// for the draft again.
	s := newTestServer(t)
	writeDoc(t, s, "412-cache-the-token.md", "# Cache the token\n\nThe token is read on every request.\n")
	s.setDoc("412-cache-the-token.md")
	fakeGh(t, issueJSON, 0)

	s.draftFromIssue(context.Background(), 412)

	assert.Empty(t, s.conversation())
	assert.False(t, s.agent.Cancel(), "the draft that is there is the one that stands")
}

func TestDraftingFromAnIssueNeedsADocumentAndANumber(t *testing.T) {
	s := newTestServer(t)
	fakeGh(t, issueJSON, 0)

	s.draftFromIssue(context.Background(), 412)
	assert.Empty(t, s.conversation(), "no document open")

	writeDoc(t, s, "412-cache-the-token.md", "# Cache the token\n")
	s.setDoc("412-cache-the-token.md")
	s.draftFromIssue(context.Background(), 0)
	assert.Empty(t, s.conversation(), "no issue to read")
}

func getDesign(t *testing.T, s *Server, query string) documentResponse {
	t.Helper()
	rec := do(t, s, "GET", "/api/design"+query, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body documentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

func TestDesignDirDefaultsToTheGitDir(t *testing.T) {
	s := newTestServer(t)
	assert.Equal(t, filepath.Join(s.ws.homeRoot, ".git", "ade", "docs"), s.docsDir)
	assert.DirExists(t, s.docsDir)
}

func TestDesignRendersADocument(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n\nCache the *cart*.\n")

	body := getDesign(t, s, "?file=plan.md")
	assert.Equal(t, "plan.md", body.File)
	assert.Contains(t, body.Body, "<em>cart</em>")
	assert.NotContains(t, body.Body, "<html", "the pane wants a fragment, not a page")
	assert.Equal(t, "# Plan\n\nCache the *cart*.\n", body.Raw, "the raw markdown comes back for editing")
	require.NotEmpty(t, body.TOC)
	assert.Equal(t, "Plan", body.TOC[0].Text)
	require.Len(t, body.Files, 1)
	assert.True(t, body.Files[0].Selected)
}

func TestDesignWithNoDocumentsReturnsAnEmptyList(t *testing.T) {
	s := newTestServer(t)

	body := getDesign(t, s, "")
	assert.Empty(t, body.File)
	assert.Empty(t, body.Files)
	assert.Empty(t, body.Body)
}

func TestDesignRejectsAPathOutsideTheDirectory(t *testing.T) {
	s := newTestServer(t)

	for _, file := range []string{"../../../etc/passwd.md", "/etc/passwd.md", "notes.txt"} {
		rec := do(t, s, "GET", "/api/design?file="+file, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, file)
	}
}

func TestDesignCreate(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/design", `{"name":"cart","title":"Cart redesign"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, "cart.md", s.currentDoc(), "creating opens it")
	body := getDesign(t, s, "")
	assert.Contains(t, body.Raw, "# Cart redesign")

	rec = do(t, s, "POST", "/api/design", `{"name":"cart"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "creating twice must not clobber the first")
}

func TestDesignSave(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n")

	rec := do(t, s, "PUT", "/api/design", `{"file":"plan.md","body":"# Edited\n"}`)
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "# Edited\n", getDesign(t, s, "?file=plan.md").Raw)

	rec = do(t, s, "PUT", "/api/design", `{"file":"ghost.md","body":"x"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "save must not create a document")
}

func TestDesignGoNeedsADocument(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/design/go", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "no design document")
}

// TestBuildStartsTheAgentItWasGiven: the fresh agent is the one the modal named.
func TestBuildStartsTheAgentItWasGiven(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n\nCache the cart.\n")
	s.setDoc("plan.md")

	rec := do(t, s, "POST", "/api/design/go", `{"provider":"codex","model":"gpt-5.6-terra"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	defer s.agent.Cancel()

	assert.Equal(t, "codex", s.agent.State().Provider)
	assert.Equal(t, "gpt-5.6-terra", s.agent.State().Model)
}

// TestABuildInAWorktreeAnswersWhereItWent: the session relocates, so the
// window has to be told where, or it keeps showing the checkout it left.
func TestABuildInAWorktreeAnswersWhereItWent(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "cart.md", "# Cart\n\nDebounce the totals.\n")
	s.setDoc("cart.md")

	proposed := s.proposedWorktree()
	require.NotNil(t, proposed)
	t.Cleanup(func() { _ = os.RemoveAll(proposed.Path) })

	rec := do(t, s, "POST", "/api/design/go", `{"worktree":true}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	defer s.agent.Cancel()

	var body struct {
		Running bool   `json:"running"`
		Path    string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.True(t, body.Running)
	assert.Equal(t, proposed.Path, body.Path)
	assert.Equal(t, proposed.Path, s.space().homeRoot, "ade followed the build")
}

// A build in place has nowhere to send the window, so it says nothing.
func TestABuildInPlaceAnswersNoPath(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "cart.md", "# Cart\n\nDebounce the totals.\n")
	s.setDoc("cart.md")

	rec := do(t, s, "POST", "/api/design/go", "")
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	defer s.agent.Cancel()
	assert.NotContains(t, rec.Body.String(), `"path"`)
}

func TestStateCarriesDesign(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n")
	s.setDoc("plan.md")

	rec := do(t, s, "GET", "/api/state", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var body stateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	require.NotNil(t, body.Design)
	assert.Equal(t, "plan.md", body.Design.File)
	require.Len(t, body.Design.Files, 1)
}

func TestSeedingAndFoldingQueueWhileTheAgentIsBusy(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n\nCache the cart.\n")
	s.setDoc("plan.md")

	_, ok := s.agent.begin(context.Background(), "review")
	require.True(t, ok)
	defer s.agent.finish(nil)

	// A build still refuses: it empties the window and may move the checkout,
	// which is not something to fire later at a conversation you are reading.
	rec := do(t, s, "POST", "/api/design/go", "")
	assert.Equal(t, http.StatusConflict, rec.Code)

	rec = do(t, s, "POST", "/api/design/seed", "")
	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Contains(t, rec.Body.String(), `"queued":1`)

	s.say(ChatMessage{Kind: chat.KindAgent, Author: "architect", Body: "The cache needs an eviction rule."})
	said := s.conversation()[0]
	require.NotEmpty(t, said.ID)
	rec = do(t, s, "POST", "/api/design/fold", `{"id":"`+said.ID+`"}`)
	assert.Equal(t, http.StatusAccepted, rec.Code, "folding is a turn too")
	assert.Contains(t, rec.Body.String(), `"queued":2`)
}

func TestBuildStartsOnAnEmptyWindowButKeepsTheDocument(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n\nCache the cart.\n")
	s.setDoc("plan.md")
	s.agent.resume("sess-design")
	s.say(ChatMessage{Kind: chat.KindYou, Body: "what about the cart?"})
	require.Equal(t, 1, s.chatStore().Len("sess-design"))

	s.clearForBuild("", "")

	assert.Empty(t, s.agent.Current(), "the build must not resume the design conversation")
	// The document is the brief, so unlike a new session this keeps it open.
	assert.Equal(t, "plan.md", s.currentDoc())
	assert.Equal(t, 1, s.chatStore().Len("sess-design"), "the design conversation is left on disk")
}

// A build on a chosen agent must not take the startFresh path: that one closes
// the document, and the document is what the build is for.
func TestBuildingOnAChosenAgentKeepsTheDocument(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n\nCache the cart.\n")
	s.setDoc("plan.md")

	s.clearForBuild("codex", "")

	assert.Equal(t, "plan.md", s.currentDoc(), "the brief stays open through the switch")
	assert.Equal(t, "codex", s.agent.providerName())
	assert.Empty(t, s.agent.Current(), "still a fresh conversation")
}

func TestBuildDoesNotDeleteADesignThatNeverNamedASession(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n\nCache the cart.\n")
	s.setDoc("plan.md")
	require.Empty(t, s.agent.Current(), "nothing has named a session yet")

	// Typed before any agent turn, so it is in the pending bucket rather than
	// under a session id. Build used to clear that bucket.
	s.say(ChatMessage{Kind: chat.KindYou, Body: "cache it per user, not per cart"})
	require.Equal(t, 1, s.chatStore().Len(chat.Pending))

	s.clearForBuild("", "")

	assert.Equal(t, 1, s.chatStore().Len(chat.Pending), "the user's own words are not the build's to delete")
}

func TestDesignAssetsAreServed(t *testing.T) {
	s := newTestServer(t)

	tests := map[string]string{
		"markdown.css":    ".markdown-body",
		"copy-buttons.js": "copy",
		"zoom.js":         "zoom",
		"mermaid-init.js": "MDVIEW_MERMAID_THEME",
	}
	for name, want := range tests {
		rec := do(t, s, "GET", "/design-assets/"+name, "")
		require.Equal(t, http.StatusOK, rec.Code, name)
		assert.Contains(t, rec.Body.String(), want, name)
	}

	rec := do(t, s, "GET", "/design-assets/nope.js", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDocPathIsRepoRelativeWhenInsideTheRepo(t *testing.T) {
	s := newTestServer(t)
	// The default docs dir lives under .git, which is inside the repo root.
	assert.Equal(t, ".git/ade/docs/plan.md", s.docPath("plan.md"))

	s.docsDir = t.TempDir()
	assert.Equal(t, filepath.Join(s.docsDir, "plan.md"), s.docPath("plan.md"),
		"a directory outside the repo needs an absolute path")
}

func TestANamedAgentIsOpenedTheDocsDirOnlyWhenItIsOutside(t *testing.T) {
	s := newTestServer(t)
	// Told an absolute path it cannot reach, a named agent reports the design
	// as missing rather than reading it.
	assert.Empty(t, s.sideDirs(), "the default docs dir is inside the checkout")

	s.docsDir = t.TempDir()
	assert.Equal(t, []string{s.docsDir}, s.sideDirs())
}

func TestMarkdownCSSCannotReachTheDiffPane(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "GET", "/design-assets/markdown.css", "")
	require.Equal(t, http.StatusOK, rec.Code)

	// Two stylesheets share this page. Every rule the design pane brings must be
	// scoped, or it silently restyles the diff.
	allowed := []string{".markdown-body", ":root", ".copy-btn", ".mermaid-error", ".mdview-zoom"}
	for _, selector := range selectors(rec.Body.String()) {
		assert.Truef(t, hasAnyPrefix(selector, allowed), "unscoped rule %q would leak into the diff", selector)
	}
}

var selectorRE = regexp.MustCompile(`(?m)^([^\s/@}][^{]*)\{`)

func selectors(css string) []string {
	var out []string
	for _, match := range selectorRE.FindAllStringSubmatch(css, -1) {
		out = append(out, strings.TrimSpace(match[1]))
	}
	return out
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func TestDesignSeedDeclinesAnUntouchedTemplate(t *testing.T) {
	s := newTestServer(t)

	// This is exactly what `ade create` scaffolds.
	writeDoc(t, s, "plan.md", "# Plan\n\n## Problem\n\n## Approach\n\n## Open questions\n")
	s.setDoc("plan.md")

	rec := do(t, s, "POST", "/api/design/seed", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, false, body["started"], "an empty template must not spend an agent run")
	assert.False(t, s.agent.State().Running)
	assert.Empty(t, s.conversation())
}

func TestDesignSeedNeedsADocument(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/design/seed", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// scaffoldFixture is what docs.Create writes, so the guard is tested against the
// document the user actually sees rather than an approximation of it.
var scaffoldFixture = func() string {
	dir, err := os.MkdirTemp("", "scaffold")
	if err != nil {
		panic(err)
	}
	file, err := docs.Create(dir, "plan.md", "Plan")
	if err != nil {
		panic(err)
	}
	body, err := docs.Read(dir, file)
	if err != nil {
		panic(err)
	}
	return body
}()

func TestHasProse(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "the create scaffold", body: "# Plan\n\n## Problem\n\n## Approach\n\n## Open questions\n"},
		{name: "headings and front matter", body: "---\ntitle: Plan\n---\n\n# Plan\n\n## Problem\n"},
		{name: "blank", body: "\n\n   \n"},
		{name: "a filled in section", body: "# Plan\n\n## Problem\n\nTotals recompute on every keystroke.\n", want: true},
		{name: "a list", body: "# Plan\n\n- debounce it\n", want: true},
		{name: "code only", body: "# Plan\n\n```go\nfunc x() {}\n```\n", want: true},
		{name: "front matter then prose", body: "---\ntitle: Plan\n---\n\n# Plan\n\nDebounce it.\n", want: true},
		{name: "unterminated front matter", body: "---\ntitle: Plan\n"},
		{name: "the real create scaffold", body: scaffoldFixture},
		{name: "one section filled in below a comment", body: scaffoldFixture + "\nDebounce the totals.\n", want: true},
		{name: "single line comment", body: "# Plan\n\n<!-- a hint -->\n"},
		{name: "prose on the same line as a comment close", body: "# Plan\n\n<!-- hint --> and then some.\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hasProse(tt.body))
		})
	}
}

func TestClosingADocumentLeavesItOnDiskAndOpensNothing(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "one.md", "# One\n")
	writeDoc(t, s, "two.md", "# Two\n")
	s.setDoc("one.md")

	rec := do(t, s, "DELETE", "/api/design", "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Empty(t, s.currentDoc())
	// Still there to open, and the other one now can be.
	state := s.currentDesign()
	require.NotNil(t, state)
	assert.Empty(t, state.File)
	assert.Len(t, state.Files, 2)
	rec = do(t, s, "GET", "/api/design?file=two.md", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "two.md", s.currentDoc())
	// The turn prompt stops naming it, and goes back to saying where one would
	// go if we start another.
	s.setDoc("")
	prompt, _ := s.turnPrompt(nil, "hi", nil)
	assert.NotContains(t, prompt, "two.md")
	assert.Contains(t, prompt, "no design document open")

	// Nothing open is nothing to do.
	rec = do(t, s, "DELETE", "/api/design", "")
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// Not out from under a build of it.
	s.setDoc("one.md")
	_, ok := s.agent.begin(context.Background(), "build")
	require.True(t, ok)
	defer s.agent.finish(nil)
	rec = do(t, s, "DELETE", "/api/design", "")
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "one.md", s.currentDoc())
}

func TestReadingADocumentMakesItCurrent(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "one.md", "# One\n")
	writeDoc(t, s, "two.md", "# Two\n")
	s.setDoc("one.md")

	// This is how the CLI hands a newly created document to a server already
	// running for the checkout, instead of starting a second one.
	rec := do(t, s, "GET", "/api/design?file=two.md", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "two.md", s.currentDoc())

	// A document it cannot read must not change what is open.
	rec = do(t, s, "GET", "/api/design?file=../escape.md", "")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "two.md", s.currentDoc())
}

func TestCreatingADesignFromATicketNamesItAfterTheTicket(t *testing.T) {
	s := newTestServer(t)

	// Typing only a ticket is the common case, and it should not then ask for a
	// name it can work out: ade create --linear does the same.
	rec := do(t, s, "POST", "/api/design", `{"ticket":"FIX-623"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "FIX-623")
	assert.Contains(t, rec.Body.String(), `"drafting":true`)

	s.agent.Cancel()
}

func TestCreatingADesignWithoutATicketDoesNotDraft(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/design", `{"name":"cart-redesign","title":"Cart redesign"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"drafting":false`)
	assert.Equal(t, "cart-redesign.md", s.currentDoc())
}

func TestADesignStillNeedsAName(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/design", `{"title":"No name"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTheDesignTabExistsBeforeADocumentDoes(t *testing.T) {
	s := newTestServer(t)

	// The tab is where you start a design. Withholding it until one existed
	// left nowhere to start from.
	rec := do(t, s, "GET", "/api/state", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Design *struct {
			Dir   string `json:"dir"`
			File  string `json:"file"`
			Files []any  `json:"files"`
		} `json:"design"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.NotNil(t, body.Design, "no design state, so no Design tab")
	assert.Empty(t, body.Design.File, "there is no document yet")
	assert.NotEmpty(t, body.Design.Dir, "but there is somewhere to put one")
	// A nil slice reaches the browser as null and breaks iterating it.
	assert.NotNil(t, body.Design.Files)
}

func TestDesignNameMatchesWhatFindLooksFor(t *testing.T) {
	// The name has to be one docs.Find will match against the branch, or the
	// agent writes a document nothing ever opens.
	for _, tc := range []struct{ branch, want string }{
		{"fix-623", "FIX-623"},
		{"owen/FIX-623-the-thing", "FIX-623"},
		{"feature/no-ticket-here", "no-ticket-here"},
		{"plain", "plain"},
		// A default branch names no work. Named after it, every design done
		// on main would be the same file.
		{"main", ""},
		{"master", ""},
		{"Trunk", ""},
		{"develop", ""},
	} {
		assert.Equal(t, tc.want, designName(tc.branch), tc.branch)
	}
}

func TestOnADefaultBranchTheAgentIsGivenTheDirectoryNotAFilename(t *testing.T) {
	s := newTestServer(t)
	s.mu.Lock()
	s.ws.branch = "main"
	s.mu.Unlock()

	doc := s.designDoc()
	assert.Empty(t, doc.New, "no filename to give: main names nothing")
	require.NotEmpty(t, doc.Dir)

	prompt, _ := s.turnPrompt(nil, "let us design project memory", nil)
	assert.Contains(t, prompt, doc.Dir)
	assert.Contains(t, prompt, "Name the file after what it designs")
	assert.NotContains(t, prompt, "main.md")
}

func TestOnATicketBranchTheFilenameIsStillTheTicket(t *testing.T) {
	s := newTestServer(t)
	s.mu.Lock()
	s.ws.branch = "owen/FIX-623-the-thing"
	s.mu.Unlock()

	doc := s.designDoc()
	assert.Empty(t, doc.Dir, "there is a name, so the directory is not the instruction")
	assert.Contains(t, doc.New, "FIX-623.md")
}

func TestADocumentNamedForTheAskIsReopenedOnTheNextStart(t *testing.T) {
	// The reason the record exists. project-memory.md matches nothing about
	// main, so only what ade wrote down brings it back.
	s := newTestServer(t)
	s.mu.Lock()
	s.ws.branch = "main"
	s.mu.Unlock()

	writeDoc(t, s, "project-memory.md", "# Project memory\n")
	require.Equal(t, "project-memory.md", s.currentDesign().File, "adopted by the poll")

	file, ok := docs.Opened(s.commonGitDir, "main", s.agent.Current())
	require.True(t, ok, "and written down")
	assert.Equal(t, "project-memory.md", file)
}

func TestADocumentWrittenMidSessionOpensWithoutARestart(t *testing.T) {
	// The agent is asked to start a design, writes it, and the Design tab has
	// to notice. Before this the branch match only ran at startup and on a
	// bind, so the file sat there unopened until ade was restarted.
	s := newTestServer(t)
	require.Empty(t, s.currentDoc(), "nothing open to begin with")

	state := s.currentDesign()
	require.NotNil(t, state, "somewhere to put one, so the tab is offered")
	assert.Empty(t, state.File)

	// Named for the branch, the way the turn prompt tells the agent to name it.
	writeDoc(t, s, designName(s.space().branch)+".md", "# Design\n")

	state = s.currentDesign()
	require.NotNil(t, state)
	assert.Equal(t, designName(s.space().branch)+".md", state.File, "picked up on the next poll")
	assert.Equal(t, state.File, s.currentDoc(), "and held, so it does not have to be found again")
}

func TestAClosedDocumentIsNotReopenedByThePoll(t *testing.T) {
	// The poll looks for a document belonging to the branch, so closing one had
	// to mean "not this", or the next poll opened it straight back up.
	s := newTestServer(t)
	name := designName(s.space().branch) + ".md"
	writeDoc(t, s, name, "# Design\n")

	require.Equal(t, name, s.currentDesign().File, "found on the first poll")

	rec := do(t, s, "DELETE", "/api/design", "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Empty(t, s.currentDesign().File, "and it stays closed")
	assert.Empty(t, s.currentDesign().File, "on the poll after that too")

	// Asking for it back is the answer to having closed it.
	rec = do(t, s, "GET", "/api/design?file="+name, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, name, s.currentDesign().File)
}

func TestDeletingADesignDocumentTakesItOffDisk(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "one.md", "# One\n")
	writeDoc(t, s, "two.md", "# Two\n")
	s.setDoc("one.md")

	// The one open goes, and nothing is left holding its name.
	rec := do(t, s, "DELETE", "/api/design/doc?file=one.md", "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Empty(t, s.currentDoc(), "a name that is gone is not held open")
	assert.NoFileExists(t, filepath.Join(s.docsDir, "one.md"))
	assert.FileExists(t, filepath.Join(s.docsDir, "two.md"), "the other is untouched")

	// One that is not open goes too, and leaves the open one alone.
	s.setDoc("two.md")
	writeDoc(t, s, "three.md", "# Three\n")
	rec = do(t, s, "DELETE", "/api/design/doc?file=three.md", "")
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "two.md", s.currentDoc())

	// Without .md, the way ade rm tolerates it.
	rec = do(t, s, "DELETE", "/api/design/doc?file=two", "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Empty(t, s.currentDoc())
}

func TestDeletingADesignDocumentStaysInTheDesignDirectory(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "one.md", "# One\n")

	for _, name := range []string{"", "../../etc/passwd.md", "/etc/passwd.md", "one.txt", "nope.md"} {
		rec := do(t, s, "DELETE", "/api/design/doc?file="+url.QueryEscape(name), "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, "accepted %q", name)
	}
	assert.FileExists(t, filepath.Join(s.docsDir, "one.md"), "nothing was deleted")
}

func TestAClosedDocumentStaysClosedHoweverItsNameWasSpelled(t *testing.T) {
	// The open document's name arrives from the branch match, from --doc and from
	// `ade create`, and those do not agree on case. An exact compare let the
	// poll reopen the document that had just been closed.
	s := newTestServer(t)
	name := designName(s.space().branch) + ".md"
	writeDoc(t, s, name, "# Design\n")
	require.Equal(t, name, s.currentDesign().File)

	// Opened under a different spelling than the one on disk.
	s.setDoc(strings.ToUpper(name))
	rec := do(t, s, "DELETE", "/api/design", "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	assert.Empty(t, s.currentDesign().File, "closed")
	assert.Empty(t, s.currentDesign().File, "and still closed on the next poll")
}

func TestADocumentShowsAPictureBesideIt(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n\n![a theme](ayu.png)\n")
	require.NoError(t, os.WriteFile(filepath.Join(s.docsDir, "ayu.png"), []byte("\x89PNG\r\n\x1a\nbytes"), 0o600))

	// The markdown keeps the plain name, which is what it means read outside
	// ade; the rendered page needs the route that serves the directory.
	// An <img> carries no header, so the token has to be on the url itself.
	assert.Contains(t, getDesign(t, s, "?file=plan.md").Body, `src="design-images/ayu.png?t=`+s.token+`"`)

	rec := do(t, s, "GET", "/design-images/ayu.png", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	assert.Equal(t, "\x89PNG\r\n\x1a\nbytes", rec.Body.String())
}

func TestADocumentPictureIsNotAWayOutOfTheDocsDir(t *testing.T) {
	s := newTestServer(t)
	require.NoError(t, os.MkdirAll(s.docsDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(s.ws.homeRoot, "secret.png"), []byte("no"), 0o600))

	for _, name := range []string{"..%2fsecret.png", "..%2f..%2fetc%2fpasswd", "plan.md", "notes.txt"} {
		rec := do(t, s, "GET", "/design-images/"+name, "")
		assert.Equalf(t, http.StatusNotFound, rec.Code, "%s was served", name)
	}
}

func TestAnAbsoluteOrRemotePictureIsLeftAlone(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "![a](https://example.com/a.png)\n\n![b](/b.png)\n")

	html := getDesign(t, s, "?file=plan.md").Body
	assert.Contains(t, html, `src="https://example.com/a.png"`)
	assert.Contains(t, html, `src="/b.png"`)
	assert.NotContains(t, html, "design-images")
}

func TestFoldingANamedAgentsReplyIntoTheDocument(t *testing.T) {
	// @architect answers into the conversation and cannot write. Folding what
	// it said into the document is the main agent's job, and this is the
	// button that asks for it.
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n\nCache the cart.\n")
	s.setDoc("plan.md")
	s.agent.resume("sess-design")
	s.say(ChatMessage{Kind: chat.KindAgent, Author: "architect", Body: "The cache needs an eviction rule."})

	said := s.chatStore().List("sess-design")[0]
	require.NotEmpty(t, said.ID)
	rec := do(t, s, "POST", "/api/design/fold", `{"id":"`+said.ID+`"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
	assert.Contains(t, s.conversation()[1].Body, "@architect", "the conversation says what was asked for")
}

func TestFoldingIsForNamedAgentsAndAnOpenDocument(t *testing.T) {
	s := newTestServer(t)
	s.agent.resume("sess-design")
	s.say(ChatMessage{Kind: chat.KindAgent, Author: "architect", Body: "The cache needs an eviction rule."})
	said := s.chatStore().List("sess-design")[0]

	// No document is not a run to spend.
	rec := do(t, s, "POST", "/api/design/fold", `{"id":"`+said.ID+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	writeDoc(t, s, "plan.md", "# Plan\n\nCache the cart.\n")
	s.setDoc("plan.md")
	rec = do(t, s, "POST", "/api/design/fold", `{"id":"nope"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "a reply that is not in the conversation")

	// The main agent's own words are the document's already.
	s.say(ChatMessage{Kind: chat.KindAgent, Body: "Done."})
	mine := s.chatStore().List("sess-design")[1]
	rec = do(t, s, "POST", "/api/design/fold", `{"id":"`+mine.ID+`"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestAPushedDocumentIsWrittenAndOpened is where a build sent to a worker
// lands: the document has to be both on disk and current before the build.
func TestAPushedDocumentIsWrittenAndOpened(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "POST", "/api/design/push", `{"name":"cart.md","body":"# Cart\n\nCache it.\n"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, "cart.md", s.currentDoc(), "the pushed document is the one a build builds")
	assert.Equal(t, "# Cart\n\nCache it.\n", getDesign(t, s, "?file=cart.md").Raw)

	// A second push is the same document again, not a conflict.
	rec = do(t, s, "POST", "/api/design/push", `{"name":"cart.md","body":"# Cart\n\nEvict it.\n"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "# Cart\n\nEvict it.\n", getDesign(t, s, "?file=cart.md").Raw)
}

func TestAnEmptyPushIsRefused(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/design/push", `{"name":"cart.md","body":"  "}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// A second Build press pushes the brief again. Under the first build that is
// the running agent's document being rewritten as it reads it.
func TestAPushIsRefusedWhileThatDocumentIsBeingBuilt(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n\nCache the cart.\n")
	s.setDoc("plan.md")
	_, ok := s.agent.begin(s.ctx, "build")
	require.True(t, ok)
	defer s.agent.finish(nil)

	rec := do(t, s, "POST", "/api/design/push", `{"name":"plan","body":"# Plan\n\nDelete the cart.\n"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "# Plan\n\nCache the cart.\n", getDesign(t, s, "?file=plan.md").Raw)

	// Another document is not the one being built, so it still lands.
	rec = do(t, s, "POST", "/api/design/push", `{"name":"cart.md","body":"# Cart\n"}`)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// stubLauncher stands in for the launcher on this machine, which is the only
// thing between a machine build and another host.
func stubLauncher(t *testing.T, ask func(ctx context.Context, method, path, body string, wait time.Duration) ([]byte, error)) {
	t.Helper()
	was := askLauncher
	askLauncher = ask
	t.Cleanup(func() { askLauncher = was })
}

// readyToBuildElsewhere is a session with a document and a repository the far
// machine could clone.
func readyToBuildElsewhere(t *testing.T, body string) *Server {
	t.Helper()
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", body)
	s.setDoc("plan.md")
	s.projectName, s.projectFor = "owen/ade", s.space().homeRoot
	return s
}

// The request is the whole of a machine build, so what it carries is the test:
// the slug to clone, the agent picked, and the document itself as the brief.
func TestAMachineBuildHandsOverTheDocument(t *testing.T) {
	s := readyToBuildElsewhere(t, "# Plan\n\nCache the cart.\n")

	var sent string
	stubLauncher(t, func(_ context.Context, method, path, body string, _ time.Duration) ([]byte, error) {
		assert.Equal(t, http.MethodPost, method)
		assert.Equal(t, "/api/open", path)
		sent = body
		return []byte(`{"url":"http://127.0.0.1:1234/s/abc/?t=x"}`), nil
	})

	rec := do(t, s, "POST", "/api/design/go", `{"machine":"ade-owen","provider":"codex","model":"gpt-5.6-terra"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	var ask map[string]any
	require.NoError(t, json.Unmarshal([]byte(sent), &ask))
	assert.Equal(t, "ade-owen", ask["machine"])
	assert.Equal(t, "owen/ade", ask["dir"], "the far machine clones a slug, not a path here")
	assert.Equal(t, "codex", ask["agent"])
	assert.Equal(t, "gpt-5.6-terra", ask["model"])
	assert.Equal(t, true, ask["fresh"])
	brief, ok := ask["brief"].(map[string]any)
	require.True(t, ok, "no brief: the agent over there would invent one")
	assert.Equal(t, "plan.md", brief["name"])
	assert.Equal(t, "# Plan\n\nCache the cart.\n", brief["body"])
	assert.Equal(t, true, brief["build"])

	var got struct {
		URL string `json:"url"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "http://127.0.0.1:1234/s/abc/?t=x", got.URL, "the page follows the build")
	assert.False(t, s.agent.State().Running, "the local agent is not what builds over there")
	assert.Equal(t, "plan.md", s.currentDoc(), "this session keeps its document")
}

// A hand-off that failed part way still opened a session, and its address is
// the only one the user will ever be given for it.
func TestAFailedMachineBuildNamesTheSessionItLeft(t *testing.T) {
	s := readyToBuildElsewhere(t, "# Plan\n\nCache the cart.\n")
	stubLauncher(t, func(context.Context, string, string, string, time.Duration) ([]byte, error) {
		body := `{"url":"http://127.0.0.1:1234/s/abc/?t=x","error":"start the build: the agent is already running"}`
		return []byte(body), errors.New("start the build: the agent is already running")
	})

	rec := do(t, s, "POST", "/api/design/go", `{"machine":"ade-owen"}`)
	require.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "http://127.0.0.1:1234/s/abc/")
	assert.False(t, s.agent.State().Running)
}

// A build aimed at a machine must leave this session's agent alone, whatever
// the hand-off does.
func TestBuildingOnAMachineDoesNotStartTheLocalAgent(t *testing.T) {
	s := readyToBuildElsewhere(t, "# Plan\n\nCache the cart.\n")
	stubLauncher(t, func(context.Context, string, string, string, time.Duration) ([]byte, error) {
		return nil, errors.New("no launcher is running on this machine")
	})

	rec := do(t, s, "POST", "/api/design/go", `{"machine":"ade-owen"}`)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.False(t, s.agent.State().Running, "the local agent is not what builds over there")
}

// A machine build with nothing to clone is refused here rather than handed to
// a launcher that would have to guess.
func TestAMachineBuildNeedsARepositoryToClone(t *testing.T) {
	s := newTestServer(t)
	writeDoc(t, s, "plan.md", "# Plan\n")
	s.setDoc("plan.md")
	stubLauncher(t, func(context.Context, string, string, string, time.Duration) ([]byte, error) {
		t.Fatal("the launcher must not be asked without a remote to clone")
		return nil, nil
	})

	rec := do(t, s, "POST", "/api/design/go", `{"machine":"ade-owen"}`)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "origin remote")
}

// Without the path there is no folder to open a window on, and a build sent to
// a machine is a build nobody can watch.
func TestAMachineBuildSaysWhereTheCheckoutIs(t *testing.T) {
	s := readyToBuildElsewhere(t, "# Plan\n\nCache the cart.\n")
	stubLauncher(t, func(context.Context, string, string, string, time.Duration) ([]byte, error) {
		return []byte(`{"url":"http://127.0.0.1:1234/s/abc/?t=x","path":"/home/ade/srv/ade"}`), nil
	})

	rec := do(t, s, "POST", "/api/design/go", `{"machine":"ade-owen"}`)
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	var got struct {
		Path    string `json:"path"`
		Machine string `json:"machine"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "/home/ade/srv/ade", got.Path)
	assert.Equal(t, "ade-owen", got.Machine)
}

// The Electron app had a Condense button. It is the condenser that does the
// work, beside the conversation, because the rewrite is a long read-and-check.
func TestDesignCondenseCallsTheCondenser(t *testing.T) {
	s := newTestServer(t)
	s.agent.bin = filepath.Join(t.TempDir(), "no-such-claude")
	defineAgent(t, s, "condenser", "---\nname: condenser\n---\nconsolidate\n")
	writeDoc(t, s, "plan.md", "# Plan\n\n## Approach\n\n- the long version\n")
	s.setDoc("plan.md")

	rec := do(t, s, "POST", "/api/design/condense", "")
	require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())

	said := s.conversation()
	require.NotEmpty(t, said, "what was asked is on screen")
	assert.Contains(t, said[0].Body, "@condenser")
	assert.Contains(t, said[0].Body, "plan.md", "the condenser asks which one otherwise")
	require.NotNil(t, said[0].Context)
	assert.Equal(t, "plan.md", said[0].Context.From)
}

func TestDesignCondenseNeedsADocument(t *testing.T) {
	s := newTestServer(t)
	rec := do(t, s, "POST", "/api/design/condense", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, s.conversation())
}
