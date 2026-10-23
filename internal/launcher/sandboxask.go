package launcher

import (
	"context"
	"os"

	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/sandbox"
)

// writeSandbox puts what the New session screen chose into the repository's own
// sandbox file. Laid over it: the screen starts from an empty list.
func writeSandbox(ctx context.Context, dir string, paths []sandbox.Path, env map[string]string) error {
	if len(paths) == 0 && len(env) == 0 {
		return nil
	}
	common, err := gitdiff.CommonGitDir(ctx, dir)
	if err != nil {
		return err
	}
	file := sandbox.WorkspacePath(common)
	was, err := sandbox.LoadWorkspace(file)
	if err != nil {
		return err
	}
	asked := sandbox.Workspace{Paths: paths, Env: env}
	home, _ := os.UserHomeDir()
	_, err = sandbox.SaveWorkspace(file, home, asked.Under(was))
	return err
}
