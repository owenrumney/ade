package remote

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheLaptopsModelListLandsInTheWorkersState(t *testing.T) {
	rec := &recorder{reply: func([]string) ([]byte, []byte, error) { return []byte(sentinel), nil, nil }}
	host := testHost(t, rec)

	catalogue := []byte(`{"models":[{"id":"claude-opus-5-5","agent":"claude"}]}`)
	require.NoError(t, host.PrepareModels(context.Background(), catalogue))

	require.Len(t, rec.calls, 1)
	command := rec.calls[0][len(rec.calls[0])-1]
	assert.True(t, hasArgs(command, "docker", "exec", "-i", workerName))
	assert.Contains(t, command, "$HOME/.ade/models.json")
	assert.Equal(t, [][]byte{catalogue}, rec.input, "the list goes over stdin, not argv")
}

func TestAnUnsyncedLaptopSendsNothing(t *testing.T) {
	rec := &recorder{reply: func([]string) ([]byte, []byte, error) { return []byte(sentinel), nil, nil }}

	require.NoError(t, testHost(t, rec).PrepareModels(context.Background(), nil))
	assert.Empty(t, rec.calls, "an empty file would leave the worker with no list at all")
}
