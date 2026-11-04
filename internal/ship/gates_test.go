package ship

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheRailIsWhatIsRegisteredAndInThatOrder(t *testing.T) {
	gates := Standard().Run(Session{})
	require.Len(t, gates, 4)
	assert.Equal(t, []string{"Threads resolved", "Tests ran", "Design approved", "Agent lines read"},
		[]string{gates[0].Name, gates[1].Name, gates[2].Name, gates[3].Name})
}

func TestAGateAddedToTheRegistryIsDrawnWithTheRest(t *testing.T) {
	registry := Standard().Add(func(Session) Gate {
		return Gate{Name: "Mine", State: Pass, Note: "fine"}
	})
	gates := registry.Run(Session{})
	require.Len(t, gates, 5)
	assert.Equal(t, "Mine", gates[4].Name)
}

func TestOpenThreadsFailTheirGateWithSomethingToPress(t *testing.T) {
	gate := threadsGate(Session{Threads: 2})
	assert.Equal(t, Fail, gate.State)
	assert.Equal(t, "2 threads unresolved on the diff", gate.Note)
	require.NotNil(t, gate.Button)
	assert.Equal(t, "threads", gate.Button.Do)
}

func TestARepositoryWithNoCheckListIsNotAskedToRunOne(t *testing.T) {
	gate := testsGate(Session{})
	assert.Equal(t, Optional, gate.State)
	assert.Nil(t, gate.Button)
}

func TestChecksThatHaveNotRunThisSessionWarnRatherThanFail(t *testing.T) {
	gate := testsGate(Session{Tests: Tests{Configured: true}})
	assert.Equal(t, Warn, gate.State)
	require.NotNil(t, gate.Button)
	assert.Equal(t, "tests", gate.Button.Do)
}

func TestAFailedCheckIsNamedByItsGate(t *testing.T) {
	gate := testsGate(Session{Tests: Tests{Configured: true, Ran: true, Failed: "go test"}})
	assert.Equal(t, Fail, gate.State)
	assert.Equal(t, "go test failed", gate.Note)
}

func TestUnresolvedDesignNotesWarnAndNameTheDocument(t *testing.T) {
	gate := designGate(Session{Design: Design{Open: true, File: "ship.md", Unresolved: 1}})
	assert.Equal(t, Warn, gate.State)
	assert.Equal(t, "1 note unresolved on ship.md", gate.Note)
}

func TestUnreadAgentLinesWarnAndNeverBlock(t *testing.T) {
	gate := readGate(Session{Ledger: Ledger{Agent: 112, Unread: 38}})
	assert.Equal(t, Warn, gate.State)
	assert.Equal(t, "38 of 112 agent lines unread", gate.Note)
	require.NotNil(t, gate.Button)
	assert.Equal(t, "read", gate.Button.Do)

	assert.Equal(t, Pass, readGate(Session{Ledger: Ledger{Agent: 112}}).State)
	assert.Equal(t, Optional, readGate(Session{}).State,
		"a branch the agent did not write is not asked to read it")
}

func TestUnreadAgentLinesAreNeverAVeto(t *testing.T) {
	project := Project{Branch: "work", Base: "origin/main"}
	session := Session{Ledger: Ledger{Agent: 112, Unread: 112}}
	assert.Empty(t, Standing().Reason(StepCommit, session, project))
	assert.Empty(t, Standing().Reason(StepRaise, session, project))
}

func TestCommittingToTheBaseBranchIsVetoed(t *testing.T) {
	project := Project{Branch: "main", Base: "origin/main", Home: true}
	why := Standing().Reason(StepCommit, Session{}, project)
	assert.Equal(t, "main is the base branch: start a branch before committing to it", why)
	assert.Empty(t, Standing().Reason(StepRaise, Session{}, project), "raising is not what this stops")
}

func TestABranchOfItsOwnCommitsWithNothingInTheWay(t *testing.T) {
	project := Project{Branch: "work", Base: "origin/main", Home: true}
	assert.Empty(t, Standing().Reason(StepCommit, Session{}, project))
}

// A mount is somebody else's repository, deliberately on its own main.
func TestAMountedRepositoryOnItsOwnMainIsNotVetoed(t *testing.T) {
	project := Project{Branch: "main", Base: "origin/main"}
	assert.Empty(t, Standing().Reason(StepCommit, Session{}, project))
}

func TestRaisingWithThreadsOpenIsVetoedAndSaysHowMany(t *testing.T) {
	project := Project{Branch: "work", Base: "origin/main"}
	why := Standing().Reason(StepRaise, Session{Threads: 3}, project)
	assert.Equal(t, "3 threads still unresolved on the diff", why)
	assert.Empty(t, Standing().Reason(StepCommit, Session{Threads: 3}, project),
		"a commit is not where the threads matter")
}
