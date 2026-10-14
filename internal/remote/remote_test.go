package remote

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recorder stands in for ssh and keeps every argv it was handed.
type recorder struct {
	calls [][]string
	input [][]byte
	// reply decides what each call returns, by the argv it was given: what the
	// command printed, and what ssh or the far side's shell said about it.
	reply func(argv []string) (stdout []byte, stderr []byte, err error)
}

func (r *recorder) run(_ context.Context, argv []string, stdin []byte) ([]byte, []byte, error) {
	r.calls = append(r.calls, argv)
	r.input = append(r.input, append([]byte(nil), stdin...))
	if r.reply == nil {
		return nil, nil, nil
	}
	return r.reply(argv)
}

// says is a reply of just the command's own output, behind the fence a real
// one arrives behind.
func says(out string) func([]string) ([]byte, []byte, error) {
	return func([]string) ([]byte, []byte, error) { return []byte(sentinel + out), nil, nil }
}

func testHost(t *testing.T, rec *recorder) *Host {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	host, err := New("pc")
	require.NoError(t, err)
	host.ssh = rec.run
	return host
}

func TestNewRejectsAHostSshWouldMisread(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tests := []struct {
		name string
		host string
	}{
		{"empty", ""},
		{"blank", "   "},
		{"an option", "-oProxyCommand=touch /tmp/x"},
		{"a path separator", "pc/../../etc"},
		{"a space", "pc rm -rf"},
		{"a shell metacharacter", "pc;reboot"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.host)
			assert.Error(t, err)
		})
	}

	for _, ok := range []string{"pc", "owen@pc", "pc.local", "build-box_2"} {
		t.Run("accepts "+ok, func(t *testing.T) {
			host, err := New(ok)
			require.NoError(t, err)
			assert.Equal(t, ok, host.Name)
		})
	}
}

func TestDialOpensAMasterAndChecksIt(t *testing.T) {
	// The check has to fail first, or Dial would adopt a connection that is
	// not there and never open one.
	opened := false
	rec := &recorder{}
	rec.reply = func(argv []string) ([]byte, []byte, error) {
		if argv[2] == "-O" && argv[3] == "check" {
			if !opened {
				return nil, nil, errors.New("no control path")
			}
			return nil, nil, nil
		}
		opened = true
		return nil, nil, nil
	}
	host := testHost(t, rec)

	require.NoError(t, host.Dial(context.Background()))
	require.Len(t, rec.calls, 3)

	assert.Equal(t, []string{"-S", host.Socket(), "-O", "check", "pc"}, rec.calls[0])
	assert.Equal(t, []string{
		"-M", "-S", host.Socket(), "-N", "-f",
		"-o", "ControlPersist=60",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"pc",
	}, rec.calls[1])
	assert.Equal(t, []string{"-S", host.Socket(), "-O", "check", "pc"}, rec.calls[2])
}

func TestDialAdoptsAMasterAlreadyRunning(t *testing.T) {
	rec := &recorder{}
	host := testHost(t, rec)

	require.NoError(t, host.Dial(context.Background()))
	assert.Len(t, rec.calls, 1, "an adopted connection is one check and nothing else")
}

func TestDialReportsWhatSshSaid(t *testing.T) {
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		if argv[0] == "-M" {
			return nil, []byte("banner\npc: Permission denied (publickey).\n"), errors.New("exit status 255")
		}
		return nil, nil, errors.New("no control path")
	}}
	host := testHost(t, rec)

	err := host.Dial(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Permission denied (publickey)")
	assert.NotContains(t, err.Error(), "banner", "the last line is the useful one")
}

func TestRunSendsArgvOverTheMaster(t *testing.T) {
	rec := &recorder{reply: says("http://127.0.0.1:20345/?t=abc\n")}
	host := testHost(t, rec)

	out, err := host.Run(context.Background(), "ade", "launch", "--no-open")
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:20345/?t=abc\n", string(out))

	// A login shell reads PATH setup without needing a terminal.
	assert.Equal(t,
		[]string{"-S", host.Socket(), "pc", "--",
			`exec "${SHELL:-/bin/sh}" -lc 'printf %s '\''__ade_out__'\''; '\''ade'\'' '\''launch'\'' '\''--no-open'\'''`},
		rec.calls[0])
}

