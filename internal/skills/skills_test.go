package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func write(t *testing.T, home, name, body string) {
	t.Helper()
	dir := filepath.Join(Dir(home), name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600))
}

func TestLoadReadsTheBodyBelowTheFrontmatter(t *testing.T) {
	home := t.TempDir()
	write(t, home, "prod-triage", "---\nname: prod-triage\ndescription: triage prod\n---\n\n# Production triage\n\nNever mutate.\n")

	skill, err := Load(home, "prod-triage")
	require.NoError(t, err)

	assert.Equal(t, "prod-triage", skill.Name)
	assert.Equal(t, "triage prod", skill.Description)
	assert.Equal(t, "# Production triage\n\nNever mutate.", skill.Prompt)
	assert.NotContains(t, skill.Prompt, "description:", "the frontmatter is metadata, not instructions")
}

func TestLoadRefusesWhatIsNotASkill(t *testing.T) {
	home := t.TempDir()
	write(t, home, "headless", "---\nname: headless\n---\n\n")
	write(t, home, "huge", "---\nname: huge\n---\n\n"+strings.Repeat("x", MaxPrompt))

	for _, tc := range []struct{ name, skill, wants string }{
		{"missing", "nowhere", "read skill nowhere"},
		{"no body", "headless", "no body below its frontmatter"},
		{"too big", "huge", "capped at"},
		// The name becomes a path, so it never leaves ~/.claude/skills.
		{"traversal", "../../../etc", "letters, digits"},
		{"empty", "", "letters, digits"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(home, tc.skill)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wants)
		})
	}
}

func writeAt(t *testing.T, dir, name string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: "+name+"\n---\n\nbody\n"), 0o600))
}

func plugins(t *testing.T, home string, enabled map[string]bool, installs map[string]string) {
	t.Helper()
	index := map[string]any{"version": 2, "plugins": map[string]any{}}
	for name, dir := range installs {
		index["plugins"].(map[string]any)[name] = []map[string]string{{"installPath": dir}}
	}
	for path, value := range map[string]any{
		filepath.Join(home, ".claude", "settings.json"):                     map[string]any{"enabledPlugins": enabled},
		filepath.Join(home, ".claude", "plugins", "installed_plugins.json"): index,
	} {
		data, err := json.Marshal(value)
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, data, 0o600))
	}
}

func names(found []Found) []string {
	var out []string
	for _, f := range found {
		out = append(out, f.Name)
	}
	return out
}

func TestAllIsTheUsersSkillsThenEachEnabledPlugins(t *testing.T) {
	home := t.TempDir()
	write(t, home, "triage", "---\nname: triage\n---\n\nbody\n")
	writeAt(t, filepath.Join(Dir(home), "synced", "abc", "docs"), "docs")
	pack := filepath.Join(home, ".claude", "plugins", "cache", "market", "pack", "1.0.0")
	writeAt(t, filepath.Join(pack, "skills", "productivity", "grill-me"), "grill-me")
	writeAt(t, filepath.Join(pack, "skills", "triage"), "triage")
	off := filepath.Join(home, ".claude", "plugins", "cache", "market", "off", "1.0.0")
	writeAt(t, filepath.Join(off, "skills", "hidden"), "hidden")
	plugins(t, home, map[string]bool{"pack@market": true, "off@market": false}, map[string]string{"pack@market": pack, "off@market": off})

	found, err := All(home)
	require.NoError(t, err)
	assert.Equal(t, []string{"docs", "triage", "grill-me"}, names(found), "the user's first, nested ones included, a plugin's duplicate dropped")
	assert.Equal(t, filepath.Join(Dir(home), "triage"), found[1].Dir, "the user's triage wins over the plugin's")
}

func TestAllWithoutPluginsIsTheUsersAlone(t *testing.T) {
	home := t.TempDir()
	write(t, home, "triage", "---\nname: triage\n---\n\nbody\n")

	found, err := All(home)
	require.NoError(t, err)
	assert.Equal(t, []string{"triage"}, names(found))

	empty, err := All(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, empty, "no ~/.claude at all is nothing, not an error")
}

func TestLinkReplacesItsOwnLinksAndKeepsTheRest(t *testing.T) {
	home := t.TempDir()
	write(t, home, "a", "---\nname: a\n---\n\nbody\n")
	write(t, home, "b", "---\nname: b\n---\n\nbody\n")
	dir := filepath.Join(home, ".codex", "skills")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "keep"), 0o755))
	foreign := t.TempDir()
	require.NoError(t, os.Symlink(foreign, filepath.Join(dir, "foreign")))

	found, err := All(home)
	require.NoError(t, err)
	require.NoError(t, Link(home, dir, found))
	for _, name := range []string{"a", "b"} {
		target, err := os.Readlink(filepath.Join(dir, name))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(Dir(home), name), target)
	}
	target, err := os.Readlink(filepath.Join(dir, "foreign"))
	require.NoError(t, err)
	assert.Equal(t, foreign, target, "a link that is not into ~/.claude is not ade's to touch")
	assert.DirExists(t, filepath.Join(dir, "keep"))

	// Gone from ~/.claude is gone from the links; a real directory keeps its name.
	require.NoError(t, os.RemoveAll(filepath.Join(Dir(home), "b")))
	write(t, home, "keep", "---\nname: keep\n---\n\nbody\n")
	found, err = All(home)
	require.NoError(t, err)
	require.NoError(t, Link(home, dir, found))
	assert.NoFileExists(t, filepath.Join(dir, "b"))
	info, err := os.Lstat(filepath.Join(dir, "keep"))
	require.NoError(t, err)
	assert.True(t, info.IsDir(), "not replaced by a link")
}

func TestShareWithCodexNeedsACodexHome(t *testing.T) {
	home := t.TempDir()
	write(t, home, "a", "---\nname: a\n---\n\nbody\n")

	require.NoError(t, ShareWithCodex(home))
	assert.NoDirExists(t, CodexDir(home), "no ~/.codex, nothing made")

	require.NoError(t, os.MkdirAll(filepath.Join(home, ".codex"), 0o755))
	require.NoError(t, ShareWithCodex(home))
	target, err := os.Readlink(filepath.Join(CodexDir(home), "a"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(Dir(home), "a"), target)
}

func TestKnownIsASkillOrACustomCommandAnywhereTheUserHasOne(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	write(t, home, "grill-me", "---\nname: grill-me\n---\n\nbody\n")
	pack := filepath.Join(home, ".claude", "plugins", "cache", "market", "pack", "1.0.0")
	writeAt(t, filepath.Join(pack, "skills", "teach"), "teach")
	plugins(t, home, map[string]bool{"pack@market": true}, map[string]string{"pack@market": pack})
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude", "commands"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude", "commands", "pr.md"), []byte("open a pr"), 0o600))
	writeAt(t, filepath.Join(root, ".claude", "skills", "local"), "local")

	for _, command := range []string{"/grill-me the design in x.md", "/pack:grill-me", "/teach", "/pr", "/local"} {
		assert.True(t, Known(home, root, command), command)
	}
	for _, command := range []string{"/compact", "/usage", "/", "", "/../grill-me"} {
		assert.False(t, Known(home, root, command), command)
	}
}
