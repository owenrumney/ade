package remote

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What docker says when the daemon has not finished releasing a name.
const conflictSaid = `docker: Error response from daemon: Conflict. The container name "/ade-worker" ` +
	`is already in use by container "ac12011b34b941d1e87a9da52fcf70cdaf5de9d65b2b251dabcc3c7df825730e".`

func TestOnlyAConflictOverANameIsRetried(t *testing.T) {
	assert.True(t, nameInUse(errors.New(conflictSaid)))
	assert.False(t, nameInUse(nil))
	assert.False(t, nameInUse(errors.New("docker: no such image")), "a missing image is not a race")
}

func TestFreeNameWaitsForTheRemovalToFinish(t *testing.T) {
	was := nameWait
	nameWait = 2 * time.Second
	t.Cleanup(func() { nameWait = was })

	left := 3
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		if hasArgs(argv[len(argv)-1], "container", "inspect") {
			left--
			if left > 0 {
				return []byte(sentinel + "ac12011b34b9\n"), nil, nil
			}
			return nil, []byte("Error: No such container: ade-worker"), errors.New("exit status 1")
		}
		return []byte(sentinel), nil, nil
	}}

	require.True(t, testHost(t, rec).freeName(context.Background(), workerName))
	assert.Equal(t, 0, left, "it asked again rather than trusting the first answer")
}

// A name still held when the wait runs out is a container that will not die.
// Returning false is what turns that into an error naming the machine's fix.
func TestFreeNameGivesUpOnAContainerThatWillNotDie(t *testing.T) {
	was := nameWait
	nameWait = 100 * time.Millisecond
	t.Cleanup(func() { nameWait = was })

	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		if hasArgs(argv[len(argv)-1], "container", "inspect") {
			return []byte(sentinel + "ac12011b34b9\n"), nil, nil
		}
		return []byte(sentinel), nil, nil
	}}

	assert.False(t, testHost(t, rec).freeName(context.Background(), workerName))
}

// It asks for the removal again before waiting: the first rm may have failed
// outright, and polling a name nobody is removing only burns the deadline.
func TestFreeNameAsksForTheRemovalAgain(t *testing.T) {
	was := nameWait
	nameWait = 100 * time.Millisecond
	t.Cleanup(func() { nameWait = was })

	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		if hasArgs(argv[len(argv)-1], "container", "inspect") {
			return nil, []byte("Error: No such container: ade-worker"), errors.New("exit status 1")
		}
		return []byte(sentinel), nil, nil
	}}
	host := testHost(t, rec)
	require.True(t, host.freeName(context.Background(), workerName))

	assert.True(t, hasArgs(rec.calls[0][len(rec.calls[0])-1], "rm", "-f", workerName))
}

// An ssh that could not reach the machine is not a container that has gone.
// Reading one as the other re-runs the thing that just lost the name race.
func TestFreeNameDoesNotReadAFailedCommandAsAFreedName(t *testing.T) {
	was := nameWait
	nameWait = 100 * time.Millisecond
	t.Cleanup(func() { nameWait = was })

	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		if hasArgs(argv[len(argv)-1], "container", "inspect") {
			return nil, []byte("ssh: connect to host pi5.local port 22: No route to host"), errors.New("exit status 255")
		}
		return []byte(sentinel), nil, nil
	}}

	assert.False(t, testHost(t, rec).freeName(context.Background(), workerName))
}

func TestGoneIsDockersOwnSentenceAndNothingElse(t *testing.T) {
	assert.True(t, gone(errors.New("Error: No such container: ade-worker")))
	assert.True(t, gone(errors.New("error: no such object: ade-worker")))
	assert.False(t, gone(nil))
	assert.False(t, gone(errors.New("ssh: no route to host")))
	assert.False(t, gone(errors.New("permission denied")))
}
