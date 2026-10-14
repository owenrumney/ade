// Package memory keeps what ade knows about a project: the user's standing
// instructions, and notes distilled from its transcripts. Not per checkout.
package memory

import (
	"context"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/owenrumney/ade/internal/gitdiff"
)

// Identity is which project a checkout belongs to.
type Identity struct {
	// Slug is the store's directory name.
	Slug string `json:"slug"`
	// Remote is the normalised origin, or empty when there is none and the
	// identity fell back to a path.
	Remote string `json:"remote,omitempty"`
	// Path is the common git dir the identity came from, when Remote is empty.
	Path string `json:"path,omitempty"`
}

// scheme matches the front of a remote URL, in either form git accepts:
// https://host/path, ssh://git@host/path, or git@host:path.
var scheme = regexp.MustCompile(`^(?:[a-zA-Z][a-zA-Z0-9+.-]*://)?(?:[^/@]+@)?`)

// unsafe is everything a store directory may not contain.
var unsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// Resolve is the project a checkout belongs to. The origin remote survives the
// checkout being moved or cloned again; without one, only the git dir does.
func Resolve(ctx context.Context, dir string) (Identity, error) {
	common, err := gitdiff.CommonGitDir(ctx, dir)
	if err != nil {
		return Identity{}, err
	}
	if remote := Normalise(gitdiff.OriginURL(ctx, common)); remote != "" {
		return Identity{Slug: Slug(remote), Remote: remote}, nil
	}
	return Identity{Slug: Slug(common), Path: common}, nil
}

// Normalise reduces a remote URL to host and path: github.com/owenrumney/ade
// from any of the forms git writes it in. Empty for anything unrecognisable.
func Normalise(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}
	url = scheme.ReplaceAllString(url, "")
	// git@host:owner/repo puts the path after a colon rather than a slash.
	if host, path, ok := strings.Cut(url, ":"); ok && !strings.HasPrefix(path, "/") {
		url = host + "/" + path
	}
	url = strings.TrimSuffix(strings.Trim(url, "/"), ".git")
	if !strings.Contains(url, "/") {
		return ""
	}
	return strings.TrimSuffix(strings.Trim(url, "/"), ".git")
}

// Slug is the store directory for an identity: readable, so instructions.md
// can be opened without a lookup table.
func Slug(identity string) string {
	slug := unsafe.ReplaceAllString(strings.TrimSpace(identity), "-")
	slug = strings.Trim(slug, "-.")
	if slug == "" {
		return ""
	}
	// A path identity is long and its tail is the distinguishing part.
	if len(slug) > 120 {
		slug = slug[len(slug)-120:]
		slug = strings.TrimLeft(slug, "-.")
	}
	return slug
}

// RepoName is the repository's own name, the last segment of either identity.
func (i Identity) RepoName() string {
	if i.Remote != "" {
		return path.Base(i.Remote)
	}
	if i.Path == "" {
		return ""
	}
	return filepath.Base(filepath.Dir(i.Path))
}

// Name is what to call the project in a prompt or a heading.
func (i Identity) Name() string {
	if i.Remote != "" {
		return i.Remote
	}
	return filepath.Base(filepath.Dir(i.Path))
}
