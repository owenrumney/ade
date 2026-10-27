package transcripts

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ade replaced only the separator, so every scratch checkout under ~/.ade
// missed its own project directory and fell through to a glob of all of them.
func TestTheProjectDirectoryReplacesDotsAndUnderscoresToo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projects := filepath.Join(home, ".claude", "projects")

	for _, tc := range []struct{ cwd, want string }{
		{"/Users/owen/code/infracost/coast", "-Users-owen-code-infracost-coast"},
		{"/Users/owen/.ade", "-Users-owen--ade"},
		{"/Users/owen/.ade/scratch/fix-674", "-Users-owen--ade-scratch-fix-674"},
		{"/private/tmp/resumeA.62YK", "-private-tmp-resumeA-62YK"},
		{"/Users/owen/code/my_repo", "-Users-owen-code-my-repo"},
		// A dash in a directory name is kept, which is why the reverse is only
		// ever a fallback.
		{"/Users/owen/code/infracost/coast-trk-1195", "-Users-owen-code-infracost-coast-trk-1195"},
		// A trailing separator is the same directory.
		{"/Users/owen/code/coast/", "-Users-owen-code-coast"},
	} {
		assert.Equalf(t, filepath.Join(projects, tc.want), ClaudeProjectDir(tc.cwd), "cwd %q", tc.cwd)
	}

	assert.Empty(t, ClaudeProjectDir(""))
}

func TestTheTranscriptPathNamesTheFileWithoutCheckingForIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got := ClaudeTranscript("/Users/owen/.ade/scratch/fix-674", "a928abe4")
	assert.Equal(t, filepath.Join(home, ".claude", "projects",
		"-Users-owen--ade-scratch-fix-674", "a928abe4.jsonl"), got)

	assert.Empty(t, ClaudeTranscript("/Users/owen/code/coast", ""))
	assert.Empty(t, ClaudeTranscript("", "a928abe4"))
}
