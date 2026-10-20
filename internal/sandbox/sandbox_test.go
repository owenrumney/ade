package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/owenrumney/ade/internal/session"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectPrefersAppleWhenItsServicesAreUp(t *testing.T) {
	have := func(bins ...string) func(string) (string, error) {
		return func(name string) (string, error) {
			for _, b := range bins {
				if b == name {
					return "/usr/local/bin/" + name, nil
				}
			}
			return "", errors.New("not found")
		}
	}
	up := func(context.Context, string, ...string) error { return nil }
	down := func(context.Context, string, ...string) error { return errors.New("not running") }

	rt, err := detect(context.Background(), have("container", "docker"), up)
	require.NoError(t, err)
	assert.Equal(t, "container", rt.Name())

	rt, err = detect(context.Background(), have("container", "docker"), down)
	require.NoError(t, err)
	assert.Equal(t, "docker", rt.Name(), "container installed but its services down falls to docker")

	rt, err = detect(context.Background(), have("docker"), up)
	require.NoError(t, err)
	assert.Equal(t, "docker", rt.Name())

	_, err = detect(context.Background(), have(), up)
	assert.ErrorContains(t, err, "no container runtime")
}

func TestBothRuntimesTakeTheSameSpec(t *testing.T) {
	spec := Spec{
		Image:   "ade-agent:latest",
		Name:    "ade-x",
		Workdir: "/Users/o/code/x",
		Mounts: []Mount{
			{Host: "/Users/o/code/x", Guest: "/Users/o/code/x", RW: true},
			{Host: "/Users/o/.claude/agents", Guest: "/Users/o/.claude/agents"},
		},
		Env:  map[string]string{"HOME": "/Users/o", "A": "1"},
		Argv: []string{"claude", "-p", "--model", "opus"},
	}
	docker := strings.Join(Docker{Socket: DockerSocket}.Args(spec), " ")
	assert.Equal(t, "run -i --rm --name ade-x -v /var/run/docker.sock:/var/run/docker.sock:rw -v /Users/o/code/x:/Users/o/code/x:rw -v /Users/o/.claude/agents:/Users/o/.claude/agents:ro -e A=1 -e HOME=/Users/o -e DOCKER_HOST=unix:///var/run/docker.sock -w /Users/o/code/x ade-agent:latest claude -p --model opus", docker)
	apple := strings.Join(Apple{}.Args(spec), " ")
	assert.Equal(t, "run -i --rm --name ade-x --mount type=bind,source=/Users/o/code/x,target=/Users/o/code/x --mount type=bind,source=/Users/o/.claude/agents,target=/Users/o/.claude/agents,readonly -e A=1 -e HOME=/Users/o -w /Users/o/code/x ade-agent:latest claude -p --model opus", apple)
}

// Design: .git/ade/docs/sandbox-file-ownership.md.
func TestALinuxRuntimeWritesAsTheHostUser(t *testing.T) {
	spec := Spec{Image: "image", Name: "agent"}

	args := strings.Join(Docker{User: "1000:1000", Groups: []string{"989"}}.Args(spec), " ")
	assert.Contains(t, args, "--user 1000:1000")
	assert.Contains(t, args, "--group-add 989")

	// Off Linux the daemon's VM maps ownership already.
	assert.NotContains(t, strings.Join(Docker{}.Args(spec), " "), "--user")
	assert.NotContains(t, strings.Join(Docker{}.Args(spec), " "), "--group-add")
}

func TestTheSocketsGroupIsOnlyAddedWhenItIsNotAlreadyYours(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("hostUser maps ids on Linux only")
	}
	user, groups := hostUser("")
	assert.Equal(t, fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), user)
	assert.Empty(t, groups, "no socket is no group to add")

	// A missing socket must not stop the run.
	_, groups = hostUser(filepath.Join(t.TempDir(), "absent.sock"))
	assert.Empty(t, groups)
}

// On a worker ade is root in a container, so its own ids are 0:0 and mapping
// them would leave the checkout root-owned exactly as before.
func TestAWorkerUsesTheIdsItWasToldRatherThanItsOwn(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("hostUser maps ids on Linux only")
	}
	t.Setenv(session.HostUserEnv, "1000")
	t.Setenv(session.HostGroupEnv, "1001")
	user, _ := hostUser("")
	assert.Equal(t, "1000:1001", user)

	// Half an answer is no answer: both or the process's own.
	t.Setenv(session.HostGroupEnv, "")
	user, _ = hostUser("")
	assert.Equal(t, fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), user)

	t.Setenv(session.HostUserEnv, "pi")
	t.Setenv(session.HostGroupEnv, "1001")
	user, _ = hostUser("")
	assert.Equal(t, fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), user)
}

func TestAWorkersAgentsShareTheHubsNetworkNamespace(t *testing.T) {
	args := strings.Join((Docker{Network: "container:ade-hub"}).Args(Spec{Image: "image", Name: "agent"}), " ")
	assert.Contains(t, args, "--network container:ade-hub")
	assert.NotContains(t, strings.Join((Docker{}).Args(Spec{Image: "image", Name: "agent"}), " "), "--network")
}

func TestDockerMountsTheActiveContextsSocketAtTheStandardPath(t *testing.T) {
	args := strings.Join((Docker{Socket: "/Users/o/.orbstack/run/docker.sock"}).Args(Spec{Image: "image", Name: "agent"}), " ")
	assert.Contains(t, args, "-v /Users/o/.orbstack/run/docker.sock:/var/run/docker.sock:rw")
	assert.Contains(t, args, "-e DOCKER_HOST=unix:///var/run/docker.sock")
	assert.Equal(t, "/Users/o/.docker/run/docker.sock", unixSocket("unix:///Users/o/.docker/run/docker.sock\n"))
	assert.Empty(t, unixSocket("tcp://127.0.0.1:2375"))
}

