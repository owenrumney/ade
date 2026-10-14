package jobs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxPrompt caps what a form may write. A definition is read on every tick, so
// an unbounded body is a file the clock re-reads forever.
const maxPrompt = 32 << 10

// What a save may do to a definition that is already there. A form knows which
// it meant, and neither one silently becomes the other.
const (
	Create  = "create"
	Replace = "replace"
)

// Draft is a job as a form submits it: the fields, before they are a file.
type Draft struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Schedule    string `json:"schedule"`
	Project     string `json:"project"`
	Continuity  string `json:"continuity"`
	Allow       string `json:"allow"`
	Prompt      string `json:"prompt"`
}

// Save writes a user job, refusing anything Load could not read back. mode is
// what it may do to a file already there: Create refuses one, Replace needs it.
func Save(home string, draft Draft, mode string) (Job, error) {
	dir := userDir(home)
	if dir == "" {
		return Job{}, errors.New("no home directory to keep jobs in")
	}
	draft.Name = strings.TrimSpace(draft.Name)
	if !nameRE.MatchString(draft.Name) {
		return Job{}, fmt.Errorf("job name %q: letters, digits, - and _ only", draft.Name)
	}
	if len(draft.Prompt) > maxPrompt {
		return Job{}, fmt.Errorf("that prompt is %dKB; %dKB at most", len(draft.Prompt)>>10, maxPrompt>>10)
	}
	if draft.Continuity == "" {
		draft.Continuity = Fresh
	}
	if draft.Allow == "" {
		draft.Allow = Reads
	}

	path := filepath.Join(dir, draft.Name+".md")
	was, existed := readJob(path)
	switch mode {
	case Create:
		if existed {
			return Job{}, fmt.Errorf("there is already a job called %s; open it to change it", draft.Name)
		}
	case Replace:
		if !existed {
			return Job{}, fmt.Errorf("there is no job called %s to change", draft.Name)
		}
	default:
		return Job{}, fmt.Errorf("unknown save mode %q", mode)
	}

	// The frontmatter names the state file, so writing over a file whose two
	// names disagree would strand the history of the name inside it.
	if existed && was.Name != "" && was.Name != draft.Name {
		return Job{}, fmt.Errorf("%s holds a job called %s; rename it there first", filepath.Base(path), was.Name)
	}

	data := Render(draft)
	job, err := Parse(path, ScopeUser, []byte(data))
	if err != nil {
		return Job{}, err
	}
	if existed {
		if err := carryState(was, job); err != nil {
			return Job{}, err
		}
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return Job{}, err
	}
	if err := writeFile(path, data); err != nil {
		return Job{}, err
	}
	return job, nil
}

// Remove deletes a user job. Its state would otherwise outlive it and be
// inherited by the next job of the same name.
func Remove(home, name string) error {
	dir := userDir(home)
	if dir == "" {
		return errors.New("no home directory to keep jobs in")
	}
	if !nameRE.MatchString(name) {
		return fmt.Errorf("job name %q: letters, digits, - and _ only", name)
	}
	path := filepath.Join(dir, name+".md")
	// State is keyed by the job's project, so finding it takes the definition —
	// but by the name of the file being removed, never the frontmatter's.
	if job, ok := readJob(path); ok {
		job.Name = name
		if state, err := StatePath(job); err == nil {
			_ = os.Remove(state)
		}
	}
	return os.Remove(path)
}

// Render is the definition file a draft writes, in the form Parse reads.
func Render(draft Draft) string {
	var out strings.Builder
	out.WriteString("---\n")
	for _, field := range [][2]string{
		{"name", draft.Name},
		{"description", draft.Description},
		{"schedule", draft.Schedule},
		{"project", draft.Project},
		{"continuity", draft.Continuity},
		{"allow", draft.Allow},
	} {
		// One line per field: the parser cuts at the first colon, so a value
		// with a newline in it would make the rest of it prompt.
		value := strings.Join(strings.Fields(field[1]), " ")
		if value == "" {
			continue
		}
		fmt.Fprintf(&out, "%s: %s\n", field[0], value)
	}
	out.WriteString("---\n\n")
	out.WriteString(strings.TrimSpace(draft.Prompt))
	out.WriteString("\n")
	return out.String()
}

// readJob is the definition at path, if there is a readable one there.
func readJob(path string) (Job, bool) {
	data, err := os.ReadFile(path) // #nosec G304 -- a file under a directory we chose
	if err != nil {
		return Job{}, false
	}
	job, err := Parse(path, ScopeUser, data)
	if err != nil {
		// Unreadable is still a file: replacing it is an edit, not a create.
		return Job{Path: path, Scope: ScopeUser}, true
	}
	return job, true
}

// carryState moves a job's history when an edit changes where it lives: state
// is keyed by project, so an edited project would orphan and unpause it.
func carryState(was, now Job) error {
	if was.Project == now.Project || was.Name == "" {
		return nil
	}
	from, err := StatePath(was)
	if err != nil {
		return nil //nolint:nilerr // no readable old state is nothing to move
	}
	to, err := StatePath(now)
	if err != nil {
		return err
	}
	if from == to {
		return nil
	}
	if _, err := os.Stat(to); err == nil {
		return fmt.Errorf("%s already has a job called %s with its own history", now.Project, now.Name)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	if err := os.Rename(from, to); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("move %s history to %s: %w", now.Name, now.Project, err)
	}
	return nil
}

// writeFile replaces path atomically. The clock re-reads the directory every
// minute, and a truncated definition reads as a job that will not parse.
func writeFile(path, data string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
