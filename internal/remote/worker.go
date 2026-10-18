package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/session"
)

const (
	// WorkerImageEnv names an image shipped to a host by hand instead of
	// pulled. Design: .git/ade/docs/shipping-a-worker.md.
	WorkerImageEnv     = "ADE_WORKER_IMAGE"
	defaultWorkerImage = "ghcr.io/owenrumney/ade-worker:latest"
	workerName         = "ade-worker"
)

// WorkerImage is the image the worker runs.
var WorkerImage = func() string {
	if named := strings.TrimSpace(os.Getenv(WorkerImageEnv)); named != "" {
		return named
	}
	return defaultWorkerImage
}()

// shipped is whether the image was carried to the host rather than published.
// Pulling one would fail, or worse, replace it with the registry's older copy.
func shipped() bool { return WorkerImage != defaultWorkerImage }

// workerWait bounds waiting for a started worker to answer on its published
// port. A var so a test can shorten it rather than sleep through the real one.
var workerWait = 10 * time.Second

// RefreshImages pulls the images this release runs and says whether the worker
// over there is behind. It restarts nothing: that stops sessions.
func (h *Host) RefreshImages(ctx context.Context, roots Roots, github Login) (bool, error) {
	if shipped() {
		h.stale = nil
		return !h.stampedWith(ctx, workerName, h.imageID(ctx, WorkerImage)), nil
	}
	h.loginRegistry(ctx, roots, github)
	if err := h.pullImage(ctx, roots, WorkerImage); err != nil {
		h.stale = err
		return false, err
	}
	h.stale = nil
	// The hub's own pull is not worth failing an update over: its container is
	// replaced on the next connect, and no session lives in it.
	_ = h.pullImage(ctx, roots, HubImage)
	stamp := h.imageID(ctx, WorkerImage)
	return !h.stampedWith(ctx, workerName, stamp), nil
}