func TestRunQuotesAnArgumentTheShellWouldEat(t *testing.T) {
	rec := &recorder{}
	host := testHost(t, rec)

	_, err := host.Run(context.Background(), "ade", "--host", "a b; rm -rf /")
	require.NoError(t, err)

	// The whole thing is one shell word per argument, so a space or a
	// semicolon in one cannot start a second command.
	sent := rec.calls[0][len(rec.calls[0])-1]
	assert.Contains(t, sent, `'a b; rm -rf /'`)
	assert.True(t, strings.HasPrefix(sent, `exec "${SHELL:-/bin/sh}" -lc `), sent)
}

func TestOutputIsFencedFromWhateverTheProfilePrints(t *testing.T) {
	// A login profile may print banners or escape codes before our command.
	rec := &recorder{reply: func([]string) ([]byte, []byte, error) {
		return []byte("Welcome to bigboy\n\x1b[?2004h" + sentinel + "ade version 1.2.3\n"), nil, nil
	}}
	host := testHost(t, rec)

	out, err := host.Run(context.Background(), "ade", "--version")
	require.NoError(t, err)
	assert.Equal(t, "ade version 1.2.3\n", string(out))
}

func TestCloseExitsTheMaster(t *testing.T) {
	rec := &recorder{}
	host := testHost(t, rec)

	require.NoError(t, host.Close())
	assert.Equal(t, []string{"-S", host.Socket(), "-O", "exit", "pc"}, rec.calls[1])
}

func TestCloseIsHappyWithAConnectionAlreadyGone(t *testing.T) {
	rec := &recorder{reply: func([]string) ([]byte, []byte, error) { return nil, nil, errors.New("no control path") }}
	host := testHost(t, rec)

	require.NoError(t, host.Close())
	assert.Len(t, rec.calls, 1, "nothing to exit, so nothing is asked to")
}

func TestCheckRequiresDockerNotADE(t *testing.T) {
	rec := &recorder{reply: says("27.5.1\n")}
	host := testHost(t, rec)

	require.NoError(t, Check(context.Background(), host, fhsRoots))
	require.Len(t, rec.calls, 5)
	assert.Contains(t, rec.calls[0][len(rec.calls[0])-1], "docker")
	assert.Contains(t, rec.calls[0][len(rec.calls[0])-1], "{{.Server.Version}}")
	assert.Contains(t, rec.calls[1][len(rec.calls[1])-1], "/srv/ade")
	assert.Contains(t, rec.calls[3][len(rec.calls[3])-1], "/run/ade")
}

func TestCheckNamesAnUnavailableDockerDaemon(t *testing.T) {
	rec := &recorder{reply: func([]string) ([]byte, []byte, error) {
		return nil, []byte("Cannot connect to the Docker daemon\n"), errors.New("exit status 1")
	}}
	host := testHost(t, rec)

	err := Check(context.Background(), host, fhsRoots)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "docker is not usable on pc")
	assert.Contains(t, err.Error(), "Cannot connect to the Docker daemon")
}

func TestCheckRequiresAWritableSessionRoot(t *testing.T) {
	checks := 0
	rec := &recorder{reply: func([]string) ([]byte, []byte, error) {
		checks++
		if checks == 1 {
			return []byte(sentinel + "27.5.1\n"), nil, nil
		}
		return nil, []byte("mkdir: /srv/ade: Read-only file system\n"), errors.New("exit status 1")
	}}
	host := testHost(t, rec)

	err := Check(context.Background(), host, fhsRoots)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create /srv/ade on pc")
}

