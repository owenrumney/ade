package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/owenrumney/ade/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// networkMade reports the ade network being created before anything joined it.
func networkMade(rec *recorder) bool {
	for _, call := range rec.calls {
		command := call[len(call)-1]
		if hasArgs(command, "network", "create", networkName) {
			return true
		}
		if hasArgs(command, "run", workerName) {
			return false
		}
	}
	return false
}

// workerReplies answers the two calls adoption makes, so a test only has to
// say whether the container is running and what it printed.
func workerReplies(running bool, logs string, otherwise func([]string) ([]byte, []byte, error)) func([]string) ([]byte, []byte, error) {
	return func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		if strings.Contains(command, "'pull'") {
			return []byte(sentinel), nil, nil
		}
		if hasArgs(command, "image", "inspect") {
			return []byte(sentinel + "sha256:same\n"), nil, nil
		}
		if strings.Contains(command, "'inspect'") {
			if !running {
				return nil, []byte("no such container"), errors.New("exit status 1")
			}
			if strings.Contains(command, "Config.Env") {
				return []byte(sentinel + "GIT_CONFIG_KEY_0=safe.directory\n"), nil, nil
			}
			if strings.Contains(command, "Config.Labels") {
				return []byte(sentinel + "sha256:same\n"), nil, nil
			}
			return []byte(sentinel + "true\n"), nil, nil
		}
		if strings.Contains(command, "'logs'") {
			return []byte(sentinel + logs), nil, nil
		}
		if strings.Contains(command, "'curl'") {
			return []byte(sentinel + `{"ade":true,"root":"` + workerHome(fhsRoots) + `/.ade"}`), nil, nil
		}
		return otherwise(argv)
	}
}

func TestStartWorkerAdoptsARunningContainer(t *testing.T) {
	rec := &recorder{reply: workerReplies(true, "http://127.0.0.1:23456/?t=abc\n", func([]string) ([]byte, []byte, error) {
		return nil, nil, errors.New("nothing else should be run")
	})}
	host := testHost(t, rec)

	got, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{Token: "github-token"})
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:23456/?t=abc", got)
	joined := make([]string, 0, len(rec.calls))
	for _, call := range rec.calls {
		joined = append(joined, call[len(call)-1])
	}
	all := strings.Join(joined, "\n")
	pulled, read := -1, -1
	for i, command := range joined {
		if pulled < 0 && hasArgs(command, "pull") {
			pulled = i
		}
		if read < 0 && strings.Contains(command, "'logs'") {
			read = i
		}
	}
	require.GreaterOrEqual(t, pulled, 0, "the tag is refreshed")
	assert.Less(t, pulled, read, "and before anything is adopted")
	assert.NotContains(t, all, "github-token", "the registry credential never reaches an argv")
	assert.Contains(t, all, "logs")
	assert.Contains(t, all, "curl", "the published port is checked, not just the log line")
	assert.Contains(t, all, "Config.Env", "and that it trusts the checkouts it serves")
	assert.NotContains(t, all, "'run'", "a current worker is kept")
}

func TestAWorkerThatDistrustsItsCheckoutsIsReplaced(t *testing.T) {
	was := workerWait
	workerWait = 200 * time.Millisecond
	t.Cleanup(func() { workerWait = was })
	trusted := false
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		switch {
		case strings.Contains(command, "Config.Env"):
			if trusted {
				return []byte(sentinel + "GIT_CONFIG_KEY_0=safe.directory\n"), nil, nil
			}
			return []byte(sentinel + "HOME=/srv/ade/.worker\n"), nil, nil
		case strings.Contains(command, "'inspect'"):
			return []byte(sentinel + "true\n"), nil, nil
		case strings.Contains(command, "'logs'"):
			return []byte(sentinel + "http://127.0.0.1:23456/?t=abc\n"), nil, nil
		case strings.Contains(command, "'curl'"):
			return []byte(sentinel + `{"ade":true,"root":"` + workerHome(fhsRoots) + `/.ade"}`), nil, nil
		case strings.Contains(command, "'run'"):
			trusted = true
			assert.Contains(t, command, "GIT_CONFIG_VALUE_0=*")
		}
		return []byte(sentinel + "id\n"), nil, nil
	}}
	host := testHost(t, rec)
	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{Token: ""})
	require.NoError(t, err)
	assert.True(t, trusted, "the old worker was replaced by one that trusts the checkouts")
}

