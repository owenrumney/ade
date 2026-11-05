// Package jobs is the scheduled work ade runs when nobody asked: a prompt on a
// schedule, in a project, resolved the way an agent is. See the design doc.
package jobs

import (
	"regexp"
)

// nameRE is what a job may be called: it names a state file and is typed at
// `ade jobs run`, so nothing that could be read as a path.
var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// How much of the last fire a job carries into the next.
const (
	// Fresh is a new session each time, carrying only the job body.
	Fresh = "fresh"
	// Summary adds the previous fire's own handover note.
	Summary = "summary"
	// History resumes one session forever. Not built; see the design doc.
	History = "history"
)

// What a job may touch.
const (
	Reads  = "reads"
	Writes = "writes"
)

const (
	ScopeRepo = "repo"
	ScopeUser = "user"
)

// Job is one definition: when it fires, where, what it recalls, and what it
// may do. The body is the prompt, and is usually a slash command.
type Job struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// Kind is empty for a prompt job. A built-in kind runs Go in the
	// launcher rather than a model with tools.
	Kind string `json:"kind,omitempty"`

	Schedule string `json:"schedule"`

	// Project is the normalised origin remote of the repository to run in, or
	// empty for a job that belongs to no project.
	Project string `json:"project,omitempty"`

	Continuity string `json:"continuity"`
	Allow      string `json:"allow"`

	Prompt string `json:"prompt"`

	Path  string `json:"path"`
	Scope string `json:"scope"`
}

// Skipped is a file that was meant to be a job and could not be read as one,
// so a misspelt schedule is not a job that silently never fires.
type Skipped struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// Find is the job called name, if there is one.
func Find(list []Job, name string) (Job, bool) {
	for _, job := range list {
		if job.Name == name {
			return job, true
		}
	}
	return Job{}, false
}

// ScopeBuiltin is a job ade defines itself rather than reads from a file.
const ScopeBuiltin = "builtin"

// Manual is the schedule of a job with no clock behind it.
const Manual = "manual"
