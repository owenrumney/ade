package web

import (
	"context"

	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/prlink"
)

// linkAt is the commit review comments link into, and how a bare filename in
// one is resolved. The head of the pull request being reviewed, so a link
// lands on the line as the author sees it.
func (s *Server) linkAt(ctx context.Context, root, owner, repo, sha string) (prlink.At, prlink.Known) {
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()
	var paths []string
	if diff != nil {
		for _, file := range diff.Files {
			paths = append(paths, file.Path)
		}
	}
	// A review names files the change only reads, so the tree answers what the
	// diff cannot. A name git will not list stays plain text.
	tracked, _ := gitdiff.Tracked(ctx, root)
	known := prlink.Or(prlink.Files(paths), prlink.Files(tracked))
	return prlink.At{Owner: owner, Repo: repo, SHA: sha}, known
}

// outgoingBody is what one finding is sent as: the badge, the reviewer's own
// words, and every file and line in them linked to the code.
func (s *Server) outgoingBody(thread comments.Thread, at prlink.At, known prlink.Known) string {
	return comments.Badged(thread.Severity, prlink.In(thread.Outgoing, at, thread.File, known))
}
