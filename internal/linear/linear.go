// Package linear reads the issues assigned to you from Linear, with a
// read-only API key. Nothing here writes.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/netcheck"
)

// Issue is one of yours that is not done: to do or in progress.
type Issue struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	State      string `json:"state"`
	// Started is whether it is in progress rather than waiting to be.
	Started    bool      `json:"started"`
	Priority   int       `json:"priority"`
	BranchName string    `json:"branchName,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt"`

	// Project is what it is part of, for grouping. Empty is an issue in
	// no project, which Linear allows and the list calls out.
	Project string `json:"project,omitempty"`

	// Description is the ticket body, for a reader sizing the work. Never
	// serialised: the dash draws titles, and this would be most of its payload.
	Description string `json:"-"`
}

// Client asks Linear's GraphQL API on your behalf.
type Client struct {
	Key string
	// BaseURL and HTTP are for tests; empty means the real thing.
	BaseURL string
	HTTP    *http.Client
}

const DefaultURL = "https://api.linear.app/graphql"

// myIssues is what the menu shows: assigned to you, not started or started,
// newest first. Linear's state types are fixed words; "unstarted" is the
// Todo column, "started" In Progress.
const myIssues = `{ viewer { assignedIssues(first: 50, orderBy: updatedAt, filter: { state: { type: { in: ["unstarted", "started"] } } }) { nodes { identifier title description url priority branchName updatedAt state { name type } project { name } } } } }`

// MyIssues lists your open issues: in progress first, then by priority,
// then newest.
func (c Client) MyIssues(ctx context.Context) ([]Issue, error) {
	if strings.TrimSpace(c.Key) == "" {
		return nil, fmt.Errorf("no Linear API key in settings")
	}
	body, err := json.Marshal(map[string]string{"query": myIssues})
	if err != nil {
		return nil, err
	}
	url := c.BaseURL
	if url == "" {
		url = DefaultURL
	}
	seen := netcheck.New()
	req, err := http.NewRequestWithContext(seen.Trace(ctx), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.Key)
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		// Which phase it reached, since the error itself never says.
		return nil, fmt.Errorf("ask Linear (got as far as: %s): %w", seen.Reached(), err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read Linear's answer: %w", err)
	}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("Linear refused the API key")
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Linear answered %s", res.Status)
	}

	var reply struct {
		Data struct {
			Viewer struct {
				AssignedIssues struct {
					Nodes []struct {
						Identifier  string    `json:"identifier"`
						Title       string    `json:"title"`
						Description string    `json:"description"`
						URL         string    `json:"url"`
						Priority    int       `json:"priority"`
						BranchName  string    `json:"branchName"`
						UpdatedAt   time.Time `json:"updatedAt"`
						State       struct {
							Name string `json:"name"`
							Type string `json:"type"`
						} `json:"state"`
						Project struct {
							Name string `json:"name"`
						} `json:"project"`
					} `json:"nodes"`
				} `json:"assignedIssues"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, fmt.Errorf("read Linear's answer: %w", err)
	}
	if len(reply.Errors) > 0 {
		return nil, fmt.Errorf("Linear: %s", reply.Errors[0].Message)
	}
	out := make([]Issue, 0, len(reply.Data.Viewer.AssignedIssues.Nodes))
	for _, n := range reply.Data.Viewer.AssignedIssues.Nodes {
		out = append(out, Issue{
			Identifier: n.Identifier, Title: n.Title, URL: n.URL, State: n.State.Name,
			Started: n.State.Type == "started", Priority: n.Priority,
			BranchName: n.BranchName, UpdatedAt: n.UpdatedAt,
			Project: n.Project.Name, Description: n.Description,
		})
	}
	Sort(out)
	return out, nil
}

// Sort puts what is in progress first, then the most urgent, then the most
// recently touched. Linear's priority is 1 urgent to 4 low, and 0 for none,
// which goes last.
func Sort(issues []Issue) {
	rank := func(p int) int {
		if p == 0 {
			return 5
		}
		return p
	}
	sort.SliceStable(issues, func(i, j int) bool {
		a, b := issues[i], issues[j]
		if a.Started != b.Started {
			return a.Started
		}
		if rank(a.Priority) != rank(b.Priority) {
			return rank(a.Priority) < rank(b.Priority)
		}
		return a.UpdatedAt.After(b.UpdatedAt)
	})
}

// refRE is an issue identifier: a team key, a dash, a number. Used where a
// ticket goes into a prompt, so the shape is the boundary.
var refRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,15}-[0-9]{1,9}$`)

// ValidRef reports whether s could be a Linear issue identifier, e.g. TRK-1072.
func ValidRef(s string) bool { return refRE.MatchString(s) }
