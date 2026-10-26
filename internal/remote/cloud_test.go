package remote

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noBoxCopy is a host with none of these files yet: `test -s` fails for each.
func noBoxCopy(argv []string) ([]byte, []byte, error) {
	if strings.Contains(strings.Join(argv, " "), "test") {
		return nil, nil, errors.New("exit status 1")
	}
	return []byte(sentinel), nil, nil
}

func TestPrepareCloudSendsEachFileAndNeverPutsItInArgv(t *testing.T) {
	rec := &recorder{reply: noBoxCopy}
	host := testHost(t, rec)

	err := host.PrepareCloud(context.Background(), fhsRoots, Cloud{
		AWSConfig:      []byte("[profile dev]"),
		AWSCredentials: []byte("[default]"),
		Kubeconfig:     []byte("apiVersion: v1"),
		SSOCache:       map[string][]byte{"abc.json": []byte("bearer")},
	})
	require.NoError(t, err)

	var sent []string
	for i, call := range rec.calls {
		command := strings.Join(call, " ")
		assert.NotContains(t, command, "bearer", "a token must never reach argv")
		if len(rec.input[i]) > 0 {
			sent = append(sent, string(rec.input[i]))
		}
	}
	assert.ElementsMatch(t, []string{"[profile dev]", "[default]", "apiVersion: v1", "bearer"}, sent)
	assert.Contains(t, strings.Join(rec.calls[0], " "), "/run/ade/hub",
		"the files live under Run, which goes with a reboot")
}

func TestPrepareCloudSendsNothingWhenThereIsNothingToSend(t *testing.T) {
	rec := &recorder{}
	require.NoError(t, testHost(t, rec).PrepareCloud(context.Background(), fhsRoots, Cloud{}))
	assert.Empty(t, rec.calls, "a host whose bookmark did not ask is never touched")
}

// A refresh over there leaves the newer copy, and newest-wins has to hold.
func TestANewerBoxCloudCopyIsKept(t *testing.T) {
	rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
		// Exit zero from the stat check is a box copy at least as new.
		if strings.Contains(argv[len(argv)-1], "stat") {
			return []byte(sentinel), nil, nil
		}
		return []byte(sentinel), nil, nil
	}}
	host := testHost(t, rec)

	err := host.PrepareCloud(context.Background(), fhsRoots, Cloud{
		AWSConfig: []byte("[profile dev]"),
		At:        time.Now().Add(-time.Hour),
	})
	require.NoError(t, err)
	for _, in := range rec.input {
		assert.NotContains(t, string(in), "[profile dev]", "the newer box copy stays")
	}
}

func TestCloudEnvNamesTheFilesAndOnlyNamesAProfileWhenThereIsOne(t *testing.T) {
	env := CloudEnv(fhsRoots, Cloud{})
	assert.Equal(t, []string{
		"AWS_CONFIG_FILE=/run/ade/hub/aws/config",
		"AWS_SHARED_CREDENTIALS_FILE=/run/ade/hub/aws/credentials",
		"KUBECONFIG=/run/ade/hub/kube/config",
	}, env)
	assert.Contains(t, CloudEnv(fhsRoots, Cloud{Profile: "work"}), "AWS_PROFILE=work")
}

func TestTheHubMountsTheCloudFilesReadOnly(t *testing.T) {
	var calls []string
	rec := &recorder{reply: hubReplies(false, &calls)}
	host := testHost(t, rec)

	require.NoError(t, host.StartHub(context.Background(), fhsRoots, Login{},
		Cloud{AWSConfig: []byte("[profile dev]"), Profile: "work"}))

	var run string
	for _, command := range calls {
		if strings.Contains(command, "'run'") {
			run = command
		}
	}
	require.NotEmpty(t, run, "the hub was started")
	assert.True(t, hasArgs(run, "/run/ade/hub:/run/ade/hub:ro"), "mounted, and read-only")
	assert.True(t, hasArgs(run, "AWS_PROFILE=work"))
	assert.True(t, hasArgs(run, "AWS_CONFIG_FILE=/run/ade/hub/aws/config"))
}

func TestTheHubIsNotGivenAMountWhenThereIsNothingToMount(t *testing.T) {
	var calls []string
	rec := &recorder{reply: hubReplies(false, &calls)}
	require.NoError(t, testHost(t, rec).StartHub(context.Background(), fhsRoots, Login{}, Cloud{}))

	for _, command := range calls {
		if strings.Contains(command, "'run'") {
			assert.NotContains(t, command, "/run/ade/hub")
			assert.NotContains(t, command, "AWS_CONFIG_FILE")
		}
	}
}

// A hub from before the bookmark asked has no mount, and adopting it would
// leave every tool unable to log in.
func TestAHubWithoutTheCloudMountIsReplaced(t *testing.T) {
	for _, test := range []struct {
		name    string
		mounts  string
		cloud   Cloud
		replace bool
	}{
		{name: "no mount, now wanted", mounts: "/srv/ade ", cloud: Cloud{AWSConfig: []byte("x")}, replace: true},
		{name: "mounted, still wanted", mounts: "/srv/ade /run/ade/hub ", cloud: Cloud{AWSConfig: []byte("x")}},
		{name: "mounted, no longer wanted", mounts: "/srv/ade /run/ade/hub ", replace: true},
		{name: "no mount, not wanted", mounts: "/srv/ade "},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			rec := &recorder{reply: func(argv []string) ([]byte, []byte, error) {
				command := argv[len(argv)-1]
				if strings.Contains(command, "Mounts") {
					return []byte(sentinel + test.mounts + "\n"), nil, nil
				}
				return hubReplies(true, &calls)(argv)
			}}
			host := testHost(t, rec)
			require.NoError(t, host.StartHub(context.Background(), fhsRoots, Login{}, test.cloud))

			started := false
			for _, command := range calls {
				if strings.Contains(command, "'run'") {
					started = true
				}
			}
			assert.Equal(t, test.replace, started, "whether a running hub was replaced")
		})
	}
}
