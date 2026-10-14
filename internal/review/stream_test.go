package review

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func block(body string) string {
	return "```" + streamFence + "\n" + body + "\n```"
}

func TestStreamerEmitsEachBlockOnce(t *testing.T) {
	s := NewStreamer()

	first := s.Push("Looking at the cart.\n" + block(`{"file":"a.go","line":3,"summary":"off by one"}`))
	require.Len(t, first, 1)
	assert.Equal(t, "a.go", first[0].File)
	assert.Equal(t, 3, first[0].Line)

	// The same buffer is rescanned, so the first block must not come back.
	second := s.Push("\nAnd now\n" + block(`{"file":"b.go","line":9,"summary":"nil deref"}`))
	require.Len(t, second, 1)
	assert.Equal(t, "b.go", second[0].File)

	assert.Empty(t, s.Push("nothing structured here"))
}

func TestStreamerEdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		chunks []string
		want   []string
	}{
		{
			name:   "two blocks in one chunk",
			chunks: []string{block(`{"file":"a.go","line":1,"summary":"x"}`) + "\n" + block(`{"file":"b.go","line":2,"summary":"y"}`)},
			want:   []string{"a.go", "b.go"},
		},
		{
			name:   "block split across chunks",
			chunks: []string{"```" + streamFence + "\n{\"file\":\"a.go\",", "\"line\":4,\"summary\":\"x\"}\n```"},
			want:   []string{"a.go"},
		},
		{
			name:   "repeated block is suppressed",
			chunks: []string{block(`{"file":"a.go","line":1,"summary":"x"}`), block(`{"file":"a.go","line":1,"summary":"x"}`)},
			want:   []string{"a.go"},
		},
		{
			name:   "malformed json is ignored",
			chunks: []string{block(`{"file":"a.go",`), block(`{"file":"b.go","line":2,"summary":"y"}`)},
			want:   []string{"b.go"},
		},
		{
			name:   "unanchored finding is dropped",
			chunks: []string{block(`{"file":"","line":2,"summary":"y"}`), block(`{"file":"a.go","line":0,"summary":"y"}`)},
			want:   nil,
		},
		{
			name:   "plain json fence is not a stream block",
			chunks: []string{"```json\n[{\"file\":\"a.go\",\"line\":1,\"summary\":\"x\"}]\n```"},
			want:   nil,
		},
		{
			name:   "empty chunk is a no-op",
			chunks: []string{""},
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStreamer()
			var got []string
			for _, chunk := range tt.chunks {
				for _, f := range s.Push(chunk) {
					got = append(got, f.File)
				}
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStreamerHasTracksWhatWasSeen(t *testing.T) {
	s := NewStreamer()
	f := Finding{File: "a.go", Line: 3, Summary: "off by one"}
	assert.False(t, s.Has(f))

	require.Len(t, s.Push(block(`{"file":"a.go","line":3,"summary":"off by one"}`)), 1)
	assert.True(t, s.Has(f))

	// A different summary on the same line is a different finding.
	assert.False(t, s.Has(Finding{File: "a.go", Line: 3, Summary: "something else"}))
}

func TestKeyIgnoresSurroundingSpace(t *testing.T) {
	a := Finding{File: " a.go ", Line: 3, Summary: " x "}
	b := Finding{File: "a.go", Line: 3, Summary: "x"}
	assert.Equal(t, Key(b), Key(a))
}

func TestStripStreamLeavesTheProse(t *testing.T) {
	text := "Two points.\n\n" + block(`{"file":"a.go","line":3,"summary":"off by one"}`) + "\nand\n" + block(`{"file":"b.go","line":9,"summary":"nil"}`) + "\nFiled 2."
	assert.Equal(t, "Two points.\n\n\nand\n\nFiled 2.", StripStream(text))
	assert.Equal(t, "", StripStream(block(`{"file":"a.go","line":3,"summary":"x"}`)))
	assert.Equal(t, "no blocks", StripStream("  no blocks\n"))
}
