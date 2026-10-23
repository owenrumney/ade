package verify

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Args is how a caller narrows a check. Everything here may have come from an
// agent, so none of it is ever part of a shell string.
type Args struct {
	// Paths are packages or files to run against, relative to the check's dir.
	Paths []string
	// Filter is a test-name pattern, passed only by a check that declares the
	// flag to pass it as.
	Filter string
}

// Empty is a call that narrowed nothing, which runs the check as it stands.
func (a Args) Empty() bool { return len(a.Paths) == 0 && a.Filter == "" }

// safeFilter is what a test-name pattern may be made of: the regexp
// metacharacters Go, vitest and pytest take, and nothing a shell reads.
var safeFilter = regexp.MustCompile(`^[A-Za-z0-9_./|^$()\[\]*+?\\-]+$`)

// Argv is the command for a check, narrowed. The agent's values go after a
// `"$@"`, which the shell never re-reads: a dash stays an argument.
func Argv(root string, check Check, args Args) ([]string, error) {
	if args.Empty() {
		return []string{"sh", "-c", check.Run}, nil
	}
	extra, err := extraArgs(root, check, args)
	if err != nil {
		return nil, err
	}
	// $0 is named so a shell error says which check it came from.
	return append([]string{"sh", "-c", check.Run + ` "$@"`, check.Name}, extra...), nil
}

func extraArgs(root string, check Check, args Args) ([]string, error) {
	var out []string
	for _, each := range args.Paths {
		one, err := safePath(root, check.Dir, each)
		if err != nil {
			return nil, err
		}
		out = append(out, one)
	}
	if args.Filter == "" {
		return out, nil
	}
	if check.Filter == "" {
		return nil, fmt.Errorf("check %q takes no test-name pattern", check.Name)
	}
	// $ anchors a pattern; $( opens a substitution. Harmless as one argument,
	// refused anyway: it is never a test name, and the next caller may quote.
	if !safeFilter.MatchString(args.Filter) || strings.Contains(args.Filter, "$(") {
		return nil, fmt.Errorf("%q is not a test-name pattern", args.Filter)
	}
	return append(out, fmt.Sprintf(check.Filter, args.Filter)), nil
}

// safePath is one path inside the checkout, as the command should see it. The
// leading ./ is what stops a file called -rf being read as a flag.
func safePath(root, dir, asked string) (string, error) {
	asked = strings.TrimSpace(asked)
	if asked == "" {
		return "", errors.New("an empty path is not one to run against")
	}
	clean := path.Clean(filepath.ToSlash(asked))
	clean = strings.TrimPrefix(clean, "./")
	if !filepath.IsLocal(filepath.FromSlash(clean)) {
		return "", fmt.Errorf("%q is outside the checkout", asked)
	}
	if _, err := os.Stat(filepath.Join(root, dir, filepath.FromSlash(clean))); err != nil {
		return "", fmt.Errorf("%q is not in this checkout", asked)
	}
	return "./" + clean, nil
}

// validFilter is a check's own flag form: one %s and nothing else, because a
// pattern goes in as a single argument.
func validFilter(form string) error {
	if form == "" {
		return nil
	}
	if strings.Count(form, "%s") != 1 || strings.Count(form, "%") != 1 {
		return errors.New("it must hold exactly one %s")
	}
	return nil
}
