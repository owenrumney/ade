package web

import (
	"context"
	"fmt"
	"time"
)

// activityBucket is one column, activityBuckets the span the badge draws.
const (
	activityBucket  = 5 * time.Second
	activityBuckets = 24
)

// activity is a ring of event counts per bucket of wall time, indexed by the
// bucket a moment falls in rather than by position.
type activity struct {
	counts [activityBuckets]int
	at     int64
}

// note counts one sign of life.
func (a *activity) note(now time.Time) {
	slot := now.UnixNano() / int64(activityBucket)
	a.roll(slot)
	a.counts[slot%activityBuckets]++
}

// series is the counts, oldest first.
func (a *activity) series(now time.Time) []int {
	slot := now.UnixNano() / int64(activityBucket)
	a.roll(slot)
	out := make([]int, activityBuckets)
	for i := range out {
		out[i] = a.counts[(slot-int64(activityBuckets-1-i))%activityBuckets]
	}
	return out
}

// roll clears the buckets passed since the last write, so silence reads flat.
func (a *activity) roll(slot int64) {
	if slot <= a.at {
		return
	}
	gap := slot - a.at
	if a.at == 0 || gap >= activityBuckets {
		a.counts = [activityBuckets]int{}
		a.at = slot
		return
	}
	for i := a.at + 1; i <= slot; i++ {
		a.counts[i%activityBuckets] = 0
	}
	a.at = slot
}

// watchActivity sends the series on its own event, because /api/state is not
// refetched while a run is in flight and the badge has to move during one.
func (s *Server) watchActivity(ctx context.Context) {
	ticker := time.NewTicker(activityBucket)
	defer ticker.Stop()
	last := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			series, running := s.agent.beat()
			if !running || s.hub.Clients() == 0 {
				last = ""
				continue
			}
			if now := fmt.Sprint(series); now != last {
				last = now
				s.hub.Broadcast("activity", activityEvent{Activity: series})
			}
		}
	}
}

// activityEvent is the series alone: one small frame a bucket, not a refetch.
type activityEvent struct {
	Activity []int `json:"activity"`
}
