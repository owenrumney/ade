package repostats

import (
	"os"
	"path/filepath"
	"strings"
)

// names maps an extension to what the page calls it. Anything unlisted is
// "Other": a language split is for seeing the shape of a repository, and a
// long tail of one-file extensions hides it.
//
//nolint:gochecknoglobals // a lookup table, not state
var names = map[string]string{
	".go": "Go", ".ts": "TypeScript", ".tsx": "TypeScript", ".js": "JavaScript",
	".mjs": "JavaScript", ".cjs": "JavaScript", ".jsx": "JavaScript",
	".py": "Python", ".rs": "Rust", ".c": "C", ".h": "C", ".cc": "C++",
	".cpp": "C++", ".hpp": "C++", ".java": "Java", ".kt": "Kotlin",
	".rb": "Ruby", ".php": "PHP", ".cs": "C#", ".swift": "Swift",
	".sh": "Shell", ".bash": "Shell", ".zsh": "Shell", ".fish": "Shell",
	".html": "HTML", ".htm": "HTML", ".css": "CSS", ".scss": "CSS",
	".md": "Markdown", ".mdx": "Markdown", ".json": "JSON", ".yaml": "YAML",
	".yml": "YAML", ".toml": "TOML", ".tf": "Terraform", ".tfvars": "Terraform",
	".sql": "SQL", ".proto": "Protobuf", ".svg": "SVG", ".lua": "Lua",
	".vim": "Vim", ".dockerfile": "Docker", ".gradle": "Gradle",
}

// basenames are the files whose name, not extension, says what they are.
//
//nolint:gochecknoglobals // a lookup table, not state
var basenames = map[string]string{
	"Dockerfile": "Docker", "Makefile": "Make", "Justfile": "Make",
	"go.mod": "Go", "go.sum": "Go",
}

func languageOf(path string) string {
	base := filepath.Base(path)
	if name, ok := basenames[base]; ok {
		return name
	}
	if name, ok := names[strings.ToLower(filepath.Ext(base))]; ok {
		return name
	}
	return "Other"
}

func openRead(path string) (*os.File, error) {
	return os.Open(path) // #nosec G304 -- a file git listed inside the repository being read
}
