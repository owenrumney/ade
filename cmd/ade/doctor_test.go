package main

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/machine"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
)

func TestTheReportLeadsWithHowOldItIs(t *testing.T) {
	now := time.Now()
	out := renderMachine(machine.Machine{WrittenAt: now.Add(-41 * time.Second)}, now)

	first, _, _ := strings.Cut(out, "\n")
	assert.Equal(t, "snapshot 41s old", first)
	assert.Contains(t, out, "no ade running on this machine")
}

func TestOneLinePerLiveSession(t *testing.T) {
	now := time.Now()
	snap := machine.Machine{
		WrittenAt: now.Add(-time.Second),
		Repos: []sessions.GraphRepo{
			{Name: "ade", Checkouts: []sessions.GraphCheckout{
				{Name: "ade", Branch: "main", Server: &sessions.GraphServer{PID: 33211, Agent: &session.AgentHealth{
					Running: true, Kind: "chat", Model: "opus-5", LastEventAt: now.Add(-724 * time.Second),
				}}},
				{Name: "ade-fix", Branch: "fix-1", Server: nil},
			}},
			{Name: "sow-review", Checkouts: []sessions.GraphCheckout{
				{Name: "sow-review", Branch: "fix-1", Server: &sessions.GraphServer{PID: 900, Agent: &session.AgentHealth{Queued: 2}}},
			}},
		},
	}

	lines := strings.Split(strings.TrimRight(renderMachine(snap, now), "\n"), "\n")
	require.Len(t, lines, 3, "the age line, and one per live server")
	assert.Contains(t, lines[1], "working")
	assert.Contains(t, lines[1], "ade/main")
	assert.Contains(t, lines[1], "pid 33211")
	assert.Contains(t, lines[1], "chat")
	assert.Contains(t, lines[1], "opus-5")
	assert.Contains(t, lines[1], "silent 12m4s")
	assert.Contains(t, lines[1], "q0")
	assert.Contains(t, lines[2], "wedged")
	assert.Contains(t, lines[2], "sow-review/fix-1")
	assert.Contains(t, lines[2], "q2")
	assert.NotContains(t, lines[2], "silent")
}

func TestTheColumnsLineUp(t *testing.T) {
	now := time.Now()
	server := func(pid int, model string) *sessions.GraphServer {
		return &sessions.GraphServer{PID: pid, Agent: &session.AgentHealth{Running: true, Kind: "chat", Model: model}}
	}
	snap := machine.Machine{
		WrittenAt: now,
		Repos: []sessions.GraphRepo{{Name: "r", Checkouts: []sessions.GraphCheckout{
			{Branch: "main", Server: server(1, "haiku")},
			{Branch: "a-much-longer-branch", Server: server(123456, "opus-5")},
		}}},
	}

	lines := strings.Split(strings.TrimRight(renderMachine(snap, now), "\n"), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, strings.Index(lines[1], "pid"), strings.Index(lines[2], "pid"))
}

func TestANameCannotForgeARowOrDriveTheTerminal(t *testing.T) {
	now := time.Now()
	snap := machine.Machine{
		WrittenAt: now,
		Repos: []sessions.GraphRepo{{Name: "repo\n idle  other/main  pid 1", Checkouts: []sessions.GraphCheckout{
			{Branch: "fix\x1b[31m", Server: &sessions.GraphServer{PID: 1, Agent: &session.AgentHealth{Running: true}}},
		}}},
	}

	out := renderMachine(snap, now)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	assert.Len(t, lines, 2, "the age line, and one per live server")
	assert.NotContains(t, out, "\x1b")
}

func TestTheColumnsLineUpWhenANameIsNotASCII(t *testing.T) {
	now := time.Now()
	server := &sessions.GraphServer{PID: 1, Agent: &session.AgentHealth{Running: true}}
	snap := machine.Machine{
		WrittenAt: now,
		Repos: []sessions.GraphRepo{{Name: "r", Checkouts: []sessions.GraphCheckout{
			{Branch: "fix/café-login", Server: server},
			{Branch: "fix/cafe-loginx", Server: server},
		}}},
	}

	// Columns are counted in characters: é is one column and two bytes.
	at := func(line string) int { return utf8.RuneCountInString(line[:strings.Index(line, "pid")]) }
	lines := strings.Split(strings.TrimRight(renderMachine(snap, now), "\n"), "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, at(lines[1]), at(lines[2]))
}

func TestAgeNeverPrintsSubSecondNoise(t *testing.T) {
	assert.Equal(t, "0s", age(0))
	assert.Equal(t, "0s", age(400*time.Millisecond))
	assert.Equal(t, "3s", age(3*time.Second))
	assert.Equal(t, "12m4s", age(724*time.Second))
}
