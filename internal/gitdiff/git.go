package gitdiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/owenrumney/ade/internal/statedir"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// slowGit is the point past which a git call is worth a line of its own.
const slowGit = 250 * time.Millisecond

// Diff modes.
const (
	ModeWorktree    = "worktree"
	ModeUncommitted = "uncommitted"
	ModeCommitted   = "committed"
	ModeRev         = "rev"
	ModePR          = "pr"
	ModeSnapshot    = "pr snapshot"
)

// maxUntracked bounds how many untracked files we diff individually, since each
// one costs a git process.
const maxUntracked = 200

var baseCandidates = []string{"origin/main", "origin/master", "main", "master"}

// Diff scopes: what a run is being asked to show.
const (
	// ScopeBranch is everything on this branch, committed or not.
	ScopeBranch = "branch"
	// ScopeCommitted is only what has been committed.
	ScopeCommitted = "committed"
	// ScopeUncommitted is only what is still pending commit.
	ScopeUncommitted = "uncommitted"
)

// Scopes reports the selectable scopes, in the order they belong in a switcher.
func Scopes() []string { return []string{ScopeBranch, ScopeCommitted, ScopeUncommitted} }

// ValidScope reports whether scope is one ade can show.
func ValidScope(scope string) bool {
	return scope == ScopeBranch || scope == ScopeCommitted || scope == ScopeUncommitted
}

// Options selects what to diff.
type Options struct {
	Repo    string
	Base    string
	Rev     string
	Scope   string
	Context int
}

// Load resolves the diff for opts, including untracked files in worktree mode.
func Load(ctx context.Context, opts Options) (*Diff, error) {
	if opts.Context <= 0 {
		opts.Context = 3
	}
	root, err := RepoRoot(ctx, opts.Repo)
	if err != nil {
		return nil, err
	}

	spec, mode, base, note, err := resolveSpec(ctx, root, opts)
	if err != nil {
		return nil, err
	}

	args := []string{"diff", "--no-color", "--no-ext-diff", "--find-renames",
		fmt.Sprintf("-U%d", opts.Context)}
	if spec != "" {
		args = append(args, spec)
	}
	out, err := runDiff(ctx, root, args...)
	if err != nil {
		return nil, err
	}

	diff := &Diff{Base: base, Mode: mode, Note: note, Files: Parse(out)}
	diff.Head = headLabel(ctx, root, opts)
	diff.Commits = commitsAhead(ctx, root, base)

	// Uncommitted work includes files git has never seen, so both live modes
	// need them.
	if mode == ModeWorktree || mode == ModeUncommitted {
		untracked, err := untrackedFiles(ctx, root, opts.Context)
		if err != nil {
			return nil, err
		}
		diff.Files = append(diff.Files, untracked...)
	}
	return diff, nil
}

// RepoRoot returns the top level of the repository containing dir.
func RepoRoot(ctx context.Context, dir string) (string, error) {
	out, err := run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s", dir)
	}
	return strings.TrimSpace(out), nil
}

// GitDir returns the absolute git directory for dir. Inside a worktree this is
// not <root>/.git, so callers storing state there must use it.
func GitDir(ctx context.Context, dir string) (string, error) {
	out, err := run(ctx, dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s", dir)
	}
	return strings.TrimSpace(out), nil
}

// CommonGitDir is the repository's main git directory, shared by every worktree.
//
// GitDir gives a worktree its own <main>/.git/worktrees/<name>, which is right
// for anything per-checkout and wrong for anything belonging to the repository
// as a whole. In an ordinary checkout the two are the same directory.
func CommonGitDir(ctx context.Context, dir string) (string, error) {
	out, err := run(ctx, dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s", dir)
	}
	common := strings.TrimSpace(out)
	if common == "" {
		return "", fmt.Errorf("no common git dir for %s", dir)
	}
	// Older git reports this relative to the working directory.
	if !filepath.IsAbs(common) {
		common = filepath.Join(dir, common)
	}
	// GitDir resolves symlinks (--absolute-git-dir does), and on macOS /var is
	// one, so without this the two disagree about the same directory.
	if resolved, err := filepath.EvalSymlinks(common); err == nil {
		return resolved, nil
	}
	return filepath.Clean(common), nil
}

// StagedFiles lists paths in the index, which is what a commit would include.
func StagedFiles(ctx context.Context, dir string) ([]string, error) {
	out, err := run(ctx, dir, "diff", "--staged", "--name-only", "-z")
	if err != nil {
		return nil, fmt.Errorf("list staged files: %w", err)
	}
	var files []string
	for _, name := range strings.Split(strings.TrimRight(out, "\x00"), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	return files, nil
}

// Change is one path in the worktree with its porcelain status letter.
type Change struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

type Count struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
}

// Status is what a commit would include and what it would leave behind.
type Status struct {
	Staged   []string `json:"staged"`
	Unstaged []Change `json:"unstaged"`
	Upstream string   `json:"upstream,omitempty"`
	Ahead    int      `json:"ahead"`

	// Behind is how many commits the base has that this branch does not, as
	// of the last fetch. A rebase is what clears it.
	Behind int `json:"behind"`

	// Lines is the +/- per path. A binary or untracked one has no entry.
	Lines map[string]Count `json:"lines,omitempty"`

	// Signing is how a commit here would be signed, or empty for not at all.
	Signing Signing `json:"signing,omitzero"`
}

