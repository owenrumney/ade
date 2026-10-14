package limits

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type codexTranscriptLine struct {
	Timestamp time.Time `json:"timestamp"`
	Payload   struct {
		Type       string `json:"type"`
		RateLimits *struct {
			Primary   *codexTranscriptWindow `json:"primary"`
			Secondary *codexTranscriptWindow `json:"secondary"`
		} `json:"rate_limits"`
	} `json:"payload"`
}

type codexTranscriptWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int     `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
}

// codexTranscript reads the latest rate-limit event Codex wrote locally.
func codexTranscript(root string) (Limits, bool) {
	paths, err := filepath.Glob(filepath.Join(root, "sessions", "*", "*", "*", "rollout-*.jsonl"))
	if err != nil {
		return Limits{}, false
	}
	out := Limits{Provider: "codex"}
	for _, path := range paths {
		file, err := os.Open(path) // #nosec G304 -- Codex's local transcript
		if err != nil {
			continue
		}
		scan := bufio.NewScanner(file)
		scan.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scan.Scan() {
			var line codexTranscriptLine
			if json.Unmarshal(scan.Bytes(), &line) != nil || line.Payload.Type != "token_count" || line.Payload.RateLimits == nil || !line.Timestamp.After(out.CheckedAt) {
				continue
			}
			windows := codexTranscriptWindows(line.Payload.RateLimits.Primary, line.Payload.RateLimits.Secondary)
			if len(windows) > 0 {
				out.CheckedAt, out.Windows = line.Timestamp, windows
			}
		}
		_ = file.Close()
	}
	return out, len(out.Windows) > 0
}

func codexTranscriptWindows(primary, secondary *codexTranscriptWindow) []Window {
	out := make([]Window, 0, 2)
	for _, item := range []struct {
		window *codexTranscriptWindow
		name   string
	}{{primary, "5h"}, {secondary, "7d"}} {
		if item.window == nil {
			continue
		}
		name := item.name
		switch {
		case item.window.WindowMinutes <= 0:
		case item.window.WindowMinutes <= 300:
			name = "5h"
		case item.window.WindowMinutes >= 7*24*60:
			name = "7d"
		default:
			name = fmt.Sprintf("%dh", item.window.WindowMinutes/60)
		}
		out = append(out, Window{Name: name, Percent: item.window.UsedPercent, ResetsAt: time.Unix(item.window.ResetsAt, 0)})
	}
	return out
}
