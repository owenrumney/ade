package comments

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/gitdiff"
)

// original has "var x = 2" on new-side line 3.
const original = `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -1,3 +1,3 @@
 package main

-var x = 1
+var x = 2`

// shifted inserts two lines above, moving "var x = 2" to new-side line 5.
const shifted = `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -1,3 +1,5 @@
 package main
+// new
+// new2

-var x = 1
+var x = 2`

// rewritten replaces the anchored line entirely.
const rewritten = `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -1,3 +1,3 @@
 package main

-var x = 1
+var y = 99`

func parseDiff(t *testing.T, out string) *gitdiff.Diff {
	t.Helper()
	files := gitdiff.Parse(out)
	require.NotEmpty(t, files)
	return &gitdiff.Diff{Files: files}
}

func TestLocate(t *testing.T) {
	diff := parseDiff(t, original)

	t.Run("finds a right side line", func(t *testing.T) {
		a, ok := Locate(diff, "a.go", SideRight, 3)
		require.True(t, ok)
		assert.Equal(t, "var x = 2", a.Content)
		assert.NotEmpty(t, a.ContextHash)
	})

	t.Run("finds a left side line", func(t *testing.T) {
		a, ok := Locate(diff, "a.go", SideLeft, 3)
		require.True(t, ok)
		assert.Equal(t, "var x = 1", a.Content)
	})

	t.Run("left and right hashes differ", func(t *testing.T) {
		left, _ := Locate(diff, "a.go", SideLeft, 3)
		right, _ := Locate(diff, "a.go", SideRight, 3)
		assert.NotEqual(t, left.ContextHash, right.ContextHash)
	})

	t.Run("missing line", func(t *testing.T) {
		_, ok := Locate(diff, "a.go", SideRight, 999)
		assert.False(t, ok)
	})

	t.Run("missing file", func(t *testing.T) {
		_, ok := Locate(diff, "nope.go", SideRight, 1)
		assert.False(t, ok)
	})

	t.Run("nil diff", func(t *testing.T) {
		_, ok := Locate(nil, "a.go", SideRight, 1)
		assert.False(t, ok)
	})
}

func TestLocateNearest(t *testing.T) {
	diff := parseDiff(t, original)

	t.Run("exact line wins", func(t *testing.T) {
		anchor, line, ok := LocateNearest(diff, "a.go", SideRight, 3, 10)
		require.True(t, ok)
		assert.Equal(t, 3, line)
		assert.Equal(t, "var x = 2", anchor.Content)
	})

	t.Run("snaps to the closest line in range", func(t *testing.T) {
		_, line, ok := LocateNearest(diff, "a.go", SideRight, 5, 10)
		require.True(t, ok)
		assert.Equal(t, 3, line, "line 5 is not in the diff; 3 is the nearest that is")
	})

	t.Run("refuses to snap beyond the window", func(t *testing.T) {
		_, _, ok := LocateNearest(diff, "a.go", SideRight, 500, 10)
		assert.False(t, ok)
	})

	t.Run("missing file cannot anchor", func(t *testing.T) {
		_, _, ok := LocateNearest(diff, "nope.go", SideRight, 3, 10)
		assert.False(t, ok)
	})
}

func TestReanchor(t *testing.T) {
	anchor, ok := Locate(parseDiff(t, original), "a.go", SideRight, 3)
	require.True(t, ok)

	newThread := func() *Thread {
		return &Thread{File: "a.go", Side: SideRight, Line: 3, Anchor: anchor}
	}

	t.Run("unchanged diff keeps the line", func(t *testing.T) {
		th := newThread()
		reanchor(th, parseDiff(t, original), time.Now())
		assert.Equal(t, 3, th.Line)
		assert.False(t, th.Orphaned)
	})

	t.Run("follows content that moved down", func(t *testing.T) {
		th := newThread()
		reanchor(th, parseDiff(t, shifted), time.Now())
		assert.Equal(t, 5, th.Line)
		assert.False(t, th.Orphaned)
	})

	t.Run("orphans when the content is rewritten", func(t *testing.T) {
		th := newThread()
		reanchor(th, parseDiff(t, rewritten), time.Now())
		assert.True(t, th.Orphaned)
	})

	t.Run("orphans when the file leaves the diff", func(t *testing.T) {
		th := newThread()
		reanchor(th, parseDiff(t, `diff --git a/other.go b/other.go
index 1..2 100644
--- a/other.go
+++ b/other.go
@@ -1 +1 @@
-a
+b`), time.Now())
		assert.True(t, th.Orphaned)
	})

	t.Run("orphans rather than trust a distant match", func(t *testing.T) {
		// The same content 500 lines away is more likely a coincidence than a move.
		distant := `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -500,3 +500,3 @@
 package main

-var x = 1
+var x = 2`
		th := newThread()
		reanchor(th, parseDiff(t, distant), time.Now())
		assert.True(t, th.Orphaned)
	})

	t.Run("un-orphans when the content comes back", func(t *testing.T) {
		th := newThread()
		th.Orphaned = true
		reanchor(th, parseDiff(t, original), time.Now())
		assert.False(t, th.Orphaned)
		assert.Equal(t, 3, th.Line)
	})
}

func TestReanchorPrefersTheNearestMatch(t *testing.T) {
	// Two identical blocks: the anchor must land on the closer one.
	twice := `diff --git a/a.go b/a.go
index 1..2 100644
--- a/a.go
+++ b/a.go
@@ -1,3 +1,3 @@
 package main

-var x = 1
+var x = 2
@@ -20,3 +20,3 @@
 package main

-var x = 1
+var x = 2`

	anchor, ok := Locate(parseDiff(t, original), "a.go", SideRight, 3)
	require.True(t, ok)

	th := &Thread{File: "a.go", Side: SideRight, Line: 21, Anchor: anchor}
	reanchor(th, parseDiff(t, twice), time.Now())
	assert.False(t, th.Orphaned)
	assert.Equal(t, 22, th.Line, "should snap to the block nearer line 21")
}
