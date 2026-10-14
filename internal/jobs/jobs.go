// Package jobs is the scheduled work ade runs when nobody asked: a prompt on a
// schedule, in a project, resolved the way an agent is. See the design doc.
package jobs

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/owenrumney/ade/internal/memory"
	"github.com/owenrumney/ade/internal/statedir"
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

	// Schedule is the cron expression or interval as written, and Every the
	// parsed form the clock asks.
	Schedule string   `json:"schedule"`
	Every    Schedule `json:"-"`

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

// Load reads the jobs visible from a checkout: the repository's own, then the
// user's. A repository job shadows a user job of the same name.
func Load(root, home string) []Job {
	list, _ := LoadAll(root, home)
	return list
}

// LoadAll is Load, and the files it could not read as jobs.
func LoadAll(root, home string) ([]Job, []Skipped) {
	seen := map[string]bool{}
	var out []Job
	var skipped []Skipped
	for _, where := range []struct{ dir, scope string }{
		{repoDir(root), ScopeRepo},
		{userDir(home), ScopeUser},
	} {
		if where.dir == "" {
			continue
		}
		found, bad := readDir(where.dir, where.scope)
		skipped = append(skipped, bad...)
		for _, job := range found {
			if seen[job.Name] {
				continue
			}
			seen[job.Name] = true
			out = append(out, job)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, skipped
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

func repoDir(root string) string {
	if root == "" {
		return ""
	}
	return filepath.Join(root, ".ade", "jobs")
}

func userDir(home string) string {
	if home == "" {
		return ""
	}
	return filepath.Join(statedir.Home(home), "jobs")
}

// Parse reads one definition. The frontmatter is the same simple kind an agent
// definition uses — one "key: value" per line between two "---" lines.
func Parse(path, scope string, data []byte) (Job, error) {
	job := Job{Path: path, Scope: scope, Continuity: Fresh, Allow: Reads}
	head, body, err := split(string(data))
	if err != nil {
		return job, err
	}
	job.Prompt = strings.TrimSpace(body)

	scanner := bufio.NewScanner(strings.NewReader(head))
	for scanner.Scan() {
		key, value, found := strings.Cut(scanner.Text(), ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch key {
		case "name":
			job.Name = value
		case "description":
			job.Description = value
		case "schedule":
			job.Schedule = value
		case "project":
			job.Project = value
		case "continuity":
			job.Continuity = strings.ToLower(value)
		case "allow":
			job.Allow = strings.ToLower(value)
		}
	}
	if job.Name == "" {
		job.Name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	return job, validate(&job)
}

// split separates the frontmatter from the body. A "schedule" line inside a
// fenced block below is body, so only the leading block is read.
func split(text string) (head, body string, err error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return "", "", errors.New("no frontmatter")
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", errors.New("frontmatter never closes")
	}
	head, body = rest[:end], rest[end+len("\n---"):]
	if i := strings.Index(body, "\n"); i >= 0 {
		body = body[i+1:]
	} else {
		body = ""
	}
	return head, body, nil
}

func validate(job *Job) error {
	if !nameRE.MatchString(job.Name) {
		return fmt.Errorf("job name %q: letters, digits, - and _ only", job.Name)
	}
	if strings.TrimSpace(job.Prompt) == "" {
		return errors.New("no prompt: the body of the file is what the job runs")
	}
	every, err := ParseSchedule(job.Schedule)
	if err != nil {
		return err
	}
	job.Every = every
	if job.Project != "" {
		normalised := memory.Normalise(job.Project)
		if normalised == "" {
			return fmt.Errorf("project %q is not a remote ade can identify", job.Project)
		}
		job.Project = normalised
	}
	switch job.Continuity {
	case Fresh, Summary:
	case History:
		return errors.New("continuity: history is not built yet; use summary, which is its floor")
	default:
		return fmt.Errorf("unknown continuity %q: fresh or summary", job.Continuity)
	}
	switch job.Allow {
	case Reads:
	case Writes:
		if job.Continuity != Fresh {
			return errors.New("a writes job keeps its memory in its branch; use continuity: fresh")
		}
	default:
		return fmt.Errorf("unknown allow %q: reads or writes", job.Allow)
	}
	return nil
}

func readDir(dir, scope string) ([]Job, []Skipped) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	var out []Job
	var skipped []Skipped
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path) // #nosec G304 -- a file under a directory we chose
		if err != nil {
			skipped = append(skipped, Skipped{Path: path, Error: err.Error()})
			continue
		}
		job, err := Parse(path, scope, data)
		if err != nil {
			skipped = append(skipped, Skipped{Path: path, Error: err.Error()})
			continue
		}
		out = append(out, job)
	}
	return out, skipped
}