// A tcp:// or ssh:// daemon has no socket to bind. Binding one anyway makes
// docker create a directory at that path, and every docker command fails oddly.
func TestATcpDaemonMountsNoSocketRatherThanADirectory(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://127.0.0.1:2375")
	assert.Empty(t, resolveDockerSocket(context.Background()))

	args := strings.Join((Docker{}).Args(Spec{Image: "image", Name: "agent"}), " ")
	assert.NotContains(t, args, DockerSocket)
	assert.NotContains(t, args, "DOCKER_HOST")
}

func TestCommandStopsTheContainerWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := Command(ctx, Docker{}, Spec{Name: "ade-x", Image: "i"})
	assert.Equal(t, "docker", filepath.Base(cmd.Path))
	assert.NotNil(t, cmd.Cancel, "a cancelled run stops the container by name, not just the client")
	assert.Equal(t, stopGrace, cmd.WaitDelay)
	cancel()
}

func TestContainerNameIsMadeFromTheCheckout(t *testing.T) {
	assert.Equal(t, "ade-users-o-code-x", ContainerName("/Users/o/code/x"))
	assert.Equal(t, ContainerName("/Users/o/code/x"), ContainerName("/Users/o/code/x"), "the same checkout is the same name")
	long := ContainerName("/" + strings.Repeat("abcdefghij/", 10))
	assert.LessOrEqual(t, len(long), 60)
	assert.False(t, strings.HasPrefix(long, "-"))
}

