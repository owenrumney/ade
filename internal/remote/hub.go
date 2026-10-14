package remote

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// One hub per machine, beside the worker, on loopback only: every agent on the
// machine shares its network namespace rather than reaching it over a network.
const (
	HubImage = "ghcr.io/infracost/mcp-hub:latest"
	hubName  = "ade-hub"
	hubPort  = 4867
	// registryHost is where every image ade starts comes from, private.
	registryHost = "ghcr.io"
	networkName  = "ade"
)

// HubState is the laptop's hub configuration: mcp-hub's sqlite file, and the
// write-ahead log that may hold its latest rows while the laptop's hub is up.
type HubState struct {
	DB  []byte
	WAL []byte
	At  time.Time
}

// hubHome is MCP_HUB_HOME for the hub container. Under the work root so the
// shared mount carries it, and its own directory so nothing else reads it.
func hubHome(roots Roots) string { return roots.Work + "/.hub" }

// PrepareHub puts the laptop's hub configuration where the machine's hub will
// read it, unless the copy there is newer: the hub over there edits its own.
func (h *Host) PrepareHub(ctx context.Context, roots Roots, state HubState) error {
	if len(state.DB) == 0 {
		return nil
	}
	home := hubHome(roots)
	if _, err := h.Run(ctx, "sh", "-c", "umask 077; mkdir -p "+quote(home)); err != nil {
		return fmt.Errorf("prepare hub on %s: %w", h.Name, err)
	}
	db := home + "/mcp-hub.db"
	if !h.olderThan(ctx, db, state.At) {
		return nil
	}
	for _, file := range []struct {
		path string
		data []byte
	}{{db, state.DB}, {db + "-wal", state.WAL}} {
		command := "umask 077; rm -f " + quote(db+"-shm") + "; cat > " + quote(file.path)
		if len(file.data) == 0 {
			command = "rm -f " + quote(file.path)
		}
		if _, err := h.RunInput(ctx, file.data, "sh", "-c", command); err != nil {
			return fmt.Errorf("send hub state to %s: %w", h.Name, err)
		}
	}
	return nil
}

// readHome is one file under the worker's HOME, read as the worker rather than
// as the ssh user: that directory is the container's and root owns it.
const readHome = `cat "$HOME/$1" 2>/dev/null || true`

// writeHome is the same file, written the same way.
const writeHome = `umask 077; cat > "$HOME/$1"`

// PrepareMCPServers puts the hub-proxied servers in the worker's ~/.claude.json,
// keeping the rest. Needs the worker up: only it can write its own HOME.
func (h *Host) PrepareMCPServers(ctx context.Context, servers map[string]json.RawMessage) error {
	if len(servers) == 0 {
		return nil
	}
	current := map[string]json.RawMessage{}
	out, err := h.Run(ctx, "docker", "exec", workerName, "sh", "-c", readHome, "sh", ".claude.json")
	if err != nil {
		return fmt.Errorf("read the worker's claude.json on %s: %w", h.Name, err)
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		if err := json.Unmarshal(out, &current); err != nil {
			return fmt.Errorf("parse the worker's claude.json on %s: %w", h.Name, err)
		}
	}
	merged, err := json.Marshal(servers)
	if err != nil {
		return err
	}
	current["mcpServers"] = merged
	body, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	if _, err := h.RunInput(ctx, body, "docker", "exec", "-i", workerName, "sh", "-c", writeHome, "sh", ".claude.json"); err != nil {
		return fmt.Errorf("send mcp servers to %s: %w", h.Name, err)
	}
	return nil
}

