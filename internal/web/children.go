package web

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/owenrumney/ade/internal/bridge"
)

// Child is a sub-agent the main agent called. Run in the background it
// outlives the turn that called it, which is when the page would read idle.
type Child struct {
	// Kind is childAgent or childTask: a sub-agent reports tool calls, a
	// background command the last line it printed.
	Kind  string    `json:"kind"`
	Name  string    `json:"name"`
	Doing string    `json:"doing,omitempty"`
	Steps int       `json:"steps,omitempty"`
	Since time.Time `json:"since"`
}

const (
	childAgent = "agent"
	childTask  = "task"
)

type child struct {
	Child
	toolID string
	msgID  string
	file   string
	offset int64
	// report is when its last line was prose with no call after it, which is
	// how a background one ends when no notice says so.
	report time.Time
	done   bool
}

// children follows the main agent's sub-agents through the transcripts
// Claude Code writes for them, since none of their work reaches the stream.
type children struct {
	done  func() <-chan struct{}
	spawn func(func())
	// dir is read on every poll because the session can move; alive reports
	// whether the agent that started these is still up.
	dir   func() string
	alive func() bool
	touch func()
	amend func(msgID, doing string, steps int)
	tell  func()

	mu      sync.Mutex
	list    []*child
	claimed map[string]bool
	polling bool
}

// childPoll is how often the transcripts are read; childSettle how long a
// report has to stand with nothing after it before it is taken as the end.
var (
	childPoll   = 2 * time.Second
	childSettle = 10 * time.Second
)

var agentIDRE = regexp.MustCompile(`\bagentId: ([A-Za-z0-9]+)`)

func newChildren() *children {
	never := make(chan struct{})
	return &children{done: func() <-chan struct{} { return never }, claimed: map[string]bool{}}
}

// open is an Agent call seen in the stream, before any transcript exists.
func (c *children) open(toolID, msgID, name string) {
	c.add(&child{Child: Child{Kind: childAgent, Name: name, Since: time.Now()}, toolID: toolID, msgID: msgID})
}

// openTask is a command left running in the background, named by what it
// runs and followed through the file its output is written to.
func (c *children) openTask(toolID, msgID, name, file string) {
	c.add(&child{Child: Child{Kind: childTask, Name: name, Since: time.Now()}, toolID: toolID, msgID: msgID, file: file})
}

func (c *children) add(ch *child) {
	c.mu.Lock()
	c.list = append(c.list, ch)
	start := !c.polling
	c.polling = true
	c.mu.Unlock()
	if start && c.spawn != nil {
		c.spawn(c.loop)
	}
}

// answered is a tool's result: an agent launched or reporting, or a command
// saying where it writes, which opens a child of its own.
func (c *children) answered(toolID, msgID, name, content string) {
	if file := backgroundTask(content); file != "" {
		if !c.has(toolID) {
			c.openTask(toolID, msgID, name, file)
		}
		return
	}
	dir := c.dir()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range c.list {
		if ch.toolID != toolID {
			continue
		}
		if id := launchedAgent(content); id != "" {
			if ch.file == "" && dir != "" {
				ch.file = filepath.Join(dir, "agent-"+id+".jsonl")
				c.claimed[ch.file] = true
			}
			return
		}
		ch.done = true
	}
}

func (c *children) has(toolID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range c.list {
		if ch.toolID == toolID {
			return true
		}
	}
	return false
}

// noticed is a task notification the harness put to the agent: the call it
// names is over.
func (c *children) noticed(notice string) {
	id := tagged(notice, "tool-use-id")
	if id == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range c.list {
		if ch.toolID == id {
			ch.done = true
		}
	}
}

// stop ends the children of a kind, or every kind when it is empty, and says
// how many it ended. What they were doing is over; nothing will report it.
func (c *children) stop(kind string) int {
	c.mu.Lock()
	n := 0
	for _, ch := range c.list {
		if ch.done || (kind != "" && ch.Kind != kind) {
			continue
		}
		ch.done = true
		n++
	}
	c.mu.Unlock()
	if n > 0 {
		c.poll()
	}
	return n
}

func (c *children) snapshot() []Child {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []Child
	for _, ch := range c.list {
		if !ch.done {
			out = append(out, ch.Child)
		}
	}
	return out
}

