package transcripts

import (
	"sort"
	"time"
)

// Share is one directory's part of a metered window.
type Share struct {
	Dir      string  `json:"dir"`
	Tokens   int     `json:"tokens"`
	Percent  float64 `json:"percent"`
	Sessions int     `json:"sessions"`
}

// InWindow totals what each directory spent since a window opened, most first.
// Only sessions last used inside it are read; the rest cannot have spent in it.
func InWindow(sessions []*Session, since time.Time) []Share {
	var candidates []*Session
	for _, s := range sessions {
		if s != nil && !s.LastUsed.Before(since) {
			candidates = append(candidates, s)
		}
	}

	tokens := map[string]int{}
	counted := map[string]map[string]bool{}
	total := 0
	for _, s := range loadAll(candidates) {
		if s == nil {
			continue
		}
		dir := s.DirLabel()
		spent := 0
		for _, c := range s.Charges {
			if c.At.Before(since) {
				continue
			}
			spent += c.Usage.Total()
		}
		if spent == 0 {
			continue
		}
		tokens[dir] += spent
		total += spent
		if counted[dir] == nil {
			counted[dir] = map[string]bool{}
		}
		counted[dir][s.ID] = true
	}

	out := make([]Share, 0, len(tokens))
	for dir, n := range tokens {
		share := Share{Dir: dir, Tokens: n, Sessions: len(counted[dir])}
		if total > 0 {
			share.Percent = float64(n) * 100 / float64(total)
		}
		out = append(out, share)
	}
	// Biggest first, and by name where two are equal so the order is stable
	// between readings rather than shuffling on every poll.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Tokens != out[j].Tokens {
			return out[i].Tokens > out[j].Tokens
		}
		return out[i].Dir < out[j].Dir
	})
	return out
}

// Price is what usage on a model cost, and whether the model has a price.
func Price(model string, u Usage) (float64, bool) { return costFor(model, u) }

// CostSince is the priced spend of one agent's requests since a window opened,
// each at its own model's rate. False when any of them has no price.
func CostSince(sessions []*Session, agent Agent, since time.Time) (float64, bool) {
	var candidates []*Session
	for _, s := range sessions {
		if s != nil && s.Agent == agent && !s.LastUsed.Before(since) {
			candidates = append(candidates, s)
		}
	}
	total, known := 0.0, true
	for _, s := range loadAll(candidates) {
		if s == nil {
			continue
		}
		for _, c := range s.Charges {
			if c.At.Before(since) {
				continue
			}
			cost, ok := costFor(c.Model, c.Usage)
			if !ok {
				known = false
			}
			total += cost
		}
	}
	return total, known
}

// LoadAll loads copies of sessions in parallel; one that fails is nil.
func LoadAll(sessions []*Session) []*Session { return loadAll(sessions) }
