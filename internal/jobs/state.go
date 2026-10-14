package jobs

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/memory"
	"github.com/owenrumney/ade/internal/settings"
)

// keptOutcomes is how much of a job's history is held: enough to see a job
// that stopped working, not a log.
const keptOutcomes = 5

// failingRuns is how many consecutive failures show a job as failing. The
// failure mode of scheduled work is a thing that quietly stopped, not a crash.
const failingRuns = 2

// writeMu serialises every read-modify-write of a state file.
var writeMu sync.Mutex

// Outcome is what came of one fire.
type Outcome struct {
	FiredAt time.Time `json:"firedAt"`
	Millis  int64     `json:"millis"`
	CostUSD float64   `json:"costUsd,omitempty"`

	// Report is what the run had to say, or empty when it had nothing. An
	// empty one stays in the job's history and never reaches the inbox.
	Report string `json:"report,omitempty"`
	Error  string `json:"error,omitempty"`

	// Session and Dir are where the fire happened, so the report links to a
	// transcript that can be read.
	Session string `json:"session,omitempty"`
	Dir     string `json:"dir,omitempty"`
	URL     string `json:"url,omitempty"`

	// NoHandover is a summary run that never wrote its fence. The next fire
	// starts with nothing, so the outcome is where that has to be visible.
	NoHandover bool `json:"noHandover,omitempty"`

	// Seen marks a report you have read, which is what takes it off the dash.
	Seen bool `json:"seen,omitempty"`
}

// State is one job's private record: machine-local, and never in a working
// tree. A committed schedule drags no session ids along with it.
type State struct {
	Name      string    `json:"name"`
	Session   string    `json:"session,omitempty"`
	Handover  string    `json:"handover,omitempty"`
	LastFired time.Time `json:"lastFired,omitempty"`
	NextDue   time.Time `json:"nextDue,omitempty"`
	Paused    bool      `json:"paused,omitempty"`
	Outcomes  []Outcome `json:"outcomes,omitempty"`
}

// Failing is whether the last runs all failed. It is what tells a job that
// stopped working from one whose schedule is simply slow.
func (s State) Failing() bool {
	if len(s.Outcomes) < failingRuns {
		return false
	}
	for _, out := range s.Outcomes[:failingRuns] {
		if out.Error == "" {
			return false
		}
	}
	return true
}

// Last is the most recent outcome, if the job has ever fired.
func (s State) Last() (Outcome, bool) {
	if len(s.Outcomes) == 0 {
		return Outcome{}, false
	}
	return s.Outcomes[0], true
}

// stateDir is where a job's state lives: beside its project's memory, or under
// ade's own state for a job that belongs to no project.
func stateDir(project string) (string, error) {
	if project == "" {
		dir, err := settings.Dir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "job-state"), nil
	}
	root, err := memory.Root()
	if err != nil {
		return "", err
	}
	slug := memory.Slug(project)
	if slug == "" {
		return "", fmt.Errorf("project %q has no store name", project)
	}
	return filepath.Join(root, slug, "jobs"), nil
}

// StatePath is the file one job's state is kept in.
func StatePath(job Job) (string, error) {
	dir, err := stateDir(job.Project)
	if err != nil {
		return "", err
	}
	if !nameRE.MatchString(job.Name) {
		return "", fmt.Errorf("job name %q", job.Name)
	}
	return filepath.Join(dir, stateFile(job)), nil
}

// stateFile keys a repository job that names no project by where it was read
// from, so the same name in two checkouts is two jobs rather than one.
func stateFile(job Job) string {
	if job.Project != "" || job.Scope != ScopeRepo {
		return job.Name + ".json"
	}
	sum := sha256.Sum256([]byte(filepath.Dir(job.Path)))
	return fmt.Sprintf("%s-%x.json", job.Name, sum[:4])
}

// ReadState is a job's record, or an empty one for a job that has never fired.
func ReadState(job Job) State {
	out := State{Name: job.Name}
	path, err := StatePath(job)
	if err != nil {
		return out
	}
	data, err := os.ReadFile(path) // #nosec G304 -- ade's own state directory
	if err != nil {
		return out
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return State{Name: job.Name}
	}
	out.Name = job.Name
	return out
}

// WriteState replaces a job's record.
func WriteState(job Job, state State) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	return writeState(job, state)
}

// writeState is WriteState with the lock already held.
func writeState(job Job, state State) error {
	path, err := StatePath(job)
	if err != nil {
		return err
	}
	state.Name = job.Name
	if len(state.Outcomes) > keptOutcomes {
		state.Outcomes = state.Outcomes[:keptOutcomes]
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", job.Name, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// Update reads a job's state, applies change, and writes it back with the lock
// held throughout, so a fire and a pause arriving together cannot lose one.
func Update(job Job, change func(*State)) (State, error) {
	writeMu.Lock()
	defer writeMu.Unlock()
	state := ReadState(job)
	change(&state)
	return state, writeState(job, state)
}

// MarkSeen puts a report away. Empty marks every outcome the job holds.
func MarkSeen(job Job, firedAt time.Time) (State, error) {
	return Update(job, func(state *State) {
		for i := range state.Outcomes {
			if firedAt.IsZero() || state.Outcomes[i].FiredAt.Equal(firedAt) {
				state.Outcomes[i].Seen = true
			}
		}
	})
}

// SetPaused stops a job firing, or starts it again. A resumed job's next fire
// is computed from now, so a pause is not a queue of missed windows.
func SetPaused(job Job, paused bool, now time.Time) (State, error) {
	return Update(job, func(state *State) {
		state.Paused = paused
		if !paused {
			state.NextDue = job.Every.Next(now)
		}
	})
}