// An image older than this ADE ignores ADE_BIND and binds its own loopback;
// docker's proxy accepts and drops, surfacing as EOF from the port stream.
func TestStartWorkerRefusesAWorkerWhosePublishedPortNeverAnswers(t *testing.T) {
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		switch {
		case strings.Contains(command, "'inspect'"):
			return []byte(sentinel + "true\n"), nil, nil
		case strings.Contains(command, "'logs'"):
			return []byte(sentinel + "http://127.0.0.1:23456/?t=abc\n"), nil, nil
		case strings.Contains(command, "'curl'"):
			return nil, []byte("empty reply from server"), errors.New("exit status 52")
		}
		return []byte(sentinel + "container-id\n"), nil, nil
	}}
	host := testHost(t, rec)
	was := workerWait
	workerWait = 50 * time.Millisecond
	t.Cleanup(func() { workerWait = was })

	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{Token: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not reach it")
	assert.Contains(t, err.Error(), "already on 23456", "names the likeliest cause")
}

// A stopped worker keeps the logs it printed, so adopting on those alone
// forwards the page to a port nothing is listening on.
func TestStartWorkerReplacesAStoppedContainerRatherThanTrustingItsLogs(t *testing.T) {
	running := false
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		switch {
		case strings.Contains(command, "'inspect'"):
			if !running {
				return []byte(sentinel + "false\n"), nil, nil
			}
			return []byte(sentinel + "true\n"), nil, nil
		case strings.Contains(command, "'logs'"):
			return []byte(sentinel + "http://127.0.0.1:23456/?t=stale\n"), nil, nil
		case strings.Contains(command, "'curl'"):
			return []byte(sentinel + `{"ade":true,"root":"` + workerHome(fhsRoots) + `/.ade"}`), nil, nil
		case strings.Contains(command, "'run'"):
			running = true
		}
		return []byte(sentinel + "container-id\n"), nil, nil
	}}
	host := testHost(t, rec)

	got, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{Token: ""})
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:23456/?t=stale", got)
	joined := make([]string, 0, len(rec.calls))
	for _, call := range rec.calls {
		joined = append(joined, call[len(call)-1])
	}
	assert.Contains(t, strings.Join(joined, " "), "'run'", "the stopped container was replaced")
}

// The url is printed once, and a rotated log no longer holds it. Destroying the
// container then kills a session the laptop only wanted to reconnect to.
func TestStartWorkerKeepsARunningWorkerWhoseLogHasRotated(t *testing.T) {
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		switch {
		case strings.Contains(command, "Config.Env"):
			return []byte(sentinel + "GIT_CONFIG_KEY_0=safe.directory\n"), nil, nil
		case strings.Contains(command, "'inspect'"):
			return []byte(sentinel + "true\n"), nil, nil
		case strings.Contains(command, "'logs'"):
			return []byte(sentinel + "GET /api/state 200\nGET /api/diff 200\n"), nil, nil
		case strings.Contains(command, "'exec'"):
			return []byte(sentinel + `{"token":"live"}`), nil, nil
		case strings.Contains(command, "'curl'"):
			return []byte(sentinel + `{"ade":true,"root":"` + workerHome(fhsRoots) + `/.ade"}`), nil, nil
		case strings.Contains(command, "'run'"), strings.Contains(command, "'rm'"):
			t.Error("a running worker was replaced because its log had rotated")
		}
		return []byte(sentinel + "container-id\n"), nil, nil
	}}
	host := testHost(t, rec)

	got, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{Token: ""})
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:23456/?t=live", got)
}

