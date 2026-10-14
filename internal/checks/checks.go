// Package checks turns failing CI checks into an agent instruction.
package checks

import (
	"fmt"
	"strings"

	"github.com/owenrumney/ade/internal/ghpr"
)

// Prompt asks the agent to find out why the checks failed and fix the cause.
// The check names and links are all it is given: reading the logs is the job,
// and guessing from the name is what we want to stop it doing.
func Prompt(number int, failing []ghpr.Check) string {
	var b strings.Builder

	fmt.Fprintf(&b, `The following checks are failing on pull request #%d.

For each one:

- Read the actual failure before changing anything. Use gh to fetch the run log
  (gh run view --log-failed, or the link given) rather than inferring the cause
  from the check's name.
- Fix the cause in the working tree. Do NOT run git commit, git add or git
  stash, and do not push; the user reviews the diff and commits it.
- Where a check fails for a reason outside this branch — a flake, a missing
  secret, an unrelated breakage on the base — say so and leave it alone. A
  correct diagnosis is a good answer; a speculative change is not.

Failing checks:

`, number)

	for i, check := range failing {
		fmt.Fprintf(&b, "%d. %s", i+1, check.Name)
		if check.Workflow != "" {
			fmt.Fprintf(&b, " (%s)", check.Workflow)
		}
		b.WriteString("\n")
		if desc := strings.TrimSpace(check.Description); desc != "" {
			fmt.Fprintf(&b, "   %s\n", desc)
		}
		if check.Link != "" {
			fmt.Fprintf(&b, "   %s\n", check.Link)
		}
	}

	b.WriteString("\nFinish by summarising what you changed, and name any check you deliberately left alone and why.")
	return b.String()
}
