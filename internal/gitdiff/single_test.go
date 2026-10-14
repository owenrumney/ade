package gitdiff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// singleLineRepo commits base, then applies want on a branch, so each case is a
// real git diff rather than a hand-written one.
func singleLineRepo(t *testing.T, base, head map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}

	git("init", "-b", "main", "-q")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o600))
	for name, body := range base {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	git("add", ".")
	git("commit", "-qm", "base")

	git("checkout", "-qb", "feature")
	for name, body := range head {
		if body == "" {
			_ = os.Remove(filepath.Join(dir, name))
			continue
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	git("add", "-A")
	git("commit", "-qm", "change")
	return dir
}

func rowCount(f File) int {
	n := 0
	for _, h := range f.Hunks {
		n += len(h.Rows)
	}
	return n
}

func TestSingleLineFiles(t *testing.T) {
	tests := []struct {
		name     string
		base     map[string]string
		head     map[string]string
		file     string
		wantRows int
		content  string
	}{
		{
			name:     "new file with one line",
			head:     map[string]string{"one.txt": "hello\n"},
			file:     "one.txt",
			wantRows: 1,
			content:  "hello",
		},
		{
			name:     "new file with one line and no trailing newline",
			head:     map[string]string{"one.txt": "hello"},
			file:     "one.txt",
			wantRows: 1,
			content:  "hello",
		},
		{
			name:     "single line modified",
			base:     map[string]string{"one.txt": "before\n"},
			head:     map[string]string{"one.txt": "after\n"},
			file:     "one.txt",
			wantRows: 1,
			content:  "after",
		},
		{
			name:     "single line file deleted",
			base:     map[string]string{"one.txt": "gone\n"},
			head:     map[string]string{"one.txt": ""},
			file:     "one.txt",
			wantRows: 1,
			content:  "gone",
		},
		{
			name:     "single line that is a SQL comment",
			head:     map[string]string{"q.sql": "-- just a comment\n"},
			file:     "q.sql",
			wantRows: 1,
			content:  "-- just a comment",
		},
		{
			name:     "deleting a SQL comment line",
			base:     map[string]string{"q.sql": "-- just a comment\n"},
			head:     map[string]string{"q.sql": ""},
			file:     "q.sql",
			wantRows: 1,
			content:  "-- just a comment",
		},
		{
			name:     "single line of yaml frontmatter",
			head:     map[string]string{"doc.md": "---\n"},
			file:     "doc.md",
			wantRows: 1,
			content:  "---",
		},
		{
			name:     "single line starting with a plus plus",
			head:     map[string]string{"n.txt": "++ incremented\n"},
			file:     "n.txt",
			wantRows: 1,
			content:  "++ incremented",
		},
		{
			name:     "single empty line",
			head:     map[string]string{"blank.txt": "\n"},
			file:     "blank.txt",
			wantRows: 1,
			content:  "",
		},
		{
			name:     "single line that looks like an index header",
			head:     map[string]string{"i.txt": "index 1234567..89abcde 100644\n"},
			file:     "i.txt",
			wantRows: 1,
			content:  "index 1234567..89abcde 100644",
		},
		{
			name:     "single line that looks like a diff header",
			head:     map[string]string{"d.txt": "diff --git a/x b/x\n"},
			file:     "d.txt",
			wantRows: 1,
			content:  "diff --git a/x b/x",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := singleLineRepo(t, tt.base, tt.head)

			diff, err := Load(context.Background(), Options{Repo: dir, Scope: ScopeCommitted})
			require.NoError(t, err)

			var got *File
			for i := range diff.Files {
				if diff.Files[i].Path == tt.file {
					got = &diff.Files[i]
				}
			}
			require.NotNil(t, got, "%s missing from diff; files were %v", tt.file, paths(diff.Files))
			assert.Equal(t, tt.wantRows, rowCount(*got), "wrong number of rendered rows")

			if tt.content != "" {
				var found bool
				for _, h := range got.Hunks {
					for _, r := range h.Rows {
						for _, c := range []*Cell{r.Left, r.Right} {
							if c != nil && c.Content == tt.content {
								found = true
							}
						}
					}
				}
				assert.True(t, found, "no rendered cell held %q", tt.content)
			}
		})
	}
}

func TestSingleLineUntrackedFile(t *testing.T) {
	dir := singleLineRepo(t, map[string]string{"tracked.txt": "a\n"}, map[string]string{"tracked.txt": "b\n"})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "new.txt"), []byte("untracked line\n"), 0o600))

	diff, err := Load(context.Background(), Options{Repo: dir})
	require.NoError(t, err)

	var got *File
	for i := range diff.Files {
		if diff.Files[i].Path == "new.txt" {
			got = &diff.Files[i]
		}
	}
	require.NotNil(t, got, "untracked single-line file missing; files were %v", paths(diff.Files))
	require.Equal(t, 1, rowCount(*got))
	assert.Equal(t, "untracked line", got.Hunks[0].Rows[0].Right.Content)
}