// StartWorker adopts the worker when it is already serving, or replaces a
// stopped container with the image selected by this ADE release.
func (h *Host) StartWorker(ctx context.Context, roots Roots, port int, github Login) (string, error) {
	// Pull first, and with the credential: the image is private, and docker run
	// would otherwise reuse a :latest from whenever it last pulled.
	if !shipped() {
		h.loginRegistry(ctx, roots, github)
		h.stale = h.pullImage(ctx, roots, WorkerImage)
	}
	stamp := h.imageID(ctx, WorkerImage)
	// Decided once: it says whether the worker runs as the ssh user, and so
	// whether a root-owned state root is a fault or simply how this host works.
	as := h.hostUser(ctx, roots)
	if found := h.workerURL(ctx, port, roots); found != "" && h.workerTrustsCheckouts(ctx) &&
		h.ownsItsHome(ctx, roots, as) && h.stampedWith(ctx, workerName, stamp) {
		h.nameTheWorkerUser(ctx, roots, as)
		return found, nil
	}
	// Before the run, not only in StartHub: the worker joins this network and
	// starts first, so creating it there is too late.
	if err := h.ensureNetwork(ctx); err != nil {
		return "", err
	}
	_, _ = h.Run(ctx, "docker", "rm", "-f", workerName)
	env := []byte{}
	if github.Token != "" {
		env = []byte("GH_TOKEN=" + github.Token + "\n")
	}
	// Published to loopback rather than --network host: Docker Desktop's host
	// networking on macOS is beta, and -p works everywhere.
	published := fmt.Sprintf("127.0.0.1:%d:%d", port, port)
	argv := []string{"docker", "run", "-d", "--name", workerName,
		"--init", "-p", published,
		"--label", imageLabel + "=" + stamp,
		"--network", networkName,
		"--env-file", "/dev/stdin",
		"-e", "ADE_HUB_CONTAINER=" + hubName,
		"-e", "ADE_WORKER_NAME=" + workerName,
		"-e", "GIT_CONFIG_COUNT=1", "-e", "GIT_CONFIG_KEY_0=safe.directory", "-e", "GIT_CONFIG_VALUE_0=*",
		"-e", "DOCKER_CONFIG=" + dockerConfig(roots),
		"-e", "ADE_BIND=0.0.0.0",
		"-e", "HOME=" + workerHome(roots), "-e", "ADE_SCAN_DIR=" + roots.ScanRoot(),
		// Where a session's clone lands. Not the scan root: on a machine with
		// its own ~/code that is the user's, and clones pile up beside it.
		"-e", "ADE_WORK_DIR=" + roots.Work,
		"-e", "ADE_CREDENTIAL_ROOT=" + roots.Run + "/worker",
		"-v", roots.Work + ":" + roots.Work, "-v", roots.Run + ":" + roots.Run,
		"-v", roots.Socket + ":/var/run/docker.sock"}
	// A scan root outside Work is the user's own checkouts, and the worker
	// cannot offer what it cannot see.
	if scan := roots.ScanRoot(); scan != roots.Work {
		argv = append(argv, "-v", scan+":"+scan)
	}
	// The ids the editor on this host runs as: who may read a record, and who a
	// sandbox writes as. Design: .git/ade/docs/sandbox-file-ownership.md.
	if as.gid != "" {
		argv = append(argv, "-e", session.HostGroupEnv+"="+as.gid)
	}
	if as.uid != "" {
		argv = append(argv, "-e", session.HostUserEnv+"="+as.uid)
	}
	argv = append(argv, h.asHostUser(ctx, roots, as)...)
	argv = append(argv, WorkerImage, "ade", "launch", "--foreground", "--no-open", "--idle", "0", "--port", strconv.Itoa(port))
	_, err := h.RunInput(ctx, env, argv...)
	// The rm above returned before docker released the name. Wait for it and
	// run once more rather than hand the user a conflict they cannot act on.
	if nameInUse(err) && h.freeName(ctx, workerName) {
		_, err = h.RunInput(ctx, env, argv...)
	}
	if err != nil {
		if nameInUse(err) {
			return "", fmt.Errorf("%s still holds a container called %s that docker will not remove: %w — "+
				"run `docker rm -f %s` there, and if that hangs, restart its docker daemon",
				h.Name, workerName, err, workerName)
		}
		if !h.hasWorkerImage(ctx) {
			return "", fmt.Errorf("%s has no %s and could not pull one: %w — build it there, or publish the image",
				h.Name, WorkerImage, err)
		}
		return "", fmt.Errorf("start worker on %s: %w", h.Name, err)
	}

	deadline := time.Now().Add(workerWait)
	for time.Now().Before(deadline) {
		if found := h.workerURL(ctx, port, roots); found != "" {
			h.nameTheWorkerUser(ctx, roots, as)
			return found, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return "", fmt.Errorf("the worker on %s is running but port %d does not reach it; "+
		"something else on that machine is already on %d, or the image predates ADE_BIND — "+
		"check with lsof -nP -iTCP:%d and docker logs %s",
		h.Name, port, port, port, workerName)
}

// workerURL is what a running worker printed, once that url answers on the
// port asked for. A worker left on another port is forwarded to the wrong one.
func (h *Host) workerURL(ctx context.Context, port int, roots Roots) string {
	if !h.workerRunning(ctx) {
		return ""
	}
	want := strconv.Itoa(port)
	out, err := h.Run(ctx, "docker", "logs", workerName)
	if err != nil {
		return ""
	}
	for _, line := range reverse(strings.Split(strings.TrimSpace(string(out)), "\n")) {
		candidate := strings.TrimSpace(line)
		parsed, err := url.Parse(candidate)
		if err == nil && parsed.Scheme == "http" && parsed.Hostname() == "127.0.0.1" && parsed.Port() != "" {
			if parsed.Port() != want || !h.workerAnswers(ctx, want, roots) {
				return ""
			}
			return candidate
		}
	}
	// The line was printed once and a rotated log no longer holds it. Ask the
	// port instead of destroying a worker that is serving the session.
	if token := h.workerToken(ctx, want, roots); token != "" {
		return "http://127.0.0.1:" + want + "/?t=" + token
	}
	return ""
}

// workerToken is the running worker's session token, read from inside the
// container: the url it printed is gone, but the token it prints it with is not.
func (h *Host) workerToken(ctx context.Context, port string, roots Roots) string {
	if !h.workerAnswers(ctx, port, roots) {
		return ""
	}
	out, err := h.Run(ctx, "docker", "exec", workerName,
		"curl", "-s", "--max-time", "3", "http://127.0.0.1:"+port+"/api/session")
	if err != nil {
		return ""
	}
	var said struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(sessionJSON(out), &said) != nil {
		return ""
	}
	return said.Token
}

// sessionJSON is the object in a reply, ignoring whatever the transport wrapped
// it in.
func sessionJSON(out []byte) []byte {
	from := bytes.IndexByte(out, '{')
	to := bytes.LastIndexByte(out, '}')
	if from < 0 || to < from {
		return nil
	}
	return out[from : to+1]
}

// workerAnswers reports the published port reaching the process inside. Run on
// the worker host, because that is the side the forward is taken from.
func (h *Host) workerAnswers(ctx context.Context, port string, roots Roots) bool {
	out, err := h.Run(ctx, "curl", "-s", "--max-time", "3",
		"http://127.0.0.1:"+port+"/healthz")
	if err != nil {
		return false
	}
	// The worker's state root is under its own HOME. Anything else answering
	// is the host's own launcher, which -p loses to without failing.
	return strings.Contains(string(out), workerHome(roots))
}

// workerHome is the HOME the worker container is given, which its /healthz
// reports as the state root and nothing on the host shares.
func workerHome(roots Roots) string { return roots.Work + "/.worker" }

// namePasswdEntry adds the id the worker runs as to the container's own passwd
// and group files, when it is not there already.
const namePasswdEntry = `set -eu
uid=$1; gid=$2; home=$3
getent group "$gid" >/dev/null 2>&1 || echo "ade:x:$gid:" >> /etc/group
getent passwd "$uid" >/dev/null 2>&1 || echo "ade:x:$uid:$gid:ade:$home:/bin/sh" >> /etc/passwd`

// nameTheWorkerUser gives the uid a passwd entry. The image cannot: the id is
// the ssh user's and is only known here, at the run. ssh-keygen calls
// getpwuid() and dies without one, which fails every signed commit with
// "No user exists for uid". Design: .git/ade/docs/fix remote.md.
func (h *Host) nameTheWorkerUser(ctx context.Context, roots Roots, as hostUser) {
	if !as.ok {
		return
	}
	// -u 0: the container runs as the unnamed id, which cannot write /etc.
	if _, err := h.Run(ctx, "docker", "exec", "-u", "0:0", workerName,
		"sh", "-c", namePasswdEntry, "sh", as.uid, as.gid, workerHome(roots)); err != nil {
		slog.Warn("the worker's id has no name; a signed commit there will fail",
			"host", h.Name, "uid", as.uid, "err", err)
	}
}

// ownsItsHome reports the worker's state root belonging to the id it runs as.
// A worker that stays root owns everything, so it is never asked.
func (h *Host) ownsItsHome(ctx context.Context, roots Roots, as hostUser) bool {
	if !as.ok {
		return true
	}
	out, err := h.Run(ctx, "sh", "-c",
		`[ -d "$1" ] || exit 0; stat -c %u "$1" 2>/dev/null || stat -f %u "$1"`, "sh", workerHome(roots))
	if err != nil {
		return true
	}
	owner := strings.TrimSpace(string(out))
	return owner == "" || owner == as.uid
}

// workerTrustsCheckouts is whether a running worker has git's safe.directory
// set. One without it fails every open, so it is replaced rather than adopted.
func (h *Host) workerTrustsCheckouts(ctx context.Context) bool {
	out, err := h.Run(ctx, "docker", "inspect", "-f", "{{range .Config.Env}}{{println .}}{{end}}", workerName)
	return err == nil && strings.Contains(string(out), "GIT_CONFIG_KEY_0=safe.directory")
}

func (h *Host) workerRunning(ctx context.Context) bool {
	out, err := h.Run(ctx, "docker", "inspect", "-f", "{{.State.Running}}", workerName)
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// hasWorkerImage reports the image already on the worker. A machine that
// cannot pull it fails at the run, and the reason is worth naming once.
func (h *Host) hasWorkerImage(ctx context.Context) bool {
	_, err := h.Run(ctx, "docker", "image", "inspect", WorkerImage)
	return err == nil
}

// StopWorker takes the worker and its hub down. Their state is on the mounted
// roots, so the next connect starts clean rather than losing anything.
func (h *Host) StopWorker(ctx context.Context) error {
	if _, err := h.Run(ctx, "docker", "rm", "-f", workerName, hubName); err != nil {
		return fmt.Errorf("stop worker on %s: %w", h.Name, err)
	}
	return nil
}

// group is the ssh user's own group on the host, as a number, or empty when
// the host would not say. Empty leaves the record private, as it was.
func (h *Host) group(ctx context.Context) string { return h.id(ctx, "-g") }

func (h *Host) user(ctx context.Context) string { return h.id(ctx, "-u") }

func (h *Host) id(ctx context.Context, flag string) string {
	out, err := h.Run(ctx, "id", flag)
	if err != nil {
		return ""
	}
	id := strings.TrimSpace(string(out))
	if _, err := strconv.Atoi(id); err != nil {
		return ""
	}
	return id
}

// hostUser is the id the worker runs as. Not ok means root, the fallback
// whenever the ssh user could not run a worker that works.
type hostUser struct {
	uid, gid, group string
	ok              bool
}

// hostUser decides it, once per start: every other question about ownership
// hangs off the answer. Design: .git/ade/docs/sandbox-file-ownership.md.
func (h *Host) hostUser(ctx context.Context, roots Roots) hostUser {
	as := hostUser{uid: h.user(ctx), gid: h.group(ctx)}
	if as.uid == "" || as.gid == "" {
		return as
	}
	// No socket group means no daemon, and no worker at all is the worse trade.
	as.group = h.socketGroup(ctx, roots.Socket)
	if as.group == "" || !h.inGroup(ctx, as.group) {
		slog.Warn("the worker stays root: its ssh user is not in the docker socket's group",
			"host", h.Name, "socket", roots.Socket)
		return as
	}
	as.ok = true
	return as
}

// asHostUser runs the worker as the ssh user rather than as root.
func (h *Host) asHostUser(ctx context.Context, roots Roots, as hostUser) []string {
	if !as.ok {
		return nil
	}
	// The migration decides it: a worker switched to an id that cannot read
	// what root left reaches nothing, and staying root still works.
	if err := h.handRootsOver(ctx, roots, as); err != nil {
		slog.Warn("the worker stays root: the root era could not be handed over",
			"host", h.Name, "err", err)
		return nil
	}
	return []string{"--user", as.uid + ":" + as.gid, "--group-add", as.group}
}

// socketGroup is the gid owning the docker socket; the two stats differ.
func (h *Host) socketGroup(ctx context.Context, socket string) string {
	if strings.TrimSpace(socket) == "" {
		return ""
	}
	out, err := h.Run(ctx, "sh", "-c", `stat -c %g "$1" 2>/dev/null || stat -f %g "$1"`, "sh", socket)
	if err != nil {
		return ""
	}
	group := strings.TrimSpace(string(out))
	if _, err := strconv.Atoi(group); err != nil {
		return ""
	}
	return group
}

func (h *Host) inGroup(ctx context.Context, group string) bool {
	out, err := h.Run(ctx, "id", "-G")
	if err != nil {
		return false
	}
	return slices.Contains(strings.Fields(string(out)), group)
}

// handRootsOver migrates what the root era left behind, in a throwaway root
// container: the ssh user owns none of it and so cannot chown any of it.
func (h *Host) handRootsOver(ctx context.Context, roots Roots, as hostUser) error {
	// No `|| true`: a chown that failed leaves a worker that cannot read its
	// own state, and the caller needs to know rather than find out later.
	script := `set -eu
own=$1; run=$2; home=$3
for d in "$run" "$home"; do
  [ -d "$d" ] || continue
  find "$d" ! -user "${own%%:*}" -exec chown "$own" {} +
done
shift 3
for root in "$@"; do
  [ -d "$root" ] || continue
  find "$root" -mindepth 2 -maxdepth 4 -type d -path '*/.git/ade' -prune \
    -exec chown -R "$own" {} +
done`
	args := []string{"docker", "run", "--rm", "--user", "0:0",
		"-v", roots.Run + ":" + roots.Run, "-v", roots.Work + ":" + roots.Work}
	if scan := roots.ScanRoot(); scan != roots.Work {
		args = append(args, "-v", scan+":"+scan)
	}
	args = append(args, WorkerImage, "sh", "-c", script, "sh",
		as.uid+":"+as.gid, roots.Run, workerHome(roots), roots.Work)
	if scan := roots.ScanRoot(); scan != roots.Work {
		args = append(args, scan)
	}
	if _, err := h.Run(ctx, args...); err != nil {
		return fmt.Errorf("hand the worker's roots to %s on %s: %w", as.uid, h.Name, err)
	}
	return nil
}
