package web

import (
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/safepath"
)

// attachmentTypes are served with a type of their own; anything else is
// downloaded, so an attachment cannot run as a page on ade's origin.
var attachmentTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp",
	".txt": "text/plain; charset=utf-8", ".log": "text/plain; charset=utf-8",
	".md": "text/plain; charset=utf-8", ".csv": "text/plain; charset=utf-8",
	".json": "text/plain; charset=utf-8", ".pdf": "application/pdf",
}

// handleAttachment serves a message's attachment by base name, so a name
// cannot reach anything else. A purged one is a 404, which renders struck.
func (s *Server) handleAttachment(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(filepath.FromSlash(r.PathValue("name")))
	if name == "." || name == ".." || name == string(filepath.Separator) {
		http.NotFound(w, r)
		return
	}
	dir := chat.AttachDir(s.space().gitDir)
	// Belt and braces over Base, whatever the router handed over, and a link
	// out of the directory is refused rather than followed.
	path, err := safepath.Inside(dir, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Lstat before the open: Stat through an opened file has already followed
	// the link it was pointed at.
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(path) // #nosec G304 -- base name only, under the attachments directory
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = file.Close() }()

	kind, known := attachmentTypes[strings.ToLower(filepath.Ext(name))]
	if !known {
		kind = "application/octet-stream"
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := io.Copy(w, file); err != nil {
		slog.Debug("write attachment", "err", err)
	}
}
