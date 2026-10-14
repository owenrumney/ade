package transcripts

import (
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// RefKind groups references by what they point at.
type RefKind string

// Reference is something a conversation pointed at, and how often.
type Reference struct {
	Kind  RefKind
	Label string
	URL   string
	Count int
}

const (
	RefLinear RefKind = "linear"
	RefGitHub RefKind = "github"
	RefNotion RefKind = "notion"
	RefLink   RefKind = "link"
)

// RefKinds is every kind, in display order.
var RefKinds = []RefKind{RefLinear, RefGitHub, RefNotion, RefLink}

// ticketDenylist are prefixes that look like issue keys but are versions,
// encodings or architectures. Without these, "gpt-5" reads as a ticket.
var ticketDenylist = map[string]bool{
	"utf": true, "gpt": true, "claude": true, "opus": true, "sonnet": true,
	"haiku": true, "fable": true, "mythos": true, "sha": true, "md": true,
	"ipv": true, "x": true, "arm": true, "amd": true, "ios": true, "py": true,
	"go": true, "node": true, "es": true, "http": true, "tls": true, "ssl": true,
	"aes": true, "rsa": true, "iso": true, "cve": true, "utc": true, "gmt": true,
	"east": true, "west": true, "north": true, "south": true, "base": true,
	"part": true, "step": true, "fig": true, "vol": true, "ch": true, "no": true,
	"rev": true, "col": true, "row": true, "id": true, "eu": true, "us": true,
	"ap": true, "ec": true, "s": true, "v": true, "cc": true, "bit": true,
	"json": true, "rfc": true, "bare": true, "sha1": true, "sha256": true,
	"oauth": true, "http2": true, "utf8": true, "iso8601": true, "rgb": true,
	"rollout": true, "height": true, "width": true, "top": true, "left": true,
	"gap": true, "text": true, "flex": true, "grid": true, "border": true,
}

// placeholderOrgs are the owners in example URLs, not repositories you work on.
var placeholderOrgs = map[string]bool{
	"org": true, "acme": true, "example": true, "owner": true, "user": true,
	"your-org": true, "myorg": true, "foo": true, "repo": true,
}

// urlTrailing is punctuation that ends a sentence rather than a URL.
const urlTrailing = ".,:;!?'\"/"

// minPrefixUses is how many linear links must share a prefix before it counts
// as one of this workspace's team keys.
const minPrefixUses = 3

var (
	prefixMu      sync.RWMutex
	learnedPrefix map[string]bool
	urlRE         = regexp.MustCompile(`https?://[^\s"'` + "`" + `<>)\]},;{}&\\]+`)
	ticketRE      = regexp.MustCompile(`[A-Za-z]{2,8}-[0-9]{1,5}`)
	linearRE      = regexp.MustCompile(`(?i)linear\.app/[^/]+/issue/([A-Za-z]+-[0-9]+)`)
	githubRE      = regexp.MustCompile(`(?i)github\.com/([^/\s]+)/([^/\s]+)/(pull|issues)/([0-9]+)`)
	commitRE      = regexp.MustCompile(`(?i)github\.com/([^/\s]+)/([^/\s]+)/commit/([0-9a-f]{7,40})`)
	hexRE         = regexp.MustCompile(`^[0-9a-f]{8,}$`)
	// linearKeyRE finds the team key in a linear issue link, wherever it appears,
	// including inside escaped JSON.
	linearKeyRE = regexp.MustCompile(`(?i)linear\.app/[^/\s"]+/issue/([A-Za-z]{2,8})-[0-9]`)
)

// LearnTicketPrefixes works out which issue-key prefixes this machine really
// uses, so bare keys in prose are recognised rather than guessed.
//
// Prose is full of things shaped like issue keys: phase-1, stage-2, height-2,
// rollout-2026. No denylist keeps up. Linear links and ticket branches name the
// real team keys, so those become the allowlist.
func LearnTicketPrefixes(sessions []*Session) {
	counts := map[string]int{}
	var mu sync.Mutex

	var wg sync.WaitGroup
	tokens := make(chan struct{}, max(runtime.NumCPU(), 2))
	for _, s := range sessions {
		wg.Go(func() {
			tokens <- struct{}{}
			defer func() { <-tokens }()
			data, err := os.ReadFile(s.Path) // #nosec G304 -- an indexed session path.
			if err != nil {
				return
			}
			found := map[string]int{}
			for _, m := range linearKeyRE.FindAllSubmatch(data, -1) {
				found[strings.ToLower(string(m[1]))]++
			}
			mu.Lock()
			defer mu.Unlock()
			for prefix, n := range found {
				counts[prefix] += n
			}
		})
	}
	wg.Wait()

	prefixes := map[string]bool{}
	for prefix, n := range counts {
		if n >= minPrefixUses {
			prefixes[prefix] = true
		}
	}
	// A ticket branch is evidence too, and costs nothing: it is already indexed.
	for _, s := range sessions {
		if prefix := branchTicketPrefix(s.Branch); prefix != "" {
			prefixes[prefix] = true
		}
	}

	prefixMu.Lock()
	learnedPrefix = prefixes
	prefixMu.Unlock()
}

// TicketPrefixes reports what LearnTicketPrefixes found.
func TicketPrefixes() map[string]bool {
	prefixMu.RLock()
	defer prefixMu.RUnlock()
	return learnedPrefix
}

// References collects the tickets and links a session mentions, most-mentioned
// first within each kind.
func References(s *Session) []Reference {
	seen := map[string]*Reference{}
	// Prefixes we have positive evidence for: a linear URL, or the branch this
	// session ran on. Bare keys from prose are checked against these.
	trusted := map[string]bool{}
	for prefix := range TicketPrefixes() {
		trusted[prefix] = true
	}
	if prefix := branchTicketPrefix(s.Branch); prefix != "" {
		trusted[prefix] = true
	}
	add := func(kind RefKind, label, url string) {
		key := string(kind) + "|" + strings.ToLower(label)
		if ref, ok := seen[key]; ok {
			ref.Count++
			if ref.URL == "" {
				ref.URL = url
			}
			return
		}
		seen[key] = &Reference{Kind: kind, Label: label, URL: url, Count: 1}
	}

	// Only what you typed. An agent listing six tickets in a summary, or quoting
	// a path, is not you referencing them; that inflation is what made this list
	// useless. The cost: a PR the agent opened but you never mentioned is missed.
	_ = ScanTexts(s, func(text Text) error {
		if text.Role != RoleUser {
			return nil
		}
		for _, url := range urlRE.FindAllString(text.Body, -1) {
			url = strings.TrimRight(url, urlTrailing)
			if !plausibleURL(url) {
				continue
			}
			kind, label := classifyURL(url)
			if kind == "" || label == "" {
				continue
			}
			if kind == RefLinear {
				trusted[keyPrefix(label)] = true
			}
			add(kind, label, url)
		}
		for _, key := range ticketKeys(text.Body) {
			add(RefLinear, key, "")
		}
		return nil
	})

	refs := make([]Reference, 0, len(seen))
	for _, ref := range seen {
		if corroborated(*ref, trusted) {
			refs = append(refs, *ref)
		}
	}
	sortReferences(refs)
	return refs
}

// corroborated decides whether a bare ticket key from prose is really a ticket.
// A denylist alone cannot know that JSON-6902 is an RFC and FIX-588 is not.
func corroborated(ref Reference, trusted map[string]bool) bool {
	// Anything with a URL behind it is evidence in itself.
	if ref.Kind != RefLinear || ref.URL != "" {
		return true
	}
	// Otherwise the prefix must be one this workspace actually uses. Repetition
	// is not evidence: rollout-2026 and phase-1 recur just as readily.
	return trusted[keyPrefix(ref.Label)]
}

// branchTicketPrefix pulls the issue prefix out of a branch such as
// feat/fix-448-observe, which is strong evidence of a real team key.
func branchTicketPrefix(branch string) string {
	for _, key := range ticketKeys(branch) {
		return keyPrefix(key)
	}
	return ""
}

func keyPrefix(key string) string {
	prefix, _, _ := strings.Cut(strings.ToLower(key), "-")
	return prefix
}

// ticketKeys finds bare issue keys such as fix-541, normalised to upper case.
// RE2 has no lookbehind, so the guards are applied over the match offsets.
func ticketKeys(body string) []string {
	var keys []string
	for _, span := range ticketRE.FindAllStringIndex(body, -1) {
		start, end := span[0], span[1]
		// Reject a key glued to what precedes it: the "east-2" of "us-east-2".
		if start > 0 && isRefByte(body[start-1]) {
			continue
		}
		// Reject version numbers: the "gpt-5" of "gpt-5.6".
		if end < len(body) && (body[end] == '.' || isDigit(body[end])) {
			continue
		}
		match := body[start:end]
		prefix, _, _ := strings.Cut(match, "-")
		if ticketDenylist[strings.ToLower(prefix)] {
			continue
		}
		keys = append(keys, strings.ToUpper(match))
	}
	return keys
}

// plausibleURL rejects fragments the scanner picked out of escaped JSON, which
// have no dotted host and are not a loopback address.
func plausibleURL(url string) bool {
	host := trimScheme(url)
	if i := strings.IndexAny(host, "/:"); i >= 0 {
		host = host[:i]
	}
	return strings.Contains(host, ".") || strings.EqualFold(host, "localhost")
}

func classifyURL(url string) (RefKind, string) {
	if m := linearRE.FindStringSubmatch(url); m != nil {
		return RefLinear, strings.ToUpper(m[1])
	}
	if owner := githubOwner(url); owner != "" && placeholderOrgs[owner] {
		return "", ""
	}
	if m := commitRE.FindStringSubmatch(url); m != nil {
		return RefGitHub, m[1] + "/" + m[2] + "@" + m[3][:min(7, len(m[3]))]
	}
	if m := githubRE.FindStringSubmatch(url); m != nil {
		symbol := "#"
		if strings.EqualFold(m[3], "pull") {
			symbol = " PR #"
		}
		return RefGitHub, m[1] + "/" + m[2] + symbol + m[4]
	}
	lower := strings.ToLower(url)
	switch {
	case strings.HasSuffix(lower, "/commit"), strings.HasSuffix(lower, "/pull"):
		return "", ""
	case strings.Contains(lower, "notion.so"), strings.Contains(lower, "notion.com"):
		return RefNotion, notionLabel(url)
	case strings.Contains(lower, "github.com"):
		return RefGitHub, trimScheme(url)
	default:
		return RefLink, trimScheme(url)
	}
}

// notionLabel turns a Notion URL's slug into a title, dropping its trailing id.
func notionLabel(url string) string {
	slug := url
	// Drop the query and fragment first, or ?source=copy_link splits duplicates.
	if i := strings.IndexAny(slug, "?#"); i >= 0 {
		slug = slug[:i]
	}
	if i := strings.LastIndex(slug, "/"); i >= 0 {
		slug = slug[i+1:]
	}
	parts := strings.Split(slug, "-")
	// Notion appends the page id to the slug; drop any trailing hex chunks.
	for len(parts) > 1 && hexRE.MatchString(strings.ToLower(parts[len(parts)-1])) {
		parts = parts[:len(parts)-1]
	}
	if label := strings.Join(parts, " "); label != "" {
		return label
	}
	return trimScheme(url)
}

// githubOwner is the owner segment of a github URL, or "" if it is not one.
func githubOwner(url string) string {
	rest, found := cutFold(url, "github.com/")
	if !found {
		return ""
	}
	owner, _, _ := strings.Cut(rest, "/")
	return strings.ToLower(owner)
}

func cutFold(text, sep string) (string, bool) {
	if i := indexFold(text, sep); i >= 0 {
		return text[i+len(sep):], true
	}
	return "", false
}

func trimScheme(url string) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	return strings.TrimPrefix(url, "www.")
}

func sortReferences(refs []Reference) {
	kindRank := map[RefKind]int{}
	for i, kind := range RefKinds {
		kindRank[kind] = i
	}
	sortSlice(refs, func(a, b Reference) bool {
		if a.Kind != b.Kind {
			return kindRank[a.Kind] < kindRank[b.Kind]
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Label < b.Label
	})
}

// isRefByte marks characters that glue a key to what precedes it. '/' is absent
// on purpose: branch names like feat/fix-448-observe do name a real ticket.
func isRefByte(b byte) bool {
	return b == '-' || b == '_' || isDigit(b) ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
