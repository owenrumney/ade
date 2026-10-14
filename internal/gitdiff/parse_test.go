package gitdiff

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseModifiedFile(t *testing.T) {
	out := `diff --git a/main.go b/main.go
index 1234567..89abcde 100644
--- a/main.go
+++ b/main.go
@@ -1,6 +1,7 @@ package main
 import "fmt"

-func main() {
-	fmt.Println("old")
+func main() {
+	fmt.Println("new")
+	fmt.Println("extra")
 }`

	files := Parse(out)
	require.Len(t, files, 1)

	f := files[0]
	assert.Equal(t, "main.go", f.Path)
	assert.Equal(t, StatusModified, f.Status)
	assert.Equal(t, 3, f.Additions)
	assert.Equal(t, 2, f.Deletions)
	require.Len(t, f.Hunks, 1)

	h := f.Hunks[0]
	assert.Equal(t, "@@ -1,6 +1,7 @@", h.Header)
	assert.Equal(t, "package main", h.Heading)
	assert.Equal(t, 1, h.OldStart)
	assert.Equal(t, 1, h.NewStart)

	require.Len(t, h.Rows, 6)

	// Context keeps old numbering on the left and new on the right.
	assert.Equal(t, 1, h.Rows[0].Left.Line)
	assert.Equal(t, 1, h.Rows[0].Right.Line)
	assert.Equal(t, KindContext, h.Rows[0].Left.Kind)
	assert.Equal(t, `import "fmt"`, h.Rows[0].Left.Content)

	// The deletion run zips against the addition run that follows it.
	assert.Equal(t, KindDel, h.Rows[2].Left.Kind)
	assert.Equal(t, 3, h.Rows[2].Left.Line)
	assert.Equal(t, KindAdd, h.Rows[2].Right.Kind)
	assert.Equal(t, 3, h.Rows[2].Right.Line)

	assert.Equal(t, 4, h.Rows[3].Left.Line)
	assert.Equal(t, 4, h.Rows[3].Right.Line)

	// The surplus addition pads the left side.
	assert.Nil(t, h.Rows[4].Left)
	require.NotNil(t, h.Rows[4].Right)
	assert.Equal(t, 5, h.Rows[4].Right.Line)
	assert.Equal(t, "\tfmt.Println(\"extra\")", h.Rows[4].Right.Content)

	// Trailing context resumes with the drifted numbering.
	assert.Equal(t, 5, h.Rows[5].Left.Line)
	assert.Equal(t, 6, h.Rows[5].Right.Line)
}

func TestParseFileStatuses(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		path    string
		oldPath string
		status  string
		binary  bool
		hunks   int
	}{
		{
			name: "added",
			out: `diff --git a/new.go b/new.go
new file mode 100644
index 0000000..1234567
--- /dev/null
+++ b/new.go
@@ -0,0 +1,2 @@
+package main
+`,
			path:   "new.go",
			status: StatusAdded,
			hunks:  1,
		},
		{
			name: "deleted",
			out: `diff --git a/gone.go b/gone.go
deleted file mode 100644
index 1234567..0000000
--- a/gone.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package main
-`,
			path:   "gone.go",
			status: StatusDeleted,
			hunks:  1,
		},
		{
			name: "pure rename has no hunks",
			out: `diff --git a/old.go b/new.go
similarity index 100%
rename from old.go
rename to new.go`,
			path:    "new.go",
			oldPath: "old.go",
			status:  StatusRenamed,
			hunks:   0,
		},
		{
			name: "rename with edits",
			out: `diff --git a/old.go b/new.go
similarity index 80%
rename from old.go
rename to new.go
index 1234567..89abcde 100644
--- a/old.go
+++ b/new.go
@@ -1,2 +1,2 @@
 package main
-var x = 1
+var x = 2`,
			path:    "new.go",
			oldPath: "old.go",
			status:  StatusRenamed,
			hunks:   1,
		},
		{
			name: "binary",
			out: `diff --git a/logo.png b/logo.png
index 1234567..89abcde 100644
Binary files a/logo.png and b/logo.png differ`,
			path:   "logo.png",
			status: StatusModified,
			binary: true,
			hunks:  0,
		},
		{
			name: "path with spaces",
			out: `diff --git a/my docs/read me.md b/my docs/read me.md
index 1234567..89abcde 100644
--- a/my docs/read me.md
+++ b/my docs/read me.md
@@ -1 +1 @@
-old
+new`,
			path:   "my docs/read me.md",
			status: StatusModified,
			hunks:  1,
		},
		{
			name: "quoted unicode path",
			out: `diff --git "a/caf\303\251.md" "b/caf\303\251.md"
index 1234567..89abcde 100644
@@ -1 +1 @@
-old
+new`,
			path:   "café.md",
			status: StatusModified,
			hunks:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := Parse(tt.out)
			require.Len(t, files, 1)
			assert.Equal(t, tt.path, files[0].Path)
			assert.Equal(t, tt.oldPath, files[0].OldPath)
			assert.Equal(t, tt.status, files[0].Status)
			assert.Equal(t, tt.binary, files[0].Binary)
			assert.Len(t, files[0].Hunks, tt.hunks)
		})
	}
}

func TestParseMultipleFilesAndHunks(t *testing.T) {
	out := `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -1 +1 @@
-a
+A
@@ -10,2 +10,2 @@ func tail()
-b
+B
 c
diff --git a/b.go b/b.go
index 3..4 100644
--- a/b.go
+++ b/b.go
@@ -5 +5 @@
-x
+X`

	files := Parse(out)
	require.Len(t, files, 2)

	require.Len(t, files[0].Hunks, 2)
	assert.Equal(t, "a.go", files[0].Path)
	assert.Equal(t, 10, files[0].Hunks[1].OldStart)
	assert.Equal(t, "func tail()", files[0].Hunks[1].Heading)
	assert.Equal(t, 2, files[0].Additions)
	assert.Equal(t, 2, files[0].Deletions)

	require.Len(t, files[1].Hunks, 1)
	assert.Equal(t, "b.go", files[1].Path)
	assert.Equal(t, 5, files[1].Hunks[0].NewStart)
}