func TestTheSpecMountsWhatClaudeNeedsAndNoMore(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "code", "x")
	gitDir := filepath.Join(home, "code", "main", ".git")
	claudeDir := filepath.Join(home, ".claude")
	require.NoError(t, os.MkdirAll(root, 0o750))
	require.NoError(t, os.MkdirAll(gitDir, 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(claudeDir, "agents"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(claudeDir, "plugins", "cache"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "plugins", "installed_plugins.json"), []byte(`{"version":2}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "CLAUDE.md"), []byte("be brief"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "remote-settings.json"), []byte(`{"otel":{"enabled":true}}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, "history.jsonl"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(claudeDir, ".i-have-adhd-always"), nil, 0o600))
	box := Box(home)
	require.NoError(t, os.MkdirAll(box, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(box, "settings.json"), []byte("stale"), 0o600))

	spec, err := Claude{Home: home, Root: root, GitDir: gitDir, Argv: []string{"claude", "-p"},
		Extra: []Mount{{Host: "/opt/other", Guest: "/opt/other"}}, Env: map[string]string{"GOFLAGS": "-mod=mod"}}.Spec(box)
	require.NoError(t, err)

	guests := map[string]Mount{}
	for _, m := range spec.Mounts {
		guests[m.Guest] = m
		info, err := os.Stat(m.Host)
		if m.Host != "/opt/other" {
			require.NoError(t, err, "the source exists: %+v", m)
			assert.True(t, info.IsDir(), "directories only, apple's container cannot mount a file: %+v", m)
		}
		assert.True(t, m.Host == m.Guest || m.Guest == claudeDir || m.Guest == CacheDir ||
			filepath.Base(m.Guest) == "node_modules", "mounted at its own path: %+v", m)
	}
	assert.True(t, guests[root].RW, "the checkout is written")
	assert.True(t, guests[gitDir].RW, "a worktree's git dir is written too")
	assert.Equal(t, box, guests[claudeDir].Host, "the guest's ~/.claude is ade's box")
	assert.True(t, guests[claudeDir].RW, "a refreshed login and whatever claude writes beside it are kept")
	assert.True(t, guests[filepath.Join(claudeDir, "projects")].RW, "transcripts land on the host")
	assert.Equal(t, Cache(home), guests[CacheDir].Host, "toolchains and build caches outlive the container")
	assert.True(t, guests[CacheDir].RW)
	assert.DirExists(t, Cache(home), "made so the mount has a source")
	assert.False(t, guests[filepath.Join(claudeDir, "agents")].RW)
	assert.Contains(t, guests, "/opt/other")
	_, history := guests[filepath.Join(claudeDir, "history.jsonl")]
	assert.False(t, history)
	_, commands := guests[filepath.Join(claudeDir, "commands")]
	assert.False(t, commands, "only what exists is mounted")
	pluginCache := guests[filepath.Join(claudeDir, "plugins", "cache")]
	assert.Equal(t, filepath.Join(claudeDir, "plugins", "cache"), pluginCache.Host, "plugin code is mounted where the index points")
	assert.False(t, pluginCache.RW)
	index, err := os.ReadFile(filepath.Join(box, "plugins", "installed_plugins.json"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":2}`, string(index), "the index is copied so the guest's plugins dir stays writable")

	copied, err := os.ReadFile(filepath.Join(box, "CLAUDE.md"))
	require.NoError(t, err)
	assert.Equal(t, "be brief", string(copied), "a file is copied into the box, not mounted")
	remote, err := os.ReadFile(filepath.Join(box, "remote-settings.json"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"otel":{"enabled":true}}`, string(remote))
	assert.NoFileExists(t, filepath.Join(box, "settings.json"), "a copy of what the host no longer has is taken out")
	assert.FileExists(t, filepath.Join(box, ".i-have-adhd-always"),
		"a plugin's opt-in flag follows you in, or it is on outside the box and off inside it")

	assert.Equal(t, root, spec.Workdir)
	assert.Equal(t, home, spec.Env["HOME"])
	assert.Equal(t, "1", spec.Env["IS_SANDBOX"], "claude will not skip permissions as root without it")
	assert.Equal(t, "-mod=mod", spec.Env["GOFLAGS"])
	assert.Equal(t, DefaultImage, spec.Image)
	assert.DirExists(t, filepath.Join(claudeDir, "projects"), "made so the mount has a source")
	assert.Empty(t, spec.Args, "no MCP servers, no --mcp-config")
	assert.NoFileExists(t, filepath.Join(box, mcpFile))

	// The user's MCP servers travel: ~/.claude.json is a file with much else
	// in it, so only the servers are written to the box and named to claude.
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude.json"),
		[]byte(`{"hasCompletedOnboarding":true,"oauthAccount":{"email":"x"},"mcpServers":{"docs":{"type":"http","url":"https://docs.example/mcp"}}}`), 0o600))
	spec, err = Claude{Home: home, Root: root}.Spec(box)
	require.NoError(t, err)
	assert.Equal(t, []string{"--mcp-config", filepath.Join(claudeDir, mcpFile)}, spec.Args)
	written, err := os.ReadFile(filepath.Join(box, mcpFile))
	require.NoError(t, err)
	assert.JSONEq(t, `{"mcpServers":{"docs":{"type":"http","url":"https://docs.example/mcp"}}}`, string(written))
	assert.NotContains(t, string(written), "oauthAccount", "only the servers")
	spec.Argv = []string{"claude", "-p"}
	line := (Docker{}).Args(spec)
	assert.Equal(t, []string{"claude", "-p", "--mcp-config", filepath.Join(claudeDir, mcpFile)}, line[len(line)-4:], "after the command, where claude reads it")

	// A main checkout's own .git is inside the root already.
	spec, err = Claude{Home: home, Root: root, GitDir: filepath.Join(root, ".git")}.Spec(box)
	require.NoError(t, err)
	for _, m := range spec.Mounts {
		assert.NotEqual(t, filepath.Join(root, ".git"), m.Guest)
	}
}

func TestASymlinkedSkillBringsItsTargetIntoTheContainer(t *testing.T) {
	home := t.TempDir()
	noKeychain(t)
	root := filepath.Join(home, "code", "x")
	skills := filepath.Join(home, ".claude", "skills")
	elsewhere := filepath.Join(home, ".watchtower", "skills", "adhd")
	require.NoError(t, os.MkdirAll(root, 0o750))
	require.NoError(t, os.MkdirAll(skills, 0o750))
	require.NoError(t, os.MkdirAll(elsewhere, 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(skills, "here"), 0o750))
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(skills, "adhd")))
	require.NoError(t, os.Symlink(filepath.Join(home, "gone"), filepath.Join(skills, "dangling")))
	require.NoError(t, os.Symlink(filepath.Join(skills, "here"), filepath.Join(skills, "sibling")))

	spec, err := Claude{Home: home, Root: root}.Spec(Box(home))
	require.NoError(t, err)

	guests := map[string]Mount{}
	for _, m := range spec.Mounts {
		guests[m.Guest] = m
	}
	target := guests[elsewhere]
	require.NotZero(t, target, "the link's target is mounted, or the skill dangles in the guest")
	assert.False(t, target.RW, "read-only, like the skills directory itself")
	assert.DirExists(t, target.Host)
	assert.NotContains(t, guests, filepath.Join(home, "gone"), "a link broken on the host stays broken")
	assert.NotContains(t, guests, filepath.Join(skills, "here"), "a sibling comes with the directory")
}

func TestCredentialsAreCopiedOnceAndThenLeftAlone(t *testing.T) {
	home := t.TempDir()
	noKeychain(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"),
		[]byte(fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"a","expiresAt":%d}}`,
			time.Now().Add(time.Hour).UnixMilli())), 0o600))

	path, err := Credentials(context.Background(), home)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".ade", "claude", ".credentials.json"), path, "in the box, the guest's ~/.claude")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	// The container refreshed it, and got a live token doing so; the host's file
	// changed too. Ours stays. A copy whose token is spent is a different case,
	// and is replaced — see TestADeadCopyIsReplacedRatherThanReused.
	live := fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"refreshed","expiresAt":%d}}`,
		time.Now().Add(time.Hour).UnixMilli())
	require.NoError(t, os.WriteFile(path, []byte(live), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"), []byte(`{"host":true}`), 0o600))
	again, err := Credentials(context.Background(), home)
	require.NoError(t, err)
	assert.Equal(t, path, again)
	data, _ := os.ReadFile(path)
	assert.Equal(t, live, string(data))

	require.NoError(t, ForgetCredentials(home))
	require.NoError(t, ForgetCredentials(home), "gone is fine")
	assert.NoFileExists(t, path)
}

// keychain stands in for a Mac whose Keychain holds the login, on any host:
// off darwin the stub is otherwise never consulted and the test is vacuous.
func keychain(t *testing.T, data string) {
	t.Helper()
	oldRead, oldOS := readKeychain, onDarwin
	readKeychain = func(context.Context) ([]byte, error) { return []byte(data), nil }
	onDarwin = true
	t.Cleanup(func() { readKeychain, onDarwin = oldRead, oldOS })
}

// noKeychain stands in for a Mac with no Claude entry in its Keychain, so a
// test about the file is about the file and never reads the real login.
func noKeychain(t *testing.T) {
	t.Helper()
	old := readKeychain
	readKeychain = func(context.Context) ([]byte, error) {
		return nil, errors.New("no keychain entry")
	}
	t.Cleanup(func() { readKeychain = old })
}

func TestTheKeychainBeatsALeftoverFileOnAMac(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the keychain is a mac")
	}
	home := t.TempDir()
	old := readKeychain
	readKeychain = func(context.Context) ([]byte, error) {
		return []byte(`{"claudeAiOauth":{"accessToken":"live"}}`), nil
	}
	t.Cleanup(func() { readKeychain = old })

	// The leftover that used to win: still there, still dead.
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"),
		[]byte(`{"claudeAiOauth":{"accessToken":"stale","expiresAt":0}}`), 0o600))

	data, _, err := hostCredentials(context.Background(), home)
	require.NoError(t, err)
	assert.JSONEq(t, `{"claudeAiOauth":{"accessToken":"live"}}`, string(data),
		"the keychain is the live login on a mac; the file is a leftover")
}

func TestTheFileIsTheFallbackWhenTheKeychainHasNothing(t *testing.T) {
	home := t.TempDir()
	noKeychain(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"),
		[]byte(`{"claudeAiOauth":{"accessToken":"f"}}`), 0o600))

	data, _, err := hostCredentials(context.Background(), home)
	require.NoError(t, err)
	assert.JSONEq(t, `{"claudeAiOauth":{"accessToken":"f"}}`, string(data))
}

func TestNoLoginAnywhereSaysWhereItLooked(t *testing.T) {
	home := t.TempDir()
	noKeychain(t)
	_, _, err := hostCredentials(context.Background(), home)
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".credentials.json")
}

func TestCredentialsComeFromTheKeychainOnAMac(t *testing.T) {
	home := t.TempDir()
	old := readKeychain
	readKeychain = func(context.Context) ([]byte, error) {
		return []byte(`{"claudeAiOauth":{"accessToken":"k"}}` + "\n"), nil
	}
	t.Cleanup(func() { readKeychain = old })

	data, _, err := hostCredentials(context.Background(), home)
	if err != nil {
		assert.Contains(t, err.Error(), "no claude login", "not a mac: says where the file should be")
		return
	}
	assert.JSONEq(t, `{"claudeAiOauth":{"accessToken":"k"}}`, string(data))
}

func TestSpecGivesGitAnIdentityAndTheCheckout(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "work", "repo")
	require.NoError(t, os.MkdirAll(root, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"),
		[]byte("[user]\n\tname = Owen\n\temail = owen@example.com\n"), 0o600))
	box := filepath.Join(home, "box")

	spec, err := Claude{Home: home, Root: root, GitDir: filepath.Join(home, "gits", "repo.git")}.Spec(box)
	require.NoError(t, err)

	guest := filepath.Join(home, ".claude", "gitconfig")
	assert.Equal(t, guest, spec.Env["GIT_CONFIG_GLOBAL"], "git must read the copy, not the host's")

	got, err := os.ReadFile(filepath.Join(box, "gitconfig"))
	require.NoError(t, err)
	assert.Contains(t, string(got), "email = owen@example.com", "the host's identity carries in")
	assert.Contains(t, string(got), "directory = "+root, "root is owned by the host user; root must trust it")
	assert.Contains(t, string(got), "directory = "+filepath.Join(home, "gits", "repo.git"))
	assert.NotContains(t, string(got), "directory = *", "only what ade mounts")
}

func TestSpecWritesAGitconfigWhenTheHostHasNone(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "repo")
	require.NoError(t, os.MkdirAll(root, 0o750))

	_, err := Claude{Home: home, Root: root}.Spec(filepath.Join(home, "box"))
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(home, "box", "gitconfig"))
	require.NoError(t, err)
	assert.Contains(t, string(got), "directory = "+root, "dubious ownership bites with or without a host config")
}

// A named key that is not there must not make the agent try to sign.
func TestSpecTurnsOffCommitSigningInTheContainer(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "repo")
	require.NoError(t, os.MkdirAll(root, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"),
		[]byte("[user]\n\tsigningkey = ~/.ssh/id_ed25519.pub\n[commit]\n\tgpgsign = true\n"), 0o600))

	_, err := Claude{Home: home, Root: root}.Spec(filepath.Join(home, "box"))
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(home, "box", "gitconfig"))
	require.NoError(t, err)
	last := strings.LastIndex(string(got), "gpgsign =")
	assert.Contains(t, string(got)[last:], "gpgsign = false", "the override must come after the host's")
}

// Design: .git/ade/docs/sandbox-commit-signing.md.
func TestSpecMountsTheSigningKeyTheConfigNames(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "repo")
	require.NoError(t, os.MkdirAll(root, 0o750))
	pub, secret := signingPair(t, home, "id_ed25519")
	require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"),
		[]byte("[user]\n\tsigningkey = ~/.ssh/id_ed25519.pub\n[gpg]\n\tformat = ssh\n[commit]\n\tgpgsign = true\n"), 0o600))

	spec, err := Claude{Home: home, Root: root}.Spec(filepath.Join(home, "box"))
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(home, "box", "gitconfig"))
	require.NoError(t, err)
	assert.NotContains(t, string(got), "gpgsign = false", "the host signs, and now the box can")

	// Apple's runtime binds only directories, so a copy of the pair goes in.
	staged := filepath.Join(home, "box", "ssh")
	assert.Contains(t, spec.Mounts, Mount{Host: staged, Guest: filepath.Join(home, ".ssh")})
	for _, m := range spec.Mounts {
		if m.Host == staged {
			assert.False(t, m.RW, "a key the agent could rewrite is a key it could replace")
		}
		assert.NotEqual(t, filepath.Join(home, ".ssh"), m.Host, "a copy, never the host's directory")
		assert.NotEqual(t, pub, m.Host, "a directory, never the file")
		assert.NotEqual(t, secret, m.Host, "a directory, never the file")
	}

	named, err := os.ReadDir(staged)
	require.NoError(t, err)
	var there []string
	for _, e := range named {
		there = append(there, e.Name())
	}
	assert.ElementsMatch(t, []string{filepath.Base(pub), filepath.Base(secret)}, there,
		"the pair and nothing else")

	was, err := os.Stat(secret)
	require.NoError(t, err)
	now, err := os.Stat(filepath.Join(staged, filepath.Base(secret)))
	require.NoError(t, err)
	assert.Equal(t, was.Mode().Perm(), now.Mode().Perm())
}

func TestOnlyAResolvableKeyUnderHomeIsMounted(t *testing.T) {
	home := t.TempDir()
	pub, secret := signingPair(t, home, "id_ed25519")

	assert.Equal(t, []string{pub, secret},
		signingKey([]byte("\tsigningkey = ~/.ssh/id_ed25519.pub\n"), home))

	// A gpg key id is not a path.
	assert.Empty(t, signingKey([]byte("\tsigningkey = ABCD1234EF567890\n"), home))
	// The public half alone cannot sign.
	require.NoError(t, os.Remove(secret))
	assert.Empty(t, signingKey([]byte("\tsigningkey = ~/.ssh/id_ed25519.pub\n"), home))
	// Nothing outside home is the user's to hand over.
	assert.Empty(t, signingKey([]byte("\tsigningkey = /etc/ssh/ssh_host_ed25519_key.pub\n"), home))
	assert.Empty(t, signingKey([]byte("\tsigningkey = ~/../elsewhere/key.pub\n"), home))
}

func signingPair(t *testing.T, home, name string) (string, string) {
	t.Helper()
	dir := filepath.Join(home, ".ssh")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	pub := filepath.Join(dir, name+".pub")
	secret := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(pub, []byte("ssh-ed25519 AAAA owen\n"), 0o600))
	require.NoError(t, os.WriteFile(secret, []byte("-----BEGIN-----\n"), 0o600))
	return pub, secret
}

func TestADeadCopyIsReplacedRatherThanReused(t *testing.T) {
	// A refresh inside the container can fail, because the host refreshes the
	// same session and the provider rotates the refresh token. Claude Code
	// records that by writing expiresAt 0, and every later run then started
	// from a token that could not work — "OAuth session expired and could not
	// be refreshed", once per run, until the file was deleted by hand.
	home := t.TempDir()
	live := time.Now().Add(time.Hour).UnixMilli()
	keychain(t, fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"fresh","expiresAt":%d}}`, live))
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o750))

	path, err := Credentials(context.Background(), home)
	require.NoError(t, err)
	assert.Contains(t, string(read(t, path)), "fresh")

	// The container comes back having failed to refresh.
	require.NoError(t, os.WriteFile(path,
		[]byte(`{"claudeAiOauth":{"accessToken":"spent","expiresAt":0}}`), 0o600))
	_, err = Credentials(context.Background(), home)
	require.NoError(t, err)
	assert.Contains(t, string(read(t, path)), "fresh", "the dead copy is replaced from the host")
}

