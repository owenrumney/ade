package ghpr

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// gh prints the url and nothing else worth reading.
func TestThePullRequestNumberIsReadOffWhatGhPrints(t *testing.T) {
	for _, tc := range []struct {
		out  string
		want int
	}{
		{"https://github.com/owenrumney/ade/pull/412\n", 412},
		{"Creating draft pull request for feat/x into main\n\nhttps://github.com/o/r/pull/7\n", 7},
	} {
		found := prURL.FindStringSubmatch(tc.out)
		assert.NotNil(t, found, tc.out)
		assert.Equal(t, tc.want, mustInt(t, found[1]))
	}
}

func TestNothingThatLooksLikeAPullRequestIsNotOne(t *testing.T) {
	for _, out := range []string{"", "no remote\n", "https://github.com/o/r/issues/4\n"} {
		assert.Nil(t, prURL.FindStringSubmatch(out), out)
	}
}

func mustInt(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
