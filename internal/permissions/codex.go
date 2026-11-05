package permissions

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// codexFile is the one rules file ade owns, beside the user's own.
const codexFile = "ade.rules"

// wrapper is rtk: a hook of the user's rewrites a command to run through it.
const wrapper = "rtk "

// Wrapped is the deny list with each command rule again behind the wrapper.
func Wrapped(deny []Rule) []Rule {
	out := slices.Clone(deny)
	for _, rule := range deny {
		p, ok := rule.parse()
		if !ok || p.tool != bashTool || p.content == "" || strings.HasPrefix(p.content, wrapper) {
			continue
		}
		text := strings.TrimSpace(string(rule))
		open := strings.Index(text, "(") + 1
		twin := Rule(text[:open] + wrapper + strings.TrimSpace(text[open:]))
		if !slices.Contains(out, twin) {
			out = append(out, twin)
		}
	}
	return out
}

// CodexRules is the deny list as codex reads it. Commands only: codex has no
// rule for a path, so a Read or an Edit deny does not carry over.
func CodexRules(deny []Rule) string {
	var b strings.Builder
	b.WriteString("# Written by ade from its deny list, and rewritten whenever that changes.\n")
	for _, rule := range deny {
		p, ok := rule.parse()
		if !ok || p.tool != bashTool || p.content == "" || strings.ContainsAny(p.content, "*?|&;<>$`\"'\\") {
			continue
		}
		words := strings.Fields(p.content)
		for i, word := range words {
			words[i] = strconv.Quote(word)
		}
		fmt.Fprintf(&b, "prefix_rule(pattern=[%s], decision=\"forbidden\")\n", strings.Join(words, ", "))
	}
	return b.String()
}

// WriteCodexRules puts the deny list where codex reads its rules from.
func WriteCodexRules(deny []Rule) error {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		user, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("find the home directory: %w", err)
		}
		home = filepath.Join(user, ".codex")
	}
	dir := filepath.Join(home, "rules")
	path := filepath.Join(dir, codexFile)
	want := CodexRules(deny)
	if have, err := os.ReadFile(path); err == nil && string(have) == want { // #nosec G304 -- codex's own rules folder
		return nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(want), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