// A Mac has no /srv and cannot grow one, and shares only a few host paths into
// the Docker VM. Its roots and its socket both live under the user's home.
func TestDetectRootsFallsBackToTheHomeItWasGiven(t *testing.T) {
	rec := &recorder{reply: says("/Users/owen\n\n\nunix:///Users/owen/.orbstack/run/docker.sock\n/Users/owen/code\n")}
	host := testHost(t, rec)

	got, err := DetectRoots(context.Background(), host)
	require.NoError(t, err)
	assert.Equal(t, "/Users/owen/.ade/srv", got.Work)
	assert.Equal(t, "/Users/owen/.ade/run", got.Run)
	assert.Equal(t, "/Users/owen/.orbstack/run/docker.sock", got.Socket)
	// The repositories worth offering are the ones already there.
	assert.Equal(t, "/Users/owen/code", got.Scan)
	assert.Equal(t, "/Users/owen/code", got.ScanRoot())
	assert.Len(t, rec.calls, 1, "one round trip answers all of it")
}

func TestDetectRootsKeepsTheFHSPathsWhenTheyAreWritable(t *testing.T) {
	rec := &recorder{reply: says("/home/ade\n/srv/ade\n/run/ade\nunix:///var/run/docker.sock\n\n")}
	host := testHost(t, rec)

	got, err := DetectRoots(context.Background(), host)
	require.NoError(t, err)
	assert.Equal(t, fhsRoots, got)
	assert.Equal(t, "/srv/ade", got.ScanRoot(), "a provisioned worker clones into what it scans")
}

// An unprovisioned Linux box fails exactly as a Mac does, and for the duller
// reason that its user is not root. The OS is not what decides this.
func TestDetectRootsDoesNotAssumeLinuxCanWriteSrv(t *testing.T) {
	rec := &recorder{reply: says("/home/owen\n\n\nunix:///var/run/docker.sock\n\n")}
	host := testHost(t, rec)

	got, err := DetectRoots(context.Background(), host)
	require.NoError(t, err)
	assert.Equal(t, "/home/owen/.ade/srv", got.Work)
	assert.Equal(t, "/home/owen/.ade/run", got.Run)
}

func TestForwarderSyncReconcilesTheSet(t *testing.T) {
	tests := []struct {
		name string
		have []Mapping
		want []Mapping
		adds []string
		dels []string
	}{
		{name: "adds what is missing", want: same(20345), adds: []string{"20345:127.0.0.1:20345"}},
		{name: "drops what is no longer wanted", have: same(20345), dels: []string{"20345:127.0.0.1:20345"}},
		{name: "leaves what is already right", have: same(20345), want: same(20345)},
		{
			name: "both at once",
			have: same(20345), want: same(27431),
			adds: []string{"27431:127.0.0.1:27431"},
			dels: []string{"20345:127.0.0.1:20345"},
		},
		{
			// The launcher's local end is not its remote one, and it can move
			// between runs. The old forward has to go before the new one, or
			// ssh holds both and the stale one wins the port.
			name: "a local end that moved",
			have: []Mapping{{Local: 29233, Remote: 29233}},
			want: []Mapping{{Local: 31000, Remote: 29233}},
			adds: []string{"31000:127.0.0.1:29233"},
			dels: []string{"29233:127.0.0.1:29233"},
		},
		{name: "nothing either way"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			fwd := NewForwarder(testHost(t, rec))
			fwd.free = func(int) error { return nil }
			for _, m := range tc.have {
				fwd.live[m.Remote] = m
			}
			rec.calls = nil

			require.NoError(t, fwd.Sync(context.Background(), tc.want))

			var adds, dels []string
			for _, call := range rec.calls {
				if call[3] == "forward" {
					adds = append(adds, call[5])
				} else {
					dels = append(dels, call[5])
				}
			}
			assert.Equal(t, tc.adds, adds)
			assert.Equal(t, tc.dels, dels)
			assert.Equal(t, tc.want, nilIfEmpty(fwd.Live()))
		})
	}
}

func same(ports ...int) []Mapping {
	out := make([]Mapping, 0, len(ports))
	for _, port := range ports {
		out = append(out, Same(port))
	}
	return out
}

