// Package agents reads the named agents a checkout can call on. The files are
// Claude Code's own — .claude/agents/<name>.md in the repository and
// ~/.claude/agents/ for the user — so an agent defined for one is defined
// for the other. Ade adds one thing: a provider prefix on the model
// ("codex/gpt-5") that routes the agent to a driver Claude Code does not have.
package agents

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// nameRE is what an agent may be called: it is typed after an @, and it is
// written into prompts as a tag, so nothing that could be read as either.
var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// Agent is one definition: who it is, what it runs on, and what it may do.
type Agent struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// Provider is which driver runs it: "claude" or "codex". Model is the
	// model within that provider, with the prefix stripped; empty means the
	// provider's default.
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`

	// Tools restricts what the agent may use, when the definition says.
	Tools []string `json:"tools,omitempty"`

	// Reviews is whether it is offered as a reviewer and has its findings
	// filed. Opt-out: an agent written before the key still reviews.
	Reviews bool `json:"reviews"`

	// Avatar is the face the page shows beside what it says: the name of
	// one of the page's line-art icons (shield, flask, wrench…), or nothing
	// and the page uses its initial.
	Avatar string `json:"avatar,omitempty"`

	// Prompt is the body of the file: the agent's system prompt.
	Prompt string `json:"-"`

	// Path is the file it was read from, and Scope whether that was the
	// repository or the user's home.
	Path  string `json:"path"`
	Scope string `json:"scope"`
}

const (
	ProviderClaude = "claude"
	ProviderCodex  = "codex"

	ScopeRepo = "repo"
	ScopeUser = "user"
)

// Skipped is a definition file that could not be used, and why, so a
// misspelt model prefix is not just an agent that silently isn't there.
type Skipped struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// Load reads the agents visible from a checkout: the repository's own, then
// the user's. A repository agent shadows a user agent of the same name, the
// way Claude Code resolves them.
func Load(root, home string) []Agent {
	list, _ := LoadAll(root, home)
	return list
}

// LoadAll is Load, and the files it could not read as agents.
func LoadAll(root, home string) ([]Agent, []Skipped) {
	seen := map[string]bool{}
	var out []Agent
	var skipped []Skipped
	for _, where := range []struct{ base, scope string }{{root, ScopeRepo}, {home, ScopeUser}} {
		if where.base == "" {
			continue
		}
		found, bad := readDir(filepath.Join(where.base, ".claude", "agents"), where.scope)
		skipped = append(skipped, bad...)
		for _, agent := range found {
			if seen[agent.Name] {
				continue
			}
			seen[agent.Name] = true
			out = append(out, agent)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, skipped
}

// Reviewers is the agents that review a change, for the review picker.
func Reviewers(list []Agent) []Agent {
	out := make([]Agent, 0, len(list))
	for _, agent := range list {
		if agent.Reviews {
			out = append(out, agent)
		}
	}
	return out
}

// Find is the agent called name, if there is one.
func Find(list []Agent, name string) (Agent, bool) {
	for _, agent := range list {
		if agent.Name == name {
			return agent, true
		}
	}
	return Agent{}, false
}

// Mentions reads every name at the start of a message — "@a @b the rest" —
// so one message can call on several agents at once. A message that does
// not start with a mention is not one, so "email @bob" goes to the main
// agent as written.
func Mentions(message string) (names []string, rest string, ok bool) {
	rest = strings.TrimSpace(message)
	for strings.HasPrefix(rest, "@") {
		end := strings.IndexFunc(rest, func(r rune) bool {
			return r == ' ' || r == '\n' || r == '\t' || r == ':' || r == ','
		})
		if end < 0 {
			end = len(rest)
		}
		name := rest[1:end]
		if name == "" {
			break
		}
		names = append(names, name)
		rest = strings.TrimSpace(strings.TrimLeft(rest[end:], ":,"))
	}
	if len(names) == 0 {
		return nil, "", false
	}
	return names, rest, true
}

// Parse reads one definition. The frontmatter is the simple kind Claude Code
// writes — one "key: value" per line between two "---" lines — and the body
// is the prompt.
func Parse(path, scope string, data []byte) (Agent, error) {
	agent := Agent{Path: path, Scope: scope, Provider: ProviderClaude, Reviews: true}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return agent, errors.New("no frontmatter")
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return agent, errors.New("frontmatter never closes")
	}
	head, body := rest[:end], rest[end+len("\n---"):]
	if i := strings.Index(body, "\n"); i >= 0 {
		body = body[i+1:]
	} else {
		body = ""
	}
	agent.Prompt = strings.TrimSpace(body)

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
			agent.Name = value
		case "description":
			agent.Description = value
		case "model":
			agent.Provider, agent.Model = splitModel(value)
		case "tools":
			agent.Tools = splitList(value)
		case "avatar":
			agent.Avatar = avatar(value)
		case "reviews":
			agent.Reviews = !strings.EqualFold(value, "false")
		}
	}
	if agent.Name == "" {
		agent.Name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	if !nameRE.MatchString(agent.Name) {
		return agent, fmt.Errorf("agent name %q: letters, digits, - and _ only", agent.Name)
	}
	if agent.Provider != ProviderClaude && agent.Provider != ProviderCodex {
		return agent, fmt.Errorf("unknown provider %q", agent.Provider)
	}
	return agent, nil
}

// avatarRE is the shape of an icon name. The page owns the set; a name it
// does not know falls back to the initial, so nothing is checked here
// beyond it being a name.
var avatarRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,23}$`)

// avatar is a definition's face: an icon name, or nothing.
func avatar(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if !avatarRE.MatchString(value) {
		return ""
	}
	return value
}

func readDir(dir, scope string) ([]Agent, []Skipped) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil // no directory is no agents, not a problem
	}
	var out []Agent
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
		agent, err := Parse(path, scope, data)
		if err != nil {
			skipped = append(skipped, Skipped{Path: path, Error: err.Error()})
			continue
		}
		out = append(out, agent)
	}
	return out, skipped
}

// splitModel reads "codex/gpt-5" as provider and model. A bare model name,
// or Claude Code's aliases and "inherit", is Claude's.
func splitModel(value string) (provider, model string) {
	if before, after, found := strings.Cut(value, "/"); found {
		return strings.ToLower(strings.TrimSpace(before)), strings.TrimSpace(after)
	}
	if value == "inherit" {
		value = ""
	}
	return ProviderClaude, value
}

// splitList reads "Read, Grep" or "[Read, Grep]" as a list.
func splitList(value string) []string {
	value = strings.Trim(value, "[]")
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
