package web

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/owenrumney/ade/internal/browse"
	"github.com/owenrumney/ade/internal/chat"
	"github.com/owenrumney/ade/internal/comments"
	"github.com/owenrumney/ade/internal/dash"
	"github.com/owenrumney/ade/internal/estimate"
	"github.com/owenrumney/ade/internal/ghpr"
	"github.com/owenrumney/ade/internal/gitdiff"
	"github.com/owenrumney/ade/internal/hub"
	"github.com/owenrumney/ade/internal/jobs"
	"github.com/owenrumney/ade/internal/limits"
	"github.com/owenrumney/ade/internal/lineage"
	"github.com/owenrumney/ade/internal/linear"
	"github.com/owenrumney/ade/internal/memory"
	"github.com/owenrumney/ade/internal/quiz"
	"github.com/owenrumney/ade/internal/sandbox"
	"github.com/owenrumney/ade/internal/session"
	"github.com/owenrumney/ade/internal/sessions"
	"github.com/owenrumney/ade/internal/settings"
	"github.com/owenrumney/ade/internal/settingsapi"
	"github.com/owenrumney/ade/internal/transcripts"
	"github.com/owenrumney/ade/internal/verify"
	"github.com/owenrumney/ade/internal/worktrees"
)

// responseShapes is every struct a client decodes, by JSON field and kind.
//
//nolint:gochecknoglobals // a table, not state
var responseShapes = map[string][]string{
	"Diff": {
		"base string", "commits int", "files slice", "head string", "mode string", "note string",
	},
	"DiffFile": {
		"additions int", "binary bool", "deletions int", "hunks slice", "oldPath string",
		"path string", "status string", "untracked bool",
	},
	"Hunk":  {"header string", "heading string", "newStart int", "oldStart int", "rows slice"},
	"Row":   {"left ptr", "right ptr"},
	"Cell":  {"content string", "kind string", "line int", "noNewline bool", "tokens slice"},
	"Token": {"c string", "e bool", "t string"},
	"Status": {
		"ahead int", "behind int", "lines map", "signing struct", "staged slice", "unstaged slice",
		"upstream string",
	},
	"Change": {"path string", "status string"},
	"Count":  {"added int", "removed int"},
	"Thread": {
		"anchor struct", "category string", "comments slice", "createdAt string", "disputed bool",
		"draftReview string", "draftThread string",
		"duplicateOf string", "externalId string", "file string", "id string", "line int",
		"orphaned bool", "orphanedAt string", "outgoing string", "resolved bool", "scope string",
		"sentAt string",
		"severity string", "side string", "withdrawn bool",
	},
	"Comment":     {"at string", "author string", "body string", "externalId string", "id string", "outcome string", "sentAt string"},
	"ContextPart": {"label string", "tokens int"},
	"Child":       {"doing string", "kind string", "name string", "since string", "steps int"},
	"Finished":    {"asking bool", "at string", "files int"},
	"RunState": {
		"activity slice", "asking slice", "children slice", "contextParts slice", "contextTokens int", "contextWindow int", "costUsd float", "current string", "doing string",
		"error string", "finished ptr", "free bool", "injectable bool",
		"kind string", "lastEventAt string", "model string", "provider string", "queued int", "queuedThreads slice",
		"repeatedAt string", "repeats int", "running bool",
		"sandbox bool", "session string", "sessionBytes int", "sideRuns slice", "startedAt string",
		"takenOver bool", "title string", "touched slice", "waiting slice", "welded string",
	},
	"SideRun": {"id string", "name string", "startedAt string"},
	"Waiting": {"chatId string", "id string", "kind string", "message string", "what string"},
	"BranchPR": {
		"approveBlocked string", "author string", "baseRef string", "body string", "bodyHtml string",
		"checkedAt string", "checks slice", "draft bool", "failing int", "headRef string",
		"headSha string", "mergeBlocked string", "number int", "requestChangesBlocked string", "reviewDecision string",
		"reviews slice", "state string",
		"title string", "url string",
	},
	"LineageTurn": {
		"at string", "commit string", "messageId string", "session string", "summary string",
	},
	"LineageResponse": {"turns slice"},
	"LineageMarks":    {"files map"},
	"DriftState":      {"at string", "file string", "items slice"},
	"DriftItem": {
		"dismissed bool", "file string", "fileLine int", "hash string", "id string", "kind string",
		"line int", "note string", "text string", "verdict string",
	},
	"Check":  {"bucket string", "description string", "link string", "name string", "state string", "workflow string"},
	"Review": {"author string", "state string"},
	"PRInfo": {
		"author string", "headRef string", "number int", "resolveBlocked string", "state string",
		"title string", "url string",
	},
	"Listed": {
		"author string", "checks string", "draft bool", "headRef string", "mine bool", "number int",
		"reviewDecision string", "reviewRequested bool", "title string", "updatedAt string", "url string",
	},
	"CreatedPR": {"number string", "url string"},
	// ghpr.Notification. The TypeScript carries one field more — dismissed —
	// which dash.Notification adds on the way out.
	"Notification": {
		"id string", "number int", "reason string", "repo string", "title string", "type string",
		"unread bool", "updatedAt string", "url string",
	},
	// This is ghpr.Request, what GitHub said. The TypeScript interface carries
	// one field more — muted — which dash.Review adds on the way out, and
	// which is ade's own state rather than anything GitHub knows.
	"ReviewRequest": {
		"author string", "checks string", "draft bool", "number int", "repo string", "reviewDecision string",
		"title string", "updatedAt string", "url string",
	},
	"CommitDraft": {"body string", "raw string", "staged slice", "title string"},
	"ChatMessage": {
		"at string", "author string", "body string", "context ptr", "doing string", "failed bool", "files slice", "foldedInto string", "for string",
		"html string", "id string", "images slice", "input string", "kind string", "model string", "output string",
		"review bool", "rule string", "run string", "short string", "shortHtml string", "steps int", "target string", "tool string", "toolId string", "undo string",
		"undone string",
	},
	"ChatContext":  {"from string", "text string"},
	"ChatFile":     {"image bool", "name string", "path string"},
	"ChatImage":    {"data string", "mediaType string"},
	"ChatResponse": {"messages slice", "session string"},
	"ChatEvent":    {"message ptr", "reset bool"},
	"Worktree":     {"branch string", "path string"},
	"DesignState": {
		"dir string", "file string", "files slice", "version string", "worktree ptr",
	},
	"DesignDoc": {
		"body string", "file string", "files slice", "raw string", "title string", "toc slice", "version string",
	},
	"SessionEntry": {
		"agent string", "archived bool", "branch string", "dir string", "from string", "group string",
		"here bool", "id string", "lastUsed string", "live bool", "model string", "name string",
		"pinned bool", "resumable bool", "title string",
	},
	"SessionGroup":      {"created string", "id string", "lastUsed string", "members slice", "title string"},
	"SessionSuggestion": {"fingerprint string", "ids slice"},
	"Dir": {
		"branch string", "groups slice", "here bool", "lastUsed string", "main string", "name string", "owner string",
		"path string", "scratch bool", "sessions slice", "suggestion ptr", "worktree bool",
	},
	"Repo": {
		"branch string", "checkouts slice", "here bool", "lastUsed string", "name string",
		"owner string", "path string", "remembered bool", "scratch bool", "sessions int",
	},
	"RecentWorkspace": {"at string", "branch string", "name string", "path string"},
	"AgentHealth": {
		"activity slice", "asking int", "children slice", "error string", "kind string", "lastEventAt string",
		"model string", "queued int",
		"running bool", "sandbox bool", "session string", "sideRuns slice", "stacked slice",
		"startedAt string",
	},
	"AgentChild":    {"doing string", "kind string", "name string"},
	"AgentQueued":   {"id string", "kind string", "what string"},
	"GraphServer":   {"agent ptr", "pid int", "startedAt string"},
	"GraphCheckout": {"branch string", "here bool", "name string", "path string", "server ptr", "sessions int", "worktree bool"},
	"GraphRepo":     {"checkouts slice", "name string", "owner string", "path string"},
	"WorktreeEntry": {
		"behind int", "branch string", "keep string", "kind string", "lastUsed string", "main bool",
		"merged bool", "name string", "path string", "removable bool", "serving string",
		"uncommitted int", "unpushed int",
	},
	"SyncReport": {
		"base string", "fetched int", "note string", "results slice",
	},
	"SyncResult": {
		"branch string", "detail string", "main bool", "name string", "outcome string", "path string",
	},
	"Meta": {
		"base string", "branch string", "files int", "launcher string", "mode string", "project string", "repo string",
		"roots slice", "scope string", "scratch bool", "store string", "view string",
	},
	"DiffRoot": {
		"branch string", "changes int", "home bool", "name string", "root string", "threads int",
	},
	"StateResponse": {
		"agent struct", "attachment ptr", "branchPr ptr", "bus ptr", "chat struct", "created ptr", "design ptr", "diffVersion string",
		"draft ptr", "hub ptr", "limits ptr", "linear ptr", "meta struct", "notifications slice",
		"openPrs slice", "pr ptr", "project ptr", "quiz ptr", "reviews slice", "threads slice",
		"verify ptr", "worktree ptr",
	},
	"Quiz": {"base string", "files int", "questions slice", "scope string", "stale bool"},
	"QuizQuestion": {
		"about string", "answer string", "file string", "line int", "options slice",
		"question string", "ref string", "why string",
	},
	"VerifyState": {
		"at string", "failed string", "narrow bool", "offer slice", "passed bool", "ran int",
		"running bool", "stopped bool", "total int",
	},
	"VerifyCheck": {
		"about string", "dir string", "filter string", "name string",
		"narrow string", "run string", "tool bool",
	},
	"BusState": {
		"instance string", "offers slice", "online bool", "replies slice", "sent slice",
		"team slice", "user string", "why string",
	},
	"BusMember": {
		"agents int", "harness string", "instance string", "label string", "node string",
		"presence string", "status string", "takesAsk bool", "takesPr bool", "user string", "you bool",
	},
	"BusReply": {
		"at string", "byAgent bool", "dir string", "forAgent bool", "from string",
		"id string", "kind string", "option string", "pr string", "question string",
		"text string", "verdict string",
	},
	"BusOffer": {
		"agent string", "answerBy string", "claimed bool", "createdAt string", "from string",
		"harness string", "id string", "kind string", "model string", "node string", "note string",
		"number int", "options slice", "pr string", "question string", "repo string",
		"to string",
	},
	"ProjectDebt": {"bytes int", "proposed int", "remembered bool", "unread int"},
	"ProjectView": {
		"bytes int", "entries slice", "instructions string", "jobs slice", "maxLength int",
		"name string", "proposed slice", "remembered bool", "remote string", "siblings slice",
		"slug string", "unread int",
	},
	"JobView": {
		"allow string", "continuity string", "description string", "failing bool", "kind string",
		"lastFired string", "name string", "nextDue string", "outcomes slice", "path string",
		"paused bool", "project string", "prompt string", "running bool", "schedule string",
		"scope string",
	},
	"JobOutcome": {
		"costUsd float", "dir string", "error string", "firedAt string", "millis int",
		"noHandover bool", "report string", "seen bool", "session string", "url string",
	},
	"JobSkipped": {"error string", "path string"},
	"MemoryEntry": {
		"added string", "body string", "heading string", "id string", "lastSeen string",
		"replaces string", "session string", "source string", "state string",
	},
	// Candidate embeds Entry, which the reflector sees as one field and the
	// TypeScript mirrors with extends. Its fields are checked as MemoryEntry.
	"MemoryCandidate": {"Entry struct", "replacesHeading string"},
	"ChatInfo":        {"count int", "session string"},
	"LinearState":     {"checkedAt string", "error string", "issues slice"},
	"LinearIssue": {
		"branchName string", "identifier string", "priority int", "project string",
		"started bool", "state string", "title string", "updatedAt string", "url string",
	},
	"SettingsView": {
		"cleanseOff bool", "colors map", "dashboardClaude string", "dashboardCodex string", "dashboardHost string", "dashboardTokenEnd string", "dashboardTokenSet bool", "dashboardUrl string", "deepBackground bool",
		"gitHubPackagesTokenEnd string", "gitHubPackagesTokenSet bool", "homeAssistantDesk string", "homeAssistantNotify string", "homeAssistantTokenEnd string", "homeAssistantTokenSet bool", "homeAssistantUrl string",
		"hosts slice", "ignoreAuthors slice", "ignoreOlderDays int", "ignoreReasons slice", "ignoreRepos slice", "ignoredDirs slice",
		"instructions string", "instructionsMaxLength int",
		"linearKeyEnd string", "linearKeySet bool", "mergeQueuedOff bool", "palette string", "path string",
		"previewOff bool", "previewUrl string",
		"sandboxEnv map", "sandboxPaths slice", "scanDirs slice",
		"sharpenOff bool",
	},
	"SettingsHost": {"label string", "name string", "ship_cloud_credentials bool"},
	"SandboxView": {
		"always slice", "env map", "fixed string", "on bool", "path string", "paths slice",
		"runtime string",
	},
	"AlwaysMount":   {"path string", "rw bool", "why string"},
	"PreviewView":   {"effective string", "off bool", "path string", "url string"},
	"SandboxPath":   {"path string", "rw bool"},
	"BrowseView":    {"dirs slice", "home string", "parent string", "path string"},
	"BrowseDir":     {"name string", "path string", "repo bool"},
	"LimitsState":   {"claude ptr", "codex ptr", "error string"},
	"Limits":        {"checkedAt string", "provider string", "windows slice"},
	"LimitWindow":   {"name string", "percent float", "resetsAt string"},
	"HubInfo":       {"running bool", "url string"},
	"Attached":      {"from string", "seq int", "text string"},
	"DiffResponse":  {"diff ptr", "version string"},
	"BuildEstimate": {"estimate ptr"},
	"Estimate": {
		"group string", "highSeconds int", "kind string", "lowSeconds int", "runs int", "window ptr",
	},
	"EstimateWindow": {"about float", "cap bool", "high float", "name string", "resetsAt string", "used float"},
}

