package web

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/statedir"
)

// The outbox is how an agent shows something it made: a sandboxed one shares
// this server's disk and not its network, so a file is the only way in.

// outboxPath is the directory an agent drops pictures into.
func outboxPath(gitDir string) string {
	if gitDir == "" {
		return ""
	}
	return filepath.Join(statedir.Git(gitDir), "outbox")
}

func (s *Server) outboxPath() string {
	return outboxPath(s.space().gitDir)
}

// outboxEnv names the outbox for the agent and makes sure it is there. A rule
// telling an agent to write somewhere has to say where, in every checkout.
func (s *Server) outboxEnv() string {
	dir := s.outboxPath()
	if dir == "" {
		return ""
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		slog.Debug("make the outbox", "err", err)
		return ""
	}
	return dir
}

// attachOutbox empties the outbox onto the turn's final reply. Every file goes
// whether or not it was taken, so one bad file cannot wedge the next turn.
func (s *Server) attachOutbox() {
	session := s.agent.Current()
	dir := s.outboxPath()
	if dir == "" {
		return
	}
	found, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Debug("read the outbox", "err", err)
		}
		return
	}

	names := make([]string, 0, len(found))
	for _, entry := range found {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	var images []chat.Image
	var dropped []string
	for _, name := range names {
		path := filepath.Join(dir, name)
		if img, ok := readOutboxImage(path); ok {
			if len(images) < maxImages {
				images = append(images, img)
			} else {
				dropped = append(dropped, name)
			}
		}
		if err := os.Remove(path); err != nil {
			slog.Debug("clear the outbox", "file", name, "err", err)
		}
	}
	// Said rather than swallowed: the first four by name is not a choice anyone
	// made, and a gallery left in the outbox shows the same four every turn.
	if len(dropped) > 0 {
		s.say(ChatMessage{Kind: chat.KindNote, Body: fmt.Sprintf(
			"A reply carries %d pictures. %d more were in the outbox and have gone: %s",
			maxImages, len(dropped), strings.Join(dropped, ", "))})
	}
	if len(images) == 0 {
		return
	}
	kept, err := pictures(images)
	if err != nil || len(kept) == 0 {
		slog.Debug("keep the outbox pictures", "err", err)
		return
	}
	if s.agent.Current() != session {
		return
	}
	s.attachImages(session, kept)
}

// readOutboxImage is one file as a picture, if it is one. The media type comes
// off the bytes rather than the name: the extension is the agent's claim.
func readOutboxImage(path string) (chat.Image, bool) {
	info, err := os.Stat(path)
	if err != nil {
		slog.Debug("stat an outbox file", "file", filepath.Base(path), "err", err)
		return chat.Image{}, false
	}
	if info.Size() == 0 || info.Size() > maxImageBytes {
		slog.Debug("an outbox file is not a usable size", "file", filepath.Base(path), "bytes", info.Size())
		return chat.Image{}, false
	}
	data, err := os.ReadFile(path) // #nosec G304 -- ade's own directory, and the name came from reading it
	if err != nil {
		slog.Debug("read an outbox file", "file", filepath.Base(path), "err", err)
		return chat.Image{}, false
	}
	media := strings.TrimSpace(strings.Split(http.DetectContentType(data), ";")[0])
	if !strings.HasPrefix(media, "image/") {
		slog.Debug("an outbox file is not an image", "file", filepath.Base(path), "type", media)
		return chat.Image{}, false
	}
	return chat.Image{MediaType: media, Data: base64.StdEncoding.EncodeToString(data)}, true
}

// attachImages puts pictures on the newest reply the main agent made itself.
// Serialised: the preview capture and the outbox both land here at once.
func (s *Server) attachImages(session string, images []chat.Image) {
	if len(images) == 0 {
		return
	}
	s.attaching.Lock()
	defer s.attaching.Unlock()
	for messages := s.chatStore().List(session); len(messages) > 0; messages = messages[:len(messages)-1] {
		message := messages[len(messages)-1]
		if message.Kind == chat.KindAgent && message.Author == "" {
			message.Images = append(message.Images, images...)
			s.amend(message)
			return
		}
	}
	// Nothing of the agent's own to hang them on — a turn that ended on a named
	// agent, or an empty conversation. Shown rather than thrown away.
	s.say(ChatMessage{Kind: chat.KindAgent, Images: images})
}