// Signing is git's answer on whether it will sign, and with what, read from
// the config rather than guessed.
type Signing struct {
	On bool `json:"on"`
	// Format is git's gpg.format: openpgp, ssh or x509. Empty means openpgp,
	// which is what git assumes when nothing says otherwise.
	Format string `json:"format,omitempty"`
	// Key is user.signingkey as configured, which for ssh is a path.
	Key string `json:"key,omitempty"`
}

// How is the format in the words a person uses, for a label.
func (s Signing) How() string {
	switch s.Format {
	case "", "openpgp":
		return "GPG"
	case "ssh":
		return "SSH"
	case "x509":
		return "X.509"
	}
	return s.Format
}

// SigningOf reads whether a commit here would be signed. A git that will not
// answer is not signing: the other way round is the wrong way to be wrong.
func SigningOf(ctx context.Context, dir string) Signing {
	on, err := run(ctx, dir, "config", "--get", "commit.gpgsign")
	if err != nil || !isTrue(strings.TrimSpace(on)) {
		return Signing{}
	}
	out := Signing{On: true}
	if format, err := run(ctx, dir, "config", "--get", "gpg.format"); err == nil {
		out.Format = strings.TrimSpace(format)
	}
	if key, err := run(ctx, dir, "config", "--get", "user.signingkey"); err == nil {
		out.Key = strings.TrimSpace(key)
	}
	return out
}

// isTrue is git's own spelling of a boolean, which is not only "true".
func isTrue(value string) bool {
	switch strings.ToLower(value) {
	case "true", "yes", "on", "1":
		return true
	}
	return false
}

// WorktreeStatus reports the index, the work left out of it, and how far the
// branch has run ahead of its upstream.
func WorktreeStatus(ctx context.Context, dir string) (Status, error) {
	out, err := run(ctx, dir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return Status{}, fmt.Errorf("read worktree status: %w", err)
	}

	status := Status{}
	for _, entry := range parsePorcelain(out) {
		if entry.index != ' ' && entry.index != '?' {
			status.Staged = append(status.Staged, entry.path)
			// A staged rename is two index entries. Unstaging the new path alone
			// would leave the old one staged for deletion.
			if entry.index == 'R' && entry.from != "" {
				status.Staged = append(status.Staged, entry.from)
			}
		}
		if entry.worktree != ' ' {
			letter := string(entry.worktree)
			if entry.index == '?' {
				letter = "??"
			}
			status.Unstaged = append(status.Unstaged, Change{Path: entry.path, Status: letter})
		}
	}

	status.Lines = numstat(ctx, dir)
	status.Upstream, status.Ahead = upstream(ctx, dir)
	return status, nil
}

// numstat counts against HEAD, index and worktree together: one row, one path.
func numstat(ctx context.Context, dir string) map[string]Count {
	out, err := run(ctx, dir, "diff", "--numstat", "-z", "HEAD")
	if err != nil {
		return nil
	}
	fields := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	lines := map[string]Count{}
	for i := range fields {
		parts := strings.SplitN(fields[i], "\t", 3)
		if len(parts) != 3 {
			continue
		}
		path := parts[2]
		// A rename spends the record on the counts and the next two on the old
		// and new paths. The new one is what the status entry calls it.
		if path == "" && i+2 < len(fields) {
			path = fields[i+2]
			fields[i+1], fields[i+2] = "", ""
		}
		added, aErr := strconv.Atoi(parts[0])
		removed, rErr := strconv.Atoi(parts[1])
		if aErr != nil || rErr != nil || path == "" {
			continue
		}
		lines[path] = Count{Added: added, Removed: removed}
	}
	return lines
}

// Stage adds paths to the index. Pathspecs are passed after -- so a file named
// like a flag cannot turn into one.
func Stage(ctx context.Context, dir string, paths []string) error {
	if len(paths) == 0 {
		return fmt.Errorf("no paths to stage")
	}
	args := append([]string{"add", "--"}, paths...)
	if _, err := run(ctx, dir, args...); err != nil {
		return fmt.Errorf("git add failed: %w", err)
	}
	return nil
}

// Unstage takes paths back out of the index and leaves the working tree alone.
// Pathspecs are passed after -- so a file named like a flag cannot turn into one.
func Unstage(ctx context.Context, dir string, paths []string) error {
	if len(paths) == 0 {
		return fmt.Errorf("no paths to unstage")
	}
	args := append([]string{"reset", "--quiet", "--"}, paths...)
	if _, err := run(ctx, dir, args...); err != nil {
		return fmt.Errorf("git reset failed: %w", err)
	}
	return nil
}

// WorktreePath is where a worktree for slug belongs: alongside the repository,
// named after it, so sibling checkouts of one project sort together.
func WorktreePath(root, slug string) string {
	root = strings.TrimSuffix(root, string(filepath.Separator))
	return filepath.Join(filepath.Dir(root), filepath.Base(root)+"-"+slug)
}

// EnsureWorktree returns the worktree at path, making it if it is not there.
// Coming back to a ticket should land you in the checkout you left, so an
// existing one on the expected branch is reused rather than refused.
func EnsureWorktree(ctx context.Context, root, path, branch string) (bool, error) {
	return EnsureWorktreeFrom(ctx, root, path, branch, nil)
}

