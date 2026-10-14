package commitrec

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// IndexFile is the merge index, published alongside the records it points at.
const IndexFile = "index.md"

// IndexPath is the merge index for a repository.
func IndexPath(repo string) string { return path.Join(Dir(repo), IndexFile) }

// Entry maps one merge commit to the records of the PR it squashed: after a
// squash, blame gives one sha for the whole branch. See the design doc.
type Entry struct {
	Merge string
	PR    int
	Keys  []string
}

// Index is the merge index, newest merge first.
type Index []Entry

var indexRE = regexp.MustCompile(`^- (\S+) #(\d+)((?: \S+)*)$`)

// ParseIndex reads the index back. An unreadable line is skipped rather than
// failing the file: losing one mapping beats losing them all.
func ParseIndex(text string) Index {
	var idx Index
	for line := range strings.SplitSeq(text, "\n") {
		m := indexRE.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		pr, err := strconv.Atoi(m[2])
		if err != nil || !ValidKey(m[1]) {
			continue
		}
		// Anyone can push to the wiki, and a key off this line becomes a path.
		var keys []string
		for _, key := range strings.Fields(m[3]) {
			if ValidKey(key) {
				keys = append(keys, key)
			}
		}
		idx = append(idx, Entry{Merge: m[1], PR: pr, Keys: keys})
	}
	return idx
}

// RenderIndex writes the index as the Markdown that is published.
func RenderIndex(idx Index) string {
	var b strings.Builder
	b.WriteString("# Merged commit records\n\n")
	b.WriteString("Each line is a merge commit, the pull request it closed, and the keys of\n")
	b.WriteString("the records that pull request carried.\n\n")
	for _, e := range idx {
		fmt.Fprintf(&b, "- %s #%d", e.Merge, e.PR)
		for _, k := range e.Keys {
			b.WriteString(" " + k)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// KeysFor is the records a merge commit stands for, empty when it is not a
// merge this index knows.
func (idx Index) KeysFor(merge string) []string {
	for _, e := range idx {
		if e.Merge == merge {
			return e.Keys
		}
	}
	return nil
}

// Put records a merge, replacing any earlier entry for the same PR. A PR can be
// merged, reverted and merged again, and the live merge is the last one.
func (idx Index) Put(e Entry) Index {
	sort.Strings(e.Keys)
	out := Index{e}
	for _, held := range idx {
		if held.PR != e.PR {
			out = append(out, held)
		}
	}
	return out
}
