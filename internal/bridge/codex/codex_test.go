package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/limits"
)

// sse writes one response's worth of events as the backend would.
func sse(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, ev := range events {
		fmt.Fprintf(w, "data: %s\n\n", ev)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func textDelta(s string) string {
	b, _ := json.Marshal(map[string]any{"type": "response.output_text.delta", "delta": s})
	return string(b)
}

func functionCall(id, name, args string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "response.output_item.done",
		"item": map[string]any{"type": "function_call", "call_id": id, "name": name, "arguments": args},
	})
	return string(b)
}

const completed = `{"type":"response.completed","response":{}}`

func TestRunAnswersToolCallsAndReturnsTheFinalText(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "plan.md"), []byte("# Plan\ncache forever\n"), 0o600))

	var calls atomic.Int32
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/responses", r.URL.Path)
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		assert.Equal(t, "acct", r.Header.Get("chatgpt-account-id"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		requests = append(requests, body)
		switch calls.Add(1) {
		case 1:
			sse(w, textDelta("Let me read it."), functionCall("c1", "read_file", `{"path":"plan.md"}`), completed)
		default:
			sse(w, textDelta("The plan "), textDelta("never evicts."), completed)
		}
	}))
	defer server.Close()

	var seen []bridge.Event
	turn, err := Run(context.Background(), Opts{
		Model:        "gpt-5",
		Instructions: "be a reviewer",
		Tools:        RepoTools(root, nil),
		Credentials:  Credentials{AccessToken: "tok", RefreshToken: "r", AccountID: "acct"},
		BaseURL:      server.URL,
		OnEvent:      func(ev bridge.Event) { seen = append(seen, ev) },
	}, "review plan.md")
	require.NoError(t, err)
	assert.Equal(t, "The plan never evicts.", turn.Text)
	assert.EqualValues(t, 2, calls.Load())

	// The system prompt and the tools went with the first request.
	assert.Equal(t, "be a reviewer", requests[0]["instructions"])
	assert.Len(t, requests[0]["tools"], 3)
	// The second carried the call and what it read, so the model saw it.
	input := requests[1]["input"].([]any)
	require.Len(t, input, 4, "prompt, assistant text, the call, its output")
	assert.Equal(t, "function_call_output", input[3].(map[string]any)["type"])
	assert.Contains(t, input[3].(map[string]any)["output"], "cache forever")

	// Events in the shape the conversation is written from: prose and the
	// tool call, then its result, then the reply.
	require.Len(t, seen, 3)
	assert.Equal(t, "Let me read it.", seen[0].Text)
	require.Len(t, seen[0].ToolUses, 1)
	assert.Equal(t, "read_file", seen[0].ToolUses[0].Name)
	require.Len(t, seen[1].ToolResults, 1)
	assert.Equal(t, "c1", seen[1].ToolResults[0].ID)
	assert.False(t, seen[1].ToolResults[0].IsError)
	assert.Equal(t, "The plan never evicts.", seen[2].Text)
}

func TestRunReportsTheWindowsTheBackendPutsOnAResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-codex-primary-used-percent", "33")
		w.Header().Set("x-codex-primary-window-minutes", "300")
		sse(w, textDelta("ok"), completed)
	}))
	defer server.Close()

	var seen []limits.Limits
	_, err := Run(context.Background(), Opts{
		Credentials: Credentials{AccessToken: "t"}, BaseURL: server.URL,
		OnLimits: func(l limits.Limits) { seen = append(seen, l) },
	}, "hi")
	require.NoError(t, err)
	require.Len(t, seen, 1)
	assert.Equal(t, "codex", seen[0].Provider)
	assert.Equal(t, 33.0, seen[0].Windows[0].Percent)
}