func TestALiveCopyIsLeftAlone(t *testing.T) {
	// A token the container refreshed successfully is the one to keep: copying
	// over it would spend the host's refresh and cost the container its own.
	home := t.TempDir()
	noKeychain(t)
	live := time.Now().Add(time.Hour).UnixMilli()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude", ".credentials.json"),
		[]byte(fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"host","expiresAt":%d}}`, live)), 0o600))

	path, err := Credentials(context.Background(), home)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path,
		[]byte(fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"refreshed","expiresAt":%d}}`, live+1000)), 0o600))

	_, err = Credentials(context.Background(), home)
	require.NoError(t, err)
	assert.Contains(t, string(read(t, path)), "refreshed", "still the container's own")
}

func TestACopyTheHostHasOvertakenIsReplaced(t *testing.T) {
	// Both sides refresh the one session and the provider rotates the token, so
	// only the side that refreshed last still works. A copy whose access token
	// has hours left says nothing about that: it went stale the moment the host
	// refreshed after it, and every run until it expired started from a refresh
	// token the provider had already retired.
	home := t.TempDir()
	noKeychain(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".claude"), 0o750))
	hostFile := filepath.Join(home, ".claude", ".credentials.json")
	write := func(path, token string, at time.Time) {
		require.NoError(t, os.WriteFile(path,
			[]byte(fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"expiresAt":%d}}`, token, at.UnixMilli())), 0o600))
	}

	write(hostFile, "host", time.Now().Add(time.Hour))
	path, err := Credentials(context.Background(), home)
	require.NoError(t, err)

	// The container refreshes, and then the host does, later.
	write(path, "container", time.Now().Add(2*time.Hour))
	write(hostFile, "host-again", time.Now().Add(3*time.Hour))

	_, err = Credentials(context.Background(), home)
	require.NoError(t, err)
	assert.Contains(t, string(read(t, path)), "host-again", "the later refresh owns the session")
}

func TestALiveCopyOutlivesAHostWithNoLoginAtAll(t *testing.T) {
	// Reading the host's login is now on the path a good copy takes, so a
	// keychain that will not answer must not cost the container a working token.
	home := t.TempDir()
	noKeychain(t)
	live := time.Now().Add(time.Hour).UnixMilli()
	require.NoError(t, os.MkdirAll(Box(home), 0o700))
	path := filepath.Join(Box(home), ".credentials.json")
	require.NoError(t, os.WriteFile(path,
		[]byte(fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"container","expiresAt":%d}}`, live)), 0o600))

	got, err := Credentials(context.Background(), home)
	require.NoError(t, err)
	assert.Equal(t, path, got)
	assert.Contains(t, string(read(t, path)), "container")
}

