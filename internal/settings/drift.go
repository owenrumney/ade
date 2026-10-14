package settings

import "strings"

// waived is the design requirements you have waved off, each with a
// fingerprint of what it said when you did.
//
// Keyed by repository, document and requirement id, because bullet text as
// common as "add tests" would otherwise collide across every project on the
// machine. Valued with a hash of the wording, so the waiver expires when the
// requirement is reworded and cannot outlive the line it was about.
var waived = &notes{store{name: "dismissed-requirements.json"}}

// DismissedRequirementsPath is that file.
func DismissedRequirementsPath() (string, error) { return waived.path() }

// DismissedRequirements is every waiver, by RequirementKey, valued with the
// hash of the requirement as it was worded when waived.
func DismissedRequirements() map[string]string { return waived.all() }

// RequirementKey identifies one requirement of one document of one repository.
func RequirementKey(repo, doc, id string) string {
	return strings.TrimSpace(repo) + "|" + strings.TrimSpace(doc) + "|" + strings.TrimSpace(id)
}

// SetDismissedRequirement waves a requirement off as it is worded. An empty
// hash brings it back.
func SetDismissedRequirement(repo, doc, id, hash string) error {
	return waived.set(RequirementKey(repo, doc, id), strings.TrimSpace(hash))
}

// IsRequirementDismissed reports whether a requirement is still waived: it is,
// until the wording changes.
func IsRequirementDismissed(aside map[string]string, repo, doc, id, hash string) bool {
	hash = strings.TrimSpace(hash)
	return hash != "" && aside[RequirementKey(repo, doc, id)] == hash
}
