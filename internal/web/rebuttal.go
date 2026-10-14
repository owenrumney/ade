package web

import (
	"context"
	"fmt"
	"strings"

	"github.com/owenrumney/ade/internal/agents"
	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/design"
	"github.com/owenrumney/ade/internal/review"
)

// reviewBatch is one review: its reviewers, what they found, and how far
// through its phases it is. A batch drains once per phase.
type reviewBatch struct {
	reviewers []string

	// phase is how far through the batch is: 0 reviewers, 1 the rebuttal
	// round, 2 the clustering pass. Filing is what follows the last.
	phase int
	// order is the finding keys in the order they were filed, which is what
	// gives each one its ref and decides which of a duplicate pair is later.
	order    []string
	found    map[string]batchFinding
	verdicts []rebuttalVerdict

	// groups is what the clustering pass joined, by key, the leading wording
	// first. unread is the files it could not compare at all.
	groups [][]string
	unread int
}

// batchFinding is a finding a reviewer reported and who reported it. No thread
// id: nothing in a batch is filed until the batch settles.
type batchFinding struct {
	by      string
	finding review.Finding
}

// rebuttalVerdict is one judgement with the reviewer who made it attached.
type rebuttalVerdict struct {
	by string
	review.Verdict
}

// newBatch opens a batch for a review of these reviewers.
func (s *Server) newBatch(reviewers []string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.batches == nil {
		s.batches = map[string]*reviewBatch{}
	}
	s.nextBatch++
	id := "b" + itoa(s.nextBatch)
	s.batches[id] = &reviewBatch{reviewers: reviewers, found: map[string]batchFinding{}}
	return id
}

// dropBatch forgets a batch that never started, so a refused review does not
// leave a batch behind waiting for runs that will not happen.
func (s *Server) dropBatch(batch string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.batches, batch)
	delete(s.held, batch)
}

