package estimate

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// MinRuns is the thinnest history a range is drawn from: fewer and nothing is shown.
const MinRuns = 3

// Enough is how many similar runs the group widens to reach.
const Enough = 5

// Range is the middle half of past runs, p25 to p75, and how many there were.
type Range struct {
	Low  time.Duration
	High time.Duration
	Runs int
}

// Spread is the p25–p75 of durations, or false under MinRuns.
func Spread(durations []time.Duration) (Range, bool) {
	if len(durations) < MinRuns {
		return Range{}, false
	}
	values := make([]float64, len(durations))
	for i, d := range durations {
		values[i] = float64(d)
	}
	sort.Float64s(values)
	return Range{
		Low:  time.Duration(percentile(values, 0.25)),
		High: time.Duration(percentile(values, 0.75)),
		Runs: len(values),
	}, true
}

// percentile interpolates linearly between the closest ranks of sorted values.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	at := p * float64(len(sorted)-1)
	lo := int(math.Floor(at))
	hi := int(math.Ceil(at))
	return sorted[lo] + (sorted[hi]-sorted[lo])*(at-float64(lo))
}

// Target is the run about to start, described by what it can be compared on.
type Target struct {
	Kind           string
	Model          string
	Repo           string
	DocWords       int
	ChecklistItems int
}

// Group is the past runs judged like the target, and the words that say which.
type Group struct {
	Runs  []Run
	Label string
}

type level struct {
	label string
	match func(Target, Run) bool
}

// levels widen in the design's order: drop the repository, coarsen the size,
// then drop the model. A target with no model starts without one.
var levels = []level{
	{"of a similar-sized design in this repository", func(t Target, r Run) bool {
		return r.Repo == t.Repo && sameModel(t, r) && fineSize(t, r)
	}},
	{"of a similar-sized design", func(t Target, r Run) bool { return sameModel(t, r) && fineSize(t, r) }},
	{"of a design about this size", func(t Target, r Run) bool { return sameModel(t, r) && coarseSize(t, r) }},
	{"of a design about this size, on any model", coarseSize},
}

// Similar is the narrowest group with Enough runs, or else the narrowest of the
// biggest: a wider level adds runs, and a tie means it added none.
func Similar(history []Run, t Target) Group {
	var out Group
	for i, l := range levels {
		g := Group{Label: l.label}
		for _, r := range history {
			if r.Kind == t.Kind && !r.Failed && l.match(t, r) {
				g.Runs = append(g.Runs, r)
			}
		}
		if i == 0 || len(g.Runs) > len(out.Runs) {
			out = g
		}
		if len(out.Runs) >= Enough {
			break
		}
	}
	return out
}

// sameModel takes an alias such as "opus" as naming the full ids it is part of.
func sameModel(t Target, r Run) bool {
	want := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(t.Model)), "[1m]")
	return want == "" || strings.Contains(strings.ToLower(r.Model), want)
}

// A size of zero is unknown, which matches nothing: a guess at size is no size.
func fineSize(t Target, r Run) bool {
	return r.DocWords > 0 && t.DocWords > 0 &&
		doubling(r.DocWords, 250) == doubling(t.DocWords, 250) &&
		doubling(r.ChecklistItems, 5) == doubling(t.ChecklistItems, 5)
}

// coarseSize is the fine word band or either neighbour, items aside, so it
// holds every run fineSize does.
func coarseSize(t Target, r Run) bool {
	if r.DocWords <= 0 || t.DocWords <= 0 {
		return false
	}
	gap := doubling(r.DocWords, 250) - doubling(t.DocWords, 250)
	return gap >= -1 && gap <= 1
}

// doubling is which power-of-two band n falls in, counting from base.
func doubling(n, base int) int {
	band := 0
	for limit := base; n >= limit; limit *= 2 {
		band++
	}
	return band
}

// Estimate is what the Build button says.
type Estimate struct {
	LowSeconds  int     `json:"lowSeconds"`
	HighSeconds int     `json:"highSeconds"`
	Runs        int     `json:"runs"`
	Kind        string  `json:"kind"`
	Group       string  `json:"group"`
	Window      *Window `json:"window,omitempty"`
}

// Window is the share of the plan window a run is expected to use.
type Window struct {
	Name     string    `json:"name"`
	Used     float64   `json:"used"`
	About    float64   `json:"about"`
	High     float64   `json:"high"`
	ResetsAt time.Time `json:"resetsAt,omitzero"`
	// Cap is the upper end going past what is left of the window.
	Cap bool `json:"cap"`
}

// Calibration is one reading of the window against what was spent in it.
type Calibration struct {
	Name            string
	UsedPct         float64
	UnitsSinceReset float64
	ResetsAt        time.Time
}

// PerUnit is what one unit of usage costs of the window. Nothing was spent
// since a reset reads as no calibration rather than a division by zero.
func (c Calibration) PerUnit() (float64, bool) {
	if c.UnitsSinceReset <= 0 || c.UsedPct <= 0 {
		return 0, false
	}
	return c.UsedPct / c.UnitsSinceReset, true
}

// For is the estimate for a target from its history, or false when too thin.
func For(history []Run, t Target, cal *Calibration) (Estimate, bool) {
	g := Similar(history, t)
	durations := make([]time.Duration, len(g.Runs))
	for i, r := range g.Runs {
		durations[i] = r.Duration()
	}
	spread, ok := Spread(durations)
	if !ok {
		return Estimate{}, false
	}
	out := Estimate{
		LowSeconds:  int(spread.Low.Seconds()),
		HighSeconds: int(spread.High.Seconds()),
		Runs:        spread.Runs,
		Kind:        t.Kind,
		Group:       g.Label,
	}
	if cal != nil {
		out.Window = windowFor(g.Runs, *cal)
	}
	return out, true
}

func windowFor(runs []Run, cal Calibration) *Window {
	rate, ok := cal.PerUnit()
	if !ok {
		return nil
	}
	var units []float64
	for _, r := range runs {
		if u := r.Units(); u > 0 {
			units = append(units, u)
		}
	}
	if len(units) < MinRuns {
		return nil
	}
	sort.Float64s(units)
	high := percentile(units, 0.75) * rate
	return &Window{
		Name:     cal.Name,
		Used:     cal.UsedPct,
		About:    percentile(units, 0.5) * rate,
		High:     high,
		ResetsAt: cal.ResetsAt,
		Cap:      cal.UsedPct+high > 100,
	}
}

// Overrun says, once, that a run has gone past the upper end of its range.
type Overrun struct {
	kind string
	past []time.Duration
	high time.Duration
	said bool
}

// NewOverrun watches a run against the group its estimate came from.
func NewOverrun(history []Run, t Target) (*Overrun, bool) {
	g := Similar(history, t)
	past := make([]time.Duration, len(g.Runs))
	for i, r := range g.Runs {
		past[i] = r.Duration()
	}
	spread, ok := Spread(past)
	if !ok {
		return nil, false
	}
	return &Overrun{kind: t.Kind, past: past, high: spread.High}, true
}

// High is when the run passes the upper end of its range.
func (o *Overrun) High() time.Duration { return o.high }

// Check is the status line for a run elapsed this long, the first time it is due.
func (o *Overrun) Check(elapsed time.Duration) (string, bool) {
	if o.said || elapsed <= o.high {
		return "", false
	}
	o.said = true
	shorter := 0
	for _, d := range o.past {
		if d < elapsed {
			shorter++
		}
	}
	return fmt.Sprintf("running longer than %d of %d past %ss", shorter, len(o.past), o.kind), true
}
