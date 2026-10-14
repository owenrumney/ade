package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sources(view UserView) map[string]string {
	out := map[string]string{}
	for _, one := range view.Agents {
		out[one.Name] = one.Source
	}
	return out
}

func TestWithNoAgentsDirectoryEveryDefaultIsListedAndNothingIsInstalled(t *testing.T) {
	home := t.TempDir()
	view, err := User(home)
	require.NoError(t, err)
	assert.False(t, view.Installed)
	assert.Equal(t, filepath.Join(home, ".claude", "agents"), view.Dir)
	shipped, err := Defaults()
	require.NoError(t, err)
	assert.Len(t, view.Agents, len(shipped))
	for _, one := range view.Agents {
		assert.Equal(t, SourceDefault, one.Source, one.Name)
		assert.Empty(t, one.Path, one.Name)
		assert.NotEmpty(t, one.Content, one.Name)
	}
}

func TestAUserFileShadowsTheDefaultAndABrokenOneSaysWhy(t *testing.T) {
	stands(t, shippedTester())
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "agents")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "security.md"), []byte("---\nname: security\ndescription: mine\n---\nbody\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "broken.md"), []byte("no frontmatter"), 0o600))

	view, err := User(home)
	require.NoError(t, err)
	assert.True(t, view.Installed)
	got := sources(view)
	assert.Equal(t, SourceUser, got["security"])
	assert.Equal(t, SourceUser, got["broken"])
	assert.Equal(t, SourceDefault, got["tester"])
	for _, one := range view.Agents {
		switch one.Name {
		case "security":
			assert.Equal(t, "mine", one.Description)
		case "broken":
			assert.Equal(t, "no frontmatter", one.Error)
		}
	}
}

func TestADefaultMustBeInstalledBeforeItCanBeSaved(t *testing.T) {
	stands(t, shippedTester())
	home := t.TempDir()
	body := "---\nname: tester\ndescription: edited\n---\nnew prompt\n"
	require.Error(t, SaveUser(home, "tester", body))

	require.NoError(t, InstallOne(home, "tester"))
	require.NoError(t, SaveUser(home, "tester", body))
	data, err := os.ReadFile(filepath.Join(home, ".claude", "agents", "tester.md"))
	require.NoError(t, err)
	assert.Equal(t, body, string(data))

	// Installing again leaves the edit alone.
	require.NoError(t, InstallOne(home, "tester"))
	data, err = os.ReadFile(filepath.Join(home, ".claude", "agents", "tester.md"))
	require.NoError(t, err)
	assert.Equal(t, body, string(data))
}

func TestSaveRefusesWhatClaudeCouldNotReadAndNamesThatEscape(t *testing.T) {
	stands(t, shippedTester())
	home := t.TempDir()
	require.NoError(t, InstallOne(home, "tester"))
	assert.Error(t, SaveUser(home, "tester", "no frontmatter"))
	assert.Error(t, SaveUser(home, "../tester", "---\nname: x\n---\n"))
	assert.Error(t, InstallOne(home, "../../etc/passwd"))
	assert.Error(t, InstallOne(home, "nosuch"))
}

func TestSaveRefusesAFrontmatterNameThatIsNotTheFile(t *testing.T) {
	stands(t, shippedTester())
	home := t.TempDir()
	require.NoError(t, InstallOne(home, "tester"))
	assert.Error(t, SaveUser(home, "tester", "---\nname: security\n---\nbody\n"))
	assert.NoError(t, SaveUser(home, "tester", "---\ndescription: no name, so the file's\n---\nbody\n"))
}

func TestSaveTakesADefinitionUpToTheCapAndNoMore(t *testing.T) {
	stands(t, shippedTester())
	home := t.TempDir()
	require.NoError(t, InstallOne(home, "tester"))
	head := "---\nname: tester\n---\n"
	assert.NoError(t, SaveUser(home, "tester", head+strings.Repeat("x", maxFile-len(head))))
	assert.Error(t, SaveUser(home, "tester", head+strings.Repeat("x", maxFile-len(head)+1)))
}

func TestOneUnreadableFileIsReportedNotFatal(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "agents")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.Symlink(filepath.Join(home, "nowhere"), filepath.Join(dir, "old.md")))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mine.md"), []byte("---\nname: mine\n---\nbody\n"), 0o600))

	view, err := User(home)
	require.NoError(t, err)
	for _, one := range view.Agents {
		switch one.Name {
		case "old":
			assert.NotEmpty(t, one.Error)
		case "mine":
			assert.Empty(t, one.Error)
		}
	}
	assert.Equal(t, SourceUser, sources(view)["old"])
}

func TestADefaultNamedByAnotherFileIsNotOfferedAgain(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "agents")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "my-security.md"), []byte("---\nname: security\n---\nbody\n"), 0o600))

	view, err := User(home)
	require.NoError(t, err)
	got := sources(view)
	assert.NotContains(t, got, "security")
	assert.Equal(t, SourceUser, got["my-security"])
}

func TestSavingThroughASymlinkKeepsTheLink(t *testing.T) {
	home := t.TempDir()
	dots := filepath.Join(t.TempDir(), "tester.md")
	require.NoError(t, os.WriteFile(dots, []byte("---\nname: tester\n---\nold\n"), 0o600))
	dir := filepath.Join(home, ".claude", "agents")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	link := filepath.Join(dir, "tester.md")
	require.NoError(t, os.Symlink(dots, link))

	require.NoError(t, SaveUser(home, "tester", "---\nname: tester\n---\nnew\n"))
	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink)
	data, err := os.ReadFile(dots)
	require.NoError(t, err)
	assert.Contains(t, string(data), "new")
}
