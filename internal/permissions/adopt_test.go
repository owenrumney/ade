package permissions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// No test reads the real Claude settings: each that wants some names its own.
func init() {
	claudeSettings = func() (string, error) { return "", os.ErrNotExist }
}

func claudeWith(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	was := claudeSettings
	claudeSettings = func() (string, error) { return path, nil }
	t.Cleanup(func() { claudeSettings = was })
}

func TestANewDenyListStartsAsACopyOfTheClaudeOne(t *testing.T) {
	claudeWith(t, `{"env":{"K":"v"},"permissions":{"allow":["Bash(terraform apply:*)"],"deny":["Bash(git commit *)","Read(~/.ssh/**)","("]}}`)

	s, err := openAt(filepath.Join(t.TempDir(), name))
	require.NoError(t, err)
	file, _ := s.Rules()
	assert.Contains(t, file.Deny, Rule("Bash(git commit *)"))
	assert.Contains(t, file.Deny, Rule("Read(~/.ssh/**)"))
	assert.NotContains(t, file.Deny, Rule("("), "a rule ade cannot match is left behind")
	assert.NotContains(t, file.Allow, Rule("Bash(terraform apply:*)"), "only the denies are copied")
}

func TestAnOlderFileTakesTheClaudeDeniesOnceAndOnlyOnce(t *testing.T) {
	claudeWith(t, `{"permissions":{"deny":["Bash(git commit *)"]}}`)
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(`{"version":1,"allow":[],"deny":["Bash(rm -rf /:*)"]}`), 0o600))

	s, err := openAt(path)
	require.NoError(t, err)
	file, _ := s.Rules()
	assert.Equal(t, []Rule{"Bash(rm -rf /:*)", "Bash(git commit *)"}, file.Deny)

	require.NoError(t, s.Forget("Bash(git commit *)"))
	again, err := openAt(path)
	require.NoError(t, err)
	file, _ = again.Rules()
	assert.Equal(t, []Rule{"Bash(rm -rf /:*)"}, file.Deny, "one taken out stays out")
}

func TestCodexIsHandedTheCommandDeniesAndNothingElse(t *testing.T) {
	got := CodexRules([]Rule{
		"Bash(git commit *)", "Bash(git push --force:*)", "Bash(rm -rf /:*)",
		"Read(~/.ssh/**)", "Bash(curl * | sh)", "WebFetch",
	})
	assert.Contains(t, got, `prefix_rule(pattern=["git", "commit"], decision="forbidden")`)
	assert.Contains(t, got, `prefix_rule(pattern=["git", "push", "--force"], decision="forbidden")`)
	assert.Contains(t, got, `prefix_rule(pattern=["rm", "-rf", "/"], decision="forbidden")`)
	assert.NotContains(t, got, ".ssh")
	assert.NotContains(t, got, "curl")
}

func TestTheCodexRulesGoInAFileOfAdesOwn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	theirs := filepath.Join(home, "rules", "default.rules")
	require.NoError(t, os.MkdirAll(filepath.Dir(theirs), 0o750))
	require.NoError(t, os.WriteFile(theirs, []byte("theirs"), 0o600))

	require.NoError(t, WriteCodexRules([]Rule{"Bash(git commit *)"}))

	ours, err := os.ReadFile(filepath.Join(home, "rules", codexFile))
	require.NoError(t, err)
	assert.Contains(t, string(ours), `["git", "commit"]`)
	kept, err := os.ReadFile(theirs)
	require.NoError(t, err)
	assert.Equal(t, "theirs", string(kept))
}

func TestAnOlderFileGainsTheCommitDenyOnceAndOnlyOnce(t *testing.T) {
	claudeWith(t, `{"permissions":{"deny":["Bash(sudo:*)"]}}`)
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(`{"version":2,"allow":[],"deny":["Bash(rm -rf /:*)"]}`), 0o600))

	s, err := openAt(path)
	require.NoError(t, err)
	file, _ := s.Rules()
	assert.Equal(t, []Rule{"Bash(rm -rf /:*)", commitDeny}, file.Deny, "a version 2 file has had its copy of the Claude denies")

	require.NoError(t, s.Forget(commitDeny))
	again, err := openAt(path)
	require.NoError(t, err)
	file, _ = again.Rules()
	assert.Equal(t, []Rule{"Bash(rm -rf /:*)"}, file.Deny, "one taken out stays out")
}

func TestAFreshFileRefusesAnAgentsCommit(t *testing.T) {
	s, err := openAt(filepath.Join(t.TempDir(), name))
	require.NoError(t, err)
	file, _ := s.Rules()
	assert.Contains(t, file.Deny, commitDeny)
}

func TestADenyAlsoHoldsBehindTheWrapper(t *testing.T) {
	got := Wrapped([]Rule{"Bash(git commit:*)", "Read(~/.ssh/**)", "Bash(rtk git push --force:*)", "Bash(bash -c)"})
	assert.Equal(t, []Rule{
		"Bash(git commit:*)", "Read(~/.ssh/**)", "Bash(rtk git push --force:*)", "Bash(bash -c)",
		"Bash(rtk git commit:*)", "Bash(rtk bash -c)",
	}, got)
	assert.Contains(t, CodexRules(got), `prefix_rule(pattern=["rtk", "git", "commit"], decision="forbidden")`)
}
