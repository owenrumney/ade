package launcher

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAPullRequestFromNoCheckoutIsAskedForByName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTestLauncher(t)

	for _, repo := range []string{"--method=POST", "-R/x", "owner", "a/b/c", "a/b c"} {
		rec := do(t, s, http.MethodGet, "/api/pr?number=1&repo="+url.QueryEscape(repo), "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, repo)
	}
}

func TestTheRepoNameIsOwnerSlashName(t *testing.T) {
	for repo, ok := range map[string]bool{
		"owenrumney/ade":  true,
		"infracost/x.y_z": true,
		"-x/y":            false,
		"x/-y":            false,
		"x":               false,
		"x/y/z":           false,
	} {
		assert.Equal(t, ok, nameWithOwner.MatchString(repo), repo)
	}
}
