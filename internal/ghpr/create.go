package ghpr

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Created is the pull request `gh pr create` just opened.
type Created struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
}

// prURL is the tail of what gh prints: the number is not reported any other way.
var prURL = regexp.MustCompile(`https://\S+/pull/(\d+)`)

// Create opens a pull request for the branch that is checked out, taking its
// title and body from the commits. The branch has to be pushed first.
func Create(ctx context.Context, dir string, draft bool) (Created, error) {
	args := []string{"pr", "create", "--fill"}
	if draft {
		args = append(args, "--draft")
	}
	out, err := exec1(ctx, dir, args, nil)
	if err != nil {
		return Created{}, err
	}
	found := prURL.FindStringSubmatch(out)
	if found == nil {
		return Created{}, fmt.Errorf("gh opened no pull request: %s", strings.TrimSpace(out))
	}
	number, err := strconv.Atoi(found[1])
	if err != nil {
		return Created{}, fmt.Errorf("gh gave no pull request number: %s", found[0])
	}
	return Created{Number: number, URL: found[0]}, nil
}
