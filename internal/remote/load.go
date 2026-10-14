package remote

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Load is what a machine is using, whole-machine rather than ade's share: a
// worker that is not the only thing running competes with whatever else is.
type Load struct {
	Name     string  `json:"name"`
	Cores    int     `json:"cores"`
	Load1    float64 `json:"load1"`
	MemTotal int64   `json:"memTotal"`
	MemUsed  int64   `json:"memUsed"`
	// Agents excludes the worker and the hub, which are infrastructure.
	Agents int `json:"agents"`
}

// CPU is the one-minute load as a percentage of the cores it ran on.
func (l Load) CPU() float64 {
	if l.Cores <= 0 {
		return 0
	}
	return l.Load1 / float64(l.Cores) * 100
}

// countAgents counts ade's containers on stdin, minus the worker and the hub:
// infrastructure counted as an agent reads as work nobody started.
const countAgents = `awk '/^ade-/ && $0 != "` + workerName + `" && $0 != "` + hubName + `" {n++} END{print n+0}'`

// readLoad prints five numbers in a fixed order, from /proc so that nothing
// has to be installed. MemAvailable is the kernel's own answer to what is free.
var readLoad = `set -eu
cut -d' ' -f1 /proc/loadavg
nproc
awk '/^MemTotal:/{t=$2} /^MemAvailable:/{a=$2} END{print t; print a}' /proc/meminfo
docker ps --format '{{.Names}}' 2>/dev/null |
  ` + countAgents

// Load reads one machine's pressure. A host without /proc — a Mac — reports
// nothing rather than a number guessed from a different shape of system.
func (h *Host) Load(ctx context.Context) (Load, error) {
	out, err := h.Run(ctx, "sh", "-c", readLoad)
	if err != nil {
		return Load{}, err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 5 {
		return Load{}, fmt.Errorf("%s did not report its load", h.Name)
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return Load{}, fmt.Errorf("%s reported %q as its load: %w", h.Name, fields[0], err)
	}
	cores, err := strconv.Atoi(fields[1])
	if err != nil || cores <= 0 {
		return Load{}, fmt.Errorf("%s reported %q as its cores", h.Name, fields[1])
	}
	total, _ := strconv.ParseInt(fields[2], 10, 64)
	free, _ := strconv.ParseInt(fields[3], 10, 64)
	agents, _ := strconv.Atoi(fields[4])
	if total <= 0 || free > total {
		return Load{}, fmt.Errorf("%s reported no memory", h.Name)
	}
	// /proc/meminfo is in kibibytes; everything else here is bytes.
	return Load{
		Name: h.Name, Cores: cores, Load1: load,
		MemTotal: total * 1024, MemUsed: (total - free) * 1024, Agents: agents,
	}, nil
}