func TestRunRefreshesAnExpiredLoginOnce(t *testing.T) {
	var responses, refreshes atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/responses", func(w http.ResponseWriter, r *http.Request) {
		if responses.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		assert.Equal(t, "Bearer fresh", r.Header.Get("Authorization"))
		sse(w, textDelta("hello"), completed)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		refreshes.Add(1)
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "refresh_token", body["grant_type"])
		assert.Equal(t, "old-refresh", body["refresh_token"])
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "fresh", "refresh_token": "newer"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	turn, err := Run(context.Background(), Opts{
		Credentials: Credentials{AccessToken: "stale", RefreshToken: "old-refresh"},
		BaseURL:     server.URL,
		TokenURL:    server.URL + "/token",
	}, "hi")
	require.NoError(t, err)
	assert.Equal(t, "hello", turn.Text)
	assert.EqualValues(t, 1, refreshes.Load())
	assert.EqualValues(t, 2, responses.Load())
}

func TestAnUnsupportedModelSaysWhichOnesWork(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/responses", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"The 'gpt-9' model is not supported when using Codex with a ChatGPT account."}`))
	})
	mux.HandleFunc("/models", func(w http.ResponseWriter, r *http.Request) {
		assert.NotEmpty(t, r.URL.Query().Get("client_version"))
		_, _ = w.Write([]byte(`{"models":[
			{"slug":"gpt-5.4-mini","display_name":"GPT-5.4-Mini","visibility":"list","priority":2},
			{"slug":"codex-auto-review","visibility":"hide","priority":9},
			{"slug":"gpt-5.5","display_name":"GPT-5.5","visibility":"list","priority":1,"context_window":400000}]}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	_, err := Run(context.Background(), Opts{Model: "gpt-9", Credentials: Credentials{AccessToken: "t"}, BaseURL: server.URL}, "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "'gpt-9' model is not supported")
	assert.Contains(t, err.Error(), "this account can use: gpt-5.5, gpt-5.4-mini")

	models, err := Models(context.Background(), Opts{Credentials: Credentials{AccessToken: "t"}, BaseURL: server.URL})
	require.NoError(t, err)
	require.Len(t, models, 2, "hidden ones are not offered")
	assert.Equal(t, "gpt-5.5", models[0].Slug)
	assert.Equal(t, 400000, models[0].ContextWindow)
}

func TestARefreshLandsInTheCLIsFile(t *testing.T) {
	home := t.TempDir()
	cli := filepath.Join(home, ".codex", "auth.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(cli), 0o750))
	require.NoError(t, os.WriteFile(cli,
		[]byte(`{"OPENAI_API_KEY":"sk-key","tokens":{"access_token":"stale","refresh_token":"r1","account_id":"acc"}}`), 0o600))

	mux := http.NewServeMux()
	mux.HandleFunc("/responses", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sse(w, textDelta("ok"), completed)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "fresh", "refresh_token": "r2"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	creds, err := LoadCredentials(home)
	require.NoError(t, err)
	_, err = Run(context.Background(), Opts{Credentials: creds, Home: home, BaseURL: server.URL, TokenURL: server.URL + "/token"}, "hi")
	require.NoError(t, err)

	// `codex` run directly must not find the token this refresh voided.
	again, err := LoadCredentials(home)
	require.NoError(t, err)
	assert.Equal(t, "fresh", again.AccessToken)
	assert.Equal(t, "r2", again.RefreshToken)
	assert.Equal(t, "acc", again.AccountID)
	data, err := os.ReadFile(cli)
	require.NoError(t, err)
	assert.Contains(t, string(data), "sk-key", "the rest of the file is not ade's")
}

func TestRunReportsABackendFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sse(w, `{"type":"response.failed","response":{"error":{"message":"quota exhausted"}}}`)
	}))
	defer server.Close()

	_, err := Run(context.Background(), Opts{Credentials: Credentials{AccessToken: "t"}, BaseURL: server.URL}, "hi")
	assert.ErrorContains(t, err, "quota exhausted")
}

func TestRunStopsAModelThatNeverStopsCallingTools(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sse(w, functionCall("c", "list_files", `{}`), completed)
	}))
	defer server.Close()

	_, err := Run(context.Background(), Opts{Credentials: Credentials{AccessToken: "t"}, BaseURL: server.URL, Tools: RepoTools(root, nil)}, "go")
	assert.ErrorContains(t, err, "gave up")
}

func TestLoadCredentialsReadsTheCLIsLogin(t *testing.T) {
	home := t.TempDir()
	_, err := LoadCredentials(home)
	assert.ErrorContains(t, err, "codex login")

	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".codex", "auth.json"),
		[]byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"a","refresh_token":"r","account_id":"acc"},"last_refresh":"2026-08-01T00:00:00Z"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".codex", "config.toml"),
		[]byte("model = \"gpt-5.4-mini\"\nmodel_reasoning_effort = \"medium\"\n\n[marketplaces.x]\nmodel = \"not this one\"\n"), 0o600))

	creds, err := LoadCredentials(home)
	require.NoError(t, err)
	assert.Equal(t, "a", creds.AccessToken)
	assert.Equal(t, "acc", creds.AccountID)
	assert.Equal(t, "gpt-5.4-mini", DefaultModel(home))
	assert.Equal(t, "medium", DefaultEffort(home))
}

func TestRepoToolsStayInsideTheRepository(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "internal", "x"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "internal", "x", "a.go"), []byte("package x\n\nfunc Cache() {}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("hello\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: Cache\n"), 0o600))
	secret := filepath.Join(t.TempDir(), "auth.json")
	require.NoError(t, os.WriteFile(secret, []byte("tokens"), 0o600))

	tools := map[string]Tool{}
	for _, tool := range RepoTools(root, nil) {
		tools[tool.Name] = tool
	}
	ctx := context.Background()

	out, err := tools["read_file"].Run(ctx, json.RawMessage(`{"path":"internal/x/a.go"}`))
	require.NoError(t, err)
	assert.Contains(t, out, "func Cache")

	for _, path := range []string{"../auth.json", secret, "internal/../../auth.json"} {
		_, err := tools["read_file"].Run(ctx, json.RawMessage(`{"path":"`+strings.ReplaceAll(path, `\`, `\\`)+`"}`))
		assert.Error(t, err, path)
	}

	// The design document is named to an agent by its path under .git, which
	// the walk skips, so read_file is the only way to it — and a model reaches
	// for the absolute form of whatever path it was handed.
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git", "ade", "docs"), 0o750))
	doc := filepath.Join(root, ".git", "ade", "docs", "fix-735.md")
	require.NoError(t, os.WriteFile(doc, []byte("# the design\n"), 0o600))
	for _, path := range []string{".git/ade/docs/fix-735.md", doc} {
		out, err := tools["read_file"].Run(ctx, json.RawMessage(`{"path":"`+strings.ReplaceAll(path, `\`, `\\`)+`"}`))
		require.NoError(t, err, path)
		assert.Contains(t, out, "the design", path)
	}

	listed, err := tools["list_files"].Run(ctx, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "README.md\ninternal/x/a.go", listed, ".git is not listed")

	found, err := tools["grep"].Run(ctx, json.RawMessage(`{"pattern":"Cache"}`))
	require.NoError(t, err)
	assert.Equal(t, "internal/x/a.go:3: func Cache() {}", found, ".git is not searched")

	none, err := tools["grep"].Run(ctx, json.RawMessage(`{"pattern":"nothing-here"}`))
	require.NoError(t, err)
	assert.Equal(t, "no matches", none)
}

func TestRepoToolsDoNotFollowASymlinkOutOfTheRepository(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(t.TempDir(), "auth.json")
	require.NoError(t, os.WriteFile(secret, []byte(`{"tokens":"SECRET"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "ok.md"), []byte("fine\n"), 0o600))
	// A branch under review can carry a symlink to anywhere.
	require.NoError(t, os.Symlink(secret, filepath.Join(root, "notes.md")))
	require.NoError(t, os.Symlink(filepath.Dir(secret), filepath.Join(root, "elsewhere")))

	tools := map[string]Tool{}
	for _, tool := range RepoTools(root, nil) {
		tools[tool.Name] = tool
	}
	ctx := context.Background()

	_, err := tools["read_file"].Run(ctx, json.RawMessage(`{"path":"notes.md"}`))
	assert.ErrorContains(t, err, "outside the repository")
	_, err = tools["read_file"].Run(ctx, json.RawMessage(`{"path":"elsewhere/auth.json"}`))
	assert.ErrorContains(t, err, "outside the repository")

	// Neither the walk nor the search goes through the link, even unasked.
	listed, err := tools["list_files"].Run(ctx, json.RawMessage(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "ok.md", listed)
	found, err := tools["grep"].Run(ctx, json.RawMessage(`{"pattern":"SECRET"}`))
	require.NoError(t, err)
	assert.Equal(t, "no matches", found)

	// A link that stays inside is fine.
	require.NoError(t, os.Symlink(filepath.Join(root, "ok.md"), filepath.Join(root, "alias.md")))
	out, err := tools["read_file"].Run(ctx, json.RawMessage(`{"path":"alias.md"}`))
	require.NoError(t, err)
	assert.Equal(t, "1\tfine\n", out)
}

func TestAReaderGetsLineNumbersAndAnEditorGetsTheFileItself(t *testing.T) {
	// A reviewer is asked to anchor every point to a line, and was reading
	// files with no line in them. An agent that edits must not be given them:
	// edit_file matches on the file's own text.
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), []byte("package x\n\nfunc Cache() {}\n"), 0o600))
	ctx := context.Background()
	read := func(tools []Tool) string {
		for _, tool := range tools {
			if tool.Name != "read_file" {
				continue
			}
			out, err := tool.Run(ctx, json.RawMessage(`{"path":"a.go"}`))
			require.NoError(t, err)
			return out
		}
		require.Fail(t, "no read_file")
		return ""
	}

	assert.Equal(t, "1\tpackage x\n2\t\n3\tfunc Cache() {}\n", read(RepoTools(root, nil)))
	assert.Equal(t, "1\tpackage x\n2\t\n3\tfunc Cache() {}\n", read(RepoTools(root, []string{"Read", "Grep", "Glob"})))
	assert.Equal(t, "package x\n\nfunc Cache() {}\n", read(RepoTools(root, []string{"Read", "Edit"})))
	assert.Equal(t, "package x\n\nfunc Cache() {}\n", read(RepoTools(root, []string{"Read", "Write"})))
}

