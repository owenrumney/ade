package spaces

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/owenrumney/ade/internal/sessions"
)

func repo(owner, name string, used ...time.Time) sessions.Repo {
	r := sessions.Repo{Owner: owner, Name: name}
	if len(used) > 0 {
		r.LastUsed = used[0]
	}
	return r
}

var found = []sessions.Repo{
	repo("owenrumney", "ade", time.Unix(300, 0)),
	repo("infracost", "coast", time.Unix(200, 0)),
	repo("infracost", "docs", time.Unix(100, 0)),
	repo("", "scratch-20260930-090327"),
}

// A space is a repository, not a directory, so every worktree of it belongs
// to one and two repositories called "docs" are two spaces.
func TestASpaceIsOwnerAndName(t *testing.T) {
	assert.Equal(t, "infracost/docs", Key("infracost", "docs"))
	assert.Equal(t, "infracost/docs", Key("Infracost", "Docs"))
	assert.NotEqual(t, Key("infracost", "docs"), Key("owenrumney", "docs"))
	assert.Equal(t, "", Key("", "docs"), "a scratch directory is nobody's space")
	assert.Equal(t, "", Key("infracost", ""))
}

func TestTheListKeepsThePersonsOrderAndDropsNonsense(t *testing.T) {
	assert.Equal(t,
		[]string{"owenrumney/ade", "infracost/coast"},
		Clean([]string{"owenrumney/ade", " ", "infracost/coast", "OWENRUMNEY/ADE", "coast", "a/b/c"}))
}

func TestOpeningIsIdempotentAndClosingTouchesNothingElse(t *testing.T) {
	open := Open(nil, "infracost/coast")
	assert.Equal(t, []string{"infracost/coast"}, open)
	assert.Equal(t, open, Open(open, "Infracost/Coast"), "opening twice is opening once")

	open = Open(open, "owenrumney/ade")
	assert.Equal(t, []string{"infracost/coast", "owenrumney/ade"}, open)
	assert.Equal(t, []string{"owenrumney/ade"}, Close(open, "infracost/coast"))
	assert.Equal(t, open, Close(open, "nobody/else"))
}

// Opening none is not asking to see less: an empty list must never leave ade
// looking empty.
func TestNoSpacesMeansEveryRepository(t *testing.T) {
	assert.Equal(t, found, Within(found, nil))
	assert.Equal(t, found, Within(found, []string{" ", "rubbish"}))
}

func TestTheSetIsShownInItsOwnOrder(t *testing.T) {
	got := Within(found, []string{"infracost/coast", "owenrumney/ade"})
	assert.Equal(t, []string{"coast", "ade"}, []string{got[0].Name, got[1].Name})
}

// A space whose clone was deleted is still a space. Dropping it silently
// would lose the one thing the person asked ade to remember.
func TestASpaceWithNoCheckoutHereIsStillListed(t *testing.T) {
	open := []string{"infracost/coast", "infracost/gone"}
	assert.Len(t, Within(found, open), 1)
	assert.Equal(t, []string{"infracost/gone"}, Missing(found, open))
	assert.Empty(t, Missing(found, []string{"infracost/coast"}))
}

func TestThePickerLeavesOutWhatIsOpenAndWhatHasNoOwner(t *testing.T) {
	got := Offerable(found, []string{"owenrumney/ade"})
	names := []string{}
	for _, r := range got {
		names = append(names, r.Name)
	}
	assert.Equal(t, []string{"coast", "docs"}, names, "newest first, no scratch, nothing already open")
}
