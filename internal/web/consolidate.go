package web

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/owenrumney/ade/internal/bridge"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/design"
	"github.com/owenrumney/ade/internal/outgoing"
	"github.com/owenrumney/ade/internal/review"
)

// point is one claim about one place, however many reviewers made it. keys is
// every wording of it, the leading one first.
type point struct {
	keys []string
	by   []string
}

// clusterWindow is how far apart two findings on one file can be and still be
// one point. Category is ignored: one bug gets three names from three readers.
const clusterWindow = 10

// clusterRun names the clustering pass in the conversation and in the slot it
// takes, so it can be stopped like any other side run.
const clusterRun = "consolidate"

// startClustering asks one pass which findings on a file are the same point.
// False means there is nothing to compare, and the batch moves on.
func (s *Server) startClustering(batch string, b *reviewBatch) bool {
	files := crowdedFiles(b)
	if len(files) == 0 {
		return false
	}
	ok := s.startJobs([]sideJob{{name: clusterRun, batch: batch, run: func(ctx context.Context) {
		s.runClustering(ctx, b, files)
	}}}, 0)
	if ok {
		s.hub.Broadcast("agent", nil)
	}
	return ok
}

// crowdedFiles is the files more than one finding landed on, in path order.
func crowdedFiles(b *reviewBatch) []string {
	count := map[string]int{}
	for _, key := range b.order {
		count[b.found[key].finding.File]++
	}
	var out []string
	for file, n := range count {
		if n > 1 {
			out = append(out, file)
		}
	}
	sort.Strings(out)
	return out
}

// runClustering is one call per file. A call that fails leaves that file
// grouped by position, which is a worse answer and not a wrong one.
func (s *Server) runClustering(ctx context.Context, b *reviewBatch, files []string) {
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()
	if diff == nil {
		return
	}
	diffText := review.RenderDiff(diff)

	refs, byRef := refsFor(b)
	failed := 0
	for _, file := range files {
		if ctx.Err() != nil {
			return
		}
		claims := claimsOn(b, file, refs)
		if len(claims) < 2 {
			continue
		}
		groups, err := s.askClusters(ctx, file, claims, diffText)
		if err != nil {
			failed++
			continue
		}
		s.mu.Lock()
		for _, g := range groups {
			b.groups = append(b.groups, keysOf(g.Same, byRef))
		}
		s.mu.Unlock()
	}
	if failed > 0 {
		s.mu.Lock()
		b.unread += failed
		s.mu.Unlock()
	}
}

// askClusters is the one model call. The small model, with no MCP: it compares
// text it was handed and reads a file at most.
func (s *Server) askClusters(ctx context.Context, file string, claims []design.Claim, diffText string) ([]review.Group, error) {
	var reply strings.Builder
	turn, err := s.oneShot(ctx, bridge.OneShotOpts{
		Bin:          s.agent.bin,
		Model:        outgoing.FastModel,
		Dir:          s.space().diffRoot(),
		Prompt:       design.ClusterPrompt(file, claims, diffText),
		AllowedTools: review.ReadOnlyTools(),
		NoMCP:        true,
		OnEvent:      func(ev bridge.Event) { reply.WriteString(ev.Text) },
	})
	if err != nil {
		return nil, err
	}
	reply.WriteString(turn.Text)
	return review.ParseGroups(reply.String()), nil
}

// refsFor numbers a batch's findings F1, F2 and back again. A key is
// file:line:summary, which is too long for a model to echo exactly.
func refsFor(b *reviewBatch) (map[string]string, map[string]string) {
	refs := make(map[string]string, len(b.order))
	byRef := make(map[string]string, len(b.order))
	for i, key := range b.order {
		ref := fmt.Sprintf("F%d", i+1)
		refs[key], byRef[ref] = ref, key
	}
	return refs, byRef
}

func claimsOn(b *reviewBatch, file string, refs map[string]string) []design.Claim {
	var out []design.Claim
	for _, key := range b.order {
		f := b.found[key]
		if f.finding.File != file {
			continue
		}
		out = append(out, design.Claim{
			Ref: refs[key], By: f.by,
			File: f.finding.File, Line: f.finding.Line,
			Category: f.finding.Category,
			Text:     review.Body(f.finding),
		})
	}
	return out
}

