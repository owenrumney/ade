package transcripts

import (
	"os"
	"testing"
)

// TestSmokeScan prints what a real scan of this machine found. Opt-in, because
// it depends on the local agent stores rather than fixtures.
func TestSmokeScan(t *testing.T) {
	if os.Getenv("SM_SMOKE") == "" {
		t.Skip("set SM_SMOKE=1 to scan the local agent stores")
	}
	sessions := Scan()
	t.Logf("found %d sessions", len(sessions))

	byAgent := map[Agent]int{}
	noCWD, noPrompt := 0, 0
	for _, s := range sessions {
		byAgent[s.Agent]++
		if s.CWD == "" {
			noCWD++
		}
		if s.FirstPrompt == "" {
			noPrompt++
		}
	}
	for _, agent := range Agents {
		t.Logf("%-11s %d", agent, byAgent[agent])
	}
	t.Logf("missing cwd: %d, missing prompt: %d", noCWD, noPrompt)

	for i, s := range sessions {
		if i >= 15 {
			break
		}
		t.Logf("%-11s %-12s %s", s.Agent, s.DirLabel(), truncate(s.Label(), 70))
	}

	for _, agent := range Agents {
		for _, s := range sessions {
			if s.Agent != agent {
				continue
			}
			if err := Load(s); err != nil {
				t.Errorf("%s load %s: %v", agent, s.Path, err)
				break
			}
			t.Logf("%-11s model=%s turns=%d tools=%v tokens=%d cost=%.4f known=%v ctx=%d/%d",
				agent, s.Model, s.Turns, s.TopTools(3), s.Usage.Total(),
				s.Usage.CostUSD, s.Usage.CostKnown, s.ContextTokens, s.ContextWindow)
			break
		}
	}
}