func TestTheReadItselfRefusesALinkOutOfTheRoot(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(t.TempDir(), "auth.json")
	require.NoError(t, os.WriteFile(secret, []byte("SECRET"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "ok.md"), []byte("fine\n"), 0o600))
	require.NoError(t, os.Symlink(secret, filepath.Join(root, "notes.md")))
	require.NoError(t, os.Symlink(filepath.Dir(secret), filepath.Join(root, "elsewhere")))

	// The open refuses the escape on its own, without the by-name check in
	// front of it: a link put there after that check is still not followed.
	base, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	for _, sub := range []string{"notes.md", "elsewhere/auth.json", "../" + filepath.Base(filepath.Dir(secret)) + "/auth.json"} {
		f, err := openUnder(base, sub)
		if err == nil {
			_ = f.Close()
		}
		assert.Error(t, err, sub)
	}

	_, err = readInside(root, "notes.md")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "SECRET")
	data, err := readInside(root, "ok.md")
	require.NoError(t, err)
	assert.Equal(t, "fine\n", string(data))
}

func TestRepoToolsAreNamedTheWayClaudeCodeNamesThem(t *testing.T) {
	root := t.TempDir()
	names := func(tools []Tool) []string {
		var out []string
		for _, tool := range tools {
			out = append(out, tool.Name)
		}
		return out
	}
	assert.Equal(t, []string{"read_file", "list_files", "grep"}, names(RepoTools(root, nil)))
	assert.Equal(t, []string{"read_file", "grep"}, names(RepoTools(root, []string{"Read", "Grep"})))
	assert.Equal(t, []string{"list_files"}, names(RepoTools(root, []string{"Glob"})))
	assert.Equal(t, []string{"write_file", "edit_file"}, names(RepoTools(root, []string{"Write", "Edit"})))
	// Tools only Claude has are not ours to offer.
	assert.Empty(t, names(RepoTools(root, []string{"Bash", "Task"})))
}

