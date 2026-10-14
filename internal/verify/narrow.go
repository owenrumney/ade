package verify

import (
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

// Narrowed is the narrow pass for a set of changed paths, relative to root.
// A check with no narrow form, or one nothing relevant changed for, is left out.
func Narrowed(root string, checks []Check, changed []string) []Check {
	var out []Check
	for _, check := range checks {
		if check.Narrow == "" {
			continue
		}
		files := within(root, check.Dir, changed)
		vars := map[string]string{}
		if len(files) > 0 {
			vars["Files"] = quoted(files)
		}
		if pkgs := packages(root, check.Dir, files); len(pkgs) > 0 {
			vars["Packages"] = quoted(pkgs)
		}
		run, ok := expand(check.Narrow, vars)
		if !ok {
			continue
		}
		out = append(out, Check{Name: check.Name, Run: run, Dir: check.Dir})
	}
	return out
}

// expand fills the narrow form in. A variable it names and vars lacks is a
// refusal: run bare, `go test {{.Packages}}` tests nothing and passes.
func expand(narrow string, vars map[string]string) (string, bool) {
	tmpl, err := template.New("narrow").Option("missingkey=error").Parse(narrow)
	if err != nil {
		return "", false
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, vars); err != nil {
		return "", false
	}
	run := strings.TrimSpace(b.String())
	return run, run != ""
}

func validNarrow(narrow string) error {
	tmpl, err := template.New("narrow").Option("missingkey=error").Parse(narrow)
	if err != nil {
		return err
	}
	return tmpl.Execute(io.Discard, map[string]string{"Files": "f", "Packages": "p"})
}

// within is the changed paths under dir that still exist, relative to dir.
func within(root, dir string, changed []string) []string {
	dir = path.Clean(filepath.ToSlash(dir))
	var out []string
	for _, file := range changed {
		file = path.Clean(filepath.ToSlash(file))
		if !filepath.IsLocal(filepath.FromSlash(file)) {
			continue
		}
		rel := file
		if dir != "." {
			var ok bool
			if rel, ok = strings.CutPrefix(file, dir+"/"); !ok {
				continue
			}
		}
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(file))); err != nil || info.IsDir() {
			continue
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

// packages is the import paths of the directories holding changed .go files.
// A file outside a module, or under testdata or vendor, names no package.
func packages(root, dir string, files []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, file := range files {
		if path.Ext(file) != ".go" {
			continue
		}
		pkg := importPath(root, path.Dir(path.Join(filepath.ToSlash(dir), file)))
		if pkg == "" || seen[pkg] {
			continue
		}
		seen[pkg] = true
		out = append(out, pkg)
	}
	sort.Strings(out)
	return out
}

func importPath(root, dir string) string {
	for _, part := range strings.Split(dir, "/") {
		if part == "testdata" || part == "vendor" {
			return ""
		}
	}
	for at := dir; ; at = path.Dir(at) {
		if module := moduleName(filepath.Join(root, filepath.FromSlash(at), "go.mod")); module != "" {
			if at == "." {
				return path.Join(module, dir)
			}
			return path.Join(module, strings.TrimPrefix(dir, at))
		}
		if at == "." {
			return ""
		}
	}
}

func moduleName(gomod string) string {
	data, err := os.ReadFile(gomod) // #nosec G304 -- the checkout's own go.mod, read not run
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module"); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
}

// quoted joins words for sh, each in single quotes unless it plainly needs none.
func quoted(words []string) string {
	out := make([]string, len(words))
	for i, word := range words {
		if strings.Trim(word, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./@:+,=-") == "" && !strings.HasPrefix(word, "-") {
			out[i] = word
			continue
		}
		out[i] = "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
	}
	return strings.Join(out, " ")
}
