package settings

import "strings"

// stashes is the work ade set aside to switch branch: repository and branch
// in, the stash entry's commit out. Never an index — the stack is shared.
var stashes = &notes{store{name: "stashes.json"}}

// StashesPath is that file.
func StashesPath() (string, error) { return stashes.path() }

// StashKey names one branch of one repository, by its common git directory.
func StashKey(repo, branch string) string {
	return strings.TrimSpace(repo) + "|" + strings.TrimSpace(branch)
}

// Stashed is the entry ade is holding for a branch, or empty.
func Stashed(repo, branch string) string {
	return stashes.all()[StashKey(repo, branch)]
}

// StashedIn is every branch of one repository ade is holding work for.
func StashedIn(repo string) map[string]string {
	out := map[string]string{}
	prefix := StashKey(repo, "")
	for key, sha := range stashes.all() {
		if branch, ok := strings.CutPrefix(key, prefix); ok && branch != "" {
			out[branch] = sha
		}
	}
	return out
}

// KeepStash records what was set aside. An empty sha forgets the branch.
func KeepStash(repo, branch, sha string) error {
	return stashes.set(StashKey(repo, branch), strings.TrimSpace(sha))
}

// ForgetStash drops the record, which a dropped or applied entry needs.
func ForgetStash(repo, branch string) error {
	return KeepStash(repo, branch, "")
}