func TestWriteToolsStayInsideTheRepositoryAndOutOfGit(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git", "hooks"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n\nfunc One() {}\nfunc Two() {}\n"), 0o600))
	elsewhere := t.TempDir()
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(root, "out")))
	require.NoError(t, os.Symlink(filepath.Join(elsewhere, "target"), filepath.Join(root, "link.go")))

	tools := map[string]Tool{}
	for _, tool := range RepoTools(root, []string{"Write", "Edit"}) {
		tools[tool.Name] = tool
	}
	ctx := context.Background()
	write := func(path, content string) error {
		args, _ := json.Marshal(map[string]string{"path": path, "content": content})
		_, err := tools["write_file"].Run(ctx, args)
		return err
	}

	require.NoError(t, write("new/dir.go", "package dir\n"), "a new file, in a new directory, is fine")
	got, err := os.ReadFile(filepath.Join(root, "new", "dir.go"))
	require.NoError(t, err)
	assert.Equal(t, "package dir\n", string(got))

	for _, path := range []string{"../escape.go", filepath.Join(elsewhere, "abs.go"), "out/x.go", "link.go", ".git/hooks/pre-commit", ".git", "", "new/"} {
		assert.Error(t, write(path, "x"), path)
	}
	assert.NoFileExists(t, filepath.Join(elsewhere, "target"))
	assert.NoFileExists(t, filepath.Join(elsewhere, "x.go"))
	assert.NoFileExists(t, filepath.Join(root, ".git", "hooks", "pre-commit"))

	_, err = tools["edit_file"].Run(ctx, json.RawMessage(`{"path":"a.go","old_string":"func ","new_string":"func X"}`))
	assert.ErrorContains(t, err, "appears 2 times")
	_, err = tools["edit_file"].Run(ctx, json.RawMessage(`{"path":"a.go","old_string":"Three","new_string":"X"}`))
	assert.ErrorContains(t, err, "not found")
	out, err := tools["edit_file"].Run(ctx, json.RawMessage(`{"path":"a.go","old_string":"func Two() {}","new_string":"func Two() int { return 2 }"}`))
	require.NoError(t, err)
	assert.Contains(t, out, "1 replacement")
	got, _ = os.ReadFile(filepath.Join(root, "a.go"))
	assert.Equal(t, "package a\n\nfunc One() {}\nfunc Two() int { return 2 }\n", string(got))
	_, err = tools["edit_file"].Run(ctx, json.RawMessage(`{"path":"a.go","old_string":"func ","new_string":"func x","replace_all":true}`))
	require.NoError(t, err)
	got, _ = os.ReadFile(filepath.Join(root, "a.go"))
	assert.Equal(t, 2, strings.Count(string(got), "func x"))
}

