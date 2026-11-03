package docnotes

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// context is how much text either side of a quote fingerprints it, so the same
// sentence appearing twice in a document is still two places.
const context = 40

// Anchor pins a note to quoted text rather than to a position: every fold and
// every condense rewrites the document under it.
type Anchor struct {
	Quote       string `json:"quote"`
	ContextHash string `json:"contextHash"`

	// At is the byte offset last found at; Nth is which occurrence that was.
	// The browser re-finds by Nth: a byte offset is a Go fact, not a JS one.
	At  int `json:"at"`
	Nth int `json:"nth"`
}

// Locate anchors to the occurrence of quote sitting at at.
func Locate(body, quote string, at int) (Anchor, bool) {
	if quote == "" || at < 0 || at+len(quote) > len(body) || body[at:at+len(quote)] != quote {
		return Anchor{}, false
	}
	return Anchor{
		Quote:       quote,
		ContextHash: hashAt(body, quote, at),
		At:          at,
		Nth:         nthAt(body, quote, at),
	}, true
}

// First anchors to the first occurrence, which is all an agent that quoted the
// document back at us can be held to.
func First(body, quote string) (Anchor, bool) {
	if quote == "" {
		return Anchor{}, false
	}
	at := strings.Index(body, quote)
	if at < 0 {
		return Anchor{}, false
	}
	return Locate(body, quote, at)
}

// At anchors to the nth occurrence of quote, counting from zero, which is how
// the browser says which one of several it means.
func At(body, quote string, n int) (Anchor, bool) {
	found := occurrences(body, quote)
	if quote == "" || n < 0 || n >= len(found) {
		return Anchor{}, false
	}
	return Locate(body, quote, found[n])
}

// Find is where the anchored quote now lives. An occurrence whose surroundings
// still match wins; failing that, the one nearest to where it was.
func Find(body string, a Anchor) (at, nth int, ok bool) {
	if a.Quote == "" {
		return 0, 0, false
	}
	found := occurrences(body, a.Quote)
	if len(found) == 0 {
		return 0, 0, false
	}
	best, bestNth, exact := -1, 0, false
	for n, where := range found {
		same := hashAt(body, a.Quote, where) == a.ContextHash
		if exact && !same {
			continue
		}
		if !exact && same {
			best, bestNth, exact = where, n, true
			continue
		}
		if best < 0 || abs(where-a.At) < abs(best-a.At) {
			best, bestNth = where, n
		}
	}
	return best, bestNth, true
}

func hashAt(body, quote string, at int) string {
	before := body[max(0, at-context):at]
	end := at + len(quote)
	after := body[end:min(len(body), end+context)]
	sum := sha256.Sum256([]byte(before + "\x00" + quote + "\x00" + after))
	return hex.EncodeToString(sum[:8])
}

// occurrences lists where quote starts, not overlapping: two notes cannot sit
// on the same characters.
func occurrences(body, quote string) []int {
	var out []int
	for from := 0; from+len(quote) <= len(body); {
		at := strings.Index(body[from:], quote)
		if at < 0 {
			break
		}
		out = append(out, from+at)
		from += at + len(quote)
	}
	return out
}

func nthAt(body, quote string, at int) int {
	for n, where := range occurrences(body, quote) {
		if where == at {
			return n
		}
	}
	return 0
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