func TestStartWorkerMountsTheStableWorkerPaths(t *testing.T) {
	running := false
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		switch {
		case strings.Contains(command, "'inspect'"):
			if !running {
				return nil, []byte("no such container"), errors.New("exit status 1")
			}
			return []byte(sentinel + "true\n"), nil, nil
		case strings.Contains(command, "'logs'"):
			return []byte(sentinel + "http://127.0.0.1:23456/?t=abc\n"), nil, nil
		case strings.Contains(command, "'curl'"):
			return []byte(sentinel + `{"ade":true,"root":"` + workerHome(fhsRoots) + `/.ade"}`), nil, nil
		case strings.Contains(command, "'run'"):
			running = true
		}
		return []byte(sentinel + "container-id\n"), nil, nil
	}}
	host := testHost(t, rec)

	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{Token: "github-token"})
	require.NoError(t, err)
	assert.True(t, networkMade(rec), "the network the worker joins is made before it starts")

	var run string
	var runAt int
	for i, call := range rec.calls {
		if command := call[len(call)-1]; strings.Contains(command, "'run'") {
			run, runAt = command, i
		}
	}
	require.NotEmpty(t, run, "the worker was started")
	assert.Contains(t, run, WorkerImage)
	// Published to loopback, wide inside: what works on Docker Desktop too.
	assert.Contains(t, run, "127.0.0.1:23456:23456")
	assert.Contains(t, run, "ADE_BIND=0.0.0.0")
	assert.NotContains(t, run, "ADE_HOST_GID", "a host that will not say its group leaves the record private")
	assert.NotContains(t, run, "--network host", "host networking is not required")
	assert.True(t, hasArgs(run, "--network", "ade"), "the worker, the hub and the agents share one network")
	assert.Contains(t, run, "ADE_HUB_CONTAINER=ade-hub")
	assert.Contains(t, run, "--port")
	assert.Contains(t, run, "23456")
	assert.Contains(t, run, "/srv/ade:/srv/ade")
	assert.Contains(t, run, "/run/ade:/run/ade")
	assert.Contains(t, run, "/var/run/docker.sock:/var/run/docker.sock")
	assert.Contains(t, run, "ADE_SCAN_DIR=/srv/ade")
	assert.Contains(t, run, "ADE_CREDENTIAL_ROOT=/run/ade/worker")
	assert.NotContains(t, run, "github-token", "the token never reaches an argv")
	assert.Equal(t, "GH_TOKEN=github-token\n", string(rec.input[runAt]))
}

// A worker told to scan the user's own checkouts must be able to see them.
func TestStartWorkerMountsAScanRootOutsideTheWorkRoot(t *testing.T) {
	roots := Roots{Work: "/Users/owen/.ade/srv", Scan: "/Users/owen/code", Run: "/Users/owen/.ade/run", Socket: "/x.sock"}
	running := false
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		switch {
		case strings.Contains(command, "'inspect'"):
			if !running {
				return nil, []byte("no such container"), errors.New("exit status 1")
			}
			return []byte(sentinel + "true\n"), nil, nil
		case strings.Contains(command, "'logs'"):
			return []byte(sentinel + "http://127.0.0.1:23456/?t=abc\n"), nil, nil
		case strings.Contains(command, "'curl'"):
			return []byte(sentinel + `{"ade":true,"root":"` + workerHome(roots) + `/.ade"}`), nil, nil
		case strings.Contains(command, "'run'"):
			running = true
		}
		return []byte(sentinel + "container-id\n"), nil, nil
	}}
	host := testHost(t, rec)

	_, err := host.StartWorker(context.Background(), roots, 23456, Login{})
	require.NoError(t, err)

	var run string
	for _, call := range rec.calls {
		if command := call[len(call)-1]; strings.Contains(command, "'run'") {
			run = command
		}
	}
	require.NotEmpty(t, run)
	assert.Contains(t, run, "/Users/owen/code:/Users/owen/code")
	assert.Contains(t, run, "ADE_SCAN_DIR=/Users/owen/code")
}

func TestStartWorkerDoesNotMountTheWorkRootTwice(t *testing.T) {
	running := false
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		switch {
		case strings.Contains(command, "'inspect'"):
			if !running {
				return nil, []byte("no such container"), errors.New("exit status 1")
			}
			return []byte(sentinel + "true\n"), nil, nil
		case strings.Contains(command, "'logs'"):
			return []byte(sentinel + "http://127.0.0.1:23456/?t=abc\n"), nil, nil
		case strings.Contains(command, "'curl'"):
			return []byte(sentinel + `{"ade":true,"root":"` + workerHome(fhsRoots) + `/.ade"}`), nil, nil
		case strings.Contains(command, "'run'"):
			running = true
		}
		return []byte(sentinel + "container-id\n"), nil, nil
	}}
	host := testHost(t, rec)

	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{Token: ""})
	require.NoError(t, err)

	var run string
	for _, call := range rec.calls {
		if command := call[len(call)-1]; strings.Contains(command, "'run'") {
			run = command
		}
	}
	assert.Equal(t, 1, strings.Count(run, "'/srv/ade:/srv/ade'"))
}

