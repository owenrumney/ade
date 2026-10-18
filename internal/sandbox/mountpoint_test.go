package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Apple's runtime answers errno 17 when the target is not already there.
func TestAMountPointIsMadeInTheCheckoutFirst(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "repo", "storybook")
	require.NoError(t, os.MkdirAll(root, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o600))
	guest := filepath.Join(root, "node_modules")

	_, err := Claude{Home: home, Root: root}.Spec(filepath.Join(home, "box"))
	require.NoError(t, err)
	assert.NoDirExists(t, guest, "a runtime that makes its own mount points is left alone")

	_, err = Claude{Home: home, Root: root, MakeMountPoints: true}.Spec(filepath.Join(home, "box"))
	require.NoError(t, err)
	assert.DirExists(t, guest)
}

func TestTheMountPointDoesNotReplaceTheOverlay(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "repo")
	require.NoError(t, os.MkdirAll(root, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o600))
	guest := filepath.Join(root, "node_modules")

	spec, err := Claude{Home: home, Root: root, MakeMountPoints: true}.Spec(filepath.Join(home, "box"))
	require.NoError(t, err)
	assert.Contains(t, spec.Mounts, Mount{Host: NodeModulesDir(home, guest), Guest: guest, RW: true})
}

// Apple's runtime refuses `type=bind,source=<file>`, and ade mounts files
// from several places: the signing pair, a linked config, an allowed path.
// Staging is at the last point before exec, so every one of them is covered.
func TestEveryFileMountIsStagedAsADirectory(t *testing.T) {
	home := t.TempDir()
	ssh := filepath.Join(home, ".ssh")
	require.NoError(t, os.MkdirAll(ssh, 0o700))
	key := filepath.Join(ssh, "id_ed25519")
	pub := key + ".pub"
	require.NoError(t, os.WriteFile(key, []byte("private"), 0o600))
	require.NoError(t, os.WriteFile(pub, []byte("public"), 0o644))
	loose := filepath.Join(home, ".gitconfig")
	require.NoError(t, os.WriteFile(loose, []byte("[user]\n"), 0o644))
	work := filepath.Join(home, "code")
	require.NoError(t, os.MkdirAll(work, 0o750))

	root := filepath.Join(t.TempDir(), "staged")
	got, err := StageFiles(Spec{Mounts: []Mount{
		{Host: work, Guest: work, RW: true},
		{Host: key, Guest: key},
		{Host: pub, Guest: pub},
		{Host: loose, Guest: loose},
	}}, root)
	require.NoError(t, err)

	for _, m := range got.Mounts {
		info, err := os.Stat(m.Host)
		require.NoError(t, err)
		assert.Truef(t, info.IsDir(), "%s is still a file", m.Host)
	}
	// The directory it was going to, so ~ still resolves inside the box.
	var guests []string
	for _, m := range got.Mounts {
		guests = append(guests, m.Guest)
	}
	assert.ElementsMatch(t, []string{work, ssh, home}, guests)

	staged := ""
	for _, m := range got.Mounts {
		if m.Guest == ssh {
			staged = m.Host
		}
	}
	require.NotEmpty(t, staged)
	named, err := os.ReadDir(staged)
	require.NoError(t, err)
	var there []string
	for _, e := range named {
		there = append(there, e.Name())
	}
	assert.ElementsMatch(t, []string{"id_ed25519", "id_ed25519.pub"}, there,
		"the pair and nothing else of the host's ~/.ssh")

	now, err := os.Stat(filepath.Join(staged, "id_ed25519"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), now.Mode().Perm())
}

// A directory mount over the same place would be hidden by a staged one.
func TestAFileUnderAMountedDirectoryIsLeftAlone(t *testing.T) {
	home := t.TempDir()
	ssh := filepath.Join(home, ".ssh")
	require.NoError(t, os.MkdirAll(ssh, 0o700))
	key := filepath.Join(ssh, "id_ed25519")
	require.NoError(t, os.WriteFile(key, []byte("private"), 0o600))

	got, err := StageFiles(Spec{Mounts: []Mount{
		{Host: ssh, Guest: ssh},
		{Host: key, Guest: key},
	}}, filepath.Join(t.TempDir(), "staged"))
	require.NoError(t, err)
	assert.Equal(t, []Mount{{Host: ssh, Guest: ssh}, {Host: key, Guest: key}}, got.Mounts)
}

func TestNothingToStageChangesNothing(t *testing.T) {
	dir := t.TempDir()
	spec := Spec{Mounts: []Mount{{Host: dir, Guest: dir, RW: true}}}
	got, err := StageFiles(spec, filepath.Join(t.TempDir(), "staged"))
	require.NoError(t, err)
	assert.Equal(t, spec, got)
}