// backgroundTask is where a backgrounded command writes its output, from the
// result that says it is running. Empty for a command that already finished.
func backgroundTask(content string) string {
	if !strings.HasPrefix(content, "Command running in background") {
		return ""
	}
	m := taskFileRE.FindStringSubmatch(content)
	if m == nil {
		return ""
	}
	return strings.TrimSuffix(m[1], ".")
}

var taskFileRE = regexp.MustCompile(`Output is being written to: (\S+)`)

func launchedAgent(content string) string {
	if !strings.HasPrefix(content, "Async agent launched") {
		return ""
	}
	m := agentIDRE.FindStringSubmatch(content)
	if m == nil {
		return ""
	}
	return m[1]
}

// tagged is the text inside <tag>…</tag>, as a notice carries its fields.
func tagged(s, tag string) string {
	_, after, ok := strings.Cut(s, "<"+tag+">")
	if !ok {
		return ""
	}
	inner, _, ok := strings.Cut(after, "</"+tag+">")
	if !ok {
		return ""
	}
	return strings.TrimSpace(inner)
}

func (c *children) loop() {
	ticker := time.NewTicker(childPoll)
	defer ticker.Stop()
	for {
		select {
		case <-c.done():
			return
		case <-ticker.C:
			if !c.poll() {
				return
			}
		}
	}
}

// poll claims new transcripts, reads what each child did since last time,
// and drops the done. False once nothing is left to follow.
func (c *children) poll() bool {
	// The agent leads the process group these run in, so its going takes them
	// with it, whatever their own file last said.
	if c.alive != nil && !c.alive() {
		c.stop("")
	}
	c.claim(c.dir())
	type change struct {
		msgID, doing string
		steps        int
	}
	var changes []change
	now := time.Now()
	c.mu.Lock()
	kept := c.list[:0]
	for _, ch := range c.list {
		moved := ch.file != "" && !ch.done && c.read(ch, now)
		if !ch.done && !ch.report.IsZero() && now.Sub(ch.report) >= childSettle {
			ch.done = true
		}
		// A done child is dropped below, so its row loses the step once.
		if ch.done {
			moved = true
		}
		if moved {
			doing := ch.Doing
			if ch.done {
				doing = ""
			}
			changes = append(changes, change{ch.msgID, doing, ch.Steps})
		}
		if !ch.done {
			kept = append(kept, ch)
		}
	}
	for i := len(kept); i < len(c.list); i++ {
		c.list[i] = nil
	}
	c.list = kept
	c.polling = len(kept) > 0
	more := c.polling
	c.mu.Unlock()

	for _, ch := range changes {
		if c.amend != nil {
			c.amend(ch.msgID, ch.doing, ch.steps)
		}
	}
	if len(changes) > 0 {
		if c.touch != nil {
			c.touch()
		}
		if c.tell != nil {
			c.tell()
		}
	}
	return more
}

// claim pairs transcripts that have appeared with the calls that made them:
// the oldest call without a file takes the oldest file written after it.
func (c *children) claim(dir string) {
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type found struct {
		path string
		at   time.Time
	}
	var files []found
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "agent-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		path := filepath.Join(dir, name)
		if c.claimed[path] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, found{path, info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.Before(files[j].at) })
	for _, ch := range c.list {
		if ch.file != "" || ch.done {
			continue
		}
		for i, f := range files {
			if f.at.Before(ch.Since.Add(-time.Second)) {
				continue
			}
			ch.file = f.path
			c.claimed[f.path] = true
			files = append(files[:i], files[i+1:]...)
			break
		}
	}
}

// read takes a child's file from where it left off. A line not yet whole is
// left for next time.
func (c *children) read(ch *child, now time.Time) bool {
	f, err := os.Open(ch.file)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(ch.offset, io.SeekStart); err != nil {
		return false
	}
	r := bufio.NewReaderSize(f, 1<<20)
	moved := false
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break
		}
		ch.offset += int64(len(line))
		if ch.Kind == childTask {
			if text := strings.TrimSpace(string(line)); text != "" {
				ch.Doing = truncate(text, 72)
				moved = true
			}
			continue
		}
		ev, ok := bridge.DecodeEvent(line)
		if !ok || ev.Type != "assistant" {
			continue
		}
		switch {
		case len(ev.ToolUses) > 0:
			ch.Doing = stepLine(ev.ToolUses[len(ev.ToolUses)-1])
			ch.Steps += len(ev.ToolUses)
			ch.report = time.Time{}
			moved = true
		case strings.TrimSpace(ev.Text) != "":
			ch.report = now
		}
	}
	return moved
}
