package transcripts

import (
	"fmt"
	"runtime"
	"sort"
	"sync"
	"time"
)

// Group is spend for one bucket: a directory, an agent, or a month.
type Group struct {
	Key      string  `json:"key"`
	Cost     float64 `json:"cost"`
	Tokens   int     `json:"tokens"`
	Sessions int     `json:"sessions"`
	// Unpriced is sessions whose model has no price; Requests is turns made.
	Unpriced int `json:"unpriced"`
	Requests int `json:"requests,omitempty"`
}

// Spend is a costed summary of every session that could be parsed.
type Spend struct {
	Total    Group
	ByDir    []Group
	ByAgent  []Group
	ByMonth  []Group
	Failed   int
	Unpriced int
}

// Stats is the machine-wide, read-only summary the launcher shows.
type Stats struct {
	Sessions        int                `json:"sessions"`
	Requests        int                `json:"requests"`
	Tools           int                `json:"tools"`
	Usage           Usage              `json:"usage"`
	Cost            float64            `json:"cost"`
	ByDay           []Group            `json:"byDay"`
	ByAgent         []Group            `json:"byAgent"`
	ByModel         []Group            `json:"byModel"`
	ByDayModel      map[string][]Group `json:"byDayModel"`
	ByDayModelTurns map[string][]Group `json:"byDayModelTurns"`
	// ByDayRepo and ByDayRepoTurns key a repository by its origin, owner/name.
	ByDayRepo        map[string][]Group       `json:"byDayRepo"`
	ByDayRepoTurns   map[string][]Group       `json:"byDayRepoTurns"`
	ByDayAgentTurns  map[string][]Group       `json:"byDayAgentTurns"`
	TopSessions      map[string][]SessionCost `json:"topSessions"`
	ByTool           []Group                  `json:"byTool"`
	ByToolFailure    []Group                  `json:"byToolFailure"`
	Completed        int                      `json:"completed"`
	Failed           int                      `json:"failed"`
	Retries          int                      `json:"retries"`
	Compactions      int                      `json:"compactions"`
	Interventions    int                      `json:"interventions"`
	TurnDurations    []float64                `json:"turnDurations"`
	SessionDurations []float64                `json:"sessionDurations"`
	Contexts         []ContextStat            `json:"contexts"`
	// ByHour has 24 entries and ByWeekday 7, always, in clock order: a chart
	// drawn round a dial needs the quiet slots as much as the busy ones.
	ByHour    []Group `json:"byHour"`
	ByWeekday []Group `json:"byWeekday"`
}

// SessionCost is one session's value on one day.
type SessionCost struct {
	ID   string  `json:"id"`
	Repo string  `json:"repo"`
	Cost float64 `json:"cost"`
}

// ContextStat is one session's remaining context share.
type ContextStat struct {
	ID       string  `json:"id"`
	Headroom float64 `json:"headroom"`
}

// summaryRow is all the machine-wide statistics need from one transcript, so
// a file that has not changed is parsed once and never again.
type summaryRow struct {
	Agent           Agent                       `json:"agent"`
	Model           string                      `json:"model,omitempty"`
	Day             string                      `json:"day"`
	Requests        int                         `json:"requests"`
	Usage           Usage                       `json:"usage"`
	ByDayModel      map[string]map[string]Usage `json:"byDayModel,omitempty"`
	ByDayModelTurns map[string]map[string]int   `json:"byDayModelTurns,omitempty"`
	Tools           map[string]int              `json:"tools,omitempty"`
	ToolFailures    map[string]int              `json:"toolFailures,omitempty"`
	TurnsDone       []Turn                      `json:"turnsDone,omitempty"`
	Retries         int                         `json:"retries,omitempty"`
	Compactions     int                         `json:"compactions,omitempty"`
	Interventions   int                         `json:"interventions,omitempty"`
	SessionDuration float64                     `json:"sessionDuration,omitempty"`
	Context         *ContextStat                `json:"context,omitempty"`
	// Hours and Weekdays count requests by when they were made, keyed 0-23 and
	// 0-6 with Sunday zero, as time.Weekday has it.
	Hours    map[int]int `json:"hours,omitempty"`
	Weekdays map[int]int `json:"weekdays,omitempty"`
}