// The image is not published yet, so a worker that has never had it built
// locally fails at the pull. Say that, rather than docker's usage hint.
func TestStartWorkerSaysWhenTheImageIsNotThereToPull(t *testing.T) {
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		switch {
		case strings.Contains(command, "'inspect'"):
			return nil, []byte("Error: No such image"), errors.New("exit status 1")
		case strings.Contains(command, "'run'"):
			return nil, []byte("docker: Error response from daemon: Head \"https://ghcr.io/...\": denied"), errors.New("exit status 125")
		}
		return []byte(sentinel), nil, nil
	}}
	host := testHost(t, rec)

	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{Token: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no "+WorkerImage)
	assert.Contains(t, err.Error(), "build it there")
}

// A worker on another port answers healthily and is the wrong one: the forward
// is built from the port ADE asked for, so the page lands on a 403.
func TestStartWorkerWillNotAdoptAWorkerOnAnotherPort(t *testing.T) {
	replaced := false
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		switch {
		case strings.Contains(command, "'inspect'"):
			return []byte(sentinel + "true\n"), nil, nil
		case strings.Contains(command, "'logs'"):
			if replaced {
				return []byte(sentinel + "http://127.0.0.1:23456/?t=new\n"), nil, nil
			}
			return []byte(sentinel + "http://127.0.0.1:29281/?t=stale\n"), nil, nil
		case strings.Contains(command, "'curl'"):
			return []byte(sentinel + `{"ade":true,"root":"` + workerHome(fhsRoots) + `/.ade"}`), nil, nil
		case strings.Contains(command, "'run'"):
			replaced = true
		}
		return []byte(sentinel + "container-id\n"), nil, nil
	}}
	host := testHost(t, rec)

	got, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{Token: ""})
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:23456/?t=new", got, "the one on the wanted port")
	assert.True(t, replaced, "the worker on another port was replaced, not adopted")
}

// A native ade binds the same port and -p loses to it silently, so the probe
// checks whose state root answered rather than that something did.
func TestStartWorkerWillNotAdoptTheHostsOwnLauncher(t *testing.T) {
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		switch {
		case strings.Contains(command, "'inspect'"):
			return []byte(sentinel + "true\n"), nil, nil
		case strings.Contains(command, "'logs'"):
			return []byte(sentinel + "http://127.0.0.1:23456/?t=abc\n"), nil, nil
		case strings.Contains(command, "'curl'"):
			return []byte(sentinel + `{"ade":true,"root":"/Users/owen/.ade","pid":37376}`), nil, nil
		}
		return []byte(sentinel + "container-id\n"), nil, nil
	}}
	host := testHost(t, rec)
	was := workerWait
	workerWait = 50 * time.Millisecond
	t.Cleanup(func() { workerWait = was })

	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{Token: ""})
	require.Error(t, err, "the host's own launcher is not this worker")
	assert.Contains(t, err.Error(), "already on 23456")
}

func TestStopWorkerRemovesTheWorkerAndItsHub(t *testing.T) {
	rec := &recorder{reply: func([]string) ([]byte, []byte, error) { return []byte(sentinel), nil, nil }}
	host := testHost(t, rec)
	require.NoError(t, host.StopWorker(context.Background()))
	require.Len(t, rec.calls, 1)
	command := rec.calls[0][len(rec.calls[0])-1]
	for _, word := range []string{"rm", "-f", "ade-worker", "ade-hub"} {
		assert.Contains(t, command, word)
	}
}

