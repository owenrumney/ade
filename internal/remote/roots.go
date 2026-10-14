package remote

import (
	"context"
	"fmt"
	"strings"
)

// Roots are the worker's directories, as the worker host names them. Same
// path inside a container as out, so a sibling's -v resolves against the host.
type Roots struct {
	// Work holds each session's checkout, and outlives a disconnect.
	Work string
	// Scan is where repositories are offered from. The same as Work on a
	// worker that clones into what it scans; the user's own checkouts locally.
	Scan string
	// Run holds credentials, and is expected to go with a reboot.
	Run string
	// Socket is the docker socket on the worker host.
	Socket string
}

// ScanRoot is where to offer repositories from, which is Work unless a worker
// was told to look somewhere it does not clone into.
func (r Roots) ScanRoot() string {
	if r.Scan != "" {
		return r.Scan
	}
	return r.Work
}

// fhsRoots are the paths a provisioned Linux worker gets.
var fhsRoots = Roots{Work: "/srv/ade", Run: "/run/ade", Socket: "/var/run/docker.sock"}

// probe asks what the worker can write, not what its OS usually allows.
// A blank line is failure, so one round trip answers all four questions.
const probe = `printf %s\\n "$HOME"
for d in /srv/ade /run/ade; do
  { mkdir -p "$d" 2>/dev/null && [ -w "$d" ] && printf %s\\n "$d"; } || printf '\n'
done
docker context inspect --format '{{.Endpoints.docker.Host}}' 2>/dev/null || printf '\n'
{ [ -d "$HOME/code" ] && printf %s\\n "$HOME/code"; } || printf '\n'`

// DetectRoots picks paths this worker can write. Not an OS test: an
// unprovisioned Linux box fails at /srv exactly as a Mac does.
func DetectRoots(ctx context.Context, host *Host) (Roots, error) {
	out, err := host.Run(ctx, "sh", "-c", probe)
	if err != nil {
		return Roots{}, fmt.Errorf("ask %s where it can work: %w", host.Name, err)
	}
	// Trailing blanks are trimmed, so a worker answering "no" to the last
	// question sends fewer lines than it was asked.
	lines := strings.Split(strings.Trim(string(out), "\n"), "\n")
	if len(lines) < 4 {
		return Roots{}, fmt.Errorf("%s did not say where it can work", host.Name)
	}
	for len(lines) < 5 {
		lines = append(lines, "")
	}
	home := strings.TrimSpace(lines[0])
	if home == "" {
		return Roots{}, fmt.Errorf("%s did not say where its home directory is", host.Name)
	}
	roots := Roots{
		Work:   pick(lines[1], home+"/.ade/srv"),
		Run:    pick(lines[2], home+"/.ade/run"),
		Socket: pick(strings.TrimPrefix(strings.TrimSpace(lines[3]), "unix://"), fhsRoots.Socket),
	}
	// A worker that keeps its checkouts somewhere else has repositories worth
	// offering. Not the work root, so clones do not land beside real work.
	if scan := strings.TrimSpace(lines[4]); scan != "" && scan != roots.Work {
		roots.Scan = scan
	}
	return roots, nil
}

func pick(got, fallback string) string {
	if got = strings.TrimSpace(got); got != "" {
		return got
	}
	return fallback
}