// TestTheResponseShapesStillMatchTheGoStructs catches a Go field renamed,
// retyped or dropped out from under a client. It needed no TypeScript to do
// that, and kept doing it when web/src went.
func TestTheResponseShapesStillMatchTheGoStructs(t *testing.T) {
	subjects := map[string]any{
		"Diff":              gitdiff.Diff{},
		"DiffFile":          gitdiff.File{},
		"Hunk":              gitdiff.Hunk{},
		"Row":               gitdiff.Row{},
		"Cell":              gitdiff.Cell{},
		"Token":             gitdiff.Token{},
		"Status":            gitdiff.Status{},
		"Change":            gitdiff.Change{},
		"Count":             gitdiff.Count{},
		"Thread":            comments.Thread{},
		"Comment":           comments.Comment{},
		"ContextPart":       transcripts.ContextPart{},
		"RunState":          RunState{},
		"Waiting":           Waiting{},
		"SideRun":           SideRun{},
		"Child":             Child{},
		"Finished":          Finished{},
		"BranchPR":          BranchPR{},
		"Check":             ghpr.Check{},
		"Review":            ghpr.Review{},
		"PRInfo":            PRInfo{},
		"Listed":            ghpr.Listed{},
		"ReviewRequest":     ghpr.Request{},
		"Notification":      ghpr.Notification{},
		"CreatedPR":         CreatedPR{},
		"CommitDraft":       CommitDraft{},
		"ChatMessage":       chat.Message{},
		"ChatContext":       chat.Context{},
		"ChatImage":         chat.Image{},
		"ChatResponse":      chatResponse{},
		"ChatEvent":         chatEvent{},
		"Worktree":          Worktree{},
		"DesignState":       DesignState{},
		"DesignDoc":         documentResponse{},
		"SessionEntry":      sessions.Session{},
		"SessionGroup":      sessions.Group{},
		"SessionSuggestion": sessions.Suggestion{},
		"Dir":               sessions.Dir{},
		"Repo":              sessions.Repo{},
		"RecentWorkspace":   settings.Workspace{},
		"AgentHealth":       session.AgentHealth{},
		"AgentChild":        session.AgentChild{},
		"AgentQueued":       session.AgentQueued{},
		"GraphServer":       sessions.GraphServer{},
		"GraphCheckout":     sessions.GraphCheckout{},
		"GraphRepo":         sessions.GraphRepo{},
		"WorktreeEntry":     worktrees.Worktree{},
		"SyncReport":        worktrees.Report{},
		"SyncResult":        worktrees.Result{},
		"Meta":              meta{},
		"DiffRoot":          DiffRoot{},
		"StateResponse":     stateResponse{},
		"BusState":          BusState{},
		"BusOffer":          BusOffer{},
		"BusReply":          BusReply{},
		"BusMember":         BusMember{},
		"ProjectDebt":       ProjectDebt{},
		"ProjectView":       ProjectView{},
		"JobView":           jobs.View{},
		"JobOutcome":        jobs.Outcome{},
		"JobSkipped":        jobs.Skipped{},
		"MemoryEntry":       memory.Entry{},
		"MemoryCandidate":   memory.Candidate{},
		"ChatFile":          chat.File{},
		"ChatInfo":          ChatInfo{},
		"LinearState":       dash.Tickets{},
		"LinearIssue":       linear.Issue{},
		"SettingsView":      settingsapi.View{},
		"SettingsHost":      settings.Host{},
		"SandboxView":       sandboxView{},
		"AlwaysMount":       alwaysMount{},
		"PreviewView":       previewView{},
		"SandboxPath":       sandbox.Path{},
		"BrowseView":        browse.View{},
		"BrowseDir":         browse.Dir{},
		"LimitsState":       LimitsState{},
		"Limits":            limits.Limits{},
		"LimitWindow":       limits.Window{},
		"DiffResponse":      diffResponse{},
		"BuildEstimate":     BuildEstimate{},
		"Estimate":          estimate.Estimate{},
		"EstimateWindow":    estimate.Window{},
		"LineageTurn":       lineage.Turn{},
		"LineageResponse":   lineageResponse{},
		"LineageMarks":      lineageMarks{},
		"DriftState":        DriftState{},
		"DriftItem":         DriftItem{},
		"HubInfo":           hub.Info{},
		"Attached":          Attachment{},
		"VerifyState":       VerifyState{},
		"VerifyCheck":       verify.Check{},
		"Quiz":              Quiz{},
		"QuizQuestion":      quiz.Question{},
	}
	require.Len(t, subjects, len(responseShapes), "a struct is in one table and not the other")

	for name, subject := range subjects {
		t.Run(name, func(t *testing.T) {
			assert.Equalf(t, responseShapes[name], shapeOf(subject),
				"%s changed on the Go side — update this table", name)
		})
	}
}

// shapeOf is a struct's JSON field names and broad kinds, sorted. Broad because
// the point is to catch a field moving, not to model TypeScript.
func shapeOf(subject any) []string {
	typ := reflect.TypeOf(subject)
	var out []string
	for i := range typ.NumField() {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		out = append(out, fmt.Sprintf("%s %s", name, kindOf(field.Type)))
	}
	sort.Strings(out)
	return out
}

func kindOf(typ reflect.Type) string {
	// A time is a string over the wire, and a *time.Time is one or absent, which
	// the TypeScript spells as an optional string either way.
	if typ == reflect.TypeOf(time.Time{}) {
		return "string"
	}
	if typ.Kind() == reflect.Pointer {
		if typ.Elem() == reflect.TypeOf(time.Time{}) {
			return "string"
		}
		return "ptr"
	}
	switch typ.Kind() {
	case reflect.Slice, reflect.Array:
		return "slice"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "int"
	case reflect.Float32, reflect.Float64:
		return "float"
	default:
		return typ.Kind().String()
	}
}
