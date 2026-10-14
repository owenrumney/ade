// Package outgoing rewrites your own review findings as something you would
// actually say to the author. It does not send them.
package outgoing

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/owenrumney/ade/internal/comments"
)

// fence is what the reply is wrapped in, so a model that adds a sentence either
// side of it does not break the parse.
const fence = "ade-outgoing"

// FastModel is what the rewrite runs on unless you have chosen otherwise. This
// is a rephrase of text you already wrote, in one pass, with no code to read,
// so the big model is not needed — but the smallest one follows the voice
// loosely and adds fixes the note never made. Sonnet holds the voice, keeps to
// the note, and is no slower: the time is claude starting, not the model.
const FastModel = "sonnet"

// Prompt rewrites every finding in one turn. One call rather than one per
// comment: they are short, they share a voice, and the round trip is most of
// the cost.
func Prompt(threads []comments.Thread) string {
	var b strings.Builder

	b.WriteString(`Rewrite these code review notes as comments to leave on a pull request.

They are the reviewer's own terse findings, written for nobody but them. You
are turning each one into what they would actually type to the author: a
colleague who has read the code closely and is on the author's side.

The voice:
- First person, as the reviewer. Plain words and contractions, the way a
  message reads, not a report. "I think", "looks like", "worth a check" where
  the reviewer is unsure; no hedging on what the note is sure of.
- Warm without being soft. A real problem stays a problem; it is just said the
  way you would say it to someone you like working with. Not deferential, not
  stiff, no praise to cushion it.
- Never "please consider", "it would be beneficial", "ensure that", "it is
  recommended", "we should" — the note's words, said by a person.
- No greeting, no sign-off, no name.

Length, which matters more than anything else here:
- One to three sentences per finding. Never a paragraph, never two.
- Under 40 words a finding. A note that is itself longer may match it, never
  exceed it.
- The rewrite is nearly always about as long as the note. If yours is much
  longer, you have added something, and the something is padding.

Where a note holds several findings, from several reviewers on the same line:
- One comment, covering every one of them. Nothing is dropped, and nothing is
  merged into a vaguer point than either reviewer made.
- A sentence or two each, in the order they appear, hardest problem first.
- Attribute nothing. The author is reading one comment from one person.

The content:
- Keep the technical content exactly. Do not drop a detail, and do not add
  one: no fix the note did not suggest, no cause it did not name.
- Say what the problem is in the first sentence.
- Where the note is a question, leave it a question.
- No preamble, no restating the code back, no explaining why the problem
  matters unless the note explained it.
- Markdown is fine. Backticks for identifiers, a fenced block only for code.
- Do not read any file or run anything. Everything you need is below.

For example, the note "err is swallowed here; caller cannot tell absence from
failure" becomes something like:

  This swallows the error, so a caller can't tell "not there" from "the lookup
  failed" — I think it needs to come back out.

And "--dev is undocumented in the README" becomes:

  Small one: ` + "`--dev`" + ` isn't in the README yet. Could you add it while
  you're in here?

What that second one must not become:

  I noticed while reading through that the ` + "`--dev`" + ` flag doesn't appear to be
  documented in the README at the moment. It might be worth adding a short
  section describing what it does, since users coming to the project fresh
  would otherwise have no way of discovering it. Happy to help if useful!

That is the same point at five times the length, with three things the note
never said. Send the short one.

Reply with a `)
	fmt.Fprintf(&b, "```%s fenced block holding a JSON object that maps each id to its\n", fence)
	b.WriteString(`rewritten text, and nothing else:

` + "```" + fence + `
{"t3": "The retry loop here never resets ...", "t7": "Could we pull this ..."}
` + "```" + `

The notes:

`)

	for _, thread := range threads {
		fmt.Fprintf(&b, "<note id=%q file=%q line=%d>\n%s\n</note>\n\n",
			thread.ID, thread.File, thread.Line, body(thread))
	}

	return b.String()
}

// Parse pulls the rewritten text out of the reply, keyed by thread id.
func Parse(reply string) (map[string]string, error) {
	block := strings.TrimSpace(between(reply, "```"+fence, "```"))
	if block == "" {
		// A model that answered with bare JSON is still answering.
		block = strings.TrimSpace(between(reply, "```json", "```"))
	}
	if block == "" {
		block = strings.TrimSpace(reply)
	}
	if block == "" {
		return nil, fmt.Errorf("no rewritten comments in the reply")
	}

	var out map[string]string
	if err := json.Unmarshal([]byte(block), &out); err != nil {
		return nil, fmt.Errorf("could not read the rewritten comments: %w", err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no rewritten comments in the reply")
	}

	for id, text := range out {
		if strings.TrimSpace(text) == "" {
			delete(out, id)
		}
	}
	return out, nil
}

// Tools is what the rewrite may use: nothing. It is a text transformation, and
// a run that cannot touch the repository cannot be slowed down by it either.
func Tools() []string { return []string{} }

// body is the thread's own text, which is what gets rewritten. Replies on the
// thread are your working notes and are not sent.
// body is every finding on the thread, attributed when more than one reviewer
// found something. Your own replies are working notes and stay out.
func body(thread comments.Thread) string {
	found := make([]comments.Comment, 0, len(thread.Comments))
	for _, c := range thread.Comments {
		if c.Author != comments.AuthorYou {
			found = append(found, c)
		}
	}
	if len(found) == 0 {
		return ""
	}
	if len(found) == 1 {
		return strings.TrimSpace(found[0].Body)
	}
	var b strings.Builder
	for i, c := range found {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "%s found:\n%s", c.Author, strings.TrimSpace(c.Body))
	}
	return b.String()
}

func between(s, open, close string) string {
	start := strings.Index(s, open)
	if start < 0 {
		return ""
	}
	rest := s[start+len(open):]
	end := strings.Index(rest, close)
	if end < 0 {
		return rest
	}
	return rest[:end]
}
