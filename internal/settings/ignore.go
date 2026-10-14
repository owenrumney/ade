package settings

import (
	"path"
	"strings"
	"time"
)

// Ignore is what never to remind you about, in ~/.ade/settings.json:
//
//	"ignore": {
//	  "authors": ["dependabot[bot]"],
//	  "repos": ["acme/legacy"],
//	  "reasons": ["comment"],
//	  "older_than_days": 90
//	}
//
// Not every rule applies to everything, because not every source knows the
// same things. GitHub's notifications API does not say who wrote the thing it
// is telling you about, so authors and age are for the review list; reasons
// are for notifications, which is the only place a reason exists.
//
// A repo means the same thing to both.
type Ignore struct {
	// Authors are logins whose pull requests never appear on the review list.
	// A bot may be written either way — "dependabot" or "dependabot[bot]" —
	// since github.com shows the suffix and the API does not.
	Authors []string `json:"authors,omitempty"`
	// Repos are dropped from both lists. An entry is a name — "acme/thing" —
	// an owner, either "acme" or "acme/*", or a pattern: "acme/legacy-*",
	// "*/sandbox".
	Repos []string `json:"repos,omitempty"`
	// Reasons are GitHub's notification reasons to drop: comment, mention,
	// review_requested, ci_activity, author, state_change, subscribed, manual.
	Reasons []string `json:"reasons,omitempty"`
	// OlderThanDays drops review requests untouched for that long. Zero is no
	// age rule at all, which is the default: a quiet pull request is still
	// waiting on you.
	OlderThanDays int `json:"older_than_days,omitempty"`
}

// SkipReview reports whether a pull request waiting on you should be left off
// the list. updatedAt is GitHub's last-touched time; an empty or unparseable
// one is never too old, since guessing would hide something real.
func (i Ignore) SkipReview(repo, author, updatedAt string) bool {
	if matchesRepo(i.Repos, repo) || matches(i.Authors, author) {
		return true
	}
	if i.OlderThanDays <= 0 || updatedAt == "" {
		return false
	}
	at, err := time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return false
	}
	return time.Since(at) > time.Duration(i.OlderThanDays)*24*time.Hour
}

// SkipNotification reports whether a notification should be left off the list.
func (i Ignore) SkipNotification(repo, reason string) bool {
	return matchesRepo(i.Repos, repo) || matches(i.Reasons, reason)
}

// matchesRepo is matches, plus the two things a repository name allows that a
// login does not: a rule with no slash names a whole owner, and "acme/*" says
// the same thing the way anyone would actually write it.
//
// Patterns are path.Match, so "*" stops at a slash: "acme/*" is every
// repository of theirs and not a path below one.
func matchesRepo(rules []string, repo string) bool {
	repo = strings.ToLower(strings.TrimSpace(repo))
	if repo == "" {
		return false
	}
	owner, _, hasOwner := strings.Cut(repo, "/")
	for _, rule := range rules {
		rule = strings.ToLower(strings.TrimSpace(rule))
		if rule == "" {
			continue
		}
		if rule == repo {
			return true
		}
		if hasOwner && rule == owner {
			return true
		}
		if wild(rule) {
			if ok, err := path.Match(rule, repo); err == nil && ok {
				return true
			}
		}
	}
	return false
}

// wild reports whether a rule is a pattern rather than a name.
//
// Only "*" and "?" count. A "[" would be a character class to path.Match, and
// the one place brackets turn up here is "dependabot[bot]" — a name, which
// bare() has already dealt with by the time anything gets this far. Treating it
// as a pattern would quietly stop it matching anything.
func wild(rule string) bool {
	return strings.ContainsAny(rule, "*?")
}

// matches is case-insensitive, because a login and a repository name are, and
// because a rule typed by hand should work whichever way it was typed.
//
// A "[bot]" suffix is ignored on both sides. github.com writes the author of a
// dependabot pull request as "dependabot[bot]", so that is what anyone writing
// this rule reaches for, while the API answers "dependabot". Matching one
// spelling and not the other is a rule that silently does nothing.
func matches(rules []string, value string) bool {
	value = strings.ToLower(bare(value))
	if value == "" {
		return false
	}
	for _, rule := range rules {
		rule = strings.ToLower(bare(rule))
		if rule == value {
			return true
		}
		if wild(rule) {
			if ok, err := path.Match(rule, value); err == nil && ok {
				return true
			}
		}
	}
	return false
}

func bare(name string) string {
	name = strings.TrimSpace(name)
	// Case-insensitively, since the rest of the comparison is: "Dependabot[Bot]"
	// has to lose its suffix the same way "dependabot[bot]" does.
	if cut := len(name) - len("[bot]"); cut > 0 && strings.EqualFold(name[cut:], "[bot]") {
		return name[:cut]
	}
	return name
}
