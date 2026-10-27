package myday

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/linear"
)

// NoProject is what a ticket in none is filed under: an unlabelled group
// reads as a bug in the grouping.
const NoProject = "No project"

// Input is everything one morning's report is made of.
type Input struct {
	Now time.Time

	// Asked is waiting on your review, Mine on somebody else's.
	Asked []ghpr.Request
	Mine  []ghpr.Request

	Tickets   []linear.Issue
	Estimates Cache

	// What could not be read, printed under the heading it belongs to.
	AskedError, MineError, TicketError string

	// Held is why no estimate was made this morning. The cached ones still print.
	Held string
}

// Empty is a morning with nothing on it and nothing to apologise for.
func (in Input) Empty() bool {
	return len(in.Asked) == 0 && len(in.Mine) == 0 && len(in.Tickets) == 0 &&
		in.AskedError == "" && in.MineError == "" && in.TicketError == "" && in.Held == ""
}

// Render is the report. The first line is the whole day in a sentence: the
// job row shows it, and it is often the only line read.
func Render(in Input) string {
	if in.Empty() {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# My Day · %s — %s\n", in.Now.Format("Mon 2 Jan"), headline(in))

	section(&b, fmt.Sprintf("Reviews waiting on you (%d)", len(in.Asked)), in.AskedError,
		"Nothing is waiting on your review.", prLines(in.Asked, true))
	section(&b, fmt.Sprintf("Your pull requests (%d)", len(in.Mine)), in.MineError,
		"You have no open pull requests.", prLines(in.Mine, false))

	heading := fmt.Sprintf("Your tickets (%d)", len(in.Tickets))
	if total := totalSpan(in); total != "" {
		heading += " · " + total
	}
	section(&b, heading, in.TicketError, "No tickets are assigned to you.", ticketLines(in))
	if in.Held != "" {
		fmt.Fprintf(&b, "\nNo new estimates this morning: %s\n", in.Held)
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// headline is the day in one clause, and only of what there is.
func headline(in Input) string {
	var parts []string
	if n := len(in.Asked); n > 0 {
		parts = append(parts, fmt.Sprintf("%d to review", n))
	}
	if n := len(in.Mine); n > 0 {
		parts = append(parts, fmt.Sprintf("%d of yours open", n))
	}
	if n := len(in.Tickets); n > 0 {
		ticket := fmt.Sprintf("%d tickets", n)
		if n == 1 {
			ticket = "1 ticket"
		}
		if total := totalSpan(in); total != "" {
			ticket += " at " + total
		}
		parts = append(parts, ticket)
	}
	if len(parts) == 0 {
		return "nothing came back"
	}
	return strings.Join(parts, ", ")
}

// totalSpan adds the estimates up, or is empty when none of them has one.
func totalSpan(in Input) string {
	var low, high float64
	sized := 0
	for _, issue := range in.Tickets {
		entry, fresh := in.Estimates.Get(issue)
		if !fresh {
			continue
		}
		low, high, sized = low+entry.Low, high+entry.High, sized+1
	}
	if sized == 0 {
		return ""
	}
	out := Span(low, high)
	// Say so rather than present a partial total as the day: an unsized
	// ticket is the one most likely to be the whole of it.
	if sized < len(in.Tickets) {
		out += fmt.Sprintf(" across %d of %d", sized, len(in.Tickets))
	}
	return out
}

func prLines(list []ghpr.Request, byAuthor bool) []string {
	out := make([]string, 0, len(list))
	for _, pr := range list {
		var facts []string
		if byAuthor && pr.Author != "" {
			facts = append(facts, pr.Author)
		}
		if pr.Draft {
			facts = append(facts, "draft")
		}
		if decision := decisionWord(pr.ReviewDecision); decision != "" {
			facts = append(facts, decision)
		}
		if pr.Checks != "" {
			facts = append(facts, "checks "+pr.Checks)
		}
		line := fmt.Sprintf("- [%s#%d](%s) **%s**", pr.Repo, pr.Number, pr.URL, pr.Title)
		if len(facts) > 0 {
			line += " — " + strings.Join(facts, " · ")
		}
		out = append(out, line)
	}
	return out
}

// ticketLines groups by project, in name order with No project last.
func ticketLines(in Input) []string {
	byProject := map[string][]linear.Issue{}
	for _, issue := range in.Tickets {
		project := issue.Project
		if project == "" {
			project = NoProject
		}
		byProject[project] = append(byProject[project], issue)
	}
	names := make([]string, 0, len(byProject))
	for name := range byProject {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == NoProject) != (names[j] == NoProject) {
			return names[j] == NoProject
		}
		return names[i] < names[j]
	})

	var out []string
	for _, name := range names {
		if len(byProject) > 1 || name != NoProject {
			out = append(out, "", "### "+name)
		}
		for _, issue := range byProject[name] {
			out = append(out, ticketLine(in, issue)...)
		}
	}
	return out
}

func ticketLine(in Input, issue linear.Issue) []string {
	entry, fresh := in.Estimates.Get(issue)
	size := "not sized"
	if fresh {
		size = "**" + Span(entry.Low, entry.High) + "**"
	}
	facts := []string{size, strings.ToLower(issue.State)}
	if word := priority(issue.Priority); word != "none" {
		facts = append(facts, word)
	}
	// One bullet, not a bullet and a note: the readers this lands in draw a
	// nested item flat, where it reads as a second ticket.
	if fresh && entry.Why != "" {
		facts = append(facts, "*"+strings.TrimSuffix(entry.Why, ".")+"*")
	}
	return []string{fmt.Sprintf("- [%s](%s) **%s** — %s",
		issue.Identifier, issue.URL, issue.Title, strings.Join(facts, " · "))}
}

// section writes one heading and its body: the lines, or why there are none.
func section(b *strings.Builder, heading, failed, empty string, lines []string) {
	fmt.Fprintf(b, "\n## %s\n\n", heading)
	if failed != "" {
		fmt.Fprintf(b, "Could not read this: %s\n", failed)
		if len(lines) == 0 {
			return
		}
		b.WriteString("\nWhat was read before it failed:\n\n")
	}
	if len(lines) == 0 {
		fmt.Fprintf(b, "%s\n", empty)
		return
	}
	b.WriteString(strings.TrimPrefix(strings.Join(lines, "\n"), "\n"))
	b.WriteString("\n")
}

func decisionWord(decision string) string {
	switch strings.ToUpper(decision) {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "changes requested"
	default:
		return ""
	}
}

// Span is a low-to-high estimate as a person reads it: "2–4h", not "2h–4h".
func Span(low, high float64) string {
	if high <= low {
		return Hours(low)
	}
	from, to := Hours(low), Hours(high)
	if from[len(from)-1] == to[len(to)-1] {
		from = from[:len(from)-1]
	}
	return from + "–" + to
}

// Hours is one figure, to the nearest quarter under an hour and never zero.
func Hours(h float64) string {
	if h < 1 {
		quarters := math.Max(1, math.Round(h*4))
		return fmt.Sprintf("%dm", int(quarters)*15)
	}
	if h == math.Trunc(h) {
		return fmt.Sprintf("%.0fh", h)
	}
	return fmt.Sprintf("%.1fh", h)
}
