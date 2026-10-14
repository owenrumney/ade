package sessions

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCreatedAtReadsTheWorktreeAndTheAddress(t *testing.T) {
	tree := t.TempDir()
	doc := filepath.Join(tree, "docs", "cancel-button.md")

	tests := map[string]struct {
		out       string
		url, path string
	}{
		"made now": {
			out:  doc + "\n" + tree + "\ncarried  .env\nhttp://127.0.0.1:27431/?t=tok\n",
			url:  "http://127.0.0.1:27431/?t=tok",
			path: tree,
		},
		"come back to": {
			out:  doc + "  (existing)\n" + tree + "  (existing)\nhttp://127.0.0.1:27431/?t=tok\n",
			url:  "http://127.0.0.1:27431/?t=tok",
			path: tree,
		},
		"no worktree said": {
			out: doc + "\nhttp://127.0.0.1:27431/?t=tok\n",
			url: "http://127.0.0.1:27431/?t=tok",
		},
		"nothing": {},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			url, path := createdAt(tc.out)
			assert.Equal(t, tc.url, url)
			assert.Equal(t, tc.path, path)
		})
	}
}