// A pushed image is only a new release if the machine actually runs it; a
// worker adopted because it answers would keep last week's binary for ever.
func TestAWorkerFromAnOlderImageIsReplaced(t *testing.T) {
	was := workerWait
	workerWait = 200 * time.Millisecond
	t.Cleanup(func() { workerWait = was })
	started := false
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		switch {
		case hasArgs(command, "image", "inspect"):
			return []byte(sentinel + "sha256:new\n"), nil, nil
		case strings.Contains(command, "Config.Labels"):
			if started {
				return []byte(sentinel + "sha256:new\n"), nil, nil
			}
			return []byte(sentinel + "sha256:old\n"), nil, nil
		case strings.Contains(command, "Config.Env"):
			return []byte(sentinel + "GIT_CONFIG_KEY_0=safe.directory\n"), nil, nil
		case strings.Contains(command, "'inspect'"):
			return []byte(sentinel + "true\n"), nil, nil
		case strings.Contains(command, "'logs'"):
			return []byte(sentinel + "http://127.0.0.1:23456/?t=abc\n"), nil, nil
		case strings.Contains(command, "'curl'"):
			return []byte(sentinel + `{"ade":true,"root":"` + workerHome(fhsRoots) + `/.ade"}`), nil, nil
		case strings.Contains(command, "'run'"):
			started = true
		}
		return []byte(sentinel + "id\n"), nil, nil
	}}
	host := testHost(t, rec)
	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{Token: ""})
	require.NoError(t, err)
	assert.True(t, started, "the worker answering on its port was still replaced")
	stamped := false
	for _, call := range rec.calls {
		if command := call[len(call)-1]; hasArgs(command, "run") && strings.Contains(command, imageLabel+"=sha256:new") {
			stamped = true
		}
	}
	assert.True(t, stamped, "the new worker records the image it was started from")
}

func TestARefusedPullKeepsTheWorkerButSaysWhy(t *testing.T) {
	adopt := workerReplies(true, "http://127.0.0.1:23456/?t=abc\n", func([]string) ([]byte, []byte, error) {
		return nil, nil, errors.New("nothing else should be run")
	})
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		if strings.Contains(argv[len(argv)-1], "'pull'") {
			return nil, []byte("Error response from daemon: error from registry: denied"), errors.New("exit status 1")
		}
		return adopt(argv)
	}}
	host := testHost(t, rec)

	got, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{Token: "github-token"})
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:23456/?t=abc", got, "the worker it has still serves")
	require.Error(t, host.Stale())
	assert.Contains(t, host.Stale().Error(), "read:packages")
}

func TestRefreshImagesReportsAWorkerBehindTheImageItPulled(t *testing.T) {
	stamps := map[string]string{"image": "sha256:new", "container": "sha256:old"}
	reply := func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		switch {
		case strings.Contains(command, "'pull'"):
			return []byte(sentinel), nil, nil
		case hasArgs(command, "image", "inspect"):
			return []byte(sentinel + stamps["image"] + "\n"), nil, nil
		case strings.Contains(command, "Config.Labels"):
			return []byte(sentinel + stamps["container"] + "\n"), nil, nil
		}
		return nil, nil, errors.New("nothing else should be run")
	}
	host := testHost(t, &recorder{reply: reply})

	behind, err := host.RefreshImages(context.Background(), fhsRoots, Login{Token: "github-token"})
	require.NoError(t, err)
	assert.True(t, behind)

	stamps["container"] = "sha256:new"
	behind, err = host.RefreshImages(context.Background(), fhsRoots, Login{Token: "github-token"})
	require.NoError(t, err)
	assert.False(t, behind, "a worker on the pulled image is not restarted")
}

func TestRefreshImagesFailsOnARefusedPull(t *testing.T) {
	host := testHost(t, &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		if strings.Contains(argv[len(argv)-1], "'pull'") {
			return nil, []byte("Error response from daemon: error from registry: denied"), errors.New("exit status 1")
		}
		return []byte(sentinel), nil, nil
	}})

	_, err := host.RefreshImages(context.Background(), fhsRoots, Login{Token: "github-token"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read:packages")
	assert.Error(t, host.Stale())
}

// A name docker never frees is a container that will not die, and the fix is
// on that machine. Saying which command beats reporting docker's own sentence.
func TestANameDockerWillNotFreeSaysWhatToRunOnTheMachine(t *testing.T) {
	was, wasName := workerWait, nameWait
	workerWait, nameWait = 100*time.Millisecond, 200*time.Millisecond
	t.Cleanup(func() { workerWait, nameWait = was, wasName })

	rec := &recorder{reply: workerReplies(false, "", func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		if hasArgs(command, "container", "inspect") {
			return []byte(sentinel + "ac12011b34b9\n"), nil, nil
		}
		if hasArgs(command, "run") {
			return nil, []byte(conflictSaid), errors.New("exit status 125")
		}
		return []byte(sentinel + "container-id\n"), nil, nil
	})}
	host := testHost(t, rec)

	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "docker rm -f ade-worker")
	assert.Contains(t, err.Error(), "restart its docker daemon")
}

