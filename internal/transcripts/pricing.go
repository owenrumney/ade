package transcripts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// pricing is USD per million tokens.
type pricing struct {
	input      float64
	output     float64
	cacheWrite float64
	cacheRead  float64
}

type priceEntry struct {
	prefix string
	p      pricing
}

// prices is matched longest-prefix-first, so a family entry cannot shadow a
// specific model. Unknown models stay unpriced rather than guessed.
var prices = []priceEntry{
	{"claude-fable-5", pricing{10.0, 50.0, 12.5, 1.0}},
	{"claude-mythos-5", pricing{10.0, 50.0, 12.5, 1.0}},
	{"claude-opus-5", pricing{5.0, 25.0, 6.25, 0.50}},
	{"claude-opus-4-8", pricing{5.0, 25.0, 6.25, 0.50}},
	{"claude-opus-4-7", pricing{5.0, 25.0, 6.25, 0.50}},
	{"claude-opus-4", pricing{15.0, 75.0, 18.75, 1.50}},
	{"claude-opus-3", pricing{15.0, 75.0, 18.75, 1.50}},
	{"claude-sonnet-5", pricing{3.0, 15.0, 3.75, 0.30}},
	{"claude-sonnet-4", pricing{3.0, 15.0, 3.75, 0.30}},
	{"claude-haiku-4", pricing{1.0, 5.0, 1.25, 0.10}},
	{"claude-3-7-sonnet", pricing{3.0, 15.0, 3.75, 0.30}},
	{"claude-3-5-sonnet", pricing{3.0, 15.0, 3.75, 0.30}},
	{"claude-3-5-haiku", pricing{0.80, 4.0, 1.0, 0.08}},
	{"claude-3-opus", pricing{15.0, 75.0, 18.75, 1.50}},
	{"gpt-5-mini", pricing{0.25, 2.0, 0.25, 0.025}},
	{"gpt-5-nano", pricing{0.05, 0.40, 0.05, 0.005}},
	{"gpt-5", pricing{1.25, 10.0, 1.25, 0.125}},
	{"o3-mini", pricing{1.10, 4.40, 1.10, 0.55}},
	{"o3", pricing{2.0, 8.0, 2.0, 0.50}},
	{"gemini-2.5-pro", pricing{1.25, 10.0, 1.25, 0.31}},
	{"gemini-2.5-flash", pricing{0.30, 2.50, 0.30, 0.075}},
}

var sortOnce sync.Once

// pricesHash identifies the table. Sorted, because lookupPricing reorders the
// slice in place and that is not a price change.
func pricesHash() string {
	lines := make([]string, 0, len(prices))
	for _, entry := range prices {
		lines = append(lines, fmt.Sprintf("%s|%g|%g|%g|%g", entry.prefix,
			entry.p.input, entry.p.output, entry.p.cacheWrite, entry.p.cacheRead))
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])[:16]
}

// costFor prices usage from the table. The bool is false for models we have no
// entry for, so the UI can say "unpriced" instead of showing $0.00.
func costFor(model string, u Usage) (float64, bool) {
	p := lookupPricing(model)
	if p == nil {
		return 0, false
	}
	cost := float64(u.Input)/1e6*p.input +
		float64(u.Output)/1e6*p.output +
		float64(u.CacheWrite)/1e6*p.cacheWrite +
		float64(u.CacheRead)/1e6*p.cacheRead
	return cost, true
}

func lookupPricing(model string) *pricing {
	if model == "" {
		return nil
	}
	sortOnce.Do(func() {
		sort.SliceStable(prices, func(i, j int) bool {
			return len(prices[i].prefix) > len(prices[j].prefix)
		})
	})
	model = strings.ToLower(model)
	for i := range prices {
		if strings.HasPrefix(model, prices[i].prefix) {
			return &prices[i].p
		}
	}
	return nil
}
