package review

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ClusterFence tags the blocks a clustering pass emits, one per group of
// findings that are the same point. Exported because the prompt asks for them.
const ClusterFence = "ade-cluster"

var clusterRE = regexp.MustCompile("(?s)```" + ClusterFence + "\\s*\\n(.*?)```")

// Group is findings the pass judged to be one point, by the short refs the
// prompt gave them. The first ref is the clearest wording of the group.
type Group struct {
	Same []string `json:"same"`
}

// ParseGroups pulls the groups out of a clustering reply. An unreadable block
// is dropped: a misread group folds two points into one comment.
func ParseGroups(text string) []Group {
	var out []Group
	for _, match := range clusterRE.FindAllStringSubmatch(text, -1) {
		var g Group
		if err := json.Unmarshal([]byte(match[1]), &g); err != nil {
			continue
		}
		refs := make([]string, 0, len(g.Same))
		seen := map[string]bool{}
		for _, ref := range g.Same {
			ref = strings.ToUpper(strings.TrimSpace(ref))
			if ref == "" || seen[ref] {
				continue
			}
			seen[ref] = true
			refs = append(refs, ref)
		}
		// One ref is not a group, and a group of none is nothing at all.
		if len(refs) < 2 {
			continue
		}
		out = append(out, Group{Same: refs})
	}
	return out
}