func TestAReaderReachesTheDesignDocOutsideTheCheckout(t *testing.T) {
	// A worktree's design documents live under the main checkout's git dir, so
	// the absolute path the agent is handed is not under its own root.
	root := t.TempDir()
	docs := t.TempDir()
	secret := filepath.Join(t.TempDir(), "auth.json")
	require.NoError(t, os.WriteFile(filepath.Join(docs, "fix-735.md"), []byte("the design\n"), 0o600))
	require.NoError(t, os.WriteFile(secret, []byte("SECRET"), 0o600))

	var tool Tool
	for _, got := range RepoTools(root, []string{"Read"}, docs) {
		if got.Name == "read_file" {
			tool = got
		}
	}
	require.NotEmpty(t, tool.Name)
	read := func(path string) (string, error) {
		raw, err := json.Marshal(map[string]string{"path": path})
		require.NoError(t, err)
		return tool.Run(context.Background(), raw)
	}

	out, err := read(filepath.Join(docs, "fix-735.md"))
	require.NoError(t, err)
	assert.Equal(t, "1\tthe design\n", out)

	_, err = read(secret)
	assert.Error(t, err)
	_, err = read(filepath.Join(docs, "..", filepath.Base(filepath.Dir(secret)), "auth.json"))
	assert.Error(t, err)
}

func TestWithNoDirBesideItTheReadIsTheCheckoutAlone(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "notes.md")
	require.NoError(t, os.WriteFile(outside, []byte("elsewhere\n"), 0o600))
	for _, tool := range RepoTools(root, []string{"Read"}) {
		if tool.Name != "read_file" {
			continue
		}
		raw, err := json.Marshal(map[string]string{"path": outside})
		require.NoError(t, err)
		_, err = tool.Run(context.Background(), raw)
		assert.Error(t, err)
	}
}