// weekdayNames is the pie's labels, indexed by time.Weekday.
var weekdayNames = [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// topSessions is how many sessions a day keeps in TopSessions. Each is a
// Prometheus series for good, so this stays small.
const topSessions = 10

// Statistics loads the discovered sessions and groups the details useful at a
// glance. Failed transcripts are omitted rather than making the page unusable.
func Statistics(sessions []*Session) Stats {
	var out Stats
	days := map[string]*Group{}
	agents := map[string]*Group{}
	models := map[string]*Group{}
	dayModels := map[string]map[string]*Group{}
	dayModelTurns := map[string]map[string]*Group{}
	dayRepos := map[string]map[string]*Group{}
	dayRepoTurns := map[string]map[string]*Group{}
	dayAgentTurns := map[string]map[string]*Group{}
	daySessions := map[string][]SessionCost{}
	// Hundreds of sessions share a handful of directories.
	repos := map[string]string{}
	tools := map[string]*Group{}
	toolFailures := map[string]*Group{}
	hours := make([]Group, 24)
	weekdays := make([]Group, 7)
	for hour := range hours {
		hours[hour] = Group{Key: fmt.Sprintf("%02d", hour)}
	}
	for day, name := range weekdayNames {
		weekdays[day] = Group{Key: name}
	}
	for i, row := range summaries(sessions) {
		if row == nil {
			continue
		}
		repo, known := repos[sessions[i].CWD]
		if !known {
			repo = repoName(sessions[i].CWD)
			repos[sessions[i].CWD] = repo
		}
		if repo == "" {
			repo = "unknown"
		}
		out.Sessions++
		out.Requests += row.Requests
		out.Usage.Input += row.Usage.Input
		out.Usage.Output += row.Usage.Output
		out.Usage.CacheRead += row.Usage.CacheRead
		out.Usage.CacheWrite += row.Usage.CacheWrite
		out.Usage.Reasoning += row.Usage.Reasoning
		out.Cost += row.Usage.CostUSD
		for day, models := range row.ByDayModel {
			var usage Usage
			usage.CostKnown = true
			for _, each := range models {
				usage.Input += each.Input
				usage.Output += each.Output
				usage.CacheRead += each.CacheRead
				usage.CacheWrite += each.CacheWrite
				usage.Reasoning += each.Reasoning
				usage.CostUSD += each.CostUSD
				usage.CostKnown = usage.CostKnown && each.CostKnown
			}
			addUsage(groupsFor(dayRepos, day), repo, usage)
			daySessions[day] = append(daySessions[day], SessionCost{ID: sessions[i].ID, Repo: repo, Cost: usage.CostUSD})
		}
		for day, models := range row.ByDayModelTurns {
			for _, count := range models {
				addCount(groupsFor(dayRepoTurns, day), repo, count)
				addCount(groupsFor(dayAgentTurns, day), string(row.Agent), count)
			}
		}
		addStats(days, row.Day, row)
		addStats(agents, string(row.Agent), row)
		addStats(models, row.Model, row)
		for day, models := range row.ByDayModel {
			if dayModels[day] == nil {
				dayModels[day] = map[string]*Group{}
			}
			for model, usage := range models {
				addUsage(dayModels[day], model, usage)
			}
		}
		for day, models := range row.ByDayModelTurns {
			if dayModelTurns[day] == nil {
				dayModelTurns[day] = map[string]*Group{}
			}
			for model, count := range models {
				addCount(dayModelTurns[day], model, count)
			}
		}
		for name, count := range row.Tools {
			addCount(tools, name, count)
			out.Tools += count
		}
		for name, count := range row.ToolFailures {
			addCount(toolFailures, name, count)
		}
		out.Retries += row.Retries
		out.Compactions += row.Compactions
		out.Interventions += row.Interventions
		for _, turn := range row.TurnsDone {
			if turn.Failed {
				out.Failed++
			} else {
				out.Completed++
			}
			if turn.Duration > 0 {
				out.TurnDurations = append(out.TurnDurations, turn.Duration.Seconds())
			}
		}
		if row.SessionDuration > 0 {
			out.SessionDurations = append(out.SessionDurations, row.SessionDuration)
		}
		if row.Context != nil {
			out.Contexts = append(out.Contexts, *row.Context)
		}
		for hour, count := range row.Hours {
			if hour >= 0 && hour < len(hours) {
				hours[hour].Sessions += count
			}
		}
		for day, count := range row.Weekdays {
			if day >= 0 && day < len(weekdays) {
				weekdays[day].Sessions += count
			}
		}
	}
	out.ByDay, out.ByAgent, out.ByModel, out.ByTool = byKey(days), byCost(agents), byCost(models), byCost(tools)
	out.ByDayModel = make(map[string][]Group, len(dayModels))
	for day, models := range dayModels {
		out.ByDayModel[day] = byCost(models)
	}
	out.ByDayModelTurns = make(map[string][]Group, len(dayModelTurns))
	for day, models := range dayModelTurns {
		out.ByDayModelTurns[day] = byCost(models)
	}
	out.ByDayRepo = make(map[string][]Group, len(dayRepos))
	for day, repos := range dayRepos {
		out.ByDayRepo[day] = byCost(repos)
	}
	out.ByDayRepoTurns = make(map[string][]Group, len(dayRepoTurns))
	for day, repos := range dayRepoTurns {
		out.ByDayRepoTurns[day] = byCount(repos)
	}
	out.ByDayAgentTurns = make(map[string][]Group, len(dayAgentTurns))
	for day, agents := range dayAgentTurns {
		out.ByDayAgentTurns[day] = byCount(agents)
	}
	out.TopSessions = make(map[string][]SessionCost, len(daySessions))
	for day, costs := range daySessions {
		sort.SliceStable(costs, func(i, j int) bool {
			if costs[i].Cost != costs[j].Cost {
				return costs[i].Cost > costs[j].Cost
			}
			return costs[i].ID < costs[j].ID
		})
		out.TopSessions[day] = costs[:min(topSessions, len(costs))]
	}
	out.ByToolFailure = byCount(toolFailures)
	out.ByHour, out.ByWeekday = hours, weekdays
	return out
}

// StatisticsByAgent is Statistics for each agent's sessions on their own, so a
// figure can be labelled with the account that produced it.
func StatisticsByAgent(sessions []*Session) map[Agent]Stats {
	split := map[Agent][]*Session{}
	for _, s := range sessions {
		if s != nil {
			split[s.Agent] = append(split[s.Agent], s)
		}
	}
	out := make(map[Agent]Stats, len(split))
	for agent, mine := range split {
		out[agent] = Statistics(mine)
	}
	return out
}

func groupsFor(byDay map[string]map[string]*Group, day string) map[string]*Group {
	if byDay[day] == nil {
		byDay[day] = map[string]*Group{}
	}
	return byDay[day]
}

// summaries is one row per session, parsing only the transcripts that have
// changed since the last time anything asked.
func summaries(sessions []*Session) []*summaryRow {
	out := make([]*summaryRow, len(sessions))
	// A fixed pool, as indexFiles has: one goroutine per session is a stack
	// apiece for a machine's whole corpus before any of it is parsed.
	work := make(chan int)
	var wg sync.WaitGroup
	for range min(max(runtime.NumCPU(), 2), len(sessions)) {
		wg.Go(func() {
			for i := range work {
				out[i] = summaryOf(sessions[i])
			}
		})
	}
	for i := range sessions {
		work <- i
	}
	close(work)
	wg.Wait()
	summaryCache.flush()
	return out
}

func summaryOf(s *Session) *summaryRow {
	if s == nil {
		return nil
	}
	at, stamped := stampOf(s.Path)
	if stamped {
		if row, hit := summaryCache.get(s.Path, at); hit {
			return row
		}
	}
	row := parseSummary(s)
	if stamped {
		summaryCache.put(s.Path, at, row)
	}
	return row
}

func parseSummary(s *Session) *summaryRow {
	loaded, err := LoadCopy(s)
	if err != nil || loaded == nil {
		return nil
	}
	row := &summaryRow{
		Agent:           loaded.Agent,
		Model:           loaded.Model,
		Day:             loaded.LastUsed.Format("2006-01-02"),
		Requests:        len(loaded.Charges),
		Usage:           loaded.Usage,
		ByDayModel:      map[string]map[string]Usage{},
		ByDayModelTurns: map[string]map[string]int{},
		Tools:           loaded.Tools,
		ToolFailures:    loaded.ToolFailures,
		TurnsDone:       loaded.TurnsDone,
		Retries:         loaded.Retries,
		Compactions:     loaded.Compactions,
		Interventions:   max(len(loaded.Prompts)-1, 0),
		SessionDuration: sessionDuration(loaded).Seconds(),
		Hours:           map[int]int{},
		Weekdays:        map[int]int{},
	}
	if loaded.ContextWindow > 0 {
		row.Context = &ContextStat{ID: loaded.ID, Headroom: 100 * (1 - float64(loaded.ContextTokens)/float64(loaded.ContextWindow))}
		row.Context.Headroom = max(row.Context.Headroom, 0)
	}
	for _, charge := range loaded.Charges {
		at := charge.At.Local()
		row.Hours[at.Hour()]++
		row.Weekdays[int(at.Weekday())]++
		usage := charge.Usage
		if !usage.CostKnown {
			usage.CostUSD, usage.CostKnown = costFor(charge.Model, usage)
		}
		day := at.Format(time.DateOnly)
		if row.ByDayModel[day] == nil {
			row.ByDayModel[day] = map[string]Usage{}
		}
		model := charge.Model
		if model == "" {
			model = "unknown"
		}
		acc, exists := row.ByDayModel[day][model]
		if !exists {
			acc.CostKnown = true
		}
		acc.Input += usage.Input
		acc.Output += usage.Output
		acc.CacheRead += usage.CacheRead
		acc.CacheWrite += usage.CacheWrite
		acc.Reasoning += usage.Reasoning
		acc.CostUSD += usage.CostUSD
		acc.CostKnown = acc.CostKnown && usage.CostKnown
		row.ByDayModel[day][model] = acc
	}
	for _, turn := range loaded.TurnsDone {
		if turn.Failed || turn.At.IsZero() {
			continue
		}
		day := turn.At.Local().Format(time.DateOnly)
		model := turn.Model
		if model == "" {
			model = loaded.Model
		}
		if model == "" {
			model = "unknown"
		}
		if row.ByDayModelTurns[day] == nil {
			row.ByDayModelTurns[day] = map[string]int{}
		}
		row.ByDayModelTurns[day][model]++
	}
	return row
}

func sessionDuration(s *Session) time.Duration {
	if len(s.Prompts) == 0 || s.Prompts[0].At.IsZero() {
		return 0
	}
	last := s.Prompts[len(s.Prompts)-1].At
	for _, charge := range s.Charges {
		if charge.At.After(last) {
			last = charge.At
		}
	}
	for _, turn := range s.TurnsDone {
		if turn.At.After(last) {
			last = turn.At
		}
	}
	return max(last.Sub(s.Prompts[0].At), 0)
}

func addStats(groups map[string]*Group, key string, row *summaryRow) {
	if key == "" {
		key = "unknown"
	}
	g := groups[key]
	if g == nil {
		g = &Group{Key: key}
		groups[key] = g
	}
	g.Cost += row.Usage.CostUSD
	g.Tokens += row.Usage.Total()
	g.Sessions++
	g.Requests += row.Requests
}

func addCount(groups map[string]*Group, key string, count int) {
	g := groups[key]
	if g == nil {
		g = &Group{Key: key}
		groups[key] = g
	}
	g.Sessions += count
}

func addUsage(groups map[string]*Group, key string, usage Usage) {
	group := groups[key]
	if group == nil {
		group = &Group{Key: key}
		groups[key] = group
	}
	group.Cost += usage.CostUSD
	group.Tokens += usage.Total()
	group.Requests++
	if !usage.CostKnown && usage.Total() > 0 {
		group.Unpriced++
	}
}

// Summarise loads every session and totals the spend. Parses into copies, so it
// is safe to run off the UI goroutine while the originals are being drawn.
func Summarise(sessions []*Session) Spend {
	loaded := loadAll(sessions)

	var spend Spend
	dirs := map[string]*Group{}
	agents := map[string]*Group{}
	months := map[string]*Group{}

	for _, s := range loaded {
		if s == nil {
			spend.Failed++
			continue
		}
		unpriced := 0
		if !s.Usage.CostKnown && s.Usage.Total() > 0 {
			unpriced = 1
			spend.Unpriced++
		}
		buckets := map[string]map[string]*Group{
			s.DirLabel():    dirs,
			string(s.Agent): agents,
		}
		// A session that never ran has no timestamp; bucketing it would invent
		// an 0001-01 month.
		if !s.LastUsed.IsZero() {
			buckets[s.LastUsed.Format("2006-01")] = months
		}
		for key, buckets := range buckets {
			bucket, ok := buckets[key]
			if !ok {
				bucket = &Group{Key: key}
				buckets[key] = bucket
			}
			bucket.Cost += s.Usage.CostUSD
			bucket.Tokens += s.Usage.Total()
			bucket.Sessions++
			bucket.Unpriced += unpriced
		}
		spend.Total.Cost += s.Usage.CostUSD
		spend.Total.Tokens += s.Usage.Total()
		spend.Total.Sessions++
		spend.Total.Unpriced += unpriced
	}

	spend.ByDir = byCost(dirs)
	spend.ByAgent = byCost(agents)
	spend.ByMonth = byKey(months)
	return spend
}

// loadAll parses every session in parallel, returning nil where a parse failed.
func loadAll(sessions []*Session) []*Session {
	loaded := make([]*Session, len(sessions))
	var wg sync.WaitGroup
	tokens := make(chan struct{}, max(runtime.NumCPU(), 2))
	for i, s := range sessions {
		wg.Go(func() {
			tokens <- struct{}{}
			defer func() { <-tokens }()
			if copy, err := LoadCopy(s); err == nil {
				loaded[i] = copy
			}
		})
	}
	wg.Wait()
	return loaded
}

// Since reports how long ago the earliest session in a summary ran.
func (s Spend) Since() time.Duration {
	if len(s.ByMonth) == 0 {
		return 0
	}
	first, err := time.Parse("2006-01", s.ByMonth[0].Key)
	if err != nil {
		return 0
	}
	return time.Since(first)
}

func byCost(buckets map[string]*Group) []Group {
	out := flatten(buckets)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Cost != out[j].Cost {
			return out[i].Cost > out[j].Cost
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func byCount(buckets map[string]*Group) []Group {
	out := flatten(buckets)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Sessions != out[j].Sessions {
			return out[i].Sessions > out[j].Sessions
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func byKey(buckets map[string]*Group) []Group {
	out := flatten(buckets)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func flatten(buckets map[string]*Group) []Group {
	out := make([]Group, 0, len(buckets))
	for _, group := range buckets {
		out = append(out, *group)
	}
	return out
}
