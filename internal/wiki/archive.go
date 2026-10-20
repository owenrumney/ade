package wiki

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ArchiveAgent is who an archived design's commit names as its author.
const ArchiveAgent = "ade-archive"

// MaxPage is the largest body a page may carry, so a caller can refuse an
// over-long one before it reaches a draft.
const MaxPage = maxDistillPage

// DesignsDir is the scope's folder archived designs go in, and the one the
// omnibox labels as an archived design.
const DesignsDir = "designs"

// DesignPath is the page a design document is archived to, or empty when the
// name has nothing a page path can be made of.
func DesignPath(repo, stem string) string {
	clean := strings.Trim(branchUnsafe.ReplaceAllString(stem, "-"), "-.")
	if clean == "" {
		return ""
	}
	return "repos/" + repo + "/" + DesignsDir + "/" + clean + ".md"
}

// PublishDesign writes one page into this repository's wiki scope and pushes
// it. The caller deletes its local document only once this has succeeded.
func (s *Store) PublishDesign(ctx context.Context, repo, stem, body string) (Submission, error) {
	if !s.Enabled() {
		return Submission{}, errors.New("wiki is disabled; run ade wiki init --remote <url>")
	}
	if !validRepo(repo) {
		return Submission{}, fmt.Errorf("%q is not a plain owner/repo", repo)
	}
	page := DesignPath(repo, stem)
	if page == "" {
		return Submission{}, fmt.Errorf("%q is not a name a wiki page can take", stem)
	}
	if err := checkPagePath(page, repo); err != nil {
		return Submission{}, err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return Submission{}, errors.New("nothing to publish")
	}
	if len(body) > MaxPage {
		return Submission{}, fmt.Errorf("%s: %d bytes, %d at most", page, len(body), MaxPage)
	}
	if err := s.Sync(ctx); err != nil {
		return Submission{}, fmt.Errorf("sync the wiki: %w", err)
	}
	session := archiveSession(repo)
	_ = s.Discard(session)
	draft, err := s.seedDraft(session, repo)
	if err != nil {
		return Submission{}, err
	}
	defer func() {
		if _, err := os.Stat(draft.Root); err == nil {
			_ = s.Discard(session)
		}
	}()
	dest := filepath.Join(draft.Root, filepath.FromSlash(page))
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return Submission{}, err
	}
	if err := os.WriteFile(dest, []byte(body+"\n"), 0o600); err != nil {
		return Submission{}, err
	}
	return s.Submit(ctx, draft, ArchiveAgent)
}

// archiveSession is the draft an archive uses: one name per repository, so a
// leftover is found and thrown away by the next one.
func archiveSession(repo string) string {
	sum := sha256.Sum256([]byte(repo))
	return fmt.Sprintf("archive-%x", sum[:6])
}
