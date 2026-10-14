package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSinceIsANumberOfDays(t *testing.T) {
	tests := []struct {
		in   string
		want int
		bad  bool
	}{
		{in: "30d", want: 30},
		{in: "1d", want: 1},
		{in: "365d", want: 365},
		{in: "30", bad: true},
		{in: "0d", bad: true},
		{in: "-5d", bad: true},
		{in: "a week", bad: true},
		{in: "", bad: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseSince(tt.in)
			if tt.bad {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "number of days")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Reading every transcript takes a minute, so a backfill with nowhere to send
// the answer must refuse before it starts rather than after.
func TestBackfillWithNoDashboardRefusesBeforeItReadsAnything(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var out bytes.Buffer
	err := backfill(t.Context(), &out, 30)
	require.ErrorContains(t, err, "no dashboard is configured")
	assert.Empty(t, out.String(), "nothing was read, so nothing was said")
}
