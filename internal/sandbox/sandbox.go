// Package sandbox runs the agent in a container rather than on the host: the
// checkout mounted, the shell inside, a bad `rm` a bad day for the container.
// Two runtimes, one shape — Apple's `container` on macOS 26 and Docker
// everywhere — both taking the same Spec and handing back a command whose
// pipes the bridge already reads.
package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/spawncap"
)

// Spec is one container: what runs, where, with what mounted.
type Spec struct {
	Image string
	// Name is the container's name, so a run the Go side gives up on can be
	// stopped by it rather than left running.
	Name    string
	Workdir string
	Mounts  []Mount
	Env     map[string]string
	// Argv runs in the container; Args go after it, for what the spec has
	// to say about the command line (an MCP config in the box, say) that
	// whoever sets Argv does not know.
	Argv []string
	Args []string
	// TTY asks for a terminal: a person drives what runs, not a stream.
	TTY bool
}

// Mount binds a host path into the container at Guest — the same path,
// nearly always, so what the agent prints and what the page shows agree.
type Mount struct {
	Host  string
	Guest string
	RW    bool
}

// Reachable keeps the paths the container has a mount for. A --add-dir on one
// it has not fails the run before the agent says anything.
func Reachable(spec Spec, dirs []string) []string {
	var out []string
	for _, dir := range dirs {
		for _, m := range spec.Mounts {
			if within(dir, m.Guest) {
				out = append(out, dir)
				break
			}
		}
	}
	return out
}

// Runtime is a container engine the CLI drives.
type Runtime interface {
	// Name is what the page and the logs call it.
	Name() string
	// Args is the command line that runs spec, without the binary.
	Args(spec Spec) []string
	// Bin is the binary.
	Bin() string
	// HasArgs asks whether the image is already on this machine. The two
	// runtimes spell it differently, and a pull is what fails without a login.
	HasArgs(image string) []string
}

// HasImage is whether the runtime already holds the image. A false is not a
// refusal: the run pulls. It is how ade says which of the two it is doing.
func HasImage(ctx context.Context, rt Runtime, image string) bool {
	look, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return exec.CommandContext(look, rt.Bin(), rt.HasArgs(image)...).Run() == nil // #nosec G204 -- runtime's own verbs
}

// Docker is `docker run`; OrbStack, Docker Desktop and the Linux daemon all
// answer to it.
type Docker struct {
	Socket string
	// Network is docker run --network. On a worker it is container:ade-hub, so
	// the agent's loopback is the hub's and 127.0.0.1:4867 needs no rewrite.
	Network string
	// CPUs is docker run --cpus: what one agent may take. Zero is no limit.
	CPUs int
	// User is --user as uid:gid. Empty keeps the image's root.
	User string
	// Groups is --group-add: the socket's group, which User is not in.
	Groups []string
}

// envHubContainer names the hub container a worker's agents share a network
// namespace with. Set on the worker by StartWorker; empty on a laptop.
const envHubContainer = "ADE_HUB_CONTAINER"

// Apple is Apple's `container` CLI, a VM per container, on macOS 26 and up.
type Apple struct {
	// CPUs is container run --cpus, the size of the VM. Zero is its default.
	CPUs int
	// Memory is container run --memory. Empty is its default, which is 1G.
	Memory string
	// Host is the machine's address on the container network; no name resolves
	// to it the way host.docker.internal does.
	Host string
}

// DefaultImage is the image the agent runs in: published from
// sandbox/Dockerfile by the sandbox-image workflow, and pulled by the
// runtime on first use. `make sandbox-image` builds the same Dockerfile
// under the same name, and a local build is what the runtime uses when
// there is one, so a change to the image is tried here before it is
// published.
const DefaultImage = "ghcr.io/owenrumney/ade-agent:latest"

// DockerSocket is the host daemon the sandbox's CLI drives. There is no
// daemon in the agent image.
const DockerSocket = "/var/run/docker.sock"

// appleMemory is per box: at Apple's 1G an agent thrashes re-reading files.
const appleMemory = "4G"

// stopGrace is how long a stop gets before the process is killed anyway.
const stopGrace = 10 * time.Second

