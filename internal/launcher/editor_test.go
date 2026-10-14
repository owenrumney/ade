package launcher

import (
	"net/http"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/editor"
)

func standIn(t *testing.T, err error) *[]string {
	t.Helper()
	var opened []string
	was := openEditor
	openEditor = func(dir string) error {
		opened = append(opened, dir)
		return err
	}
	t.Cleanup(func() { openEditor = was })
	return &opened
}

func TestTheEditorOpensOnlyARealCheckout(t *testing.T) {
	s := newTestLauncher(t)
	opened := standIn(t, nil)

	repo := t.TempDir()
	require.NoError(t, exec.Command("git", "-C", repo, "init", "-q").Run())

	rec := do(t, s, "POST", "/api/editor", `{"dir":"`+repo+`"}`)
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, []string{repo}, *opened)

	for _, dir := range []string{t.TempDir(), "relative/path", "", "--goto"} {
		rec := do(t, s, "POST", "/api/editor", `{"dir":"`+dir+`"}`)
		assert.Equal(t, http.StatusBadRequest, rec.Code, dir)
	}
	assert.Len(t, *opened, 1, "nothing but the checkout reached the editor")
}

func TestNoEditorIsSaidRatherThanFailed(t *testing.T) {
	s := newTestLauncher(t)
	standIn(t, editor.ErrNone)

	repo := t.TempDir()
	require.NoError(t, exec.Command("git", "-C", repo, "init", "-q").Run())

	rec := do(t, s, "POST", "/api/editor", `{"dir":"`+repo+`"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "no editor cli")
}
