// Package ratchet counts what should only ever get fewer, against a checked-in
// file of today's numbers. See the design doc.
package ratchet

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// File is the recorded counts, at the repository root.
const File = "ratchet.json"

// longFile is the length a file is counted at.
const longFile = 1000

// Count is how many, and the file holding the worst of them: a number nobody
// can act on is a score, not a limit.
type Count struct {
	Count int    `json:"count"`
	Worst string `json:"worst,omitempty"`
}

// Counts are the measurements by name.
type Counts map[string]Count

const (
	Unrecovered = "goroutines without a recover"
	LongFiles   = "files over 1000 lines"
	Nolint      = "nolint directives"
)

// Measure walks the Go under root and counts what the ratchet holds down.
func Measure(root string) (Counts, error) {
	counts := Counts{Unrecovered: {}, LongFiles: {}, Nolint: {}}
	worst := map[string]int{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir(d.Name()) && path != root {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		body, err := os.ReadFile(path) // #nosec G304 -- a file this walk found under root
		if err != nil {
			return err
		}
		add(counts, worst, Nolint, rel, strings.Count(string(body), "//nolint"))
		if lines := strings.Count(string(body), "\n") + 1; lines > longFile {
			add(counts, worst, LongFiles, rel, 1)
			// The worst long file is the longest one.
			if lines > worst[LongFiles] {
				counts[LongFiles] = Count{Count: counts[LongFiles].Count, Worst: rel}
				worst[LongFiles] = lines
			}
		}
		bare, err := unrecovered(path, body)
		if err != nil {
			return err
		}
		add(counts, worst, Unrecovered, rel, bare)
		return nil
	})
	return counts, err
}

// add records n of a count against the file, keeping the file with the most.
func add(counts Counts, worst map[string]int, name, file string, n int) {
	if n == 0 {
		return
	}
	got := counts[name]
	got.Count += n
	if n > worst[name] {
		got.Worst, worst[name] = file, n
	}
	counts[name] = got
}

// skipDir is everything that is not ade's own Go.
func skipDir(name string) bool {
	switch name {
	case "node_modules", "vendor", "dist", "testdata", ".git", ".github":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// unrecovered counts `go func() { … }()` with no deferred recover. A
// `go namedFunc()` is not counted: what it defers is written elsewhere.
func unrecovered(path string, body []byte) (int, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), path, body, parser.SkipObjectResolution)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", path, err)
	}
	bare := 0
	ast.Inspect(parsed, func(n ast.Node) bool {
		started, ok := n.(*ast.GoStmt)
		if !ok {
			return true
		}
		lit, ok := started.Call.Fun.(*ast.FuncLit)
		if !ok {
			return true
		}
		if !recovers(lit.Body) {
			bare++
		}
		return true
	})
	return bare, nil
}

// recovers is whether the block defers a recover, its own or a named one.
func recovers(block *ast.BlockStmt) bool {
	held := false
	ast.Inspect(block, func(n ast.Node) bool {
		deferred, ok := n.(*ast.DeferStmt)
		if !ok {
			return !held
		}
		if name := called(deferred.Call.Fun); name == "recover" || name == "contain" || name == "contained" {
			held = true
		}
		if lit, ok := deferred.Call.Fun.(*ast.FuncLit); ok {
			ast.Inspect(lit.Body, func(in ast.Node) bool {
				if call, ok := in.(*ast.CallExpr); ok && called(call.Fun) == "recover" {
					held = true
				}
				return true
			})
		}
		return !held
	})
	return held
}

func called(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// Load reads the recorded counts.
func Load(file string) (Counts, error) {
	body, err := os.ReadFile(file) // #nosec G304 -- the repository's own ratchet file
	if err != nil {
		return nil, err
	}
	var counts Counts
	if err := json.Unmarshal(body, &counts); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return counts, nil
}

// Record writes counts back, so a count that fell is the new ceiling.
func Record(file string, counts Counts) error {
	body, err := json.MarshalIndent(counts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, append(body, '\n'), 0o600)
}

// Names are the counts in a stable order.
func Names(counts Counts) []string {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