// StartHub adopts the machine's hub when it is running, or replaces it. Its
// port is not published: agents reach it from inside its network namespace.
func (h *Host) StartHub(ctx context.Context, roots Roots, github Login) error {
	// Login then pull before the adoption check: the image is private, and a
	// hub from last week's image is not one to keep.
	h.loginRegistry(ctx, roots, github)
	// The worker pulled first from the same registry with the same token, so its
	// failure is already on show. Only a hub-only failure needs saying.
	if err := h.pullImage(ctx, roots, HubImage); err != nil && h.stale == nil {
		h.stale = err
	}
	stamp := h.imageID(ctx, HubImage)
	if h.containerRunning(ctx, hubName) && h.stampedWith(ctx, hubName, stamp) && h.hubAnswers(ctx) {
		return nil
	}
	if err := h.ensureNetwork(ctx); err != nil {
		return err
	}
	_, _ = h.Run(ctx, "docker", "rm", "-f", hubName)
	argv := []string{"docker", "run", "-d", "--name", hubName, "--init",
		"--label", imageLabel + "=" + stamp,
		"--network", networkName,
		"-e", "MCP_HUB_HOME=" + hubHome(roots),
		"-v", roots.Work + ":" + roots.Work,
		HubImage, "serve", "--addr", "127.0.0.1:" + strconv.Itoa(hubPort)}
	_, err := h.Run(ctx, argv...)
	if nameInUse(err) && h.freeName(ctx, hubName) {
		_, err = h.Run(ctx, argv...)
	}
	if err != nil {
		if nameInUse(err) {
			return fmt.Errorf("%s still holds a container called %s that docker will not remove: %w — "+
				"run `docker rm -f %s` there, and if that hangs, restart its docker daemon",
				h.Name, hubName, err, hubName)
		}
		if !h.hasImage(ctx, HubImage) {
			return fmt.Errorf("%s could not pull %s: %w — the image is private; the token needs read:packages (gh auth refresh -s read:packages)", h.Name, HubImage, err)
		}
		return fmt.Errorf("start hub on %s: %w", h.Name, err)
	}
	deadline := time.Now().Add(workerWait)
	for time.Now().Before(deadline) {
		if h.hubAnswers(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("the hub on %s started but did not answer within %s", h.Name, workerWait)
}

// hubAnswers asks the hub for its health from inside its own container, the
// one place its loopback port can be reached.
func (h *Host) hubAnswers(ctx context.Context) bool {
	out, err := h.Run(ctx, "docker", "exec", hubName, "curl", "-s", "--max-time", "3",
		"http://127.0.0.1:"+strconv.Itoa(hubPort)+"/healthz")
	return err == nil && strings.Contains(string(out), `"mcpHub":true`)
}

// ensureNetwork makes the network the worker, the hub and the agents share, so
// they resolve each other by name. Creating it twice is the one error ignored.
func (h *Host) ensureNetwork(ctx context.Context) error {
	if _, err := h.Run(ctx, "docker", "network", "inspect", networkName); err == nil {
		return nil
	}
	if _, err := h.Run(ctx, "docker", "network", "create", networkName); err != nil {
		if h.hasNetwork(ctx) {
			return nil
		}
		return fmt.Errorf("create the %s network on %s: %w", networkName, h.Name, err)
	}
	return nil
}

func (h *Host) hasNetwork(ctx context.Context) bool {
	_, err := h.Run(ctx, "docker", "network", "inspect", networkName)
	return err == nil
}

// Login is a GitHub login and a token that can read the private hub image.
type Login struct {
	User  string
	Token string
}

// dockerConfig is an ade-owned docker configuration. `docker login` is no use
// on a Mac: it stores the credential in a keychain no ssh session can open.
func dockerConfig(roots Roots) string { return roots.Run + "/docker" }

// loginRegistry writes the registry credential where the pulls will read it.
// The file, not `docker login`, so it works on a machine with no one at it.
func (h *Host) loginRegistry(ctx context.Context, roots Roots, github Login) {
	if github.Token == "" {
		return
	}
	user := github.User
	if user == "" {
		user = "x-access-token"
	}
	body, err := json.Marshal(map[string]any{
		"auths": map[string]any{registryHost: map[string]string{
			"auth": base64.StdEncoding.EncodeToString([]byte(user + ":" + github.Token)),
		}},
	})
	if err != nil {
		return
	}
	dir := dockerConfig(roots)
	_, _ = h.RunInput(ctx, body, "sh", "-c",
		`umask 077; mkdir -p "$1"; cat > "$1/config.json"`, "sh", dir)
}

func (h *Host) containerRunning(ctx context.Context, name string) bool {
	out, err := h.Run(ctx, "docker", "inspect", "-f", "{{.State.Running}}", name)
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// pullImage refreshes a tag, because docker run reuses whatever :latest it
// already has and a machine would keep running a release nobody published.
func (h *Host) pullImage(ctx context.Context, roots Roots, image string) error {
	_, err := h.Run(ctx, "env", "DOCKER_CONFIG="+dockerConfig(roots), "docker", "pull", "-q", image)
	if err == nil {
		return nil
	}
	// ghcr says only "denied" to a token without the scope, which reads as nothing.
	if strings.Contains(err.Error(), "denied") {
		return fmt.Errorf("could not pull %s: the registry refused the GitHub token (ADE_GITHUB_TOKEN, else gh auth token); it needs read:packages: gh auth refresh -s read:packages, then reconnect", image)
	}
	return fmt.Errorf("could not pull %s: %w", image, err)
}

// imageLabel is the image a container was started from. A container's own
// digest never equals its tag's, so only a stamp can be compared.
const imageLabel = "ade.image"

// imageID is what docker calls the tag now, and what gets stamped.
func (h *Host) imageID(ctx context.Context, image string) string {
	out, err := h.Run(ctx, "docker", "image", "inspect", "-f", "{{.Id}}", image)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// stampedWith reports a container started from stamp. A container with no
// stamp predates this and is replaced, which is what a new release wants.
func (h *Host) stampedWith(ctx context.Context, name, stamp string) bool {
	if stamp == "" {
		return false
	}
	out, err := h.Run(ctx, "docker", "inspect", "-f", `{{index .Config.Labels "`+imageLabel+`"}}`, name)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == stamp
}

func (h *Host) hasImage(ctx context.Context, image string) bool {
	_, err := h.Run(ctx, "docker", "image", "inspect", image)
	return err == nil
}