func TestAStaleAccessTokenStillOwnsTheSession(t *testing.T) {
	// The reported bug. A copy whose access token ran out hours ago still
	// holds the refresh chain that works, because it refreshed last and the
	// provider voided the host's doing it. Judging it by whether the access
	// token is in date copies the dead token over the live one.
	home := t.TempDir()
	noKeychain(t)
	hostPath := filepath.Join(home, ".claude", ".credentials.json")
	writeLogin(t, hostPath, "host", time.Now().Add(-48*time.Hour))
	path := filepath.Join(Box(home), ".credentials.json")
	writeLogin(t, path, "container", time.Now().Add(-8*time.Hour))

	got, err := Credentials(context.Background(), home)
	require.NoError(t, err)
	assert.Equal(t, path, got)
	assert.Contains(t, string(read(t, path)), "container", "spent access token, live refresh chain")
	assert.Contains(t, string(read(t, hostPath)), "container", "and the host was holding the voided one")
}

func TestAFailedRefreshNeverWinsWhateverTheHostHas(t *testing.T) {
	// expiresAt 0 is Claude Code recording a refresh that failed.
	home := t.TempDir()
	noKeychain(t)
	hostPath := filepath.Join(home, ".claude", ".credentials.json")
	writeLogin(t, hostPath, "host", time.Now().Add(-72*time.Hour))
	path := filepath.Join(Box(home), ".credentials.json")
	require.NoError(t, os.MkdirAll(Box(home), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"claudeAiOauth":{"accessToken":"spent","expiresAt":0}}`), 0o600))

	_, err := Credentials(context.Background(), home)
	require.NoError(t, err)
	assert.Contains(t, string(read(t, path)), "host")
	assert.Contains(t, string(read(t, hostPath)), "host", "the host store is not touched by a dead copy")
}

func TestTheHostStoreIsWrittenAtRunEndNotOnlyAtTheNextStart(t *testing.T) {
	// A write-back only in Credentials repairs the host at the start of the
	// next run, which is after the user has been asked to log in.
	home := t.TempDir()
	noKeychain(t)
	hostPath := filepath.Join(home, ".claude", ".credentials.json")
	writeLogin(t, hostPath, "host", time.Now())
	path := filepath.Join(Box(home), ".credentials.json")
	writeLogin(t, path, "refreshed-mid-run", time.Now().Add(time.Hour))

	SyncCredentials(context.Background(), home)
	assert.Contains(t, string(read(t, hostPath)), "refreshed-mid-run")
}

func TestAnUnchangedLoginIsNotRewritten(t *testing.T) {
	// Credentials runs every turn, and rewriting the store each time would be
	// a Keychain write per turn once that is built.
	home := t.TempDir()
	noKeychain(t)
	hostPath := filepath.Join(home, ".claude", ".credentials.json")
	writeLogin(t, hostPath, "host", time.Now())

	_, err := Credentials(context.Background(), home)
	require.NoError(t, err)
	before, err := os.Stat(hostPath)
	require.NoError(t, err)

	_, err = Credentials(context.Background(), home)
	require.NoError(t, err)
	after, err := os.Stat(hostPath)
	require.NoError(t, err)
	assert.Equal(t, before.ModTime(), after.ModTime(), "nothing changed, so nothing is written")
}

func TestAFailedWriteBackStillYieldsALoginToRunWith(t *testing.T) {
	// Failing here costs the host a login prompt, which is what it did
	// before any of this. It is not a reason to fail the run.
	home := t.TempDir()
	noKeychain(t)
	hostPath := filepath.Join(home, ".claude", ".credentials.json")
	writeLogin(t, hostPath, "host", time.Now().Add(-time.Hour))
	path := filepath.Join(Box(home), ".credentials.json")
	writeLogin(t, path, "container", time.Now())
	// A directory where the lock file goes: the lock cannot be taken.
	require.NoError(t, os.MkdirAll(hostPath+".lock", 0o700))

	got, err := Credentials(context.Background(), home)
	require.NoError(t, err)
	assert.Equal(t, path, got)
	assert.Contains(t, string(read(t, path)), "container", "the run goes ahead on the live copy")
	assert.Contains(t, string(read(t, hostPath)), "host", "and the store is left as it was")
}

func TestTheLaterTokenSurvivesTwoWritersAtOnce(t *testing.T) {
	// Two ade worktrees reach the one host store. Without the lock the older
	// token can land last and void the newer one.
	home := t.TempDir()
	noKeychain(t)
	hostPath := filepath.Join(home, ".claude", ".credentials.json")
	writeLogin(t, hostPath, "host", time.Now().Add(-24*time.Hour))
	require.NoError(t, os.MkdirAll(Box(home), 0o700))

	var wg sync.WaitGroup
	for i, name := range []string{"first", "second", "third", "fourth"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			writeBack(context.Background(), home, storeFile, login(name, time.Now().Add(time.Duration(i)*time.Hour)))
		}()
	}
	wg.Wait()

	assert.Contains(t, string(read(t, hostPath)), "fourth", "the latest refresh owns the session")
}

// login is one credentials file, with expiresAt standing for when it last
// refreshed.
func login(token string, at time.Time) []byte {
	return fmt.Appendf(nil, `{"claudeAiOauth":{"accessToken":%q,"refreshToken":"r","expiresAt":%d}}`, token, at.UnixMilli())
}

func writeLogin(t *testing.T, path, token string, at time.Time) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, login(token, at), 0o600))
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // a path this test just wrote
	require.NoError(t, err)
	return data
}

func TestStartingTakesAwayAnOrphanHoldingTheName(t *testing.T) {
	// The container name is the checkout, so `ade stop` can find it. The cost is
	// that a run whose client died without its container leaves the name taken,
	// and every later start failed with "the container name is already in use" —
	// surfaced to the page as "claude closed", which says nothing useful.
	var removed []string
	old := removeContainer
	removeContainer = func(_ context.Context, bin, name string) {
		removed = append(removed, bin+" rm -f "+name)
	}
	t.Cleanup(func() { removeContainer = old })

	cmd := Command(context.Background(), Docker{}, Spec{Name: "ade-a-checkout", Image: "img"})
	require.NotNil(t, cmd)
	assert.Equal(t, []string{"docker rm -f ade-a-checkout"}, removed)

	// Nothing to clear when there is no name to clear.
	removed = nil
	Command(context.Background(), Docker{}, Spec{Image: "img"})
	assert.Empty(t, removed)
}

func TestRemoveNeedsARuntimeAndAName(t *testing.T) {
	// The startup sweep runs before anything is known to be working, so it has
	// to be safe to call with nothing to call it on.
	var removed []string
	old := removeContainer
	removeContainer = func(_ context.Context, bin, name string) {
		removed = append(removed, bin+" "+name)
	}
	t.Cleanup(func() { removeContainer = old })

	Remove(context.Background(), nil, "ade-a-checkout")
	Remove(context.Background(), Docker{}, "")
	assert.Empty(t, removed, "nothing to remove, nothing removed")

	Remove(context.Background(), Docker{}, "ade-a-checkout")
	assert.Equal(t, []string{"docker ade-a-checkout"}, removed)
}

// A worktree's design document usually lives in the main checkout's .scratch,
// which is outside both the worktree and the git dir. Without a mount of its
// own the agent's write lands in the container and the Design tab never sees
// the document it asked for.
func TestTheDesignDirectoryIsMountedWhereverItIs(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "code", "repo-ticket")
	main := filepath.Join(home, "code", "repo")
	gitDir := filepath.Join(main, ".git")
	store := filepath.Join(home, "watchtower", "repo")
	require.NoError(t, os.MkdirAll(root, 0o750))
	require.NoError(t, os.MkdirAll(gitDir, 0o750))
	require.NoError(t, os.MkdirAll(store, 0o750))
	require.NoError(t, os.Symlink(store, filepath.Join(main, ".scratch")))
	box := Box(home)
	require.NoError(t, os.MkdirAll(box, 0o700))

	spec, err := Claude{Home: home, Root: root, GitDir: gitDir, Docs: filepath.Join(main, ".scratch")}.Spec(box)
	require.NoError(t, err)

	guests := map[string]Mount{}
	for _, m := range spec.Mounts {
		guests[m.Guest] = m
	}
	scratch := filepath.Join(main, ".scratch")
	require.Contains(t, guests, scratch, "bound where the agent was told to write")
	target, err := filepath.EvalSymlinks(store)
	require.NoError(t, err)
	assert.Equal(t, target, guests[scratch].Host, "the symlink's target is what it lands on")
	assert.True(t, guests[scratch].RW, "the agent writes the document")

	// Inside the checkout already, so Root's mount carries it.
	spec, err = Claude{Home: home, Root: root, GitDir: gitDir, Docs: root}.Spec(box)
	require.NoError(t, err)
	assert.NotContains(t, mountedAt(spec), root+"/docs")
	assert.Equal(t, 1, count(spec, root), "not mounted twice")

	// Under the git dir, which is mounted whole.
	docs := filepath.Join(gitDir, "ade", "docs")
	require.NoError(t, os.MkdirAll(docs, 0o750))
	spec, err = Claude{Home: home, Root: root, GitDir: gitDir, Docs: docs}.Spec(box)
	require.NoError(t, err)
	assert.NotContains(t, mountedAt(spec), docs)
}

func mountedAt(spec Spec) []string {
	var guests []string
	for _, m := range spec.Mounts {
		guests = append(guests, m.Guest)
	}
	return guests
}

func count(spec Spec, guest string) int {
	n := 0
	for _, m := range spec.Mounts {
		if m.Guest == guest {
			n++
		}
	}
	return n
}

func TestEachPackageGetsItsOwnNodeModulesInTheContainer(t *testing.T) {
	// The checkout is mounted at its host path, and a native binding is
	// built for one platform: sharing one node_modules breaks whichever of
	// the host and the container installed second.
	home := t.TempDir()
	root := filepath.Join(home, "code", "x")
	pkg := func(dir string) {
		require.NoError(t, os.MkdirAll(dir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o600))
	}
	pkg(root)
	pkg(filepath.Join(root, "web"))
	pkg(filepath.Join(root, "a", "b", "c", "deep"))
	pkg(filepath.Join(root, "web", "node_modules", "dep"))
	require.NoError(t, os.MkdirAll(Box(home), 0o700))

	spec, err := Claude{Home: home, Root: root, Argv: []string{"claude"}}.Spec(Box(home))
	require.NoError(t, err)

	guests := map[string]Mount{}
	for _, m := range spec.Mounts {
		guests[m.Guest] = m
	}
	for _, dir := range []string{root, filepath.Join(root, "web")} {
		guest := filepath.Join(dir, "node_modules")
		m, ok := guests[guest]
		require.True(t, ok, "a package's node_modules is the container's own: %s", guest)
		assert.True(t, m.RW, "the container installs into it")
		assert.True(t, within(m.Host, Cache(home)), "kept in the cache, not the checkout: %s", m.Host)
		assert.DirExists(t, m.Host, "made so the mount has a source")
	}
	_, deep := guests[filepath.Join(root, "a", "b", "c", "deep", "node_modules")]
	assert.False(t, deep, "the search is bounded")
	_, dep := guests[filepath.Join(root, "web", "node_modules", "dep", "node_modules")]
	assert.False(t, dep, "a dependency's own package.json is not the checkout's")
}

func TestTheSameCheckoutKeepsItsNodeModulesBetweenRuns(t *testing.T) {
	home := t.TempDir()
	one := filepath.Join(home, "code", "x", "web", "node_modules")
	two := filepath.Join(home, "code", "y", "web", "node_modules")

	assert.Equal(t, NodeModulesDir(home, one), NodeModulesDir(home, one),
		"an install survives the container it was made in")
	assert.NotEqual(t, NodeModulesDir(home, one), NodeModulesDir(home, two),
		"two checkouts with a web/node_modules do not collide")
}

// fakeKeychain stands in for the Mac store: readable, writable, and never the
// real one. It returns what the store holds, for a test to assert on.
func fakeKeychain(t *testing.T, held []byte) *[]byte {
	t.Helper()
	oldRead, oldOS := readKeychain, onDarwin
	oldAcct, oldWrite := keychainAccount, writeKeychain
	onDarwin = true
	store := &held
	readKeychain = func(context.Context) ([]byte, error) {
		if len(*store) == 0 {
			return nil, errors.New("no keychain entry")
		}
		return *store, nil
	}
	keychainAccount = func(context.Context) (string, error) { return "owen", nil }
	writeKeychain = func(_ context.Context, account string, data []byte) error {
		assert.Equal(t, "owen", account, "the acct the item already carries")
		*store = data
		return nil
	}
	t.Cleanup(func() {
		readKeychain, onDarwin = oldRead, oldOS
		keychainAccount, writeKeychain = oldAcct, oldWrite
	})
	return store
}

func TestTheKeychainTakesTheLoginTheContainerRefreshed(t *testing.T) {
	// The reported bug on a Mac: the container refreshes, the provider voids
	// the Keychain's refresh token, and the next `claude` in a terminal asks
	// for a login. Nothing repaired the Keychain until this.
	home := t.TempDir()
	store := fakeKeychain(t, login("host", time.Now().Add(-8*time.Hour)))
	path := filepath.Join(Box(home), ".credentials.json")
	writeLogin(t, path, "container", time.Now())

	got, err := Credentials(context.Background(), home)
	require.NoError(t, err)
	assert.Equal(t, path, got)
	assert.Contains(t, string(*store), "container", "the keychain now holds the live refresh chain")
	assert.NoFileExists(t, hostFile(home), "never as a substitute for the keychain")
}

func TestTheKeychainIsWrittenAtRunEndToo(t *testing.T) {
	home := t.TempDir()
	store := fakeKeychain(t, login("host", time.Now()))
	writeLogin(t, filepath.Join(Box(home), ".credentials.json"), "refreshed-mid-run", time.Now().Add(time.Hour))

	SyncCredentials(context.Background(), home)
	assert.Contains(t, string(*store), "refreshed-mid-run")
}

func TestAnUnchangedKeychainIsNotRewritten(t *testing.T) {
	// Credentials runs every turn; a Keychain write per turn is not free.
	home := t.TempDir()
	fakeKeychain(t, login("host", time.Now()))
	writes := 0
	old := writeKeychain
	writeKeychain = func(ctx context.Context, account string, data []byte) error {
		writes++
		return old(ctx, account, data)
	}
	t.Cleanup(func() { writeKeychain = old })

	for range 2 {
		_, err := Credentials(context.Background(), home)
		require.NoError(t, err)
	}
	assert.Zero(t, writes, "nothing changed, so nothing is written")
}

func TestAFailedKeychainWriteStillYieldsALoginToRunWith(t *testing.T) {
	home := t.TempDir()
	fakeKeychain(t, login("host", time.Now().Add(-time.Hour)))
	writeKeychain = func(context.Context, string, []byte) error {
		return errors.New("keychain says no")
	}
	path := filepath.Join(Box(home), ".credentials.json")
	writeLogin(t, path, "container", time.Now())

	got, err := Credentials(context.Background(), home)
	require.NoError(t, err)
	assert.Equal(t, path, got)
	assert.Contains(t, string(read(t, path)), "container", "the run goes ahead on the live copy")
	assert.NoFileExists(t, hostFile(home), "a failed keychain write never falls back to plaintext")
}

func TestTheKeychainAccountIsReadFromTheItem(t *testing.T) {
	dump := "keychain: \"login.keychain-db\"\nattributes:\n    \"acct\"<blob>=\"owen\"\n    \"svce\"<blob>=\"Claude Code-credentials\"\n"
	match := keychainAcct.FindStringSubmatch(dump)
	require.NotNil(t, match, "the acct attribute is what a write must reuse")
	assert.Equal(t, "owen", match[1])
}

func TestACPULimitIsPassedToBothRuntimes(t *testing.T) {
	spec := Spec{Image: "image", Name: "agent"}
	assert.Equal(t, "run -i --rm --name agent --cpus 5 image", strings.Join(Docker{CPUs: 5}.Args(spec), " "))
	assert.Equal(t, "run -i --rm --name agent --cpus 5 image", strings.Join(Apple{CPUs: 5}.Args(spec), " "))
	assert.NotContains(t, Docker{}.Args(spec), "--cpus")
}

func TestAppleBoxesAreGivenMoreThanTheDefaultGigabyte(t *testing.T) {
	spec := Spec{Image: "image", Name: "agent"}
	assert.Equal(t, "run -i --rm --name agent --memory 4G image", strings.Join(Apple{Memory: "4G"}.Args(spec), " "))
	assert.NotContains(t, Apple{}.Args(spec), "--memory")
}

// The two runtimes spell the question differently, and asking the wrong one
// reads as "no image" on a machine that has it.
func TestEachRuntimeAsksForAnImageItsOwnWay(t *testing.T) {
	assert.Equal(t, []string{"image", "inspect", "ade-agent:latest"},
		Docker{}.HasArgs("ade-agent:latest"))
	assert.Equal(t, []string{"images", "inspect", "ade-agent:latest"},
		Apple{}.HasArgs("ade-agent:latest"))
}
