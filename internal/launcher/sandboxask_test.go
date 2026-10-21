package launcher

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func repoForSandbox(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	require.NoError(t, exec.Command("git", "-C", repo, "init", "-q").Run())
	return repo
}

func TestTheNewSessionScreenAddsToWhatTheRepositoryAlreadyAllowed(t *testing.T) {
	repo := repoForSandbox(t)
	file := sandbox.WorkspacePath(filepath.Join(repo, ".git"))
	kept := t.TempDir()
	_, err := sandbox.SaveWorkspace(file, "", sandbox.Workspace{
		Paths: []sandbox.Path{{Path: kept}},
		Env:   map[string]string{"KEPT": "yes"},
	})
	require.NoError(t, err)

	added := t.TempDir()
	require.NoError(t, writeSandbox(context.Background(), repo,
		[]sandbox.Path{{Path: added, RW: true}}, map[string]string{"NEW": "x"}))

	ws, err := sandbox.LoadWorkspace(file)
	require.NoError(t, err)
	assert.ElementsMatch(t, []sandbox.Path{{Path: kept}, {Path: added, RW: true}}, ws.Paths)
	assert.Equal(t, map[string]string{"KEPT": "yes", "NEW": "x"}, ws.Env)
}

func TestAStartWithNoMountsChosenLeavesTheFileAlone(t *testing.T) {
	repo := repoForSandbox(t)
	require.NoError(t, writeSandbox(context.Background(), repo, nil, nil))
	assert.NoFileExists(t, sandbox.WorkspacePath(filepath.Join(repo, ".git")))
}

func TestAMountThatIsNotThereIsRefusedBeforeTheSessionStarts(t *testing.T) {
	repo := repoForSandbox(t)
	err := writeSandbox(context.Background(), repo,
		[]sandbox.Path{{Path: filepath.Join(repo, "nowhere")}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nowhere")
}
