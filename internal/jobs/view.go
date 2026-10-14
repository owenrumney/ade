package jobs

import "time"

// View is one job as a page shows it: the definition, and what has become of
// it. The Jobs pane and the Project modal both read this.
type View struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Schedule    string `json:"schedule"`
	Project     string `json:"project,omitempty"`
	Continuity  string `json:"continuity"`
	Allow       string `json:"allow"`
	Scope       string `json:"scope"`
	Path        string `json:"path"`

	// Prompt is the body, so the form that edits a job has what it says now.
	Prompt string `json:"prompt"`

	NextDue   *time.Time `json:"nextDue,omitempty"`
	LastFired *time.Time `json:"lastFired,omitempty"`
	Paused    bool       `json:"paused,omitempty"`

	// Failing is two consecutive failures, which is a job that stopped
	// working rather than one whose schedule is merely slow.
	Failing bool `json:"failing,omitempty"`

	// Running is a fire in flight, so a click on "Run now" shows something.
	Running bool `json:"running,omitempty"`

	Outcomes []Outcome `json:"outcomes"`
}

// Viewed is a job and its state as one row.
func Viewed(job Job, state State) View {
	out := View{
		Name: job.Name, Description: job.Description, Kind: job.Kind, Schedule: job.Schedule,
		Project: job.Project, Continuity: job.Continuity, Allow: job.Allow,
		Scope: job.Scope, Path: job.Path, Prompt: job.Prompt,
		Paused: state.Paused, Failing: state.Failing(),
		Outcomes: state.Outcomes,
	}
	if out.Outcomes == nil {
		out.Outcomes = []Outcome{}
	}
	if !state.NextDue.IsZero() {
		at := state.NextDue
		out.NextDue = &at
	}
	if !state.LastFired.IsZero() {
		at := state.LastFired
		out.LastFired = &at
	}
	return out
}

// Views is every job with its state read.
func Views(list []Job) []View {
	out := make([]View, 0, len(list))
	for _, job := range list {
		out = append(out, Viewed(job, ReadState(job)))
	}
	return out
}
