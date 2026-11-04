package ship

import "fmt"

// How a gate stands. A gate reports; it never blocks, which is what a veto is
// for.
const (
	Pass     = "pass"
	Warn     = "warn"
	Fail     = "fail"
	Pending  = "pending"
	Optional = "optional"
)

// Gate is one answer in the rail: what it looked at, how that stands, and at
// most one thing to press.
type Gate struct {
	Name   string  `json:"name"`
	State  string  `json:"state"`
	Note   string  `json:"note"`
	Button *Button `json:"button,omitempty"`
}

// Button is a gate's one verb. Do is what the page sends back, so the page
// knows no gate by name.
type Button struct {
	Label string `json:"label"`
	Do    string `json:"do"`
}

// Session is the facts a gate may read: what ade has already read for its own
// reasons, never a fresh pass of its own.
type Session struct {
	Projects []Project

	// Threads is the review threads still unresolved on the diff.
	Threads int

	Tests  Tests
	Design Design
	Ledger Ledger
}

// Ledger is the read ledger: how many of the lines the agent wrote have been
// read on screen.
type Ledger struct {
	Agent  int
	Unread int
}

// Tests is the repository's own check list, as the session last ran it.
type Tests struct {
	// Configured is whether this repository has a list at all.
	Configured bool
	Running    bool
	// Ran is whether a full run has finished this session.
	Ran    bool
	Passed bool
	// Failed is the check that failed, when one did.
	Failed string
}

// Design is the open design document and the notes left on it.
type Design struct {
	Open       bool
	File       string
	Unresolved int
}

// Check is one gate's reading of the session.
type Check func(Session) Gate

// Registry is the gates the rail draws, in the order they were added.
type Registry struct{ checks []Check }

// Add registers a gate. It returns the registry so Standard reads as a list.
func (r *Registry) Add(check Check) *Registry {
	r.checks = append(r.checks, check)
	return r
}

// Run is every gate's answer for this session.
func (r *Registry) Run(s Session) []Gate {
	out := make([]Gate, 0, len(r.checks))
	for _, check := range r.checks {
		out = append(out, check(s))
	}
	return out
}

// Standard is the gates ade ships.
func Standard() *Registry {
	return (&Registry{}).Add(threadsGate).Add(testsGate).Add(designGate).Add(readGate)
}

// readGate reports and never blocks: committing unread work is allowed, and
// recorded in the commit's trailer.
func readGate(s Session) Gate {
	gate := Gate{Name: "Agent lines read"}
	switch {
	case s.Ledger.Agent == 0:
		gate.State, gate.Note = Optional, "the agent has written nothing on this branch"
	case s.Ledger.Unread == 0:
		gate.State = Pass
		gate.Note = fmt.Sprintf("every one of %s is read", plural(s.Ledger.Agent, "agent line"))
	default:
		gate.State = Warn
		gate.Note = fmt.Sprintf("%d of %d agent lines unread", s.Ledger.Unread, s.Ledger.Agent)
		gate.Button = &Button{Label: "Open the diff", Do: "read"}
	}
	return gate
}

func threadsGate(s Session) Gate {
	if s.Threads == 0 {
		return Gate{Name: "Threads resolved", State: Pass, Note: "nothing is left open on the diff"}
	}
	return Gate{
		Name:   "Threads resolved",
		State:  Fail,
		Note:   fmt.Sprintf("%s unresolved on the diff", plural(s.Threads, "thread")),
		Button: &Button{Label: "Resolve them", Do: "threads"},
	}
}

func testsGate(s Session) Gate {
	gate := Gate{Name: "Tests ran"}
	switch {
	case !s.Tests.Configured:
		gate.State, gate.Note = Optional, "this repository has no check list"
	case s.Tests.Running:
		gate.State, gate.Note = Pending, "the checks are running"
	case !s.Tests.Ran:
		gate.State, gate.Note = Warn, "the checks have not run this session"
		gate.Button = &Button{Label: "Run them", Do: "tests"}
	case s.Tests.Passed:
		gate.State, gate.Note = Pass, "the checks passed"
	default:
		gate.State, gate.Note = Fail, failedNote(s.Tests.Failed)
		gate.Button = &Button{Label: "Run them", Do: "tests"}
	}
	return gate
}

func failedNote(name string) string {
	if name == "" {
		return "the last run did not finish"
	}
	return name + " failed"
}

func designGate(s Session) Gate {
	gate := Gate{Name: "Design approved"}
	switch {
	case !s.Design.Open:
		gate.State, gate.Note = Optional, "no design document is open"
	case s.Design.Unresolved > 0:
		gate.State = Warn
		gate.Note = fmt.Sprintf("%s unresolved on %s",
			plural(s.Design.Unresolved, "note"), s.Design.File)
		gate.Button = &Button{Label: "Open the design", Do: "design"}
	default:
		gate.State, gate.Note = Pass, "every note on "+s.Design.File+" is resolved"
	}
	return gate
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