// docker's rm returns when the kill is sent, not when the name is released.
// On a slow machine the run that follows loses the race to its own cleanup.
func TestAWorkerRetriesTheRunThatLostToItsOwnCleanup(t *testing.T) {
	was, wasName := workerWait, nameWait
	workerWait, nameWait = 200*time.Millisecond, time.Second
	t.Cleanup(func() { workerWait, nameWait = was, wasName })

	runs := 0
	rec := &recorder{reply: workerReplies(false, "", func(argv []string) ([]byte, []byte, error) {
		if hasArgs(argv[len(argv)-1], "run") {
			runs++
			if runs == 1 {
				return nil, []byte(conflictSaid), errors.New("exit status 125")
			}
		}
		return []byte(sentinel + "container-id\n"), nil, nil
	})}
	host := testHost(t, rec)

	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{})
	require.Error(t, err, "nothing answers the port in this test")
	assert.NotContains(t, err.Error(), "already in use", "the conflict was retried, not reported")
	assert.Equal(t, 2, runs)
}

func TestTheHostsGroupIsANumberOrNothing(t *testing.T) {
	tests := []struct {
		name string
		said string
		want string
	}{
		{"the ssh user's group", "1000\n", "1000"},
		{"something that is not a group", "pi\n", ""},
		{"nothing at all", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{reply: func([]string) ([]byte, []byte, error) {
				return []byte(sentinel + tt.said), nil, nil
			}}
			assert.Equal(t, tt.want, testHost(t, rec).group(context.Background()))
		})
	}
}

// freshWorker answers everything a replacement run asks, with the ids and the
// socket group a Linux worker reports.
func freshWorker(t *testing.T, group string, groups string) *recorder {
	t.Helper()
	running := false
	return &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		switch {
		case strings.Contains(command, "'inspect'"):
			if !running {
				return nil, []byte("no such container"), errors.New("exit status 1")
			}
			return []byte(sentinel + "true\n"), nil, nil
		case strings.Contains(command, "'logs'"):
			return []byte(sentinel + "http://127.0.0.1:23456/?t=abc\n"), nil, nil
		case strings.Contains(command, "'curl'"):
			return []byte(sentinel + `{"ade":true,"root":"` + workerHome(fhsRoots) + `/.ade"}`), nil, nil
		case hasArgs(command, "id", "-u"):
			return []byte(sentinel + "1000\n"), nil, nil
		case hasArgs(command, "id", "-g"):
			return []byte(sentinel + "1000\n"), nil, nil
		case hasArgs(command, "id", "-G"):
			return []byte(sentinel + groups + "\n"), nil, nil
		case strings.Contains(command, "stat -c %g"):
			return []byte(sentinel + group + "\n"), nil, nil
		case strings.Contains(command, "stat -c %u"):
			return []byte(sentinel + "1000\n"), nil, nil
		case hasArgs(command, "run", workerName):
			running = true
		}
		return []byte(sentinel + "container-id\n"), nil, nil
	}}
}

func ranWith(rec *recorder) string {
	for _, call := range rec.calls {
		command := call[len(call)-1]
		if hasArgs(command, "run", workerName) {
			return command
		}
	}
	return ""
}

// Root in the worker is the whole of the ownership problem: every directory it
// makes for the agent, and every file it writes into one, is root's.
func TestTheWorkerRunsAsTheSshUser(t *testing.T) {
	rec := freshWorker(t, "973", "1000 27 973")
	host := testHost(t, rec)

	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{})
	require.NoError(t, err)

	run := ranWith(rec)
	assert.True(t, hasArgs(run, "--user", "1000:1000"))
	assert.True(t, hasArgs(run, "--group-add", "973"), "or it cannot reach the daemon it drives")
}