// Detect picks the runtime: Apple's when its CLI is there and its services
// are up, otherwise Docker, otherwise nothing. lookPath and run stand in for
// exec in tests.
func Detect(ctx context.Context) (Runtime, error) {
	rt, err := detect(ctx, exec.LookPath, func(ctx context.Context, name string, args ...string) error {
		return exec.CommandContext(ctx, name, args...).Run() // #nosec G204 -- fixed runtime probes
	})
	if _, ok := rt.(Docker); ok {
		return DockerRuntime(ctx), err
	}
	if _, ok := rt.(Apple); ok {
		return Apple{CPUs: spawncap.Jobs(), Memory: appleMemory, Host: appleGateway(ctx)}, err
	}
	return rt, err
}

// DockerRuntime is Docker as Detect hands it back, for a fallback. nil when
// there is no docker on PATH.
func DockerRuntime(ctx context.Context) Runtime {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil
	}
	docker := Docker{Socket: resolveDockerSocket(ctx)}
	if hub := strings.TrimSpace(os.Getenv(envHubContainer)); hub != "" {
		docker.Network = "container:" + hub
	}
	docker.CPUs = min(spawncap.Jobs(), dockerCPUs(ctx))
	docker.User, docker.Groups = hostUser(docker.Socket)
	return docker
}

// dockerCPUs is how many the daemon has, which on a Mac is its VM and not the
// host: asking for more fails the run. Zero when it will not say.
func dockerCPUs(ctx context.Context) int {
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probe, "docker", "info", "--format", "{{.NCPU}}").Output()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// appleGateway is the host's address on the default container network.
func appleGateway(ctx context.Context) string {
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probe, "container", "network", "inspect", "default").Output()
	if err != nil {
		slog.Debug("could not read the container network", "err", err)
		return ""
	}
	return parseAppleGateway(out)
}

func parseAppleGateway(out []byte) string {
	var nets []struct {
		Status struct {
			IPv4Gateway string `json:"ipv4Gateway"`
		} `json:"status"`
	}
	if err := json.Unmarshal(out, &nets); err != nil || len(nets) == 0 {
		return ""
	}
	return nets[0].Status.IPv4Gateway
}

func cpuArgs(args []string, cpus int) []string {
	if cpus <= 0 {
		return args
	}
	return append(args, "--cpus", strconv.Itoa(cpus))
}

func detect(ctx context.Context, lookPath func(string) (string, error), run func(context.Context, string, ...string) error) (Runtime, error) {
	if _, err := lookPath("container"); err == nil {
		probe, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		err := run(probe, "container", "system", "status")
		if err == nil {
			slog.Debug("runtime detected", "runtime", "container")
			return Apple{}, nil
		}
		slog.Debug("container is on PATH but not running", "err", err)
	}
	if _, err := lookPath("docker"); err == nil {
		slog.Debug("runtime detected", "runtime", "docker")
		return Docker{}, nil
	}
	slog.Warn("no container runtime found; the sandbox is unavailable")
	return nil, errors.New("no container runtime: install Docker, or Apple's container on macOS 26")
}