// recordFound holds a reviewer's findings until the batch settles. False means
// no batch, which is the caller's signal to file as it always did.
func (s *Server) recordFound(batch, by string, found []review.Finding) bool {
	if batch == "" || len(found) == 0 {
		return batch != ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.batches[batch]
	if b == nil {
		return false
	}
	for _, f := range found {
		// Keyed by reviewer as well: the same words from two reviewers are two
		// reports of one point, which is the support the clustering counts.
		key := by + "\x00" + review.Key(f)
		if _, dup := b.found[key]; dup {
			continue
		}
		b.order = append(b.order, key)
		b.found[key] = batchFinding{by: by, finding: f}
	}
	return true
}

// heldSlots is how many slots batches are keeping back. Callers hold s.mu.
func (s *Server) heldSlots() int {
	n := 0
	for _, held := range s.held {
		n += held
	}
	return n
}

// batchRunning reports whether any run of this batch is still in flight.
// Callers hold s.mu.
func (s *Server) batchRunning(batch string) bool {
	for _, side := range s.sides {
		if side.batch == batch {
			return true
		}
	}
	return false
}

// batchDrained starts the next phase, or settles. It loops because a phase
// with nothing to run is skipped rather than waited on.
func (s *Server) batchDrained(batch string) {
	for {
		s.mu.Lock()
		b := s.batches[batch]
		if b == nil {
			s.mu.Unlock()
			return
		}
		phase := b.phase
		b.phase++
		s.mu.Unlock()

		var started bool
		switch phase {
		case 0:
			started = s.startRebuttal(batch, b)
		case 1:
			started = s.startClustering(batch, b)
		default:
			s.mu.Lock()
			delete(s.batches, batch)
			s.mu.Unlock()
			s.settleBatch(b)
			return
		}
		if started {
			return
		}
	}
}

// startRebuttal asks each reviewer to judge the findings the others filed.
// False means no round ran, and the batch is finished with.
func (s *Server) startRebuttal(batch string, b *reviewBatch) bool {
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()
	if diff == nil || len(b.order) == 0 {
		return false
	}
	diffText := review.RenderDiff(diff)

	// Refs are global to the batch, not per reviewer: a duplicate-of can name
	// a finding the answering reviewer was not shown, and both sides of the
	// pair have to mean the same ref.
	refs := make(map[string]string, len(b.order))
	for i, key := range b.order {
		refs[key] = fmt.Sprintf("F%d", i+1)
	}

	list := s.userAgentsList()
	var calls []sideCall
	for _, name := range b.reviewers {
		agent, ok := agents.Find(list, name)
		if !ok {
			continue
		}
		claims := make([]design.Claim, 0, len(b.order))
		for _, key := range b.order {
			f := b.found[key]
			if f.by == name {
				continue
			}
			claims = append(claims, design.Claim{
				Ref: refs[key], By: f.by,
				File: f.finding.File, Line: f.finding.Line,
				Category: f.finding.Category,
				Text:     review.Body(f.finding),
			})
		}
		if len(claims) == 0 {
			continue
		}
		calls = append(calls, sideCall{agent: agent, prompt: design.RebuttalPrompt(name, claims, diffText), batch: batch})
	}
	if len(calls) == 0 {
		return false
	}
	if !s.startRebuttalJobs(batch, b, refs, calls) {
		return false
	}
	s.hub.Broadcast("agent", nil)
	return true
}

// startRebuttalJobs runs the rebuttal calls. They collect verdicts and say
// nothing: a judgement is a reply on the comment it answers, not prose in the
// conversation, which is the noise this feature exists to remove.
func (s *Server) startRebuttalJobs(batch string, b *reviewBatch, refs map[string]string, calls []sideCall) bool {
	byRef := make(map[string]string, len(refs))
	for key, ref := range refs {
		byRef[ref] = key
	}
	jobs := make([]sideJob, 0, len(calls))
	for _, call := range calls {
		jobs = append(jobs, sideJob{name: call.agent.Name, batch: batch, run: func(ctx context.Context) {
			s.runRebuttal(ctx, call, b, byRef)
		}})
	}
	return s.startJobs(jobs, 0)
}

// runRebuttal is one reviewer's pass over the others' findings.
func (s *Server) runRebuttal(ctx context.Context, call sideCall, b *reviewBatch, byRef map[string]string) {
	var reply strings.Builder
	onEvent := func(ev bridge.Event) {
		if t := strings.TrimSpace(ev.Text); t != "" {
			reply.WriteString(t)
			reply.WriteString("\n\n")
		}
	}

	agent := call.agent
	var err error
	switch agent.Provider {
	case agents.ProviderClaude:
		tools := agentTools(agent)
		_, err = s.coldClaude(ctx, bridge.OneShotOpts{
			Bin:            s.agent.bin,
			Dir:            s.space().diffRoot(),
			Model:          agent.Model,
			Agent:          agent.Name,
			Prompt:         call.prompt,
			Tools:          tools,
			AllowedTools:   tools,
			PermissionMode: "default",
			SystemPrompt:   s.sideProject(),
			OnEvent:        onEvent,
		})
	case agents.ProviderCodex:
		err = s.runCodex(ctx, agent, call.prompt, onEvent)
	default:
		err = errProviderUnsupported(agent.Provider)
	}
	if err != nil {
		s.say(ChatMessage{Kind: chat.KindNote, Failed: true, Author: agent.Name, Run: runSubagent,
			Body: fmt.Sprintf("@%s could not judge the other findings: %v", agent.Name, err)})
		return
	}

	var out []rebuttalVerdict
	for _, v := range review.ParseVerdicts(reply.String()) {
		key, ok := byRef[v.Ref]
		if !ok {
			continue
		}
		if v.Verdict == review.VerdictDuplicate {
			of, ok := byRef[v.Of]
			if !ok {
				continue
			}
			v.Of = of
		}
		v.Ref = key
		out = append(out, rebuttalVerdict{by: agent.Name, Verdict: v})
	}
	if len(out) == 0 {
		return
	}
	s.mu.Lock()
	b.verdicts = append(b.verdicts, out...)
	s.mu.Unlock()
}

// judgement is what the round concluded about one finding, before filing. The
// objection comes with it: it becomes a reply on the thread the finding joins.
type judgement struct {
	withdrawn   bool
	disputed    bool
	duplicateOf string
	disputers   []string
	reasons     []string
}

// settleBatch files what the batch found, consolidated. The round's verdicts
// decide what exists rather than annotating what already does.
func (s *Server) settleBatch(b *reviewBatch) {
	s.fileBatch(b, judgements(b))
}

// judgements is the round's conclusion per finding key. It writes nothing.
func judgements(b *reviewBatch) map[string]*judgement {
	// Ranked by report order, so of a duplicate pair the later one is the one
	// that gets folded into the earlier.
	rank := make(map[string]int, len(b.order))
	for i, key := range b.order {
		rank[key] = i
	}

	byKey := map[string]*judgement{}
	at := func(key string) *judgement {
		if byKey[key] == nil {
			byKey[key] = &judgement{}
		}
		return byKey[key]
	}

	// Counted, not flagged: withdrawal weighs the objections against the
	// defences, so one of each has to be a draw rather than a veto.
	agreed := map[string]int{}
	for _, v := range b.verdicts {
		if _, ok := b.found[v.Ref]; !ok {
			continue
		}
		switch v.Verdict.Verdict {
		case review.VerdictAgree:
			agreed[v.Ref]++
		case review.VerdictDispute:
			// A reviewer who reported the same point is not judging it.
			if v.by == b.found[v.Ref].by {
				continue
			}
			j := at(v.Ref)
			j.disputed = true
			j.disputers = append(j.disputers, v.by)
			j.reasons = append(j.reasons, v.Reason)
		}
	}

	// Duplicates in a second pass: the verdict flags the later thread, which
	// is not the one being iterated, so it cannot be folded into the first.
	for _, v := range b.verdicts {
		if v.Verdict.Verdict != review.VerdictDuplicate {
			continue
		}
		a, b1 := v.Ref, v.Of
		if _, ok := b.found[a]; !ok {
			continue
		}
		if _, ok := b.found[b1]; !ok {
			continue
		}
		later, first := a, b1
		if rank[first] > rank[later] {
			later, first = first, later
		}
		at(later).duplicateOf = first
	}

	for key, j := range byKey {
		// Withdrawn is the objections outnumbering the defences, on at least
		// two verdicts. Unanimity was the old bar, and with four reviewers one
		// "agree" kept a finding the other two had shot down.
		//
		// Two verdicts is the floor because silence is not a judgement: one
		// reviewer objecting while the rest say nothing is an objection worth
		// reading, not a finding the room rejected.
		votes := len(j.disputers) + agreed[key]
		if votes >= 2 && len(j.disputers) > agreed[key] {
			j.withdrawn = true
		}
	}
	return byKey
}