func TestParseNoNewlineMarker(t *testing.T) {
	out := `diff --git a/f.txt b/f.txt
index 1..2 100644
--- a/f.txt
+++ b/f.txt
@@ -1 +1 @@
-old
\ No newline at end of file
+new
\ No newline at end of file`

	files := Parse(out)
	require.Len(t, files, 1)
	rows := files[0].Hunks[0].Rows
	require.Len(t, rows, 1)
	assert.True(t, rows[0].Left.NoNewline)
	assert.True(t, rows[0].Right.NoNewline)
}

func TestParseEmptyContextLine(t *testing.T) {
	// git writes " " for an empty context line, not a bare empty line.
	out := "diff --git a/f.txt b/f.txt\nindex 1..2 100644\n--- a/f.txt\n+++ b/f.txt\n@@ -1,3 +1,3 @@\n a\n \n-b\n+B"

	files := Parse(out)
	require.Len(t, files, 1)
	rows := files[0].Hunks[0].Rows
	require.Len(t, rows, 3)
	assert.Equal(t, KindContext, rows[1].Left.Kind)
	assert.Empty(t, rows[1].Left.Content)
	assert.Equal(t, 2, rows[1].Left.Line)
	assert.Equal(t, 2, rows[1].Right.Line)
	assert.Equal(t, 3, rows[2].Left.Line)
}

func TestParseIgnoresTrailingNewline(t *testing.T) {
	// Real git output ends in a newline. Splitting it yields a final empty string
	// that must not become a context line at a line number that does not exist.
	out := "diff --git a/f.txt b/f.txt\nindex 1..2 100644\n--- a/f.txt\n+++ b/f.txt\n@@ -1,3 +1,3 @@\n a\n \n-b\n+B\n"

	files := Parse(out)
	require.Len(t, files, 1)
	rows := files[0].Hunks[0].Rows
	require.Len(t, rows, 3, "the trailing newline must not add a phantom row")

	last := rows[len(rows)-1]
	assert.Equal(t, KindDel, last.Left.Kind)
	assert.Equal(t, 3, last.Right.Line, "the last new-side line is 3, not 4")
}

func TestParseStopsAtTheDeclaredHunkSize(t *testing.T) {
	// Anything past the counts in the @@ header is not part of the hunk.
	out := `diff --git a/f.txt b/f.txt
index 1..2 100644
--- a/f.txt
+++ b/f.txt
@@ -1,1 +1,1 @@
-a
+A
 stray context beyond the hunk
+stray addition beyond the hunk`

	files := Parse(out)
	require.Len(t, files, 1)
	assert.Len(t, files[0].Hunks[0].Rows, 1)
	assert.Equal(t, 1, files[0].Additions)
	assert.Equal(t, 1, files[0].Deletions)
}

func TestParseEmptyInput(t *testing.T) {
	assert.Empty(t, Parse(""))
	assert.Empty(t, Parse("\n\n"))
}

func TestPairRuns(t *testing.T) {
	tests := []struct {
		name  string
		lines []line
		want  []string // "left|right" per row, "." for a nil cell
	}{
		{
			name:  "equal runs zip",
			lines: []line{{kind: KindDel, old: 1}, {kind: KindAdd, new: 1}},
			want:  []string{"del|add"},
		},
		{
			name:  "surplus additions pad left",
			lines: []line{{kind: KindDel, old: 1}, {kind: KindAdd, new: 1}, {kind: KindAdd, new: 2}},
			want:  []string{"del|add", ".|add"},
		},
		{
			name:  "surplus deletions pad right",
			lines: []line{{kind: KindDel, old: 1}, {kind: KindDel, old: 2}, {kind: KindAdd, new: 1}},
			want:  []string{"del|add", "del|."},
		},
		{
			name:  "pure addition",
			lines: []line{{kind: KindAdd, new: 1}},
			want:  []string{".|add"},
		},
		{
			name:  "pure deletion",
			lines: []line{{kind: KindDel, old: 1}},
			want:  []string{"del|."},
		},
		{
			name: "context separates two change runs",
			lines: []line{
				{kind: KindDel, old: 1}, {kind: KindAdd, new: 1},
				{kind: KindContext, old: 2, new: 2},
				{kind: KindDel, old: 3}, {kind: KindAdd, new: 3},
			},
			want: []string{"del|add", "context|context", "del|add"},
		},
		{
			name: "addition followed by deletion starts a new run",
			lines: []line{
				{kind: KindAdd, new: 1},
				{kind: KindDel, old: 1},
			},
			want: []string{".|add", "del|."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := pair(tt.lines)
			got := make([]string, 0, len(rows))
			for _, r := range rows {
				got = append(got, describe(r.Left)+"|"+describe(r.Right))
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func describe(c *Cell) string {
	if c == nil {
		return "."
	}
	return c.Kind
}

func TestParseNeverReturnsNil(t *testing.T) {
	// This goes straight to the browser as JSON, where a nil slice becomes null
	// and blows up the first loop that touches it.
	for _, name := range []string{"empty string", "whitespace", "noise"} {
		t.Run(name, func(t *testing.T) {
			var out string
			switch name {
			case "whitespace":
				out = "\n\n  \n"
			case "noise":
				out = "not a diff at all\n"
			}
			files := Parse(out)
			assert.NotNil(t, files)
			assert.Empty(t, files)
		})
	}
}