// Command is the container as a process: stdin, stdout and stderr are the
// agent's, and cancelling the context stops the container by name before
// the client is killed, so nothing is left running with the checkout mounted.
func Command(ctx context.Context, rt Runtime, spec Spec) *exec.Cmd {
	clearName(ctx, rt, spec.Name)
	if _, apple := rt.(Apple); apple {
		staged, err := StageFiles(spec, stagingRoot(spec.Name))
		if err != nil {
			slog.Warn("could not stage the file mounts; the run will refuse them", "err", err)
		} else {
			spec = staged
		}
	}
	cmd := exec.CommandContext(ctx, rt.Bin(), rt.Args(spec)...) // #nosec G204 -- runtime and spec built here
	slog.Info("container", "runtime", rt.Name(), "image", spec.Image, "name", spec.Name,
		"workdir", spec.Workdir, "mounts", len(spec.Mounts), "env", len(spec.Env))
	slog.Debug("container argv", "argv", strings.Join(redactEnv(rt.Args(spec)), " "))
	cmd.Cancel = func() error {
		stop, cancel := context.WithTimeout(context.Background(), stopGrace)
		defer cancel()
		slog.Info("stopping the container", "name", spec.Name)
		_ = exec.CommandContext(stop, rt.Bin(), "stop", spec.Name).Run() // #nosec G204 -- fixed
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = stopGrace
	return cmd
}

func (Docker) Name() string { return "docker" }
func (Docker) Bin() string  { return "docker" }

func (Docker) HasArgs(image string) []string { return []string{"image", "inspect", image} }

// Args is docker run: interactive for the stream on stdin, removed when it
// exits, and on Linux the host's ids so files it writes are yours.
func (d Docker) Args(spec Spec) []string {
	args := cpuArgs(runArgs(spec), d.CPUs)
	if d.User != "" {
		args = append(args, "--user", d.User)
		for _, gid := range d.Groups {
			args = append(args, "--group-add", gid)
		}
	}
	if d.Network != "" {
		args = append(args, "--network", d.Network)
	}
	// No socket is a daemon reached over tcp or ssh: binding a path that is not
	// one makes docker create a directory and every command inside fail oddly.
	if d.Socket != "" {
		args = append(args, "-v", d.Socket+":"+DockerSocket+":rw")
	}
	for _, m := range spec.Mounts {
		mode := "ro"
		if m.RW {
			mode = "rw"
		}
		args = append(args, "-v", fmt.Sprintf("%s:%s:%s", m.Host, m.Guest, mode))
	}
	for _, kv := range envArgs(spec.Env) {
		args = append(args, "-e", kv)
	}
	if d.Socket != "" {
		args = append(args, "-e", "DOCKER_HOST=unix://"+DockerSocket)
	}
	if spec.Workdir != "" {
		args = append(args, "-w", spec.Workdir)
	}
	args = append(args, spec.Image)
	args = append(args, spec.Argv...)
	return append(args, spec.Args...)
}

// resolveDockerSocket is the active daemon's unix socket, or "" for a tcp or
// ssh endpoint, which has no path to bind.
func resolveDockerSocket(ctx context.Context) string {
	if endpoint := strings.TrimSpace(os.Getenv("DOCKER_HOST")); endpoint != "" {
		return unixSocket(endpoint)
	}
	probe, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probe, "docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return DockerSocket
	}
	return unixSocket(string(out))
}

func unixSocket(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if !strings.HasPrefix(endpoint, "unix://") {
		return ""
	}
	return strings.TrimPrefix(endpoint, "unix://")
}

func (Apple) Name() string { return "container" }
func (Apple) Bin() string  { return "container" }

func (Apple) HasArgs(image string) []string { return []string{"images", "inspect", image} }

// Args is container run, which takes docker's flags for everything here.
func (a Apple) Args(spec Spec) []string {
	args := cpuArgs(runArgs(spec), a.CPUs)
	if a.Memory != "" {
		args = append(args, "--memory", a.Memory)
	}
	for _, m := range spec.Mounts {
		mount := fmt.Sprintf("type=bind,source=%s,target=%s", m.Host, m.Guest)
		if !m.RW {
			mount += ",readonly"
		}
		args = append(args, "--mount", mount)
	}
	for _, kv := range envArgs(spec.Env) {
		args = append(args, "-e", kv)
	}
	if spec.Workdir != "" {
		args = append(args, "-w", spec.Workdir)
	}
	args = append(args, spec.Image)
	args = append(args, spec.Argv...)
	return append(args, spec.Args...)
}

// envArgs is the environment as KEY=value, in a fixed order so a command
// line is the same twice.
// redactEnv hides every -e value: spec.Env carries NODE_AUTH_TOKEN, and the
// ring this lands in is served to the browser.
func redactEnv(args []string) []string {
	out := append([]string(nil), args...)
	for i := 1; i < len(out); i++ {
		if out[i-1] != "-e" {
			continue
		}
		if key, _, ok := strings.Cut(out[i], "="); ok {
			out[i] = key + "=***"
		}
	}
	return out
}

