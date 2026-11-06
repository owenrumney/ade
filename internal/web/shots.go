package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/oneshot"
)

// shotKinds are the pictures the Shots tab can draw, and what to serve them as.
var shotKinds = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
}

// shotsPoll is how often the outbox is read. A picture is looked at by hand,
// so one arriving a second late costs nothing.
const shotsPoll = 2 * time.Second

// shotsKept bounds the carousel: nothing else prunes the outbox.
const shotsKept = 50

type shot struct {
	Name string    `json:"name"`
	At   time.Time `json:"at"`
	Size int64     `json:"size"`
}

// outbox is where `make app-shots` and `ade show` leave pictures for this
// session.
func (s *Server) outbox() string {
	if at := os.Getenv(oneshot.OutboxEnv); at != "" {
		return at
	}
	return filepath.Join(s.space().gitDir, "ade", "outbox")
}

// shots is what the outbox holds, newest first.
func (s *Server) shots() []shot {
	entries, err := os.ReadDir(s.outbox())
	if err != nil {
		return nil
	}
	out := make([]shot, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || shotKinds[strings.ToLower(filepath.Ext(entry.Name()))] == "" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		out = append(out, shot{Name: entry.Name(), At: info.ModTime(), Size: info.Size()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if len(out) > shotsKept {
		out = out[:shotsKept]
	}
	return out
}

func (s *Server) handleShots(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"shots": s.shots()})
}

// handleShot serves one picture. The name is a base name in the outbox and
// nothing else: a path here would read any file the server can.
func (s *Server) handleShot(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	kind := shotKinds[strings.ToLower(filepath.Ext(name))]
	if kind == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		writeError(w, http.StatusBadRequest, "not a picture in the outbox")
		return
	}
	raw, err := os.ReadFile(filepath.Join(s.outbox(), name)) // #nosec G304 -- a base name in the outbox
	if err != nil {
		writeError(w, http.StatusNotFound, "no such picture")
		return
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// shotsStamp changes when the outbox does, which is all the watcher compares.
func shotsStamp(all []shot) string {
	parts := make([]string, 0, len(all))
	for _, one := range all {
		parts = append(parts, one.Name+":"+strconv.FormatInt(one.At.UnixNano(), 10))
	}
	return strings.Join(parts, "\n")
}

// watchShots tells the window when the outbox changes, so the Shots tab fills
// without the page polling for it.
func (s *Server) watchShots(ctx context.Context) {
	ticker := time.NewTicker(shotsPoll)
	defer ticker.Stop()
	last := shotsStamp(s.shots())
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := shotsStamp(s.shots())
			if now == last {
				continue
			}
			last = now
			s.hub.Broadcast("shots", nil)
		}
	}
}
