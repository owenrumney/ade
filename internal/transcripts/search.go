package transcripts

import (
	"bytes"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// Hit is a session whose transcript matched, with the lines that matched.
type Hit struct {
	Session  *Session
	Excerpts []Text
	Matches  int
}

const (
	// searchExcerpts caps how many matching lines we keep per session.
	searchExcerpts = 6
	// excerptWidth is how much context to keep around a match.
	excerptWidth = 160
	// prefilterMax skips byte-scanning anything absurdly large.
	prefilterMax = 128 << 20
)

// Search finds sessions whose transcript contains query, case-insensitively. A
// cheap parallel byte scan rejects most files, then survivors are parsed.
func Search(sessions []*Session, query string) []Hit {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	needle := []byte(strings.ToLower(query))

	candidates := prefilter(sessions, needle)
	hits := make([]Hit, 0, len(candidates))
	for _, s := range candidates {
		if hit := confirm(s, query); hit != nil {
			hits = append(hits, *hit)
		}
	}
	// Most matches first, then most recent, so the strongest answer is on top.
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Matches != hits[j].Matches {
			return hits[i].Matches > hits[j].Matches
		}
		return hits[i].Session.LastUsed.After(hits[j].Session.LastUsed)
	})
	return hits
}

// prefilter keeps only the sessions whose raw transcript contains the needle.
func prefilter(sessions []*Session, needle []byte) []*Session {
	var (
		mu     sync.Mutex
		kept   []*Session
		wg     sync.WaitGroup
		tokens = make(chan struct{}, max(runtime.NumCPU(), 2))
	)
	for _, s := range sessions {
		wg.Go(func() {
			tokens <- struct{}{}
			defer func() { <-tokens }()
			if !fileContainsFold(s.Path, needle) {
				return
			}
			mu.Lock()
			kept = append(kept, s)
			mu.Unlock()
		})
	}
	wg.Wait()
	return kept
}

// fileContainsFold reports whether a file contains needle, which must already be
// lower case. Lowercases a copy of the file: one transient allocation per file.
func fileContainsFold(path string, needle []byte) bool {
	info, err := os.Stat(path)
	if err != nil || info.Size() > prefilterMax {
		return false
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path comes from an indexed session.
	if err != nil {
		return false
	}
	return bytes.Contains(bytes.ToLower(data), needle)
}

// confirm parses a candidate to check the match is in readable text, collecting
// excerpts. Returns nil when the raw hit was only inside an id or a blob.
func confirm(s *Session, query string) *Hit {
	hit := Hit{Session: s}
	_ = ScanTexts(s, func(text Text) error {
		if indexFold(text.Body, query) < 0 {
			return nil
		}
		hit.Matches++
		if len(hit.Excerpts) < searchExcerpts {
			text.Body = excerpt(text.Body, query, excerptWidth)
			hit.Excerpts = append(hit.Excerpts, text)
		}
		return nil
	})
	if hit.Matches == 0 {
		return nil
	}
	return &hit
}

// excerpt trims body to a window around the first match.
func excerpt(body, query string, width int) string {
	if len(body) <= width {
		return body
	}
	at := indexFold(body, query)
	if at < 0 {
		return truncateRunes(body, width) + "…"
	}
	// Both ends must land on a rune boundary or the excerpt renders as mojibake.
	start := max(at-width/3, 0)
	for start > 0 && !utf8.RuneStart(body[start]) {
		start--
	}
	out := truncateRunes(body[start:], width)
	if start > 0 {
		out = "…" + out
	}
	if start+len(out) < len(body) {
		out += "…"
	}
	return out
}

// sortSlice is a tiny wrapper so callers can pass a value-typed less function.
func sortSlice[T any](items []T, less func(a, b T) bool) {
	sort.SliceStable(items, func(i, j int) bool { return less(items[i], items[j]) })
}
