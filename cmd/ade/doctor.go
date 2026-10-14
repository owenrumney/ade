package main

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/owenrumney/ade/internal/machine"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
)

// columns is state, checkout, pid, kind, model, silence, queue.
const columns = 7

// newDoctorCommand reads the machine snapshot, and reaches nothing: the file
// is all that crosses into a place with no route to a port.
func newDoctorCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "What every ade on this machine is doing, from the last snapshot",
		Long: `doctor reads ~/.ade/machine/machine.json, which a running launcher — or a
running ade, where there is no launcher — refreshes. Nothing is probed here, so
the first line is how old the snapshot is, and every line under it is that old.

It reports and exits 0. Deciding that a silent agent is a stuck one is the
reader's call: no threshold separates a hang from a long shell command.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			snap, ok := readMachine()
			if asJSON {
				return printMachineJSON(snap, ok)
			}
			if !ok {
				fmt.Println("no snapshot; is the launcher running?")
				return nil
			}
			fmt.Print(renderMachine(snap, time.Now()))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the snapshot as it is on disk, for an agent to parse")
	return cmd
}

// readMachine is the snapshot, or none. No home directory is nothing to
// report, and doctor reports: it never fails a step that runs it.
func readMachine() (machine.Machine, bool) {
	dir, err := machine.Dir()
	if err != nil {
		return machine.Machine{}, false
	}
	return machine.Read(dir)
}

// printMachineJSON prints the snapshot unchanged, or null when there is none.
func printMachineJSON(snap machine.Machine, ok bool) error {
	var out any
	if ok {
		out = snap
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

// renderMachine is the whole report, age line first, always: a reader who
// misses it draws conclusions from a dead file.
func renderMachine(snap machine.Machine, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "snapshot %s old\n", age(now.Sub(snap.WrittenAt)))
	rows := machineRows(snap, now)
	if len(rows) == 0 {
		b.WriteString("no ade running on this machine\n")
		return b.String()
	}
	var width [columns]int
	for _, row := range rows {
		for i, cell := range row {
			if n := utf8.RuneCountInString(cell); n > width[i] {
				width[i] = n
			}
		}
	}
	for _, row := range rows {
		var line strings.Builder
		for i, cell := range row {
			if i > 0 {
				line.WriteString("  ")
			}
			line.WriteString(cell)
			line.WriteString(strings.Repeat(" ", width[i]-utf8.RuneCountInString(cell)))
		}
		b.WriteString(strings.TrimRight(line.String(), " ") + "\n")
	}
	return b.String()
}

// machineRows is one row per checkout with a live server, snapshot order.
func machineRows(snap machine.Machine, now time.Time) [][columns]string {
	var out [][columns]string
	for _, repo := range snap.Repos {
		for _, dir := range repo.Checkouts {
			if dir.Server == nil {
				continue
			}
			out = append(out, machineRow(repo, dir, now))
		}
	}
	return out
}

func machineRow(repo sessions.GraphRepo, dir sessions.GraphCheckout, now time.Time) [columns]string {
	agent := dir.Server.Agent
	row := [columns]string{
		machine.State(agent),
		printable(path.Join(repo.Name, checkoutName(dir))),
		fmt.Sprintf("pid %d", dir.Server.PID),
		dash(kindOf(agent)),
		dash(modelOf(agent)),
		"-",
		fmt.Sprintf("q%d", queuedOf(agent)),
	}
	if silent, ok := machine.Silence(agent, now); ok {
		row[5] = "silent " + age(silent)
	}
	return row
}

// printable drops the control bytes a directory name may hold: a newline
// there would forge a row, and an ESC would drive the reader's terminal.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

func checkoutName(dir sessions.GraphCheckout) string {
	if dir.Branch != "" {
		return dir.Branch
	}
	return dir.Name
}

func kindOf(a *session.AgentHealth) string {
	if a == nil {
		return ""
	}
	return a.Kind
}

func modelOf(a *session.AgentHealth) string {
	if a == nil {
		return ""
	}
	return a.Model
}

func queuedOf(a *session.AgentHealth) int {
	if a == nil {
		return 0
	}
	return a.Queued
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// age rounds to the second, which is as precise as a polled snapshot is.
func age(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	return d.Round(time.Second).String()
}