// EnsureWorktreeFrom is EnsureWorktree branching a new checkout off start,
// a function because resolving one costs a fetch and reuse must not wait.
func EnsureWorktreeFrom(ctx context.Context, root, path, branch string, start func() string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		from := ""
		if start != nil {
			from = start()
		}
		return true, AddWorktreeFrom(ctx, root, path, branch, from)
	}

	// Something is there. Only reuse it if it really is this branch's checkout,
	// rather than writing into whatever happens to occupy the path. Checked in
	// this order because Branch reports "detached" for anything it cannot read,
	// which would describe an unrelated directory as a checkout.
	if _, err := RepoRoot(ctx, path); err != nil {
		return false, fmt.Errorf("%s already exists and is not a checkout", path)
	}
	if on := Branch(ctx, path); !strings.EqualFold(on, branch) {
		return false, fmt.Errorf("%s already exists and is on %s, not %s", path, on, branch)
	}
	return false, nil
}

// AddWorktree checks the repository out again at path, on a new branch off HEAD.
// Branching from HEAD rather than the default branch means the worktree's diff
// shows what was built there and nothing else.
// AddDetachedWorktree adds a worktree at path on no branch, for a caller that
// will check something out there itself.
func AddDetachedWorktree(ctx context.Context, root, path string) error {
	if path == "" {
		return fmt.Errorf("a worktree needs a path")
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	if _, err := run(ctx, root, "worktree", "add", "--detach", path); err != nil {
		return fmt.Errorf("git worktree add failed: %w", err)
	}
	return nil
}

func AddWorktree(ctx context.Context, root, path, branch string) error {
	if path == "" || branch == "" {
		return fmt.Errorf("a worktree needs a path and a branch")
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	if _, err := run(ctx, root, "worktree", "add", path, "-b", branch); err != nil {
		return fmt.Errorf("git worktree add failed: %w", err)
	}
	return nil
}

// HasBranch reports whether the repository already has this branch.
func HasBranch(ctx context.Context, root, branch string) bool {
	_, err := run(ctx, root, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// WorktreeOn is where this repository has branch checked out, or empty. Git
// refuses a second checkout of a branch, so the caller reuses what it names.
func WorktreeOn(ctx context.Context, root, branch string) string {
	out, err := run(ctx, root, "worktree", "list", "--porcelain")
	if err != nil {
		return ""
	}
	at := ""
	for _, line := range strings.Split(out, "\n") {
		if after, ok := strings.CutPrefix(line, "worktree "); ok {
			at = strings.TrimSpace(after)
		}
		if after, ok := strings.CutPrefix(line, "branch "); ok && strings.TrimSpace(after) == "refs/heads/"+branch {
			return at
		}
	}
	return ""
}

// AddWorktreeOn checks a branch the repository already has out at path: the
// second repository to get a session's branch is not creating it.
func AddWorktreeOn(ctx context.Context, root, path, branch string) error {
	if path == "" || branch == "" {
		return fmt.Errorf("a worktree needs a path and a branch")
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	if _, err := run(ctx, root, "worktree", "add", path, branch); err != nil {
		return fmt.Errorf("git worktree add failed: %w", err)
	}
	return nil
}

// BaseBranch is the first of the usual base branches the repository has, or
// empty when it has none of them.
func BaseBranch(ctx context.Context, root string) string {
	for _, candidate := range baseCandidates {
		if _, err := run(ctx, root, "rev-parse", "--verify", "--quiet", candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// OriginURL is the checkout's origin remote, or empty when it has none. It is
// the only identity a repository keeps across clones and moves.
func OriginURL(ctx context.Context, dir string) string {
	out, err := run(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// RemoteBase is origin's copy of a base branch — origin/main for main — when
// the repository has one, so a diff is against what the remote has rather
// than a local branch that is only as fresh as its last pull. Without one
// the branch is returned as given.
func RemoteBase(ctx context.Context, root, branch string) string {
	branch = strings.TrimPrefix(strings.TrimSpace(branch), "origin/")
	if branch == "" {
		return ""
	}
	if _, err := run(ctx, root, "rev-parse", "--verify", "--quiet", "origin/"+branch); err == nil {
		return "origin/" + branch
	}
	return branch
}

// FetchBase brings origin's copy of the base branch up to date, so the
// merge-base — and the diff — is against the branch as it is now, which is
// what GitHub compares a pull request to. base is the diff's base as
// configured: origin/main, main, or empty for the repository's default.
// It returns the remote-tracking ref that was fetched.
func FetchBase(ctx context.Context, root, base string) (string, error) {
	if base == "" {
		base = BaseBranch(ctx, root)
	}
	branch := strings.TrimPrefix(base, "origin/")
	if branch == "" {
		return "", errors.New("no base branch to fetch")
	}
	if _, err := runEnv(ctx, root, noPrompt, "fetch", "--no-tags", "origin", branch); err != nil {
		return "", fmt.Errorf("fetch origin/%s: %w", branch, err)
	}
	return "origin/" + branch, nil
}

// AddWorktreeFrom checks the repository out at path on a new branch off start:
// the base branch, for work that is unrelated to whatever the checkout it was
// started from happens to be on. An empty start is HEAD.
func AddWorktreeFrom(ctx context.Context, root, path, branch, start string) error {
	if start == "" {
		return AddWorktree(ctx, root, path, branch)
	}
	if path == "" || branch == "" {
		return fmt.Errorf("a worktree needs a path and a branch")
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	// --no-track, or git records the start point as the branch's upstream: a
	// branch off origin/main would track main, and push.default refuses that.
	if _, err := run(ctx, root, "worktree", "add", "--no-track", path, "-b", branch, start); err != nil {
		return fmt.Errorf("git worktree add failed: %w", err)
	}
	return nil
}

// ReviewWorktreePath is where a pull request review checkout belongs: under the
// repository's own git directory, which is ade's scratch space rather than
// somewhere you work. It resolves through CommonGitDir because in a linked
// worktree .git is a file, not a directory.
func ReviewWorktreePath(ctx context.Context, dir, slug string) (string, error) {
	common, err := CommonGitDir(ctx, dir)
	if err != nil {
		return "", err
	}
	if slug == "" {
		return "", errors.New("a review worktree needs a name")
	}
	return filepath.Join(statedir.Git(common), "worktrees", slug), nil
}

// AddPRWorktree checks a pull request's head out at path, detached. AddWorktree
// does not fit: it passes -b to branch off HEAD, and a review checks out a ref
// that already exists upstream. The pull ref is served by the base repository,
// so this works for a fork too.
func AddPRWorktree(ctx context.Context, root, path string, number int) error {
	if path == "" || number <= 0 {
		return fmt.Errorf("a review worktree needs a path and a pull request number")
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}

	ref := fmt.Sprintf("refs/ade/pr-%d", number)
	spec := fmt.Sprintf("+refs/pull/%d/head:%s", number, ref)
	if _, err := run(ctx, root, "fetch", "--no-tags", "origin", spec); err != nil {
		return fmt.Errorf("fetch pull request %d: %w", number, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create worktree parent: %w", err)
	}
	if _, err := run(ctx, root, "worktree", "add", "--detach", path, ref); err != nil {
		return fmt.Errorf("git worktree add failed: %w", err)
	}
	return nil
}

// IsWorktree reports whether dir is a linked worktree rather than the checkout
// that owns the repository.
func IsWorktree(ctx context.Context, dir string) bool {
	gitDir, err := GitDir(ctx, dir)
	if err != nil {
		return false
	}
	common, err := CommonGitDir(ctx, dir)
	if err != nil {
		return false
	}
	return gitDir != common
}

// RemoveWorktree detaches a linked checkout. It does not force, so git refuses
// while anything is uncommitted; the caller checks that first to give a better
// reason than git does.
func RemoveWorktree(ctx context.Context, root, path string) error {
	if _, err := run(ctx, root, "worktree", "remove", path); err != nil {
		return fmt.Errorf("git worktree remove failed: %w", err)
	}
	return nil
}

// RemoveWorktreeForce detaches a linked checkout whatever is in it. What was
// uncommitted there is gone; the caller has said so.
func RemoveWorktreeForce(ctx context.Context, root, path string) error {
	if _, err := run(ctx, root, "worktree", "remove", "--force", path); err != nil {
		return fmt.Errorf("git worktree remove failed: %w", err)
	}
	return nil
}

// OnlyOn counts the commits reachable from branch and from no other local or
// remote branch: the work that exists nowhere else, which is what removing
// the branch's checkout must not lose. Zero means every commit is held
// somewhere else — merged, pushed, or never made.
func OnlyOn(ctx context.Context, dir, branch string) (int, error) {
	if branch == "" {
		return 0, fmt.Errorf("no branch named")
	}
	refs, err := run(ctx, dir, "for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes")
	if err != nil {
		return 0, fmt.Errorf("list branches: %w", err)
	}
	args := []string{"rev-list", "--count", "refs/heads/" + branch}
	for _, ref := range strings.Split(strings.TrimSpace(refs), "\n") {
		ref = strings.TrimSpace(ref)
		if ref == "" || ref == "refs/heads/"+branch || strings.HasSuffix(ref, "/HEAD") {
			continue
		}
		args = append(args, "^"+ref)
	}
	out, err := run(ctx, dir, args...)
	if err != nil {
		return 0, fmt.Errorf("count commits only on %s: %w", branch, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("count commits only on %s: %w", branch, err)
	}
	return n, nil
}

// PruneWorktrees clears the admin records git keeps for worktree directories
// that were deleted by hand.
func PruneWorktrees(ctx context.Context, root string) error {
	if _, err := run(ctx, root, "worktree", "prune"); err != nil {
		return fmt.Errorf("git worktree prune failed: %w", err)
	}
	return nil
}

// CommitsAhead counts commits on HEAD that ref does not have. A review checkout
// is detached, so it has no upstream to compare against: the ref it was fetched
// from is what stands in for one.
func CommitsAhead(ctx context.Context, dir, ref string) (int, error) {
	out, err := run(ctx, dir, "rev-list", "--count", ref+"..HEAD")
	if err != nil {
		return 0, fmt.Errorf("count commits ahead of %s: %w", ref, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("count commits ahead of %s: %w", ref, err)
	}
	return n, nil
}

// Worktrees lists every checkout of this repository, the main one first.
func Worktrees(ctx context.Context, dir string) ([]string, error) {
	out, err := run(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("list worktrees: %w", err)
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if after, ok := strings.CutPrefix(line, "worktree "); ok {
			paths = append(paths, strings.TrimSpace(after))
		}
	}
	return paths, nil
}

// Push publishes the current branch, setting the upstream on the first push, and
// returns the upstream it ended up tracking.
func Push(ctx context.Context, dir string) (string, error) {
	return push(ctx, dir, false)
}

// PushForce replaces what is on the remote, which is what an amended commit
// needs. It uses --force-with-lease, so a push that would discard somebody
// else's work fails instead of doing it.
func PushForce(ctx context.Context, dir string) (string, error) {
	return push(ctx, dir, true)
}

func push(ctx context.Context, dir string, force bool) (string, error) {
	args := []string{"push"}
	if force {
		args = append(args, "--force-with-lease")
	}
	if !tracksItsOwnName(ctx, dir) {
		args = append(args, "--set-upstream", "origin", "HEAD")
	}
	if _, err := run(ctx, dir, args...); err != nil {
		return "", fmt.Errorf("git push failed: %w", err)
	}
	name, _ := upstream(ctx, dir)
	return name, nil
}

// HeadMessage is the last commit's message, for amending it with an edit.
func HeadMessage(ctx context.Context, dir string) (string, error) {
	out, err := run(ctx, dir, "log", "-1", "--pretty=%B")
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}

// HasCommits reports whether there is a commit to amend. A branch with none is
// where amend has nothing to fold into.
func HasCommits(ctx context.Context, dir string) bool {
	_, err := run(ctx, dir, "rev-parse", "--verify", "HEAD")
	return err == nil
}

// upstream returns the tracked branch and how many commits are unpushed. Both
// are empty when the branch has never been pushed.
// tracksItsOwnName is whether a bare push would reach the branch of the same
// name. A session branch does not: see the design doc.
func tracksItsOwnName(ctx context.Context, dir string) bool {
	name, _ := upstream(ctx, dir)
	if name == "" {
		return false
	}
	branch := Branch(ctx, dir)
	if branch == "" || branch == "HEAD" {
		return false
	}
	_, tracked, ok := strings.Cut(name, "/")
	return ok && tracked == branch
}

func upstream(ctx context.Context, dir string) (string, int) {
	out, err := run(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return "", 0
	}
	name := strings.TrimSpace(out)
	if name == "" {
		return "", 0
	}
	count, err := run(ctx, dir, "rev-list", "--count", name+"..HEAD")
	if err != nil {
		return name, 0
	}
	ahead, err := strconv.Atoi(strings.TrimSpace(count))
	if err != nil {
		return name, 0
	}
	return name, ahead
}

type porcelainEntry struct {
	index    byte
	worktree byte
	path     string

	// from is where a rename or a copy came from, in its own NUL-terminated
	// field after the record.
	from string
}

// parsePorcelain splits NUL-separated status records.
func parsePorcelain(out string) []porcelainEntry {
	fields := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	var entries []porcelainEntry
	for i := 0; i < len(fields); i++ {
		record := fields[i]
		if len(record) < 4 {
			continue
		}
		entry := porcelainEntry{index: record[0], worktree: record[1], path: record[3:]}
		if entry.index == 'R' || entry.index == 'C' {
			if i+1 < len(fields) {
				entry.from = fields[i+1]
			}
			i++
		}
		entries = append(entries, entry)
	}
	return entries
}

// Commit commits the index with message and returns the new short SHA. It shells
// out to git so the user's own config applies, including commit signing.
func Commit(ctx context.Context, dir, message string) (string, error) {
	return commit(ctx, dir, message, false)
}

// Amend folds what is staged into the last commit. message replaces the old
// one; an empty message keeps it.
//
// This rewrites the last commit, so a branch already pushed needs PushForce
// afterwards. That is the caller's decision and the caller's warning to give.
func Amend(ctx context.Context, dir, message string) (string, error) {
	return commit(ctx, dir, message, true)
}

func commit(ctx context.Context, dir, message string, amend bool) (string, error) {
	args := []string{"commit", "--cleanup=whitespace"}
	if amend {
		args = append(args, "--amend")
	}
	if strings.TrimSpace(message) == "" {
		if !amend {
			return "", errors.New("a commit needs a message")
		}
		args = append(args, "--no-edit")
	} else {
		args = append(args, "-F", "-")
	}

	// --cleanup=whitespace keeps lines starting with # instead of treating them as
	// comments, so a message containing markdown survives intact.
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204
	cmd.Dir = dir
	if strings.TrimSpace(message) != "" {
		cmd.Stdin = strings.NewReader(message)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		what := "git commit"
		if amend {
			what = "git commit --amend"
		}
		return "", fmt.Errorf("%s failed: %s", what, detail)
	}

	out, err := run(ctx, dir, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// LastCommitBy is the newest commit whose author email is exactly email.
// GitHub maps a commit back to the account owning the address.
func LastCommitBy(ctx context.Context, dir, email string) (string, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", nil
	}
	// The author filter is a substring match, so the address is compared
	// exactly below: liam@ must not answer for xliam@.
	out, err := run(ctx, dir, "log", "--all", "--fixed-strings",
		"--author="+email, "--max-count=20", "--format=%ae %H")
	if err != nil {
		return "", fmt.Errorf("look for a commit by %s: %w", email, err)
	}
	for _, line := range strings.Split(out, "\n") {
		got, sha, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && strings.EqualFold(got, email) {
			return sha, nil
		}
	}
	return "", nil
}

// HeadSHA returns the commit currently checked out.
func HeadSHA(ctx context.Context, dir string) (string, error) {
	out, err := run(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve HEAD: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// Branch returns the current branch name, or "detached" when there isn't one.
func Branch(ctx context.Context, dir string) string {
	out, err := run(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "detached"
	}
	if name := strings.TrimSpace(out); name != "" && name != "HEAD" {
		return name
	}
	return "detached"
}

// Branches lists the repository's local branch names.
func Branches(ctx context.Context, dir string) ([]string, error) {
	out, err := run(ctx, dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// NewBranch starts name at HEAD and checks it out. Uncommitted work comes with
// it, which is what moving off a base branch before committing needs.
func NewBranch(ctx context.Context, dir, name string) error {
	if strings.HasPrefix(name, "-") || strings.ContainsAny(name, " \t\n") {
		return fmt.Errorf("%q is not a branch name", name)
	}
	// git's own answer rather than a regex of ours: refs have more rules than
	// they look like they do.
	if _, err := run(ctx, dir, "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("%q is not a branch name git will take", name)
	}
	if _, err := run(ctx, dir, "checkout", "-b", name); err != nil {
		return fmt.Errorf("git checkout -b %s failed: %w", name, err)
	}
	return nil
}

// resolveSpec turns Options into a git diff revision argument.
func resolveSpec(ctx context.Context, root string, opts Options) (spec, mode, base, note string, err error) {
	if opts.Rev != "" {
		return opts.Rev, ModeRev, opts.Rev, "", nil
	}

	base, err = mergeBase(ctx, root, opts.Base)
	if err != nil {
		if opts.Base != "" {
			return "", "", "", "", err
		}
		// No default branch shares history with HEAD, so review the branch itself
		// rather than refusing to open.
		base, note, err = branchOnlyBase(ctx, root, opts.Scope == ScopeCommitted)
		if err != nil {
			return "", "", "", "", err
		}
	}

	switch opts.Scope {
	case ScopeCommitted:
		return base + "..HEAD", ModeCommitted, base, note, nil
	case ScopeUncommitted:
		// Everything not yet committed, which is the question "what goes in the
		// next commit". The base is kept as reported so commit counts and the
		// header still describe the branch.
		return "HEAD", ModeUncommitted, base, note, nil
	default:
		return base, ModeWorktree, base, note, nil
	}
}

// branchOnlyBase picks something to diff against when there is no base branch.
// Committed mode shows every commit on the branch; otherwise it shows the
// uncommitted work against HEAD.
func branchOnlyBase(ctx context.Context, root string, committed bool) (base, note string, err error) {
	empty, emptyErr := emptyTree(ctx, root)

	if !committed {
		if head, headErr := run(ctx, root, "rev-parse", "HEAD"); headErr == nil {
			return strings.TrimSpace(head), "no base branch found, so this is uncommitted work against HEAD", nil
		}
		if emptyErr != nil {
			return "", "", errors.New("this branch has no commits and no base branch")
		}
		return empty, "this branch has no commits, so every file is shown as new", nil
	}

	if emptyErr != nil {
		return "", "", errors.New("could not find a base branch; pass --base")
	}
	return empty, "no base branch found, so this is every commit on the branch", nil
}

// emptyTree is the hash of git's empty tree, which differs between sha1 and
// sha256 repositories, so it is asked for rather than hardcoded.
func emptyTree(ctx context.Context, root string) (string, error) {
	out, err := run(ctx, root, "hash-object", "-t", "tree", "/dev/null")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// commitsAhead counts what HEAD has on top of the base. Zero means HEAD is the
// base branch, so there is nothing a pull request could propose.
func commitsAhead(ctx context.Context, root, base string) int {
	if base == "" {
		return 0
	}
	out, err := run(ctx, root, "rev-list", "--count", base+"..HEAD")
	if err != nil {
		return 0
	}
	count, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0
	}
	return count
}

// mergeBase finds the fork point against explicit, or the first of the usual
// default branches that shares history with HEAD.
func mergeBase(ctx context.Context, root, explicit string) (string, error) {
	if explicit != "" {
		out, err := run(ctx, root, "merge-base", explicit, "HEAD")
		if err != nil {
			return "", fmt.Errorf("no merge base between %s and HEAD", explicit)
		}
		return strings.TrimSpace(out), nil
	}
	for _, candidate := range baseCandidates {
		out, err := run(ctx, root, "merge-base", candidate, "HEAD")
		if err == nil {
			return strings.TrimSpace(out), nil
		}
	}
	return "", errors.New("could not find a base branch; pass --base")
}

func headLabel(ctx context.Context, root string, opts Options) string {
	if opts.Rev != "" {
		return opts.Rev
	}
	if opts.Scope == ScopeCommitted {
		if out, err := run(ctx, root, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
			return strings.TrimSpace(out)
		}
		return "HEAD"
	}
	return "working tree"
}

// Tracked lists every file git tracks under root, relative to it. This is the
// permitted set for reading source off disk: .gitignore is honoured by
// construction rather than reimplemented.
func Tracked(ctx context.Context, root string) ([]string, error) {
	out, err := run(ctx, root, "ls-files", "-z")
	if err != nil {
		return nil, fmt.Errorf("list tracked: %w", err)
	}
	var files []string
	for _, name := range strings.Split(strings.TrimRight(out, "\x00"), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	return files, nil
}

// Untracked lists every file git does not track and does not ignore under root,
// relative to it. These are the new files a branch adds before they are staged:
// the diff shows them, so anything reading source off disk has to see them too.
func Untracked(ctx context.Context, root string) ([]string, error) {
	out, err := run(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("list untracked: %w", err)
	}
	var files []string
	for _, name := range strings.Split(strings.TrimRight(out, "\x00"), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	return files, nil
}

// Readable is Tracked plus Untracked: every file under root that git will admit
// to, ignored ones excluded. This is the permitted set for reading source off
// disk, and it matches what the diff covers.
func Readable(ctx context.Context, root string) ([]string, error) {
	tracked, err := Tracked(ctx, root)
	if err != nil {
		return nil, err
	}
	untracked, err := Untracked(ctx, root)
	if err != nil {
		return nil, err
	}
	files := append(tracked, untracked...)
	sort.Strings(files)
	return files, nil
}

// untrackedFiles diffs each untracked file against nothing so new files show up
// without touching the index.
func untrackedFiles(ctx context.Context, root string, contextLines int) ([]File, error) {
	names, err := Untracked(ctx, root)
	if err != nil {
		return nil, err
	}

	var files []File
	for i, name := range names {
		if i >= maxUntracked {
			break
		}
		parsed, err := runDiff(ctx, root, "diff", "--no-color", "--no-ext-diff", "--no-index",
			fmt.Sprintf("-U%d", contextLines), "/dev/null", name)
		if err != nil {
			continue
		}
		for _, f := range Parse(parsed) {
			// --no-index reports the literal /dev/null path, so restate the file.
			f.Path = filepath.ToSlash(name)
			f.OldPath = ""
			f.Status = StatusAdded
			f.Untracked = true
			files = append(files, f)
		}
	}
	return files, nil
}

func run(ctx context.Context, dir string, args ...string) (string, error) {
	return runEnv(ctx, dir, nil, args...)
}

// runStdin is run with input piped in, for the plumbing that reads a patch.
func runStdin(ctx context.Context, dir, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- args are internally constructed
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// noPrompt makes git fail rather than ask: nothing ade runs has a terminal to
// answer a credential prompt on, so one would hang until the caller's deadline.
var noPrompt = []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS="}

func runEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- args are internally constructed
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	started := time.Now()
	err := cmd.Run()
	took := time.Since(started)

	// The poll loop runs git every 1.5s, so a line per call would be the whole
	// ring. Only what a reader would have wanted: the failures and the slow.
	switch {
	case err != nil:
		slog.Debug("git failed", "args", strings.Join(args, " "), "dir", dir, "ms", took.Milliseconds(), "err", err, "stderr", strings.TrimSpace(stderr.String()))
	case took >= slowGit:
		slog.Debug("git was slow", "args", strings.Join(args, " "), "dir", dir, "ms", took.Milliseconds())
	}

	// stdout is returned even on failure so runDiff can use it after exit 1.
	if err != nil {
		return stdout.String(), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// runDiff runs git diff, tolerating the exit status 1 that --no-index uses to
// mean "files differ".
func runDiff(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := run(ctx, dir, args...)
	if err == nil {
		return out, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return out, nil
	}
	return "", err
}

// Origin is the owner and repository name from the origin remote, so the page
// can say which project you are in rather than only which directory.
//
// Read from git rather than asked of the forge: it is local, it is instant, and
// a checkout with no network still knows what it is.
func Origin(ctx context.Context, dir string) (string, string) {
	out, err := run(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return "", ""
	}
	return parseOrigin(strings.TrimSpace(out))
}

// parseOrigin handles the forms a remote is written in: scp-style ssh, a URL
// with a scheme, and either with or without the .git suffix.
func parseOrigin(remote string) (string, string) {
	if remote == "" {
		return "", ""
	}
	// git@host:owner/repo.git — the colon is a separator, not a port.
	if at := strings.Index(remote, "@"); at >= 0 && !strings.Contains(remote, "://") {
		if colon := strings.Index(remote[at:], ":"); colon >= 0 {
			remote = remote[at+colon+1:]
		}
	} else if scheme := strings.Index(remote, "://"); scheme >= 0 {
		rest := remote[scheme+3:]
		if slash := strings.Index(rest, "/"); slash >= 0 {
			remote = rest[slash+1:]
		} else {
			return "", ""
		}
	}

	remote = strings.TrimSuffix(strings.Trim(remote, "/"), ".git")
	parts := strings.Split(remote, "/")
	if len(parts) < 2 {
		return "", ""
	}
	// The last two: a self-hosted forge can nest an owner under a group.
	owner, repo := parts[len(parts)-2], parts[len(parts)-1]
	if owner == "" || repo == "" {
		return "", ""
	}
	return owner, repo
}

// ParseRemote is parseOrigin for a caller that holds the URL already.
func ParseRemote(remote string) (string, string) { return parseOrigin(remote) }

// SameBase reports whether two base refs name the same commit. Empty is the
// repository's default base, which is what an unset base resolves to.
func SameBase(ctx context.Context, root, a, b string) bool {
	left := resolveBaseRev(ctx, root, a)
	return left != "" && left == resolveBaseRev(ctx, root, b)
}

func resolveBaseRev(ctx context.Context, root, ref string) string {
	if ref == "" {
		if ref = BaseBranch(ctx, root); ref == "" {
			return ""
		}
	}
	out, err := run(ctx, root, "rev-parse", "--verify", "--quiet", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// CommitsBehind counts commits ref has that HEAD does not: how far a checkout
// has fallen behind the branch it was cut from.
func CommitsBehind(ctx context.Context, dir, ref string) (int, error) {
	out, err := run(ctx, dir, "rev-list", "--count", "HEAD.."+ref)
	if err != nil {
		return 0, fmt.Errorf("count commits behind %s: %w", ref, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("count commits behind %s: %w", ref, err)
	}
	return n, nil
}

// RefSHA resolves a ref to a commit, or empty when the repository has no such
// ref — a base branch that has never been fetched, most often.
func RefSHA(ctx context.Context, dir, ref string) string {
	out, err := run(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// FastForward moves the checkout's branch up to ref. It refuses rather than
// merging, so a checkout with local commits is left for the caller to report.
func FastForward(ctx context.Context, dir, ref string) error {
	if _, err := run(ctx, dir, "merge", "--ff-only", ref); err != nil {
		return fmt.Errorf("fast-forward onto %s: %w", ref, err)
	}
	return nil
}

// Rebase replays the checkout's commits on top of ref. A conflict leaves the
// rebase in progress: a caller that will not resolve it must abort.
func Rebase(ctx context.Context, dir, ref string) error {
	if _, err := run(ctx, dir, "rebase", ref); err != nil {
		return fmt.Errorf("rebase onto %s: %w", ref, err)
	}
	return nil
}

// RebaseAbort puts the checkout back where the rebase found it.
func RebaseAbort(ctx context.Context, dir string) error {
	if _, err := run(ctx, dir, "rebase", "--abort"); err != nil {
		return fmt.Errorf("git rebase --abort failed: %w", err)
	}
	return nil
}

// CountRange counts the commits new has that old does not.
func CountRange(ctx context.Context, dir, old, new string) (int, error) {
	out, err := run(ctx, dir, "rev-list", "--count", old+".."+new)
	if err != nil {
		return 0, fmt.Errorf("count %s..%s: %w", old, new, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("count %s..%s: %w", old, new, err)
	}
	return n, nil
}

// FreshBase is the ref a new worktree branches from, resolved against the
// main checkout: the fetched base, the local one, or HEAD. Note says which.
func FreshBase(ctx context.Context, root string) (ref, note string) {
	main := root
	if common, err := CommonGitDir(ctx, root); err == nil {
		main = filepath.Dir(common)
	}
	base := BaseBranch(ctx, main)
	if base == "" {
		return "", "no main or master branch here, so the worktree starts at HEAD"
	}
	fetched, err := FetchBase(ctx, main, base)
	if err != nil {
		return base, fmt.Sprintf("could not fetch %s (%v), so the worktree starts at the local %s", base, err, base)
	}
	return fetched, ""
}

// Stash puts tracked changes aside and says whether there were any. Untracked
// files are left: -u would sweep up build output nobody meant to keep.
func Stash(ctx context.Context, dir, label string) (bool, error) {
	out, err := run(ctx, dir, "stash", "push", "--quiet", "-m", label)
	if err != nil {
		return false, fmt.Errorf("stash: %w", err)
	}
	return !strings.Contains(out, "No local changes"), nil
}

// StashPop brings them back, and says what conflicted. A pop that conflicts
// leaves the entry in the stash, so nothing is lost by stopping there.
func StashPop(ctx context.Context, dir string) ([]string, error) {
	if _, err := run(ctx, dir, "stash", "pop"); err == nil {
		return nil, nil
	}
	stuck, err := ConflictedFiles(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("pop the stash: %w", err)
	}
	if len(stuck) == 0 {
		return nil, errors.New("the stash would not come back")
	}
	return stuck, nil
}

// StashDrop removes the entry a conflicted pop left behind, once its changes
// are in the tree.
func StashDrop(ctx context.Context, dir string) error {
	if _, err := run(ctx, dir, "stash", "drop", "--quiet"); err != nil {
		return fmt.Errorf("drop the stash: %w", err)
	}
	return nil
}

// StartRebase begins a rebase and says what is left conflicted. No conflicts
// and no error means it landed; the shape StartMerge has.
func StartRebase(ctx context.Context, dir, ref string) ([]string, error) {
	if _, err := run(ctx, dir, "rebase", ref); err == nil {
		return nil, nil
	}
	stuck, err := ConflictedFiles(ctx, dir)
	if err != nil || len(stuck) == 0 {
		_ = RebaseAbort(ctx, dir)
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("rebase onto %s failed", ref)
	}
	return stuck, nil
}

// RebaseContinue carries on after the conflicts of one commit are staged, and
// says what the next commit left conflicted. Empty means the rebase finished.
func RebaseContinue(ctx context.Context, dir string) ([]string, error) {
	env := append(os.Environ(), "GIT_EDITOR=true")
	if _, err := runEnv(ctx, dir, env, "rebase", "--continue"); err == nil {
		return nil, nil
	}
	stuck, err := ConflictedFiles(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("continue the rebase: %w", err)
	}
	if len(stuck) == 0 {
		return nil, errors.New("the rebase stopped for something other than a conflict")
	}
	return stuck, nil
}

// StageAll marks the named files resolved.
func StageAll(ctx context.Context, dir string, files []string) error {
	if len(files) == 0 {
		return nil
	}
	if _, err := run(ctx, dir, append([]string{"add", "--"}, files...)...); err != nil {
		return fmt.Errorf("stage the resolved files: %w", err)
	}
	return nil
}
