package ship

import (
	"fmt"
	"strings"
)

// Step is a point work can be stopped at. These two are the only ones: a veto
// guards what sends, not every button.
type Step string

const (
	StepCommit Step = "commit"
	StepRaise  Step = "raise"
)

// Guard is one veto's reading. It returns the reason to stop, or "" to allow.
type Guard func(Step, Session, Project) string

// Vetoes is what may stop a step, in the order they were added.
type Vetoes struct{ guards []Guard }

// Add registers a veto. It returns the list so Standing reads as one.
func (v *Vetoes) Add(guard Guard) *Vetoes {
	v.guards = append(v.guards, guard)
	return v
}

// Reason is why this step is refused, or "" to let it through. The first
// refusal wins: a person fixes one thing at a time.
func (v *Vetoes) Reason(step Step, s Session, p Project) string {
	for _, guard := range v.guards {
		if why := guard(step, s, p); why != "" {
			return why
		}
	}
	return ""
}

// Standing is the vetoes ade ships.
func Standing() *Vetoes {
	return (&Vetoes{}).Add(notOnTheBase).Add(threadsBeforeRaising)
}

// notOnTheBase stops a commit onto main, which is the one commit nobody meant
// to make. The session's own checkout only: a mounted repository is usually
// sitting on its own main on purpose, and a routine override is no veto.
func notOnTheBase(step Step, _ Session, p Project) string {
	if step != StepCommit || !p.Home || p.Branch == "" || p.Base == "" {
		return ""
	}
	if !strings.EqualFold(p.Branch, strings.TrimPrefix(p.Base, "origin/")) {
		return ""
	}
	return fmt.Sprintf("%s is the base branch: start a branch before committing to it", p.Branch)
}

// threadsBeforeRaising stops a pull request going up with review threads still
// open on the diff it is made of.
func threadsBeforeRaising(step Step, s Session, _ Project) string {
	if step != StepRaise || s.Threads == 0 {
		return ""
	}
	return fmt.Sprintf("%s still unresolved on the diff", plural(s.Threads, "thread"))
}
