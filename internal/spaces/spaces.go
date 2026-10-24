// Package spaces is the set of repositories kept open, and what that set
// hides. A space is a repository, so every worktree of it belongs to one.
package spaces

import (
	"sort"
	"strings"

	"github.com/owenrumney/ade/internal/sessions"
)

// Key names a space: owner and name, never the bare name. Two repositories
// called "docs" under different owners are two spaces.
func Key(owner, name string) string {
	owner, name = strings.TrimSpace(owner), strings.TrimSpace(name)
	if owner == "" || name == "" || strings.ContainsAny(owner+name, "/\\") {
		return ""
	}
	return strings.ToLower(owner + "/" + name)
}

// KeyOf is Key for a repository the scan found.
func KeyOf(repo sessions.Repo) string {
	return Key(repo.Owner, repo.Name)
}

// Clean drops blanks, anything that is not owner/name, and repeats, keeping
// the order given: the list is the person's, so it is never sorted.
func Clean(open []string) []string {
	out := make([]string, 0, len(open))
	seen := map[string]bool{}
	for _, one := range open {
		owner, name, found := strings.Cut(strings.TrimSpace(one), "/")
		key := Key(owner, name)
		if !found || key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}

// Open adds a space at the end, or leaves the list alone if it is already in.
func Open(list []string, repo string) []string {
	cleaned := Clean(list)
	key := Clean([]string{repo})
	if len(key) == 0 {
		return cleaned
	}
	for _, one := range cleaned {
		if one == key[0] {
			return cleaned
		}
	}
	return append(cleaned, key[0])
}

// Close removes one. Nothing on disk goes with it.
func Close(list []string, repo string) []string {
	key := Clean([]string{repo})
	out := make([]string, 0, len(list))
	for _, one := range Clean(list) {
		if len(key) > 0 && one == key[0] {
			continue
		}
		out = append(out, one)
	}
	return out
}

// Within is the repositories an open set covers, in the set's own order. An
// empty set is every repository: opening none is not asking to see less.
func Within(all []sessions.Repo, open []string) []sessions.Repo {
	cleaned := Clean(open)
	if len(cleaned) == 0 {
		return all
	}
	by := map[string]sessions.Repo{}
	for _, repo := range all {
		if key := KeyOf(repo); key != "" {
			by[key] = repo
		}
	}
	out := make([]sessions.Repo, 0, len(cleaned))
	for _, key := range cleaned {
		if repo, found := by[key]; found {
			out = append(out, repo)
		}
	}
	return out
}

// Missing is the open spaces this machine has no checkout of, so they can be
// listed with Clone rather than left out as though they were never opened.
func Missing(all []sessions.Repo, open []string) []string {
	here := map[string]bool{}
	for _, repo := range all {
		here[KeyOf(repo)] = true
	}
	out := []string{}
	for _, key := range Clean(open) {
		if !here[key] {
			out = append(out, key)
		}
	}
	return out
}

// Offerable is what the picker shows: a repository with an owner that is not
// open already. A scratch directory has no owner and is nobody's space.
func Offerable(all []sessions.Repo, open []string) []sessions.Repo {
	isOpen := map[string]bool{}
	for _, key := range Clean(open) {
		isOpen[key] = true
	}
	out := []sessions.Repo{}
	for _, repo := range all {
		if key := KeyOf(repo); key != "" && !isOpen[key] {
			out = append(out, repo)
		}
	}
	// Newest work first: the handful worth opening are the handful used.
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastUsed.After(out[j].LastUsed) })
	return out
}
