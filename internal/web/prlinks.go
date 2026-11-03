package web

import (
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/prlink"
)

// linkAt is the commit review comments link into, and how a bare filename in
// one is resolved. The head of the pull request being reviewed, so a link
// lands on the line as the author sees it.
func (s *Server) linkAt(owner, repo, sha string) (prlink.At, prlink.Known) {
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()
	var paths []string
	if diff != nil {
		for _, file := range diff.Files {
			paths = append(paths, file.Path)
		}
	}
	return prlink.At{Owner: owner, Repo: repo, SHA: sha}, prlink.Files(paths)
}

// outgoingBody is what one finding is sent as: the badge, the reviewer's own
// words, and every file and line in them linked to the code.
func (s *Server) outgoingBody(thread comments.Thread, at prlink.At, known prlink.Known) string {
	return comments.Badged(thread.Severity, prlink.In(thread.Outgoing, at, thread.File, known))
}
