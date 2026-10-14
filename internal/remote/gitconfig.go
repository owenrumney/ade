package remote

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// GitConfig is one of the laptop's git configuration files, named as it should
// land in the worker's HOME.
type GitConfig struct {
	Name string
	Data []byte
}

// gitdirRule is an includeIf path. They key on the laptop's checkouts, which
// sit elsewhere on a worker, so every one is rewritten or none of them match.
var gitdirRule = regexp.MustCompile(`(\[\s*includeIf\s+"gitdir:)([^"]*)(")`)

// signingRule is any signing key. Only the one ade sends exists over there.
var signingRule = regexp.MustCompile(`(?mi)^(\s*signingkey\s*=\s*).*$`)

var urlSection = regexp.MustCompile(`(?i)^\[\s*url\s+"([^"]*)"\s*\]`)

var insteadOfKey = regexp.MustCompile(`(?i)^(push)?insteadof\s*=`)

// RetargetGitConfig rewrites what is machine-specific. Left alone, includeIf
// matches nothing and the top-level identity signs for every repository.
func RetargetGitConfig(data []byte, home, local, work string) []byte {
	out := gitdirRule.ReplaceAllFunc(data, func(match []byte) []byte {
		parts := gitdirRule.FindSubmatch(match)
		moved := retarget(string(parts[2]), home, local, work)
		return []byte(string(parts[1]) + moved + string(parts[3]))
	})
	out = signingRule.ReplaceAll(out, []byte("${1}~/.ssh/ade_signing.pub"))
	return dropSSHRewrites(out)
}

// dropSSHRewrites strikes out the rules sending https to ssh: over there the
// gh token is what logs in.
func dropSSHRewrites(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	var toSSH, more bool
	for i, line := range lines {
		rest := strings.TrimSpace(line)
		// A value continued over a backslash goes with the line that opened it:
		// left behind, git reads it as a section of its own and refuses the file.
		if more {
			lines[i] = struck(line)
			more = continued(line)
			continue
		}
		if head := urlSection.FindStringSubmatch(rest); head != nil {
			toSSH = !strings.HasPrefix(strings.ToLower(head[1]), "http")
			rest = strings.TrimSpace(rest[len(head[0]):])
		} else if strings.HasPrefix(rest, "[") {
			toSSH = false
		}
		if toSSH && insteadOfKey.MatchString(rest) {
			lines[i] = struck(strings.TrimSpace(line))
			more = continued(line)
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

func struck(line string) string {
	return "# ade removed this, the worker pushes over https: " + line
}

func continued(line string) bool {
	return strings.HasSuffix(strings.TrimRight(line, " \t\r"), `\`)
}

// retarget moves one path from where the laptop keeps checkouts to where the
// worker does. Anything outside that is left alone: it means something else.
func retarget(path, home, local, work string) string {
	home, local = strings.TrimSuffix(home, "/"), strings.TrimSuffix(local, "/")
	work = strings.TrimSuffix(work, "/")
	if home != "" && strings.HasPrefix(path, "~/") {
		path = home + "/" + strings.TrimPrefix(path, "~/")
	}
	if local == "" || !strings.HasPrefix(path, local+"/") {
		return path
	}
	return work + "/" + strings.TrimPrefix(path, local+"/")
}

// writeGitConfig puts one file in the worker's HOME, read from stdin so a
// config with a token in a url never reaches a process list.
const writeGitConfig = `set -eu
cat > "$HOME/$1"
chmod 600 "$HOME/$1"`

// PrepareGitConfig copies the laptop's git configuration, retargeted. The
// includeIf files carry the per-repository identity, which is the point.
func (h *Host) PrepareGitConfig(ctx context.Context, files []GitConfig, home, local, work string) error {
	for _, file := range files {
		if len(file.Data) == 0 || strings.ContainsAny(file.Name, "/\\") {
			continue
		}
		body := RetargetGitConfig(file.Data, home, local, work)
		if _, err := h.RunInput(ctx, body, "docker", "exec", "-i", workerName,
			"sh", "-c", writeGitConfig, "sh", file.Name); err != nil {
			return fmt.Errorf("send %s to %s: %w", file.Name, h.Name, err)
		}
	}
	return nil
}
