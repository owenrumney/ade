package comments

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAClaimsFateIsReadOffWhatIsAlreadyRecorded(t *testing.T) {
	agent := func(outcome string) Comment { return Comment{Author: AuthorAgent, Outcome: outcome} }

	tests := []struct {
		name   string
		thread Thread
		want   string
	}{
		{"nobody has ruled", Thread{}, FateOpen},
		{"the resolver changed the code", Thread{Comments: []Comment{agent("fixed")}}, FateFixed},
		{"the resolver disagreed", Thread{Comments: []Comment{agent("declined")}}, FateDeclined},
		{"the resolver wants more", Thread{Comments: []Comment{agent("unclear")}}, FateUnclear},
		{"closed by hand counts as fixed", Thread{Resolved: true}, FateFixed},
		{"a withdrawn claim was never owed a fix", Thread{Withdrawn: true, Resolved: true, Comments: []Comment{agent("fixed")}}, FateWithdrawn},
		{"a duplicate is answered by the thread it repeats", Thread{DuplicateOf: "other", Comments: []Comment{agent("fixed")}}, FateDuplicate},
		{"a reply after the verdict does not erase it", Thread{Comments: []Comment{agent("declined"), {Author: AuthorYou, Body: "I still think so"}}}, FateDeclined},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.thread.Fate())
		})
	}
}

func TestADisputedClaimIsStillOpenUntilSomebodyRulesOnIt(t *testing.T) {
	assert.Equal(t, FateOpen, Thread{Disputed: true}.Fate())
}
