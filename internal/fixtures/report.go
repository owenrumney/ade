package fixtures

import (
	"fmt"
	"sort"
	"strings"
)

// Count is how many runs an assertion held in, out of how many made it.
type Count struct {
	Passed int `json:"passed"`
	Total  int `json:"total"`
}

// Row is one assertion across two suite runs.
type Row struct {
	Assertion string `json:"assertion"`
	Before    Count  `json:"before"`
	After     Count  `json:"after"`
}

// Report is two suite runs side by side. Before is empty for a single run.
type Report struct {
	Rows   []Row    `json:"rows"`
	Models []string `json:"models"`
	Stale  []string `json:"stale,omitempty"`
	Errors []string `json:"errors,omitempty"`
	Cost   float64  `json:"costUSD"`
	Two    bool     `json:"two"`
}

// Compare tallies each assertion over both runs. A result with an error counts
// against every assertion it made: hiding a run that never got measured flatters the rule.
func Compare(before, after []Result) Report {
	report := Report{Two: before != nil}
	b, a := tally(before), tally(after)
	for _, name := range Assertions {
		if b[name].Total == 0 && a[name].Total == 0 {
			continue
		}
		report.Rows = append(report.Rows, Row{Assertion: name, Before: b[name], After: a[name]})
	}
	seen := map[string]bool{}
	for _, r := range append(append([]Result{}, before...), after...) {
		report.Cost += r.CostUSD
		if r.Model != "" && !seen["m:"+r.Model] {
			seen["m:"+r.Model] = true
			report.Models = append(report.Models, r.Model)
		}
		if r.Stale && !seen["s:"+r.Name] {
			seen["s:"+r.Name] = true
			report.Stale = append(report.Stale, r.Name)
		}
		if r.Error != "" {
			report.Errors = append(report.Errors, fmt.Sprintf("%s run %d: %s", r.Name, r.Run, r.Error))
		}
	}
	sort.Strings(report.Stale)
	return report
}

func tally(results []Result) map[string]Count {
	counts := map[string]Count{}
	for _, r := range results {
		for _, name := range r.Checked {
			c := counts[name]
			c.Total++
			if !failedOn(r, name) {
				c.Passed++
			}
			counts[name] = c
		}
	}
	return counts
}

func failedOn(r Result, assertion string) bool {
	if r.Error != "" {
		return true
	}
	for _, f := range r.Failed {
		if strings.HasPrefix(f, assertion+":") {
			return true
		}
	}
	return false
}

// String is the table the suite is run for. A smoke test, not a score: the
// denominator stays in every cell so seven in ten reads as seven in ten.
func (r Report) String() string {
	var b strings.Builder
	if r.Two {
		fmt.Fprintf(&b, "%-11s %8s %8s\n", "", "before", "after")
	}
	for _, row := range r.Rows {
		if !r.Two {
			fmt.Fprintf(&b, "%-11s %8s\n", row.Assertion, row.After.String())
			continue
		}
		delta := ""
		if d := row.After.Passed - row.Before.Passed; d != 0 {
			delta = fmt.Sprintf("  %+d", d)
		}
		fmt.Fprintf(&b, "%-11s %8s %8s%s\n", row.Assertion, row.Before.String(), row.After.String(), delta)
	}
	if len(r.Rows) == 0 {
		b.WriteString("nothing was asserted\n")
	}
	if len(r.Models) > 0 {
		fmt.Fprintf(&b, "\nmodel %s; a rule may behave differently on a larger one\n", strings.Join(r.Models, ", "))
	}
	if r.Cost > 0 {
		fmt.Fprintf(&b, "cost $%.2f\n", r.Cost)
	}
	if len(r.Stale) > 0 {
		fmt.Fprintf(&b, "stale, captured over %d days ago: %s\n", int(StaleAfter.Hours()/24), strings.Join(r.Stale, ", "))
	}
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "error: %s\n", e)
	}
	return b.String()
}

func (c Count) String() string {
	if c.Total == 0 {
		return "-"
	}
	return fmt.Sprintf("%d/%d", c.Passed, c.Total)
}
