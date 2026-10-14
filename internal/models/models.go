// Package models is the list of agent models ade offers. Built in so an
// unsynced machine has one, synced from cost.dev so a new model can be picked.
package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/settings"
)

// Model is one thing an agent can be started on. ID is what the CLI is given.
type Model struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Agent   string `json:"agent"`
	Context int    `json:"context,omitempty"`
	Latest  bool   `json:"latest,omitempty"`
	// In and Out are USD per million tokens, as strings: they are printed,
	// never arithmetic, and a float would round the cheap ones to nothing.
	In  string `json:"in,omitempty"`
	Out string `json:"out,omitempty"`
}

// Catalogue is the whole list and when it was last fetched. A pointer, because
// a zero time.Time is not omitted and would read as a sync in year one.
type Catalogue struct {
	SyncedAt *time.Time `json:"syncedAt,omitempty"`
	Models   []Model    `json:"models"`
}

// API is where the list is synced from. The query pins one price per model:
// list price, direct from the provider, no region or batch discount.
// ADE_MODELS_API points a mirrored or air-gapped install somewhere else.
const API = "https://api.dev.cost.dev/v1/models" +
	"?lifecycle=current&channel=direct&service_mode=realtime&region=global&currency=USD&providers=anthropic,openai"

// maxAnswer caps what a sync will read. The answer is written to disk and
// re-read on every model picker paint.
const maxAnswer = 4 << 20

const file = "models.json"

// fetchWait caps the sync. It is one request behind a button, not a startup
// dependency: a slow answer must not hold the settings pane open.
const fetchWait = 20 * time.Second

// agents maps a cost.dev provider onto the ade agent that can drive it.
var agents = map[string]string{"anthropic": "claude", "openai": "codex"}

// builtIn is the curated head of the list: the aliases Claude Code resolves
// itself, which no catalogue carries, and ade's long-window default.
var builtIn = []Model{
	{ID: "claude-opus-5[1m]", Label: "opus", Agent: "claude", Context: 1_000_000},
	{ID: "opus", Label: "opus", Agent: "claude", Context: 200_000},
	{ID: "sonnet", Label: "sonnet", Agent: "claude", Context: 200_000},
	{ID: "haiku", Label: "haiku", Agent: "claude", Context: 200_000},
	{ID: "gpt-5.6-terra", Label: "GPT-5.6 Terra", Agent: "codex", Context: 1_050_000},
	{ID: "gpt-5.6-sol", Label: "GPT-5.6 Sol", Agent: "codex", Context: 1_050_000},
	{ID: "gpt-5.6-luna", Label: "GPT-5.6 Luna", Agent: "codex", Context: 1_050_000},
	{ID: "gpt-5.5", Label: "GPT-5.5", Agent: "codex", Context: 1_050_000},
}

// cached is Load's answer, held for windowTTL: the context meter asks for one
// model's window on every state build.
var cached struct {
	sync.Mutex
	at   time.Time
	list []Model
}

const windowTTL = time.Minute

// Built is the list a machine that has never synced offers.
func Built() Catalogue {
	return Catalogue{Models: append([]Model(nil), builtIn...)}
}

// Window is the catalogue's context window for a model id, or 0 for one it
// does not list.
func Window(id string) int {
	id = strings.TrimSpace(id)
	if id == "" {
		return 0
	}
	cached.Lock()
	defer cached.Unlock()
	if time.Since(cached.at) > windowTTL {
		cached.list, cached.at = Load().Models, time.Now()
	}
	for _, one := range cached.list {
		if one.ID == id {
			return one.Context
		}
	}
	return 0
}

// Path is the cached catalogue, beside the settings.
func Path() (string, error) {
	dir, err := settings.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, file), nil
}

// Load is the synced catalogue, or the built-in one when nothing was synced.
func Load() Catalogue {
	path, err := Path()
	if err != nil {
		return Built()
	}
	body, err := os.ReadFile(path) // #nosec G304 -- ade's own state directory
	if err != nil {
		return Built()
	}
	var out Catalogue
	if err := json.Unmarshal(body, &out); err != nil || len(out.Models) == 0 {
		return Built()
	}
	return out
}

// Sync fetches the catalogue and writes it. The built-in aliases stay at the
// head: they are what the agents resolve, and no price list names them.
func Sync(ctx context.Context) (Catalogue, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchWait)
	defer cancel()

	fetched, err := fetch(ctx)
	if err != nil {
		return Catalogue{}, err
	}
	at := time.Now().UTC()
	out := Catalogue{SyncedAt: &at, Models: merge(builtIn, fetched)}
	if err := save(out); err != nil {
		return Catalogue{}, err
	}
	return out, nil
}

func fetch(ctx context.Context) ([]Model, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(), nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ask cost.dev for the model list: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cost.dev answered HTTP %d", res.StatusCode)
	}
	return decode(io.LimitReader(res.Body, maxAnswer))
}

func endpoint() string {
	if from := strings.TrimSpace(os.Getenv("ADE_MODELS_API")); from != "" {
		return from
	}
	return API
}

// item is only the fields ade shows or keys on; the answer carries benchmarks
// and provenance this has no use for.
type item struct {
	ID       string `json:"id"`
	Name     string `json:"display_name"`
	Provider struct {
		ID string `json:"id"`
	} `json:"provider"`
	Context int  `json:"context_window"`
	Latest  bool `json:"latest"`
	Prices  struct {
		In  string `json:"input_tokens_per_1000000"`
		Out string `json:"output_tokens_per_1000000"`
	} `json:"prices"`
}

func decode(body io.Reader) ([]Model, error) {
	var payload struct {
		Items []item `json:"items"`
	}
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("read the cost.dev model list: %w", err)
	}
	out := make([]Model, 0, len(payload.Items))
	for _, one := range payload.Items {
		agent, ok := agents[one.Provider.ID]
		// A provider ade cannot drive is not a model you can start on.
		if !ok {
			continue
		}
		id := strings.TrimPrefix(one.ID, one.Provider.ID+":")
		if id == "" {
			continue
		}
		out = append(out, Model{
			ID: id, Label: one.Name, Agent: agent, Context: one.Context,
			Latest: one.Latest, In: one.Prices.In, Out: one.Prices.Out,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Agent != out[j].Agent {
			return out[i].Agent < out[j].Agent
		}
		if out[i].Latest != out[j].Latest {
			return out[i].Latest
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// merge keeps the curated entries first and drops a fetched duplicate of one.
func merge(head, fetched []Model) []Model {
	seen := make(map[string]bool, len(head))
	out := make([]Model, 0, len(head)+len(fetched))
	for _, one := range head {
		seen[one.ID] = true
		out = append(out, one)
	}
	for _, one := range fetched {
		if seen[one.ID] {
			continue
		}
		seen[one.ID] = true
		out = append(out, one)
	}
	return out
}

func save(c Catalogue) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("make ade's state directory: %w", err)
	}
	body, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return fmt.Errorf("write the model list: %w", err)
	}
	cached.Lock()
	cached.list, cached.at = c.Models, time.Now()
	cached.Unlock()
	return nil
}
