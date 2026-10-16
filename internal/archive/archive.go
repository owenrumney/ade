// Package archive finishes with a design document: it distils one into a page
// on the shared wiki, then deletes the local file once that page is pushed.
package archive

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/owenrumney/ade/internal/cleanse"
	"github.com/owenrumney/ade/internal/docs"
	"github.com/owenrumney/ade/internal/prompt"
	"github.com/owenrumney/ade/internal/wiki"
)

// Model is the cheap one-shot that distils, the same one a reply is rewritten
// short with: condensing prose is not reasoning.
const Model = cleanse.Model

// Runner sends one prompt to a model with no tools. Tests inject one.
type Runner func(ctx context.Context, prompt string) (string, error)

// Doc is one design document: the directory designs live in, and its name
// within it.
type Doc struct {
	Dir  string `json:"dir"`
	File string `json:"file"`
}

// Outcome is what became of one archive.
type Outcome struct {
	File   string `json:"file"`
	Page   string `json:"page"`
	State  string `json:"state"`
	Commit string `json:"commit,omitempty"`
}

// One distils a document, publishes the page, and deletes the local file. A
// publish that does not land returns the error and leaves the document alone.
func One(ctx context.Context, store *wiki.Store, repo string, doc Doc, pr string, model Runner) (Outcome, error) {
	out := Outcome{File: doc.File, Page: wiki.DesignPath(repo, docs.Stem(doc.File))}
	if store == nil || !store.Enabled() {
		return out, errors.New("wiki is disabled; run ade wiki init --remote <url>")
	}
	if model == nil {
		return out, errors.New("no model to distil with")
	}
	body, err := docs.Read(doc.Dir, doc.File)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(body) == "" {
		return out, fmt.Errorf("%s is empty; ade rm deletes a document without publishing one", doc.File)
	}
	reply, err := model(ctx, Prompt(repo, doc.File, body))
	if err != nil {
		return out, fmt.Errorf("distil %s: %w", doc.File, err)
	}
	page, err := Page(reply, pr)
	if err != nil {
		return out, fmt.Errorf("distil %s: %w", doc.File, err)
	}
	sub, err := store.PublishDesign(ctx, repo, docs.Stem(doc.File), page)
	out.State, out.Commit = sub.State, sub.Commit
	if err != nil {
		return out, err
	}
	if sub.State != "published" && sub.State != "unchanged" {
		return out, fmt.Errorf("publish %s: %s", out.Page, sub.State)
	}
	if _, err := docs.Delete([]string{doc.Dir}, doc.File); err != nil {
		return out, fmt.Errorf("published %s, but %s is still here: %w", out.Page, doc.File, err)
	}
	return out, nil
}

// Prompt is the whole of what the one-shot reads: the document, and what a
// page made of it is for.
func Prompt(repo, file, body string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `The work described by this design document of the repository %s is finished.
Write the wiki page it leaves behind, for someone who was not there and is
about to touch the same code.

What belongs on the page:

- What was built, and the decisions, each with the reason it was made that way.
- The gotchas: what looked right and was not, what a measurement ruled out.
- Nothing else. Not the argument that got there, not a question that was
  settled on the way, not anything that can be read off the code, and never a
  secret, key or token.

The page starts with a single "# " heading naming the design. Keep it under
%d bytes, and well under that where the design was small: a page nobody
finishes is worse than a short one.

<document name=%q>
%s
</document>

Answer with the page's Markdown and nothing else: no preamble, no explanation,
and no code fence around the whole answer.`,
		repo, wiki.MaxPage, file, prompt.Escape(strings.TrimSpace(body)))
	return b.String()
}

// Page is the model's reply as a page: unfenced, with the pull request it came
// from under its heading. An empty or over-long answer is refused.
func Page(reply, pr string) (string, error) {
	page := strings.TrimSpace(unfence(strings.TrimSpace(reply)))
	if page == "" {
		return "", errors.New("the model wrote no page")
	}
	page = withPR(page, pr)
	if len(page) > wiki.MaxPage {
		return "", fmt.Errorf("the page is %d bytes, %d at most", len(page), wiki.MaxPage)
	}
	return page, nil
}

// withPR puts the pull request under the heading. It is the only route left to
// the diff: the branch the document was named for is gone.
func withPR(page, pr string) string {
	pr = strings.TrimSpace(pr)
	if pr == "" {
		return page
	}
	line := "Pull request: " + pr
	heading, rest, ok := strings.Cut(page, "\n")
	if !ok || !strings.HasPrefix(heading, "# ") {
		return line + "\n\n" + page
	}
	if rest = strings.TrimSpace(rest); rest == "" {
		return heading + "\n\n" + line
	}
	return heading + "\n\n" + line + "\n\n" + rest
}

// unfence strips one fence round the whole answer and leaves fences inside it.
func unfence(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) < 2 || !fence(lines[0]) {
		return text
	}
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "```" && last != "~~~" {
		return text
	}
	for _, line := range lines[1 : len(lines)-1] {
		if fence(line) {
			return text
		}
	}
	return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
}

func fence(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~")
}
