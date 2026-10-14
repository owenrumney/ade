package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Reading a checkout is what a reviewer needs; these are that and no more.
// Every path is confined to root, so a model cannot be talked into reading
// the login it is running on.
const (
	maxFileBytes = 200 * 1024
	maxMatches   = 200
	maxListed    = 500
)

// RepoTools is the tools over a checkout: read a file, list a directory,
// search for a pattern, and — only when named — write or edit a file.
// names picks by the names Claude Code uses (Read, Grep, Glob/LS, Write,
// Edit), so one agent definition restricts both drivers the same way; none
// is the read-only set.
func RepoTools(root string, names []string, also ...string) []Tool {
	want := map[string]bool{}
	for _, name := range names {
		switch strings.ToLower(name) {
		case "read", "read_file":
			want["read_file"] = true
		case "grep":
			want["grep"] = true
		case "glob", "ls", "list_files":
			want["list_files"] = true
		case "write", "write_file":
			want["write_file"] = true
		case "edit", "edit_file":
			want["edit_file"] = true
		}
	}
	// An agent that only reads gets numbered lines, so it can cite one. An
	// agent that edits does not: it matches on the file's own text.
	edits := want["write_file"] || want["edit_file"]
	reading := []Tool{readFile(root, also, !edits), listFiles(root), grep(root)}
	if len(names) == 0 {
		return reading
	}
	var out []Tool
	for _, tool := range append(reading, writeFile(root), editFile(root)) {
		if want[tool.Name] {
			out = append(out, tool)
		}
	}
	return out
}

func readFile(root string, also []string, numbered bool) Tool {
	description := "Read a file in the repository. Paths are relative to the repository root."
	if len(also) > 0 {
		description += " An absolute path you were given, such as the design document's, works as well."
	}
	if numbered {
		description += " Each line comes back as `<number>\t<text>`; the number is the file's line number, not part of the line. Cite it."
	}
	return Tool{
		Name:        "read_file",
		Description: description,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "path relative to the repository root"},
			},
			"required": []string{"path"},
		},
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", err
			}
			data, err := readAt(root, also, args.Path)
			if err != nil {
				return "", err
			}
			if len(data) > maxFileBytes {
				return number(string(data[:maxFileBytes]), numbered) + "\n… (truncated)", nil
			}
			return number(string(data), numbered), nil
		},
	}
}

// readAt reads rel from the checkout, or from a directory beside it when rel is an absolute path under one.
func readAt(root string, also []string, rel string) ([]byte, error) {
	if filepath.IsAbs(rel) {
		for _, dir := range also {
			if dir == "" {
				continue
			}
			base, err := filepath.EvalSymlinks(dir)
			if err != nil {
				continue
			}
			if sub, err := local(rel, dir, base); err == nil {
				return readInside(dir, sub)
			}
		}
	}
	return readInside(root, rel)
}

// readInside reads rel, confined to root twice over: by name, through
// inside, and then at the open, through an os.Root over the resolved
// checkout. A link swapped in between the check and the read resolves
// against the root rather than the filesystem, and one that leads out of it
// is refused by the open itself.
func readInside(root, rel string) ([]byte, error) {
	real, err := inside(root, rel)
	if err != nil {
		return nil, err
	}
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	sub, err := filepath.Rel(base, real)
	if err != nil {
		return nil, err
	}
	f, err := openUnder(base, sub)
	if err != nil {
		return nil, fmt.Errorf("%s is outside the repository: %w", rel, err)
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, maxFileBytes+1))
}

// openUnder opens sub through an os.Root at base, so nothing sub goes
// through can lead outside base.
func openUnder(base, sub string) (*os.File, error) {
	r, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	return r.Open(sub)
}

// number prefixes each line with its own number, the way Claude Code's Read does.
func number(text string, on bool) string {
	if !on || text == "" {
		return text
	}
	// A file ends in a newline; that is not a last empty line to number.
	body, end := strings.CutSuffix(text, "\n")
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		lines[i] = strconv.Itoa(i+1) + "\t" + line
	}
	if end {
		return strings.Join(lines, "\n") + "\n"
	}
	return strings.Join(lines, "\n")
}

