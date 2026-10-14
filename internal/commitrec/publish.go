package commitrec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/owenrumney/ade/internal/wiki"
)

// sessionPrefix names the reserved wiki draft records publish through, so they
// get the secret scan and the no-delete rule that refs/notes would not have.
const sessionPrefix = "ade-commit-records-"

// Wiki is the part of the shared wiki publishing needs.
type Wiki interface {
	Enabled() bool
	Draft(ctx context.Context, session, repoRoot string) (wiki.Draft, error)
	Submit(ctx context.Context, draft wiki.Draft, agent string) (wiki.Submission, error)
	Page(path string) ([]byte, error)
}

// Session is one repository's draft. Per repository, because a held draft keeps
// the repo it was seeded for and would file another's records under it.
func Session(repo string) string {
	return sessionPrefix + strings.ReplaceAll(repo, "/", "-")
}

// Load reads a record back out of the wiki, by a key it was published under.
// An amend files the record afresh and the older key's file stays as it was.
func Load(w Wiki, repo, key string) (Record, bool) {
	if !ValidKey(key) {
		return Record{}, false
	}
	data, err := w.Page(Path(repo, key))
	if err != nil {
		return Record{}, false
	}
	r, err := Parse(repo, string(data))
	if err != nil {
		return Record{}, false
	}
	// A file that disagrees with the path it was found under would be re-filed
	// somewhere else on the next save.
	if r.Key() != key {
		return Record{}, false
	}
	return r, true
}

// LoadIndex reads the merge index, empty when there is not one yet.
func LoadIndex(w Wiki, repo string) Index {
	data, err := w.Page(IndexPath(repo))
	if err != nil {
		return nil
	}
	return ParseIndex(string(data))
}

// Save publishes records and, when idx is not nil, the merge index with them —
// one submit, so a merge and its records cannot land half-applied.
func Save(ctx context.Context, w Wiki, repoRoot, repo string, records []Record, idx Index) error {
	if !w.Enabled() {
		return errors.New("wiki is disabled; commit records have nowhere to go")
	}
	if len(records) == 0 && idx == nil {
		return nil
	}

	draft, err := w.Draft(ctx, Session(repo), repoRoot)
	if err != nil {
		return fmt.Errorf("open the commit-record draft: %w", err)
	}
	if draft.Repo != repo {
		return fmt.Errorf("the commit-record draft is for %s, not %s", draft.Repo, repo)
	}
	for _, r := range records {
		if !ValidKey(r.Key()) {
			// A merge commit has no diff of its own and so no patch-id. There
			// is nothing to key it by and nothing it uniquely carries.
			continue
		}
		if err := write(draft.Root, Path(repo, r.Key()), Render(r)); err != nil {
			return err
		}
	}
	if idx != nil {
		if err := write(draft.Root, IndexPath(repo), RenderIndex(idx)); err != nil {
			return err
		}
	}

	out, err := w.Submit(ctx, draft, "ade")
	if err != nil {
		return fmt.Errorf("publish commit records: %w", err)
	}
	if out.State == "rejected" {
		return fmt.Errorf("commit records rejected: %s", out.Error)
	}
	return nil
}

func write(root, rel, body string) error {
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", rel, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", rel, err)
	}
	return nil
}
