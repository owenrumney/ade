// Command seedchat puts a named agent's reply in a fixture conversation, so the
// screenshots have one. Runs first: bbolt locks the file to the server.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/render"
)

// failure is what a check's verdict folds away, so the note is shot with
// something real under it rather than one line.
const failure = `# github.com/owenrumney/ade/internal/launcher
internal/launcher/hosts.go:412:2: unreachable code
internal/launcher/hosts.go:587:21: non-constant format string in call to fmt.Errorf`

const question = `{"questions":[{"question":"How should the diff tree be ordered?","header":"Ordering","options":[{"label":"By path","description":"alphabetical, stable between runs"},{"label":"By churn","description":"most-changed first"}],"multiSelect":false}]}`

func main() {
	short := flag.String("short", "", "the reply rewritten short, as the cleanse would have")
	flag.Parse()
	args := flag.Args()
	if len(args) != 4 && len(args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: seedchat [-short text] <git-dir> <session> <agent> <text> [tool-output]")
		os.Exit(2)
	}
	store, err := chat.Open(chat.Path(args[0]))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = store.Close() }()

	// Rendered here, as the server would: without html the page draws the
	// markdown as text and a table shot shows pipes.
	msg := chat.Message{Kind: chat.KindAgent, Author: args[2], Body: args[3], HTML: markdown(args[3])}
	if *short != "" {
		msg.Short, msg.ShortHTML = *short, markdown(*short)
	}
	if _, err := store.Append(args[1], msg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(args) == 5 {
		tool := chat.Message{Kind: chat.KindTool, Tool: "Bash", Target: "npm run dev", Output: args[4]}
		if _, err := store.Append(args[1], tool); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		ruled := chat.Message{Kind: chat.KindTool, Tool: "Bash", Target: "go test ./internal/permissions/ 2>&1 | tail -20",
			Rule: "Bash(go test:*), Bash(tail:*)", Output: "ok  github.com/owenrumney/ade/internal/permissions  0.2s"}
		if _, err := store.Append(args[1], ruled); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		held := chat.Message{Kind: chat.KindTool, Tool: "Bash", ToolID: "seed-permission", Target: "curl -s https://example.com | head -1"}
		if _, err := store.Append(args[1], held); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		asked := chat.Message{Kind: chat.KindTool, Tool: "AskUserQuestion", ToolID: "seed-question", Input: question}
		if _, err := store.Append(args[1], asked); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		verdict := chat.Message{Kind: chat.KindNote, Failed: true, Body: "vet failed", Output: failure}
		if _, err := store.Append(args[1], verdict); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		// Two prompts typed during one run and sent as one turn.
		first, err := store.Append(args[1], chat.Message{Kind: chat.KindYou, Body: "make the retry loop reset its counter on success"})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		folded := chat.Message{Kind: chat.KindYou, Body: "and cover it in retry_test.go", FoldedInto: first.ID}
		if _, err := store.Append(args[1], folded); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

func markdown(body string) string {
	html, err := render.Markdown([]byte(body), render.Options{Theme: render.ThemeDark})
	if err != nil {
		return ""
	}
	return string(html)
}