// ssh-keygen calls getpwuid() and dies without an entry, so an unnamed id
// fails every signed commit with "No user exists for uid".
func TestTheIdTheWorkerRunsAsIsGivenAName(t *testing.T) {
	rec := freshWorker(t, "973", "1000 27 973")
	host := testHost(t, rec)

	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{})
	require.NoError(t, err)

	named := namedTheUser(rec)
	require.NotEmpty(t, named, "the uid gets a passwd entry")
	assert.True(t, hasArgs(named, "-u", "0:0"), "the unnamed id cannot write /etc itself")
	assert.Contains(t, named, "/etc/passwd")
	assert.Contains(t, named, "/etc/group", "ssh wants the gid resolved too")
	assert.True(t, hasArgs(named, "1000", "1000", workerHome(fhsRoots)),
		"and its home is the HOME the worker was given")
}

func TestARootWorkerNeedsNoPasswdEntry(t *testing.T) {
	rec := freshWorker(t, "973", "1000 27")
	host := testHost(t, rec)

	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{})
	require.NoError(t, err)
	assert.Empty(t, namedTheUser(rec), "root is in every image's passwd file")
}

// namedTheUser is the exec that added the passwd entry, or empty.
func namedTheUser(rec *recorder) string {
	for _, call := range rec.calls {
		command := call[len(call)-1]
		if hasArgs(command, "exec") && strings.Contains(command, "/etc/passwd") {
			return command
		}
	}
	return ""
}

func TestTheWorkerStaysRootWhenItCouldNotReachTheDaemon(t *testing.T) {
	// No worker at all is worse than a worker whose files need repairing.
	rec := freshWorker(t, "973", "1000 27")
	host := testHost(t, rec)

	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{})
	require.NoError(t, err)

	run := ranWith(rec)
	assert.NotContains(t, run, "--user")
	assert.Contains(t, run, session.HostUserEnv, "so the repairs still have ids to work from")
}

func TestTheWorkerStaysRootWhenTheHostWillNotSayWhoItIs(t *testing.T) {
	rec := freshWorker(t, "", "1000 973")
	host := testHost(t, rec)

	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{})
	require.NoError(t, err)

	assert.NotContains(t, ranWith(rec), "--user")
}

func TestTheRootEraIsHandedOverBeforeTheWorkerStarts(t *testing.T) {
	// A session state directory root wrote is one the ssh user cannot write,
	// so a migration has to run before anything tries.
	rec := freshWorker(t, "973", "1000 973")
	host := testHost(t, rec)

	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{})
	require.NoError(t, err)

	chowned, started := -1, -1
	for i, call := range rec.calls {
		command := call[len(call)-1]
		if chowned < 0 && strings.Contains(command, "chown") {
			chowned = i
		}
		if started < 0 && hasArgs(command, "run", workerName) {
			started = i
		}
	}
	require.GreaterOrEqual(t, chowned, 0, "the root era is migrated")
	assert.Less(t, chowned, started, "and before the worker runs as someone else")
	all := rec.calls[chowned][len(rec.calls[chowned])-1]
	assert.Contains(t, all, "1000:1000")
	assert.Contains(t, all, "/.git/ade", "each session's state directory too")
	// Root in a container is the one id that can chown what root left.
	assert.True(t, hasArgs(all, "docker", "run", "--rm", "--user", "0:0"), "the chown runs as root in a container")
	assert.True(t, hasArgs(all, WorkerImage), "in an image this host already pulled")
	assert.True(t, hasArgs(all, fhsRoots.Run+":"+fhsRoots.Run), "over the state root")
	assert.True(t, hasArgs(all, fhsRoots.Work+":"+fhsRoots.Work), "and the work root")
	assert.NotContains(t, all, "|| true", "a chown that failed is not swallowed")
}

func TestARootOwnedStateRootIsNotAdopted(t *testing.T) {
	was := workerWait
	workerWait = 200 * time.Millisecond
	t.Cleanup(func() { workerWait = was })
	rec := &recorder{reply: workerReplies(true, "http://127.0.0.1:23456/?t=abc\n", func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		switch {
		case strings.Contains(command, "stat -c %u"):
			return []byte(sentinel + "0\n"), nil, nil
		case strings.Contains(command, "stat -c %g"):
			return []byte(sentinel + "973\n"), nil, nil
		case hasArgs(command, "id", "-u"), hasArgs(command, "id", "-g"):
			return []byte(sentinel + "1000\n"), nil, nil
		case hasArgs(command, "id", "-G"):
			return []byte(sentinel + "1000 973\n"), nil, nil
		}
		return []byte(sentinel + "container-id\n"), nil, nil
	})}
	host := testHost(t, rec)

	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{})
	require.NoError(t, err)

	assert.NotEmpty(t, ranWith(rec), "a worker over root's files is replaced, not kept")
}

