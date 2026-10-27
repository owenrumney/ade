package launcher

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Read before anything is checked out: a link that is not a pull request
// should not reach a clone, and the message should say what one looks like.
func TestAPastedLinkIsReadBeforeAnythingIsCheckedOut(t *testing.T) {
	s := newTestLauncher(t)

	for _, bad := range []string{
		`{"link":"https://github.com/infracost/cli"}`,
		`{"link":"https://github.com/infracost/cli/issues/12"}`,
		`{"link":"not a link"}`,
	} {
		rec := do(t, s, http.MethodPost, "/api/reviews/open", bad)
		require.Equalf(t, http.StatusBadRequest, rec.Code, "took %s", bad)
		assert.Contains(t, rec.Body.String(), "pull request link")
	}

	// No link at all is the old request, which names the two fields and
	// answers from the review list.
	rec := do(t, s, http.MethodPost, "/api/reviews/open", `{"link":"   ","repo":"a/b","number":1}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
