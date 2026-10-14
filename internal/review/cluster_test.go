package review

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseGroups(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want [][]string
	}{
		{
			name: "a group of two",
			in:   "```ade-cluster\n{\"same\": [\"F1\", \"F3\"]}\n```",
			want: [][]string{{"F1", "F3"}},
		},
		{
			name: "several, in order, around prose",
			in: "these two agree\n```ade-cluster\n{\"same\":[\"F1\",\"F2\"]}\n```\nand\n" +
				"```ade-cluster\n{\"same\":[\"F4\",\"F5\",\"F6\"]}\n```",
			want: [][]string{{"F1", "F2"}, {"F4", "F5", "F6"}},
		},
		{
			name: "refs are upper cased and trimmed",
			in:   "```ade-cluster\n{\"same\": [\" f1 \", \"f2\"]}\n```",
			want: [][]string{{"F1", "F2"}},
		},
		{
			name: "a group of one is not a group",
			in:   "```ade-cluster\n{\"same\": [\"F1\"]}\n```",
		},
		{
			name: "a ref repeated does not make a pair of itself",
			in:   "```ade-cluster\n{\"same\": [\"F1\", \"F1\"]}\n```",
		},
		{
			name: "a block that will not parse is dropped, not guessed at",
			in:   "```ade-cluster\nF1 and F2 are the same\n```",
		},
		{
			name: "a verdict block is not a cluster block",
			in:   "```ade-verdict\n{\"ref\":\"F1\",\"verdict\":\"agree\"}\n```",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got [][]string
			for _, g := range ParseGroups(tc.in) {
				got = append(got, g.Same)
			}
			assert.Equal(t, tc.want, got)
		})
	}
}
