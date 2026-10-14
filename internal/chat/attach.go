package chat

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"github.com/owenrumney/ade/internal/statedir"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Incoming is a file as the browser sends it, base64 encoded.
type Incoming struct {
	Name      string `json:"name"`
	MediaType string `json:"mediaType"`
	Data      string `json:"data"`
}

// What one message can carry. BlockBytes is the API's limit on an image
// block; an image over it is still attached, just without one.
const (
	MaxFiles      = 10
	MaxFileBytes  = 20 << 20
	MaxTotalBytes = 40 << 20
	BlockBytes    = 5 << 20
)

// numbered matches the counter Save prefixes, so the next carries on from
// the highest on disk rather than from what this process wrote.
var numbered = regexp.MustCompile(`^(\d{4})-`)

// unsafeRune is anything not kept in a stored name: it reaches a path.
var unsafeRune = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// controlRune is anything below a space. The shown name reaches the agent as
// a line of the prompt, so a newline in it would be a line of its own.
var controlRune = regexp.MustCompile(`[\x00-\x1f\x7f]+`)

// AttachDir is where a checkout's attachments live, beside its conversations.
func AttachDir(gitDir string) string { return filepath.Join(statedir.Git(gitDir), "attachments") }

// Check applies the caps before anything is written, so the message can
// name the limit rather than the request failing.
func Check(files []Incoming) error {
	if len(files) > MaxFiles {
		return fmt.Errorf("%d files is too many; %d at most on one message", len(files), MaxFiles)
	}
	var total int
	for _, f := range files {
		size := decodedSize(f.Data)
		if size > MaxFileBytes {
			return fmt.Errorf("%s is over %dMB; ask the agent to read it from where it lives", displayName(f.Name), MaxFileBytes>>20)
		}
		total += size
	}
	if total > MaxTotalBytes {
		return fmt.Errorf("that is over %dMB of attachments for one message", MaxTotalBytes>>20)
	}
	return nil
}

// Save writes each file under AttachDir and reports where it went. The
// counter prefix keeps two files of one name apart; empty ones are skipped.
func Save(gitDir string, files []Incoming) ([]File, error) {
	if len(files) == 0 {
		return nil, nil
	}
	if err := Check(files); err != nil {
		return nil, err
	}
	dir := AttachDir(gitDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("make the attachments directory: %w", err)
	}
	next := highest(dir) + 1
	var out []File
	for _, f := range files {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(f.Data))
		if err != nil {
			return nil, fmt.Errorf("%s did not arrive whole", displayName(f.Name))
		}
		if len(raw) == 0 {
			continue
		}
		name := displayName(f.Name)
		path := filepath.Join(dir, fmt.Sprintf("%04d-%s-%s", next, token(), safeName(name)))
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", name, err)
		}
		next++
		out = append(out, File{Name: name, Path: path, Image: strings.HasPrefix(f.MediaType, "image/")})
	}
	return out, nil
}

// Block is an attached image small enough for a content block, read back off
// disk. Anything else reaches the agent as a path alone.
func Block(f File) (mediaType, data string, ok bool) {
	media := mediaOf(f.Name)
	if !f.Image || media == "" {
		return "", "", false
	}
	info, err := os.Stat(f.Path)
	if err != nil || info.Size() > BlockBytes || info.Size() == 0 {
		return "", "", false
	}
	raw, err := os.ReadFile(f.Path) // #nosec G304 -- a path under the attachments directory
	if err != nil {
		return "", "", false
	}
	return media, base64.StdEncoding.EncodeToString(raw), true
}

// decodedSize is how many bytes a base64 string carries. DecodedLen rounds
// past the padding, putting a file exactly on the cap over it.
func decodedSize(data string) int {
	data = strings.TrimSpace(data)
	n := len(data)
	if n == 0 {
		return 0
	}
	return n/4*3 - strings.Count(data[max(0, n-2):], "=")
}

// token is what a purge cannot hand back. Emptying the directory restarts the
// counter, and an old message resolves its chip by stored name.
func token() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "0"
	}
	return hex.EncodeToString(b[:])
}

// highest is the largest counter already used in dir, read back so a restart
// does not reuse a number.
func highest(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var seen []int
	for _, entry := range entries {
		m := numbered.FindStringSubmatch(entry.Name())
		if m == nil {
			continue
		}
		if n, err := strconv.Atoi(m[1]); err == nil {
			seen = append(seen, n)
		}
	}
	if len(seen) == 0 {
		return 0
	}
	sort.Ints(seen)
	return seen[len(seen)-1]
}

// displayName is the base of what was dropped, so no path travels with it.
func displayName(name string) string {
	name = strings.TrimSpace(controlRune.ReplaceAllString(name, " "))
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(filepath.Clean("/" + name))
	if name == "/" || name == "." || name == ".." {
		return "attachment"
	}
	if len(name) > 120 {
		name = name[:120]
	}
	return name
}

// safeName is displayName reduced to what goes in a filename.
func safeName(name string) string {
	name = unsafeRune.ReplaceAllString(name, "_")
	name = strings.Trim(name, "._")
	if name == "" {
		return "attachment"
	}
	return name
}

// mediaOf is a stored image's media type, from its extension: what the browser
// claimed is not kept, the file is. Empty is a kind the API has no block for.
func mediaOf(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	default:
		return ""
	}
}
