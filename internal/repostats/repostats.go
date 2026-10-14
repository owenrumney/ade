// Package repostats reads what a repository is and what ade has done in it.
// Design: .git/ade/docs/repo-stats-webview.md.
package repostats

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/owenrumney/ade/internal/transcripts"
)

// Stats is one repository: what it is, and what ade spent in it.
type Stats struct {
	Repo   string `json:"repo"`
	Dir    string `json:"dir"`
	Branch string `json:"branch"`
	Remote string `json:"remote,omitempty"`

	// Ahead and Behind compare the branch with its upstream, when it has one.
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	Dirty    int    `json:"dirty"`

	Languages []Language `json:"languages"`
	Lines     int        `json:"lines"`
	Files     int        `json:"files"`

	// Days is a year of commit counts, oldest first, with the quiet days in it.
	Days         []Day    `json:"days"`
	Commits      int      `json:"commits"`
	BusiestDay   string   `json:"busiestDay,omitempty"`
	Contributors []Person `json:"contributors"`
	Last         *Commit  `json:"last,omitempty"`

	// Agent is this machine's transcripts for the repository, last 30 days.
	Agent Agent `json:"agent"`
}

// Language is one file type's share, by line.
type Language struct {
	Name  string `json:"name"`
	Lines int    `json:"lines"`
	Files int    `json:"files"`
}

// Day is one date's commit count.
type Day struct {
	Date    string `json:"date"`
	Commits int    `json:"commits"`
}

// Person is one author and how often they commit.
type Person struct {
	Name    string `json:"name"`
	Email   string `json:"email,omitempty"`
	Commits int    `json:"commits"`
}

// Commit is the tip.
type Commit struct {
	SHA     string    `json:"sha"`
	Subject string    `json:"subject"`
	Author  string    `json:"author"`
	At      time.Time `json:"at"`
}

// Agent is the repository's slice of this machine's transcripts.
type Agent struct {
	Cost     float64             `json:"cost"`
	Turns    int                 `json:"turns"`
	Sessions int                 `json:"sessions"`
	Tokens   transcripts.Usage   `json:"tokens"`
	ByDay    []transcripts.Group `json:"byDay"`
	Tools    []transcripts.Group `json:"tools"`
	Models   []transcripts.Group `json:"models"`
}

// window is how much history the page shows, in days.
const window = 365

// agentWindow is how far back the agent numbers go.
const agentWindow = 30

// gitWait bounds one git call. A year of log on a large repository is fast;
// a repository on a sleeping network drive is not.
const gitWait = 20 * time.Second

// Collect reads dir. Git facts and the agent numbers are independent, so a
// repository with no transcripts still draws, and a directory that is not a
// repository is an error rather than an empty page.
func Collect(ctx context.Context, dir string, sessions []*transcripts.Session, now time.Time) (*Stats, error) {
	top, err := gitLine(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s is not a git repository: %w", dir, err)
	}
	out := &Stats{Dir: top}
	out.Remote, _ = gitLine(ctx, top, "config", "--get", "remote.origin.url")
	out.Repo = transcripts.RepoKey(top)
	out.Branch, _ = gitLine(ctx, top, "rev-parse", "--abbrev-ref", "HEAD")

	out.upstream(ctx, top)
	out.history(ctx, top, now)
	out.languages(ctx, top)
	if status, err := gitOut(ctx, top, "status", "--porcelain"); err == nil {
		out.Dirty = len(nonEmpty(status))
	}
	out.Agent = agentOf(sessions, out.Repo, now)
	return out, nil
}

