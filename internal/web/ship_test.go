package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/audit"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/ship"
	"github.com/owenrumney/ade/internal/verify"
)

func shipState(t *testing.T, s *Server) ShipState {
	t.Helper()
	rec := do(t, s, "GET", "/api/ship", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got ShipState
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	return got
}

func TestShipAnswersAOneCheckoutSessionAsOneColumn(t *testing.T) {
	s := newTestServer(t)
	got := shipState(t, s)

	require.Len(t, got.Projects, 1)
	assert.True(t, got.Projects[0].Home)
	assert.Equal(t, "feature", got.Projects[0].Branch)
	assert.Equal(t, 1, len(got.Projects[0].Unpushed), "the branch's one commit")
	assert.Equal(t, s.space().homeRoot, got.Home)
	assert.Empty(t, got.View, "the diff is on this session's own checkout")
	assert.Len(t, got.Gates, 4)
}

func TestShipAnswersEveryRepositoryTheSessionTouches(t *testing.T) {
	other := otherRepo(t, "ci")
	s := withRoot(t, other)

	got := shipState(t, s)
	require.Len(t, got.Projects, 2)
	assert.True(t, got.Projects[0].Home)
	assert.Equal(t, "ci", got.Projects[1].Name)
	assert.Equal(t, 1, got.Projects[1].Files, "the mount's uncommitted edit")
}

func TestShipTakesTheBranchesPullRequestFromThePollThatAlreadyReadIt(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{
		Number: 12, Title: "the change", HeadRef: "feature", State: "OPEN",
		MergeBlocked: "its branch protection rules are not satisfied yet",
		Checks:       []ghpr.Check{{Name: "test", Bucket: "fail", Description: "one failed"}},
	})

	got := shipState(t, s)
	require.NotNil(t, got.Projects[0].PR)
	assert.Equal(t, 12, got.Projects[0].PR.Number)
	assert.Equal(t, 1, got.Projects[0].PR.Failing)
	assert.Equal(t, "test: one failed", got.Projects[0].PR.FailLine)
	assert.NotEmpty(t, got.Projects[0].PR.MergeBlocked)
}

func TestShipLeavesSomebodyElsesPullRequestOffThisBranchsCard(t *testing.T) {
	s := newTestServer(t)
	s.setBranchPR(&BranchPR{Number: 99, HeadRef: "someone-else", State: "OPEN"})

	assert.Nil(t, shipState(t, s).Projects[0].PR,
		"the poll is describing a pull request this branch is not")
}

func TestTheGateRailReportsTheThreadsAndTheChecks(t *testing.T) {
	s := newTestServer(t)
	s.mu.Lock()
	s.verifyLast = &verify.Outcome{Total: 1, Ran: []verify.Result{{Name: "go test", Passed: true}}}
	s.mu.Unlock()
	require.NoError(t, verify.Save(s.verifyPath(), verify.Config{
		Checks: []verify.Check{{Name: "go test", Run: "true"}},
	}))

	byName := map[string]ship.Gate{}
	for _, gate := range shipState(t, s).Gates {
		byName[gate.Name] = gate
	}
	assert.Equal(t, ship.Pass, byName["Threads resolved"].State)
	assert.Equal(t, ship.Pass, byName["Tests ran"].State)
	assert.Equal(t, ship.Optional, byName["Design approved"].State,
		"no design document is open in a bare test session")
}

// onBase moves the checkout, and this session's idea of it, onto main.
func onBase(t *testing.T, s *Server) {
	t.Helper()
	dir := s.space().homeRoot
	cmd := exec.Command("git", "checkout", "-q", "main")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git checkout: %s", out)
	s.followBranch(context.Background())
	require.Equal(t, "main", s.space().branch)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.go"), []byte("package main\n"), 0o600))
	add := exec.Command("git", "add", "b.go")
	add.Dir = dir
	out, err = add.CombinedOutput()
	require.NoError(t, err, "git add: %s", out)
}

func TestCommittingToTheBaseBranchIsRefusedWithTheReason(t *testing.T) {
	s := newTestServer(t)
	onBase(t, s)

	rec := do(t, s, "POST", "/api/commit", `{"title":"onto main"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "start a branch")
}

func TestCommittingAnywayGoesThroughAndIsWrittenDown(t *testing.T) {
	// The override is recorded in the trail, which is under the home directory.
	t.Setenv("HOME", t.TempDir())
	s := newTestServer(t)
	onBase(t, s)

	rec := do(t, s, "POST", "/api/commit", `{"title":"onto main","anyway":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	dir, err := audit.Dir()
	require.NoError(t, err)
	days, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	require.NoError(t, err)
	require.Len(t, days, 1, "the override wrote a line")
	raw, err := os.ReadFile(days[0])
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"kind":"override"`)
	assert.Contains(t, string(raw), `"what":"commit"`)
	assert.Contains(t, string(raw), "start a branch")
}

func TestRaisingWithThreadsOpenIsRefusedUntilTheyAre(t *testing.T) {
	s := newTestServer(t)
	createThread(t, s, `{"file":"a.go","side":"right","line":3,"body":"this one"}`)
	require.NotEmpty(t, s.space().store.Unresolved())

	rec := do(t, s, "POST", "/api/pr-create", `{}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "unresolved")
}