func TestForwarderArgvIsTheSamePortBothEnds(t *testing.T) {
	rec := &recorder{}
	host := testHost(t, rec)
	fwd := NewForwarder(host)
	fwd.free = func(int) error { return nil }

	require.NoError(t, fwd.Sync(context.Background(), same(27431)))
	assert.Equal(t,
		[]string{"-S", host.Socket(), "-O", "forward", "-L", "27431:127.0.0.1:27431", "pc"},
		rec.calls[0])

	rec.calls = nil
	require.NoError(t, fwd.Sync(context.Background(), nil))
	assert.Equal(t,
		[]string{"-S", host.Socket(), "-O", "cancel", "-L", "27431:127.0.0.1:27431", "pc"},
		rec.calls[0])
}

func TestForwarderKeepsLiveHonestWhenAnAddFails(t *testing.T) {
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		if len(argv) > 5 && argv[5] == "27431:127.0.0.1:27431" {
			return nil, []byte("Port forwarding failed"), errors.New("exit status 255")
		}
		return nil, nil, nil
	}}
	fwd := NewForwarder(testHost(t, rec))
	fwd.free = func(int) error { return nil }

	err := fwd.Sync(context.Background(), same(20345, 27431))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "27431")
	assert.Equal(t, same(20345), fwd.Live(), "the one that failed is not claimed")

	// And so it is tried again rather than assumed dead.
	rec.reply = nil
	require.NoError(t, fwd.Sync(context.Background(), same(20345, 27431)))
	assert.Equal(t, same(20345, 27431), fwd.Live())
}

func TestForwarderRefusesAPortThisMachineHolds(t *testing.T) {
	rec := &recorder{}
	fwd := NewForwarder(testHost(t, rec))
	fwd.free = func(port int) error { return fmt.Errorf("port %d is already in use on this machine", port) }

	err := fwd.Sync(context.Background(), same(27431))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "27431 is already in use")
	assert.Empty(t, rec.calls, "ssh is never asked for a forward that cannot work")
}

func nilIfEmpty(in []Mapping) []Mapping {
	if len(in) == 0 {
		return nil
	}
	return in
}

// docker ends every failure with "Run 'docker run --help'", which is the last
// line and says nothing. The line before it is the reason.
func TestTheReasonBeatsTheUsageHintDockerEndsWith(t *testing.T) {
	rec := &recorder{reply: func([]string) ([]byte, []byte, error) {
		return nil, []byte("docker: Error response from daemon: Head \"https://ghcr.io/v2/owenrumney/ade-worker/manifests/latest\": denied\n\nRun 'docker run --help' for more information\n"), errors.New("exit status 125")
	}}
	host := testHost(t, rec)

	_, err := host.Run(context.Background(), "docker", "run", "x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "denied")
	assert.NotContains(t, err.Error(), "--help")
}

// A worker with no ~/code answers the last question with a blank, which the
// shell trims off the end entirely.
func TestDetectRootsAcceptsAWorkerWithNoCodeDirectory(t *testing.T) {
	rec := &recorder{reply: says("/home/ade\n/srv/ade\n/run/ade\nunix:///var/run/docker.sock")}
	host := testHost(t, rec)

	got, err := DetectRoots(context.Background(), host)
	require.NoError(t, err)
	assert.Empty(t, got.Scan)
	assert.Equal(t, "/srv/ade", got.ScanRoot())
}

func TestDetectRootsStillRefusesAWorkerThatSaidNothing(t *testing.T) {
	rec := &recorder{reply: says("/home/ade\n")}
	host := testHost(t, rec)

	_, err := DetectRoots(context.Background(), host)
	require.Error(t, err)
}

// TestAHostNobodyDialledReportsNoStaleness guards the launcher's host list,
// which asks every row whether its worker is stale, including rows that never
// got a connection.
func TestAHostNobodyDialledReportsNoStaleness(t *testing.T) {
	var none *Host
	assert.NoError(t, none.Stale())
}