// upstream fills the ahead and behind counts. A branch with no upstream is
// ordinary, not an error: that is most of a worktree's life.
func (s *Stats) upstream(ctx context.Context, dir string) {
	name, err := gitLine(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil || name == "" {
		return
	}
	s.Upstream = name
	counts, err := gitLine(ctx, dir, "rev-list", "--left-right", "--count", name+"...HEAD")
	if err != nil {
		return
	}
	parts := strings.Fields(counts)
	if len(parts) == 2 {
		s.Behind, _ = strconv.Atoi(parts[0])
		s.Ahead, _ = strconv.Atoi(parts[1])
	}
}

// history is a year of commits in one pass: the heatmap, the contributors and
// the tip all come from the same log.
func (s *Stats) history(ctx context.Context, dir string, now time.Time) {
	since := now.AddDate(0, 0, -(window - 1)).Format(time.DateOnly)
	log, err := gitOut(ctx, dir, "log", "--since="+since, "--date=format:%Y-%m-%d", "--pretty=format:%ad\x1f%an\x1f%ae")
	if err != nil {
		return
	}
	counts := map[string]int{}
	people := map[string]*Person{}
	for _, line := range nonEmpty(log) {
		parts := strings.Split(line, "\x1f")
		if len(parts) < 3 {
			continue
		}
		counts[parts[0]]++
		s.Commits++
		who := people[parts[2]]
		if who == nil {
			who = &Person{Name: parts[1], Email: parts[2]}
			people[parts[2]] = who
		}
		who.Commits++
	}
	s.Days = make([]Day, 0, window)
	busiest := 0
	for i := window - 1; i >= 0; i-- {
		date := now.AddDate(0, 0, -i).Format(time.DateOnly)
		s.Days = append(s.Days, Day{Date: date, Commits: counts[date]})
		if counts[date] > busiest {
			busiest, s.BusiestDay = counts[date], date
		}
	}
	for _, who := range people {
		s.Contributors = append(s.Contributors, *who)
	}
	sort.SliceStable(s.Contributors, func(i, j int) bool {
		if s.Contributors[i].Commits != s.Contributors[j].Commits {
			return s.Contributors[i].Commits > s.Contributors[j].Commits
		}
		return s.Contributors[i].Name < s.Contributors[j].Name
	})
	s.Last = tip(ctx, dir)
}

func tip(ctx context.Context, dir string) *Commit {
	line, err := gitLine(ctx, dir, "log", "-1", "--date=iso-strict", "--pretty=format:%h\x1f%s\x1f%an\x1f%ad")
	if err != nil || line == "" {
		return nil
	}
	parts := strings.Split(line, "\x1f")
	if len(parts) < 4 {
		return nil
	}
	at, _ := time.Parse(time.RFC3339, parts[3])
	return &Commit{SHA: parts[0], Subject: parts[1], Author: parts[2], At: at}
}

// languages counts lines per extension across the tracked files. Reading every
// file is the only honest count, so the caller caches it against HEAD.
func (s *Stats) languages(ctx context.Context, dir string) {
	listed, err := gitOut(ctx, dir, "ls-files", "-z")
	if err != nil {
		return
	}
	byName := map[string]*Language{}
	for _, name := range strings.Split(strings.TrimRight(listed, "\x00"), "\x00") {
		if name == "" {
			continue
		}
		lines, ok := countLines(filepath.Join(dir, name))
		if !ok {
			continue // binary, or gone since ls-files listed it
		}
		kind := languageOf(name)
		lang := byName[kind]
		if lang == nil {
			lang = &Language{Name: kind}
			byName[kind] = lang
		}
		lang.Lines += lines
		lang.Files++
		s.Lines += lines
		s.Files++
	}
	for _, lang := range byName {
		s.Languages = append(s.Languages, *lang)
	}
	sort.SliceStable(s.Languages, func(i, j int) bool {
		if s.Languages[i].Lines != s.Languages[j].Lines {
			return s.Languages[i].Lines > s.Languages[j].Lines
		}
		return s.Languages[i].Name < s.Languages[j].Name
	})
}

// agentOf is the repository's share of this machine's transcripts.
func agentOf(sessions []*transcripts.Session, repo string, now time.Time) Agent {
	if repo == "" || len(sessions) == 0 {
		return Agent{}
	}
	mine := make([]*transcripts.Session, 0, len(sessions))
	for _, s := range sessions {
		if s != nil && transcripts.RepoKey(s.CWD) == repo {
			mine = append(mine, s)
		}
	}
	if len(mine) == 0 {
		return Agent{}
	}
	stats := transcripts.Statistics(mine)
	out := Agent{
		Sessions: stats.Sessions,
		Tokens:   stats.Usage,
		Tools:    top(stats.ByTool, 8),
		Models:   top(stats.ByModel, 8),
	}
	have := map[string]transcripts.Group{}
	for _, group := range stats.ByDay {
		have[group.Key] = group
	}
	for i := agentWindow - 1; i >= 0; i-- {
		key := now.AddDate(0, 0, -i).Format(time.DateOnly)
		day := have[key]
		day.Key = key
		out.ByDay = append(out.ByDay, day)
		out.Cost += day.Cost
	}
	for _, turns := range stats.ByDayRepoTurns {
		for _, group := range turns {
			if group.Key == repo {
				out.Turns += group.Sessions
			}
		}
	}
	return out
}

func top(groups []transcripts.Group, n int) []transcripts.Group {
	if len(groups) <= n {
		return groups
	}
	return groups[:n]
}

// countLines counts the newlines in a file, and says no to anything with a NUL
// in its first block: a binary has no line count worth showing.
func countLines(path string) (int, bool) {
	file, err := openRead(path)
	if err != nil {
		return 0, false
	}
	defer func() { _ = file.Close() }()

	reader := bufio.NewReaderSize(file, 64*1024)
	head, _ := reader.Peek(8000)
	if bytes.IndexByte(head, 0) >= 0 {
		return 0, false
	}
	lines := 0
	buf := make([]byte, 64*1024)
	for {
		n, err := reader.Read(buf)
		lines += bytes.Count(buf[:n], []byte{'\n'})
		if err != nil {
			return lines, true
		}
	}
}

func nonEmpty(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func gitLine(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := gitOut(ctx, dir, args...)
	return strings.TrimSpace(out), err
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitWait)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- args are internally constructed
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
