package transcripts

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIndexFoldHandlesNonASCII(t *testing.T) {
	tests := []struct {
		name      string
		text, sub string
		want      int
	}{
		{"ascii", "hello world", "WORLD", 6},
		{"accented lower query", "the CAFÉ closed", "café", 4},
		{"accented upper query", "the café closed", "CAFÉ", 4},
		{"german", "STRASSE und GRÜN", "grün", 12},
		{"cyrillic", "приложение", "ПРИЛОЖ", 0},
		{"turkish dotless", "IZMIR", "izmir", 0},
		{"no match", "hello", "zzz", -1},
		{"empty sub", "hello", "", 0},
		{"offset is a byte index into text", "aé needle", "NEEDLE", 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, indexFold(tc.text, tc.sub))
		})
	}
}

func TestSearchFindsNonASCIIQuery(t *testing.T) {
	home := fakeHome(t)
	writeFixture(t, filepath.Join(home, ".claude", "projects", "-work-uni", "uni-0001.jsonl"),
		`{"type":"user","timestamp":"2026-08-01T10:00:00Z","cwd":"/work/uni","message":{"role":"user","content":"the CAFÉ needs a résumé of GRÜN"}}`+"\n")

	for _, query := range []string{"café", "CAFÉ", "résumé", "RÉSUMÉ", "grün"} {
		t.Run(query, func(t *testing.T) {
			hits := Search(Scan(), query)
			require.Len(t, hits, 1, "expected to find %q", query)
			require.NotEmpty(t, hits[0].Excerpts)
		})
	}
}