func keysOf(group []string, byRef map[string]string) []string {
	out := make([]string, 0, len(group))
	for _, ref := range group {
		if key, ok := byRef[ref]; ok {
			out = append(out, key)
		}
	}
	return out
}

// cluster groups a batch's findings into points: same file, within the window
// of the cluster's first line, plus anything a verdict or the pass called one.
func cluster(b *reviewBatch, judged map[string]*judgement) []point {
	parent := map[string]string{}
	var find func(string) string
	find = func(key string) string {
		up, ok := parent[key]
		if !ok || up == key {
			return key
		}
		root := find(up)
		parent[key] = root
		return root
	}
	join := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		parent[rb] = ra
	}
	for _, key := range b.order {
		parent[key] = key
	}

	byFile := map[string][]string{}
	for _, key := range b.order {
		file := b.found[key].finding.File
		byFile[file] = append(byFile[file], key)
	}
	files := make([]string, 0, len(byFile))
	for file := range byFile {
		files = append(files, file)
	}
	sort.Strings(files)
	for _, file := range files {
		var leads []string
		for _, key := range byFile[file] {
			line := b.found[key].finding.Line
			joined := false
			for _, lead := range leads {
				if abs(line-b.found[lead].finding.Line) <= clusterWindow {
					join(lead, key)
					joined = true
					break
				}
			}
			if !joined {
				leads = append(leads, key)
			}
		}
	}

	// A reader saying so beats the distance between two lines.
	for _, key := range b.order {
		if j := judged[key]; j != nil && j.duplicateOf != "" {
			if b.found[key].finding.File == b.found[j.duplicateOf].finding.File {
				join(j.duplicateOf, key)
			}
		}
	}
	for _, group := range b.groups {
		for _, key := range group[1:] {
			join(group[0], key)
		}
	}

	out := make([]point, 0, len(b.order))
	at := map[string]int{}
	for _, key := range b.order {
		root := find(key)
		i, ok := at[root]
		if !ok {
			at[root] = len(out)
			out = append(out, point{keys: []string{key}, by: []string{b.found[key].by}})
			continue
		}
		out[i].keys = append(out[i].keys, key)
		if !slices.Contains(out[i].by, b.found[key].by) {
			out[i].by = append(out[i].by, b.found[key].by)
		}
	}
	return out
}

// fileBatch files one thread per point. Nothing before it touches the store.
func (s *Server) fileBatch(b *reviewBatch, judged map[string]*judgement) {
	points := cluster(b, judged)

	var filed, dropped, disputed int
	var gone []string
	var sendable []comments.Thread
	for _, p := range points {
		// Withdrawn applies to the point, not the wording: objecting to one of
		// three reports of a bug has not refuted the bug.
		if withdrawnPoint(p, judged) {
			dropped++
			gone = append(gone, droppedLine(b, p, judged))
			continue
		}
		thread, ok := s.filePoint(b, p, judged)
		if !ok {
			continue
		}
		filed++
		if disputedPoint(p, judged) {
			disputed++
		}
		if notSendable(thread) == "" {
			sendable = append(sendable, thread)
		}
	}
	rewriting := s.startRewrite(sendable)
	s.sayConsolidated(b, len(b.order), filed, dropped, disputed, rewriting, gone)
	s.hub.Broadcast("state", nil)
}

// startRewrite puts each filed point in your words, as Convert does one. Only
// with a pull request: there is no author to address otherwise.
func (s *Server) startRewrite(pending []comments.Thread) int {
	if len(pending) == 0 || s.currentBranchPR() == nil {
		return 0
	}
	ok := s.startJobs([]sideJob{{name: kindConvert, run: func(ctx context.Context) {
		s.runConvert(ctx, pending)
	}}}, 0)
	if !ok {
		return 0
	}
	s.hub.Broadcast("agent", nil)
	return len(pending)
}