func listFiles(root string) Tool {
	return Tool{
		Name:        "list_files",
		Description: "List the files under a directory of the repository, recursively. Omit path for the whole repository.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "directory relative to the repository root"},
			},
		},
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Path string `json:"path"`
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &args); err != nil {
					return "", err
				}
			}
			dir, err := inside(root, args.Path)
			if err != nil {
				return "", err
			}
			base, _ := filepath.EvalSymlinks(root)
			var out []string
			err = walk(dir, func(path string) bool {
				rel, _ := filepath.Rel(base, path)
				out = append(out, filepath.ToSlash(rel))
				return len(out) < maxListed
			})
			if err != nil {
				return "", err
			}
			sort.Strings(out)
			if len(out) >= maxListed {
				out = append(out, "… (more)")
			}
			return strings.Join(out, "\n"), nil
		},
	}
}

func grep(root string) Tool {
	return Tool{
		Name:        "grep",
		Description: "Search the repository for a regular expression. Returns path:line: text for each match.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string", "description": "Go/RE2 regular expression"},
				"path":    map[string]any{"type": "string", "description": "directory or file to search, relative to the root; omit for everything"},
			},
			"required": []string{"pattern"},
		},
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Pattern string `json:"pattern"`
				Path    string `json:"path"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", err
			}
			re, err := regexp.Compile(args.Pattern)
			if err != nil {
				return "", fmt.Errorf("bad pattern: %w", err)
			}
			start, err := inside(root, args.Path)
			if err != nil {
				return "", err
			}
			base, _ := filepath.EvalSymlinks(root)
			var out []string
			err = walk(start, func(path string) bool {
				data, err := os.ReadFile(path) // #nosec G304 -- under root
				if err != nil || len(data) > 2*maxFileBytes || !utf8Like(data) {
					return true
				}
				rel, _ := filepath.Rel(base, path)
				for i, line := range strings.Split(string(data), "\n") {
					if re.MatchString(line) {
						out = append(out, fmt.Sprintf("%s:%d: %s", filepath.ToSlash(rel), i+1, strings.TrimSpace(line)))
						if len(out) >= maxMatches {
							return false
						}
					}
				}
				return true
			})
			if err != nil {
				return "", err
			}
			if len(out) == 0 {
				return "no matches", nil
			}
			if len(out) >= maxMatches {
				out = append(out, "… (more)")
			}
			return strings.Join(out, "\n"), nil
		},
	}
}

// Writing is what "fix it" needs, and it is confined the way reading is,
// with one more refusal: nothing under .git, where a file is a hook the
// user's next commit would run.
const maxWriteBytes = 1024 * 1024

func writeFile(root string) Tool {
	return Tool{
		Name:        "write_file",
		Description: "Write a file in the repository, creating it or replacing its contents. Paths are relative to the repository root.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "path relative to the repository root"},
				"content": map[string]any{"type": "string", "description": "the whole contents of the file"},
			},
			"required": []string{"path", "content"},
		},
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", err
			}
			if err := writeInside(root, args.Path, []byte(args.Content)); err != nil {
				return "", err
			}
			return fmt.Sprintf("wrote %s (%d bytes)", args.Path, len(args.Content)), nil
		},
	}
}

func editFile(root string) Tool {
	return Tool{
		Name:        "edit_file",
		Description: "Replace text in a file in the repository. old_string must appear exactly once unless replace_all is set; include enough surrounding text to make it unique.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":        map[string]any{"type": "string", "description": "path relative to the repository root"},
				"old_string":  map[string]any{"type": "string", "description": "the exact text to replace"},
				"new_string":  map[string]any{"type": "string", "description": "what to replace it with"},
				"replace_all": map[string]any{"type": "boolean", "description": "replace every occurrence rather than requiring exactly one"},
			},
			"required": []string{"path", "old_string", "new_string"},
		},
		Run: func(_ context.Context, raw json.RawMessage) (string, error) {
			var args struct {
				Path       string `json:"path"`
				Old        string `json:"old_string"`
				New        string `json:"new_string"`
				ReplaceAll bool   `json:"replace_all"`
			}
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", err
			}
			if args.Old == "" {
				return "", errors.New("old_string is empty")
			}
			data, err := readInside(root, args.Path)
			if err != nil {
				return "", err
			}
			if len(data) > maxFileBytes {
				return "", fmt.Errorf("%s is too big to edit", args.Path)
			}
			text := string(data)
			n := strings.Count(text, args.Old)
			switch {
			case n == 0:
				return "", fmt.Errorf("old_string was not found in %s", args.Path)
			case n > 1 && !args.ReplaceAll:
				return "", fmt.Errorf("old_string appears %d times in %s; add context to make it unique, or set replace_all", n, args.Path)
			}
			text = strings.ReplaceAll(text, args.Old, args.New)
			if err := writeInside(root, args.Path, []byte(text)); err != nil {
				return "", err
			}
			return fmt.Sprintf("edited %s (%d replacement(s))", args.Path, n), nil
		},
	}
}

// writeInside writes rel under root, creating directories on the way, with
// the confinement readInside has: the nearest ancestor on disk resolved
// through inside, every step of the open through an os.Root. It refuses
// .git: hooks run on the user's machine at their next commit, and a
// worktree's .git is the file that makes it one.
func writeInside(root, rel string, data []byte) error {
	if len(data) > maxWriteBytes {
		return fmt.Errorf("%s: too big to write (%d bytes)", rel, len(data))
	}
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." || strings.HasSuffix(rel, "/") {
		return errors.New("path must name a file")
	}
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	// The absolute path a read handed back, when it is this checkout's. A
	// directory only read_file may reach is not one, so it is refused here.
	if filepath.IsAbs(rel) {
		sub, err := local(rel, root, base)
		if err != nil {
			return err
		}
		rel = sub
	}
	clean := filepath.Clean(rel)
	if first := strings.SplitN(filepath.ToSlash(clean), "/", 2)[0]; first == ".git" || first == ".." {
		return errors.New("nothing under .git or outside the repository is written")
	}
	gitDir := filepath.Join(base, ".git")
	for probe := filepath.Dir(clean); ; probe = filepath.Dir(probe) {
		real, err := inside(root, probe)
		if err == nil {
			if under(real, gitDir) {
				return errors.New("nothing under .git is written")
			}
			break
		}
		if !errors.Is(err, fs.ErrNotExist) || probe == "." {
			return err
		}
	}
	r, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	if dir := filepath.Dir(clean); dir != "." {
		if err := r.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("%s is outside the repository: %w", rel, err)
		}
	}
	f, err := r.OpenFile(clean, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644) // #nosec G302 -- source, not a secret
	if err != nil {
		return fmt.Errorf("%s is outside the repository: %w", rel, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// inside resolves rel under root and refuses anything that escapes it —
// lexically, and then for real: a symlink committed in the checkout that
// points at the login file resolves inside root by name and outside it on
// disk, and the check is on disk.
func inside(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		rel = "."
	}
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	// A model hands back the absolute path it was given — the design document's,
	// say. Only one that leads out of the checkout is a refusal.
	if filepath.IsAbs(rel) {
		sub, err := local(rel, root, base)
		if err != nil {
			return "", err
		}
		rel = sub
	}
	path := filepath.Clean(filepath.Join(root, rel))
	if !under(path, root) {
		return "", fmt.Errorf("%s is outside the repository", rel)
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	if !under(real, base) {
		return "", fmt.Errorf("%s is outside the repository", rel)
	}
	return real, nil
}

// local turns an absolute path under one of dirs into a path relative to the
// root, so the checkout's own spelling and its resolved one both work.
func local(abs string, dirs ...string) (string, error) {
	for _, dir := range dirs {
		if sub, err := filepath.Rel(dir, filepath.Clean(abs)); err == nil && filepath.IsLocal(sub) {
			return sub, nil
		}
	}
	return "", fmt.Errorf("%s is outside the repository", abs)
}

func under(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

// walk visits the files under dir (or dir itself, if it is a file), skipping
// what no reviewer wants: .git, node_modules, vendor. visit returns false to
// stop.
func walk(dir string, visit func(path string) bool) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		visit(dir)
		return nil
	}
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable is skipped, not fatal
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "dist":
				if path != dir {
					return filepath.SkipDir
				}
			}
			return nil
		}
		// Only plain files. A symlink is followed nowhere: it could lead
		// out of the repository, and nothing here should.
		if !d.Type().IsRegular() {
			return nil
		}
		if !visit(path) {
			return filepath.SkipAll
		}
		return nil
	})
}

// utf8Like is a cheap "is this text" check: no NUL in the first block.
func utf8Like(data []byte) bool {
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	return !strings.ContainsRune(string(head), 0)
}