func envArgs(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// removeGrace is how long the runtime gets to take an orphan away.
const removeGrace = 10 * time.Second

// clearName takes away a container already holding the name this run wants.
//
// The name is the checkout, deliberately, so that `ade stop` can find the
// container without being told which one. The cost is that a run whose client
// died without its container — a killed process, a resume that failed partway,
// a server restart mid-turn — leaves the name taken, and every later start
// fails with "the container name is already in use". That surfaced as
// "claude closed", which says nothing about what to do.
//
// Reaching here means this server is starting a run, and it runs one agent per
// checkout, so a container under this name is from a run that is over whether
// it knows it or not. Errors are ignored: if there was nothing to remove, the
// run below is the thing that reports the real problem.
//
// removeContainer stands in for exec in tests.
var removeContainer = func(ctx context.Context, bin, name string) {
	kill, cancel := context.WithTimeout(ctx, removeGrace)
	defer cancel()
	_ = exec.CommandContext(kill, bin, "rm", "-f", name).Run() // #nosec G204 -- fixed verb, name built here
}

// Remove takes away the container called name, if there is one. Exported for
// the sweep a server does at startup; Command does the same thing for the run
// it is about to start.
func Remove(ctx context.Context, rt Runtime, name string) {
	if rt == nil || name == "" {
		return
	}
	removeContainer(ctx, rt.Bin(), name)
}

// StageFiles rewrites file mounts as directory ones, because Apple's runtime
// binds directories only. Files of one guest directory are copied into one.
func StageFiles(spec Spec, root string) (Spec, error) {
	covered := map[string]bool{}
	var out []Mount
	byDir := map[string][]Mount{}
	for _, m := range spec.Mounts {
		info, err := os.Stat(m.Host)
		switch {
		case err != nil || info.IsDir():
			// A path that cannot be read is the runtime's complaint to make.
			covered[m.Guest] = true
			out = append(out, m)
		default:
			guestDir := filepath.Dir(m.Guest)
			byDir[guestDir] = append(byDir[guestDir], m)
		}
	}
	if len(byDir) == 0 {
		return spec, nil
	}
	if err := os.RemoveAll(root); err != nil {
		return spec, err
	}
	for _, guestDir := range sortedKeys(byDir) {
		// Already mounted whole: staging over it would hide what is there.
		if covered[guestDir] {
			out = append(out, byDir[guestDir]...)
			continue
		}
		staged := filepath.Join(root, strings.ReplaceAll(strings.Trim(guestDir, string(filepath.Separator)), string(filepath.Separator), "_"))
		if err := os.MkdirAll(staged, 0o700); err != nil {
			return spec, err
		}
		for _, m := range byDir[guestDir] {
			if err := copyFile(m.Host, filepath.Join(staged, filepath.Base(m.Guest))); err != nil {
				return spec, err
			}
		}
		out = append(out, Mount{Host: staged, Guest: guestDir})
	}
	spec.Mounts = out
	return spec, nil
}

// stagingRoot is where one container's staged files go, wiped at each start.
func stagingRoot(name string) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "ade", "mounts", name)
}

func copyFile(from, to string) error {
	info, err := os.Stat(from)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(from) // #nosec G304 -- a path ade already mounts
	if err != nil {
		return err
	}
	return os.WriteFile(to, data, info.Mode().Perm())
}

func sortedKeys(set map[string][]Mount) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func clearName(ctx context.Context, rt Runtime, name string) {
	Remove(ctx, rt, name)
}

// ContainerName is a name a runtime accepts, from the checkout's path: one
// container per checkout, and a second start with the first still running
// is a name clash rather than two agents on one tree.
// ColdSlots bounds the cold names, so a startup sweep can name them all.
const ColdSlots = 8

// ColdContainerName is an isolated task beside the main agent. slot keeps
// concurrent ones apart: Command takes a name from whoever holds it.
func ColdContainerName(root string, slot int) string {
	suffix := fmt.Sprintf("-cold%d", ((slot%ColdSlots)+ColdSlots)%ColdSlots)
	name := ContainerName(root)
	if len(name)+len(suffix) > 63 {
		name = name[:63-len(suffix)]
	}
	return name + suffix
}

func ContainerName(root string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(root) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	if len(name) > 56 {
		name = strings.Trim(name[len(name)-56:], "-")
	}
	return "ade-" + name
}

func runArgs(spec Spec) []string {
	args := []string{"run", "-i"}
	if spec.TTY {
		args = append(args, "-t")
	}
	return append(args, "--rm", "--name", spec.Name)
}

// TUIContainerName is apart from the agent it replaces, which may still be stopping.
func TUIContainerName(root string) string {
	const suffix = "-tui"
	name := ContainerName(root)
	if len(name)+len(suffix) > 63 {
		name = name[:63-len(suffix)]
	}
	return name + suffix
}
