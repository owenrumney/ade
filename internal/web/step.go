package web

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/owenrumney/ade/internal/bridge"
)

// stepLine is a tool call as one short line, for the badge that says what
// the agent is doing right now.
func stepLine(use bridge.ToolUse) string {
	var in struct {
		Command  string `json:"command"`
		FilePath string `json:"file_path"`
		Notebook string `json:"notebook_path"`
		Path     string `json:"path"`
		Pattern  string `json:"pattern"`
		URL      string `json:"url"`
		Query    string `json:"query"`
		// Description names a sub-agent's task, which is all its call says.
		Description string `json:"description"`
	}
	_ = json.Unmarshal(use.Input, &in)
	detail := ""
	switch {
	case in.Command != "":
		detail = command(in.Command)
	case in.FilePath != "":
		detail = filepath.Base(in.FilePath)
	case in.Notebook != "":
		detail = filepath.Base(in.Notebook)
	case in.Pattern != "":
		detail = in.Pattern
	case in.URL != "":
		if u, err := url.Parse(in.URL); err == nil && u.Host != "" {
			detail = u.Host
		} else {
			detail = in.URL
		}
	case in.Query != "":
		detail = in.Query
	case in.Path != "":
		detail = filepath.Base(in.Path)
	case in.Description != "":
		detail = in.Description
	}
	if detail == "" {
		return use.Name
	}
	if r := []rune(detail); len(r) > stepWidth {
		detail = string(r[:stepWidth-1]) + "…"
	}
	return use.Name + ": " + detail
}

// stepWidth is how much of a call the badge gets. It shares a title bar with
// the session's name, so a step that runs on crowds the name out.
const stepWidth = 32

// command is the part of a shell line that does something. A leading `cd` is
// where the agent already is, and spending the badge on it cuts off the rest.
func command(raw string) string {
	line := strings.SplitN(strings.TrimSpace(raw), "\n", 2)[0]
	for {
		rest, ok := strings.CutPrefix(line, "cd ")
		if !ok {
			return line
		}
		_, after, found := strings.Cut(rest, "&&")
		if !found {
			return line
		}
		line = strings.TrimSpace(after)
	}
}
