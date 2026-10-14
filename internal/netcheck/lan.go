package netcheck

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/owenrumney/ade/internal/settings"
)

// HostResult is one bookmarked machine, dialled on the port ssh would use.
type HostResult struct {
	Name      string `json:"name"`
	Address   string `json:"address"`
	Reachable bool   `json:"reachable"`
	Took      string `json:"took"`
	Err       string `json:"err,omitempty"`

	// Hint is set only for the failure macOS's Local Network refusal produces.
	Hint string `json:"hint,omitempty"`
}

const (
	dialWait    = 3 * time.Second
	resolveWait = 2 * time.Second
	sshPort     = "22"

	// How far up the process tree to look for the app macOS holds responsible.
	ancestors = 8
)

// DialHosts dials every bookmarked machine at once, from this process: the
// verdict macOS gives ade's own ssh is the only one that counts.
func DialHosts(ctx context.Context, hosts []settings.Host) []HostResult {
	app := responsibleApp()
	out := make([]HostResult, len(hosts))
	var wg sync.WaitGroup
	for i, host := range hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = dialHost(ctx, host.Name, sshAddress(ctx, host.Name), runtime.GOOS, app)
		}()
	}
	wg.Wait()
	return out
}

// Line is one result as a log line.
func (r HostResult) Line() string {
	if r.Reachable {
		return r.Name + " ssh port open at " + r.Address + " in " + r.Took
	}
	line := r.Name + " UNREACHABLE at " + r.Address + " after " + r.Took + ": " + r.Err
	if r.Hint != "" {
		line += " — " + r.Hint
	}
	return line
}

func dialHost(ctx context.Context, name, address, goos, app string) HostResult {
	out := HostResult{Name: name, Address: address}
	start := time.Now()
	conn, err := (&net.Dialer{Timeout: dialWait}).DialContext(ctx, "tcp", address)
	out.Took = time.Since(start).Round(time.Millisecond).String()
	if err != nil {
		out.Err = reason(err)
		out.Hint = refusalHint(goos, err, app, address)
		return out
	}
	_ = conn.Close()
	out.Reachable = true
	return out
}

// refusalHint names the Settings row to turn on. Only "no route to host" is a
// refusal: a timeout or a closed port is the machine, and gets no hint.
func refusalHint(goos string, err error, app, address string) string {
	if goos != "darwin" || !errors.Is(err, syscall.EHOSTUNREACH) {
		return ""
	}
	host, port, _ := net.SplitHostPort(address)
	if app == "" {
		app = "the application that started ade (your editor)"
	}
	return "run `nc -vz -G 3 " + host + " " + port + "` in a terminal first. If that connects, macOS is " +
		"refusing ade the local network: turn on " + app + " under System Settings → Privacy & Security → " +
		"Local Network. ade has no row of its own there."
}

// sshAddress asks ssh where a name leads, since a bookmark may be an alias
// from ~/.ssh/config that no resolver knows.
func sshAddress(ctx context.Context, name string) string {
	bare := name[strings.LastIndex(name, "@")+1:]
	if settings.CheckHostName(name) != nil {
		return net.JoinHostPort(bare, sshPort)
	}
	ctx, cancel := context.WithTimeout(ctx, resolveWait)
	defer cancel()
	held, err := exec.CommandContext(ctx, "ssh", "-G", name).Output()
	if err != nil {
		return net.JoinHostPort(bare, sshPort)
	}
	return addressFrom(string(held), bare)
}

// addressFrom reads the hostname and port out of what `ssh -G` printed.
func addressFrom(config, fallback string) string {
	host, port := fallback, sshPort
	for _, line := range strings.Split(config, "\n") {
		key, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch key {
		case "hostname":
			host = value
		case "port":
			port = value
		}
	}
	return net.JoinHostPort(host, port)
}

// responsibleApp is the nearest .app above this process, which is what macOS
// judges a local network connection by. Empty when nothing above is an app.
func responsibleApp() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	pid := os.Getppid()
	for range ancestors {
		if pid <= 1 {
			return ""
		}
		held, err := exec.Command("ps", "-o", "ppid=,comm=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return ""
		}
		parent, comm, _ := strings.Cut(strings.TrimSpace(string(held)), " ")
		if app := appOf(comm); app != "" {
			return app
		}
		if pid, err = strconv.Atoi(parent); err != nil {
			return ""
		}
	}
	return ""
}

// appOf is the outermost bundle in an executable's path: a helper lives in a
// .app inside the editor's, and the editor's row is the one in Settings.
func appOf(comm string) string {
	before, _, found := strings.Cut(comm, ".app/")
	if !found {
		return ""
	}
	return before[strings.LastIndex(before, "/")+1:]
}
