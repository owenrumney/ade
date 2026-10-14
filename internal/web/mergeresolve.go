package web

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/outgoing"
	"github.com/owenrumney/ade/internal/prompt"
	"github.com/owenrumney/ade/internal/safepath"
)

// What a resolve will attempt before it gives up and puts the tree back. Past
// these the merge is a decision about the work, not a tidy-up.
const (
	maxConflictFiles = 12
	maxConflictBytes = 120_000
)

// markers are what git leaves in a conflicted file. A resolution that still
// has any of them is not a resolution.
var markers = []string{"<<<<<<<", "=======", ">>>>>>>"}

// resolveConflicts asks one model call per conflicted file. A file it cannot
// settle fails the whole resolve: a merge is all of it or none.
func (s *Server) resolveConflicts(ctx context.Context, dir string, files []string) error {
	if len(files) > maxConflictFiles {
		return fmt.Errorf("%d files conflict, which is too many to resolve automatically", len(files))
	}
	for _, name := range files {
		if err := s.resolveOne(ctx, dir, name); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) resolveOne(ctx context.Context, dir, name string) error {
	path, err := safepath.Inside(dir, name)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- a path safepath resolved inside dir
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	if len(raw) > maxConflictBytes {
		return fmt.Errorf("%s is too big to resolve automatically", name)
	}
	if !hasMarkers(string(raw)) {
		return fmt.Errorf("%s is conflicted but has no markers to resolve", name)
	}

	var reply strings.Builder
	turn, err := s.oneShot(ctx, bridge.OneShotOpts{
		Bin:    s.agent.bin,
		Model:  outgoing.FastModel,
		Dir:    dir,
		Prompt: conflictPrompt(name, string(raw)),
		// No tools at all: the file goes in as text and comes back as text,
		// so nothing it says can reach the checkout except through this.
		Tools:   []string{},
		NoMCP:   true,
		OnEvent: func(ev bridge.Event) { reply.WriteString(ev.Text) },
	})
	if err != nil {
		return fmt.Errorf("resolve %s: %w", name, err)
	}
	reply.WriteString(turn.Text)

	merged, err := resolved(reply.String())
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if err := os.WriteFile(path, []byte(merged), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := gitdiff.Stage(ctx, dir, []string{name}); err != nil {
		return fmt.Errorf("stage %s: %w", name, err)
	}
	return nil
}

// resolved reads the file back out of the reply and refuses anything that
// still carries a marker: a half-resolved file staged is work silently lost.
func resolved(reply string) (string, error) {
	body, ok := between(reply, "<merged>", "</merged>")
	if !ok {
		return "", fmt.Errorf("the model did not answer with a merged file")
	}
	body = strings.Trim(body, "\n")
	if body == "" {
		return "", fmt.Errorf("the model answered with an empty file")
	}
	if hasMarkers(body) {
		return "", fmt.Errorf("the model left conflict markers in its answer")
	}
	return body + "\n", nil
}

func between(text, open, close string) (string, bool) {
	start := strings.Index(text, open)
	if start < 0 {
		return "", false
	}
	rest := text[start+len(open):]
	end := strings.LastIndex(rest, close)
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

func hasMarkers(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		for _, mark := range markers {
			if strings.HasPrefix(line, mark) {
				return true
			}
		}
	}
	return false
}

// conflictPrompt hands over one file and asks for it back whole. The file is
// fenced as data: it is somebody's source, and some of it is a diff.
func conflictPrompt(name, body string) string {
	return fmt.Sprintf(`You are resolving one git merge conflict. %s

The file %s is below between <file> tags, exactly as git left it, with
conflict markers. <<<<<<< opens the version already on this branch, ======= it
separates the two, and >>>>>>> closes the version being merged in.

Resolve every conflict by keeping what both sides meant. Where the two changed
different things, keep both. Where they changed the same thing, keep the
incoming version. Change nothing outside the conflicted regions.

Answer with the whole file between <merged> and </merged> tags and nothing
else: no explanation, no code fence, no conflict markers. The answer replaces
the file byte for byte.

%s`,
		prompt.Untrusted,
		filepath.Base(name),
		prompt.Fence("file", body),
	)
}

// settleMerge resolves what the merge could not and commits it. Anything it
// cannot settle puts the tree back: a merge is all of it or none.
func (s *Server) settleMerge(ctx context.Context, dir, from, into string, stuck []string) error {
	if err := s.resolveConflicts(ctx, dir, stuck); err != nil {
		if abortErr := gitdiff.AbortMerge(ctx, dir); abortErr != nil {
			return fmt.Errorf("%v, and the merge could not be undone: %w", err, abortErr)
		}
		return fmt.Errorf("%v; nothing was merged", err)
	}
	message := fmt.Sprintf("Merge branch '%s' into %s\n\n%s resolved conflicts in: %s",
		from, into, outgoing.FastModel, strings.Join(stuck, ", "))
	if err := gitdiff.FinishMerge(ctx, dir, message); err != nil {
		_ = gitdiff.AbortMerge(ctx, dir)
		return err
	}
	return nil
}