// The worker stays root on this host, so root owning its state is not a fault.
func TestARootWorkerKeepsItsRootOwnedHome(t *testing.T) {
	rec := &recorder{reply: workerReplies(true, "http://127.0.0.1:23456/?t=abc\n", func(argv []string) ([]byte, []byte, error) {
		command := argv[len(argv)-1]
		switch {
		case strings.Contains(command, "stat -c %u"):
			return []byte(sentinel + "0\n"), nil, nil
		case strings.Contains(command, "stat -c %g"):
			return []byte(sentinel + "973\n"), nil, nil
		case hasArgs(command, "id", "-u"), hasArgs(command, "id", "-g"):
			return []byte(sentinel + "1000\n"), nil, nil
		case hasArgs(command, "id", "-G"):
			// Not in 973: no socket, so the worker stays root.
			return []byte(sentinel + "1000\n"), nil, nil
		}
		return []byte(sentinel + "container-id\n"), nil, nil
	})}
	host := testHost(t, rec)

	got, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{})
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:23456/?t=abc", got)
	assert.Empty(t, ranWith(rec), "the serving worker is adopted, not replaced")
}

// A failed migration leaves root's files unreadable: the switch has to give.
func TestAFailedHandoverKeepsTheWorkerRoot(t *testing.T) {
	rec := freshWorker(t, "973", "1000 973")
	was := rec.reply
	rec.reply = func(argv []string) ([]byte, []byte, error) {
		if strings.Contains(argv[len(argv)-1], "chown") {
			return nil, []byte("chown: operation not permitted"), errors.New("exit status 1")
		}
		return was(argv)
	}
	host := testHost(t, rec)

	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{})
	require.NoError(t, err)

	assert.NotContains(t, ranWith(rec), "--user", "a worker that cannot read its state is not started on it")
}

// A hand-built image is the only way a fix reaches a host before CI publishes
// one, and a pull on the next connect would put the published one back.
func TestAShippedWorkerImageIsNotPulledOver(t *testing.T) {
	was := WorkerImage
	WorkerImage = "ade-worker:shipped"
	t.Cleanup(func() { WorkerImage = was })
	rec := freshWorker(t, "973", "1000 973")
	host := testHost(t, rec)

	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{Token: "t"})
	require.NoError(t, err)

	for _, call := range rec.calls {
		command := call[len(call)-1]
		assert.False(t, hasArgs(command, "pull"), "the shipped image would be replaced")
	}
	assert.Contains(t, ranWith(rec), "ade-worker:shipped")
}

func TestThePublishedWorkerImageIsStillPulled(t *testing.T) {
	rec := freshWorker(t, "973", "1000 973")
	host := testHost(t, rec)

	_, err := host.StartWorker(context.Background(), fhsRoots, 23456, Login{Token: "t"})
	require.NoError(t, err)

	pulled := false
	for _, call := range rec.calls {
		if hasArgs(call[len(call)-1], "pull") {
			pulled = true
		}
	}
	assert.True(t, pulled, "a published image goes stale without one")
}

func TestRefreshingAShippedImageAsksNoRegistry(t *testing.T) {
	was := WorkerImage
	WorkerImage = "ade-worker:shipped"
	t.Cleanup(func() { WorkerImage = was })
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		assert.False(t, hasArgs(argv[len(argv)-1], "pull"), "nothing to pull")
		return []byte(sentinel + "sha256:same\n"), nil, nil
	}}
	host := testHost(t, rec)

	_, err := host.RefreshImages(context.Background(), fhsRoots, Login{Token: "t"})
	require.NoError(t, err)
	assert.NoError(t, host.stale)
}

// The script and the override have to agree on the tag, or ade pulls the
// published image over the one just carried across.
func TestTheShipScriptTagsWhatTheOverrideExpects(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("../..", "tools", "worker-ship.sh"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "${"+WorkerImageEnv+":-ade-worker:shipped}")
	assert.NotEqual(t, defaultWorkerImage, "ade-worker:shipped",
		"a shipped image must not wear the published tag")
}