// filePoint files one point: the lead wording, then the rest as replies.
func (s *Server) filePoint(b *reviewBatch, p point, judged map[string]*judgement) (comments.Thread, bool) {
	lead := b.found[p.keys[0]].finding
	lead.Severity = worstOf(b, p)

	body := review.Body(lead)
	if len(p.by) > 1 {
		body += "\n\nFound by " + strings.Join(mentions(p.by), " · ") + "."
	}
	added, _, err := s.fileOne(lead, b.found[p.keys[0]].by, body)
	if err != nil {
		return comments.Thread{}, false
	}

	store := s.space().store
	// Every reviewer's words, without three rows on the diff.
	for _, key := range p.keys[1:] {
		other := b.found[key]
		if sameWords(other.finding, lead) {
			continue
		}
		if _, err := store.Reply(added.ID, other.by, review.Body(other.finding)); err != nil {
			break
		}
	}
	if disputedPoint(p, judged) {
		if marked, err := store.SetVerdict(added.ID, comments.Verdict{Disputed: true}); err == nil {
			added = marked
		}
		for _, key := range p.keys {
			j := judged[key]
			if j == nil {
				continue
			}
			for i, who := range j.disputers {
				if _, err := store.Reply(added.ID, who, j.reasons[i]); err != nil {
					break
				}
			}
		}
	}
	return added, true
}

// fileOne files a finding with a body of its own, which fileFindings cannot do.
func (s *Server) fileOne(f review.Finding, author, body string) (comments.Thread, bool, error) {
	s.mu.RLock()
	diff := s.diff
	s.mu.RUnlock()
	anchor, line, ok := comments.LocateNearest(diff, f.File, comments.SideRight, f.Line, snapWindow)
	return s.space().store.AddOrReply(comments.Thread{
		File:     f.File,
		Side:     comments.SideRight,
		Line:     line,
		Anchor:   anchor,
		Category: f.Category,
		Severity: f.Severity,
		Orphaned: !ok,
		Comments: []comments.Comment{{Author: author, Body: body}},
	})
}

func (s *Server) sayConsolidated(b *reviewBatch, found, filed, dropped, disputed, rewriting int, gone []string) {
	if found == 0 {
		return
	}
	lines := []string{fmt.Sprintf("%d finding(s) from %d reviewer(s) → %d point(s).",
		found, len(b.reviewers), filed)}
	if dropped > 0 {
		lines = append(lines, fmt.Sprintf("%d dropped: one reviewer, objected by the others.", dropped))
		lines = append(lines, gone...)
	}
	if disputed > 0 {
		lines = append(lines, fmt.Sprintf("%d disputed: the objection is a reply on the comment.", disputed))
	}
	if b.unread > 0 {
		lines = append(lines, fmt.Sprintf("%d file(s) grouped by position only: the comparison failed.", b.unread))
	}
	if rewriting > 0 {
		lines = append(lines, fmt.Sprintf("%d being put in your words, ready to send to GitHub.", rewriting))
	}
	s.say(ChatMessage{Kind: chat.KindNote, Run: runSubagent, Body: strings.Join(lines, "\n")})
}

func droppedLine(b *reviewBatch, p point, judged map[string]*judgement) string {
	f := b.found[p.keys[0]]
	who := map[string]bool{}
	var names []string
	for _, key := range p.keys {
		j := judged[key]
		if j == nil {
			continue
		}
		for _, name := range j.disputers {
			if !who[name] {
				who[name] = true
				names = append(names, name)
			}
		}
	}
	return fmt.Sprintf("- %s:%d — %s, objected by %s",
		f.finding.File, f.finding.Line, f.finding.Headline, strings.Join(mentions(names), " · "))
}

func withdrawnPoint(p point, judged map[string]*judgement) bool {
	for _, key := range p.keys {
		j := judged[key]
		if j == nil || !j.withdrawn {
			return false
		}
	}
	return true
}

func disputedPoint(p point, judged map[string]*judgement) bool {
	for _, key := range p.keys {
		if j := judged[key]; j != nil && j.disputed {
			return true
		}
	}
	return false
}

func worstOf(b *reviewBatch, p point) string {
	worst := b.found[p.keys[0]].finding.Severity
	for _, key := range p.keys[1:] {
		if s := b.found[key].finding.Severity; comments.Worse(s, worst) {
			worst = s
		}
	}
	return worst
}

func sameWords(a, b review.Finding) bool {
	return strings.TrimSpace(review.Body(a)) == strings.TrimSpace(review.Body(b))
}

func mentions(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, "@"+name)
	}
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
