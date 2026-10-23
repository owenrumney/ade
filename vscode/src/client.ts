import type { DriftState } from "./driftrow";
import type { ProjectView } from "./projectrow";
import type { Body as SandboxBody, SandboxFile } from "./sandboxfile";
import type { Repo } from "./launcher";
import type { Server } from "./discover";
import type { Quiz } from "./quizpanel/view";
import type { Asked } from "./busreview";

// Thread mirrors comments.Thread. Only the fields the editor renders are named;
// the rest of the JSON is carried without being described twice.
export interface Thread {
  id: string;
  scope?: string;
  file: string;
  side: string;
  line: number;
  comments: Comment[];
  resolved?: boolean;
  orphaned?: boolean;
  category?: string;
  severity?: string;

  // What a rebuttal round made of this finding: another reviewer objected,
  // more objected than defended it, or it repeats the thread named here.
  disputed?: boolean;
  withdrawn?: boolean;
  duplicateOf?: string;

  // What it would say to the author once rewritten, and when it went. Both
  // decide which of Convert and Send a comment is offered.
  outgoing?: string;
  sentAt?: string;
  externalId?: string;
}

export interface Comment {
  id: string;
  author: string;
  body: string;
  at: string;
  outcome?: string;

  // Set on a reply imported from GitHub, and on one of yours once it has gone.
  externalId?: string;
  sentAt?: string;
}

// Diff is the change under review: which two ends, and the files between.
// Hunks are not named — the editor draws its own from the two sides.
export interface Diff {
  base: string;
  head: string;
  mode: string;
  note?: string;
  commits: number;
  files: DiffFile[] | null;
}

export interface DiffFile {
  path: string;
  oldPath?: string;
  status: string;
  binary: boolean;
  untracked?: boolean;
  additions: number;
  deletions: number;
}

// How long a build like this one took before, and what share of the plan
// window it is expected to want.
export interface Estimate {
  lowSeconds: number;
  highSeconds: number;
  runs: number;
  kind: string;
  group: string;
  window?: Window | null;
}

export interface Window {
  name: string;
  used: number;
  about: number;
  high: number;
  cap?: boolean;
  resetsAt?: string;
}

// Change is one path git reports as not yet in the index.
export interface Change {
  path: string;
  status: string;
}

// Worktree is what a commit would include and what it would leave behind.
// How a commit here would be signed, read from git's own config.
export interface Signing {
  on: boolean;
  format?: string;
  key?: string;
}

export interface Count {
  added: number;
  removed: number;
}

export interface Past {
  sha: string;
  subject: string;
  author: string;
  at: string;
  // The name the file had at that commit, which a rename makes differ from
  // the one asked about. Reading the blob there needs this one.
  path: string;
}

export interface Blame {
  sha: string;
  short: string;
  summary: string;
  author: string;
  at: string;
  // false for a line that is still in the worktree or the index.
  committed: boolean;
}

export interface Worktree {
  staged: string[] | null;
  unstaged: Change[] | null;
  lines?: Record<string, Count> | null;
  signing?: Signing;
  upstream?: string;
  ahead: number;
  // behind is how many commits the base has that this branch does not, as of
  // the last fetch. A rebase is what clears it.
  behind?: number;
}

// CommitDraft is the message the agent wrote for what is staged.
export interface CommitDraft {
  title: string;
  body: string;
  staged: string[] | null;
}

// One CI run. Bucket is ade's word for where it stands: fail, pending,
// cancel, skipping, pass.
export interface Check {
  name: string;
  workflow?: string;
  state: string;
  bucket: string;
  description?: string;
  link?: string;
}

// The pull request for the branch this session is on, with its checks.
export interface BranchPR {
  number: number;
  title: string;
  url: string;
  state: string;
  draft?: boolean;
  author?: string;
  baseRef?: string;
  headRef?: string;
  body?: string;
  reviews?: { author: string; state: string }[] | null;
  checks?: Check[] | null;
  failing?: number;
  mergeBlocked?: string;
  approveBlocked?: string;
  requestChangesBlocked?: string;
  reviewDecision?: string;
}

// RunState is what the agent is doing. Only what a status bar shows is named.
export interface RunState {
  running: boolean;
  // The conversation this window is in, and what it is called.
  session?: string;
  title?: string;
  kind?: string;
  error?: string;
  model?: string;
  provider?: string;
  startedAt?: string;
  lastEventAt?: string;
  asking?: unknown[];
  queued?: number;
  waiting?: Waiting[] | null;
  sandbox?: boolean;
  sideRuns?: SideRun[] | null;
  free?: boolean;
  welded?: string;
  doing?: string;
  touched?: string[];
  repeats?: number;
  repeatedAt?: string;
  children?: Child[] | null;
  finished?: Finished | null;
}

// Finished is how the last turn ended, until the next one starts.
export interface Finished {
  files?: number;
  asking?: boolean;
  at: string;
}

// Child is a sub-agent or a backgrounded command at work.
export interface Child {
  kind: "agent" | "task" | string;
  name: string;
  doing?: string;
  steps?: number;
  since: string;
}

// SideRun is one named agent at work beside the main one.
export interface SideRun {
  id: string;
  name: string;
  startedAt?: string;
}

// Logs is the server's own log from a sequence number on.
export interface Logs {
  records: LogRecord[] | null;
  latest: number;
}

export interface LogRecord {
  at: string;
  level: string;
  msg: string;
  attrs?: { key: string; value: string }[];
  seq: number;
}

// Waiting is one queued turn, in the order it will run.
export interface Waiting {
  id: string;
  kind: string;
  what?: string;
  // The whole prompt, where the entry is one. what is truncated for the row.
  message?: string;
}

// Meta is what the session is looking at: which branch, at which scope, and
// how many files that comes to.
export interface Meta {
  repo?: string;
  project?: string;
  branch?: string;
  base?: string;
  mode?: string;
  scope?: string;
  files?: number;
  roots?: DiffRoot[];
  view?: string;
}

export interface DiffRoot {
  root: string;
  name: string;
  branch: string;
  home?: boolean;
  changes: number;
  threads: number;
}

// Issue is a Linear ticket assigned to you.
export interface Issue {
  identifier: string;
  title: string;
  url: string;
  state: string;
  started?: boolean;
  priority?: number;
  branchName?: string;
  updatedAt?: string;
}

export interface Tickets {
  issues: Issue[] | null;
  error?: string;
  checkedAt?: string;
}

// Listed is one open pull request on this repository.
export interface Listed {
  number: number;
  title: string;
  author: string;
  headRef: string;
  draft?: boolean;
  mine?: boolean;
  url: string;
  reviewRequested?: boolean;
  updatedAt?: string;
  reviewDecision?: string;
  checks?: string;
}

// BusOffer is one request waiting on you over agentbus: a question from
// somebody's agent, or a pull request they want reviewed. Kind says which.
export interface BusOffer {
  id: string;
  kind: string;
  from: string;
  node?: string;
  harness?: string;
  agent?: string;
  model?: string;
  question?: string;
  options?: string[] | null;
  answerBy?: string;
  pr?: string;
  repo?: string;
  number?: number;
  note?: string;
  createdAt?: string;
  claimed?: boolean;
  // to is who the sender addressed it to: you, a group, "anyone" or "*".
  to?: string;
}

// BusOnline is one row of who is on the bus.
export interface BusOnline {
  user: string;
  node: string;
  instance: { id: string; harness?: string; label?: string };
  accepts?: { ask?: boolean; review?: { repos?: string[] | null } | null };
  presence: string;
  status?: string;
  connected: boolean;
}

export interface BusWho {
  online: boolean;
  instances?: BusOnline[] | null;
}

// BusMember is somebody else's harness, as the bus last described it.
export interface BusMember {
  user: string;
  node?: string;
  instance: string;
  harness?: string;
  label?: string;
  presence: string;
  status?: string;
  agents?: number;
  takesAsk?: boolean;
  takesPr?: boolean;
  you?: boolean;
}

// BusReply is an answer to something asked from here, pushed by the server
// rather than waited for. Kind is "answer" or "review".
export interface BusReply {
  id: string;
  kind: string;
  from: string;
  question?: string;
  pr?: string;
  text?: string;
  option?: string;
  verdict?: string;
  byAgent?: boolean;
  at?: string;
  dir?: string;
  forAgent?: boolean;
}

// BusSent is a request put to somebody from here that has no answer yet.
export interface BusSent {
  id: string;
  kind: string;
  to?: string;
  question?: string;
  pr?: string;
  at?: string;
  forAgent?: boolean;
}

export interface BusState {
  online: boolean;
  user?: string;
  instance?: string;
  offers?: BusOffer[] | null;
  replies?: BusReply[] | null;
  sent?: BusSent[] | null;
  team?: BusMember[] | null;
}

export interface State {
  diffVersion: string;
  threads: Thread[] | null;
  worktree?: Worktree | null;
  branchPr?: BranchPR | null;
  agent?: RunState | null;
  meta?: Meta;
  linear?: Tickets | null;
  draft?: CommitDraft | null;
  quiz?: Quiz | null;
  design?: DesignState | null;
  bus?: BusState | null;
  verify?: VerifyState | null;
}

// VerifyCheck is one of the repository's own commands, run after a turn.
// Not Check: that is a CI check on a pull request, and these never meet.
export interface VerifyCheck {
  name: string;
  run: string;
  dir?: string;
  narrow?: string;
  // filter is the flag form this check takes a test-name pattern as, and
  // tool offers it to the agent as a tool of its own.
  filter?: string;
  tool?: boolean;
  about?: string;
}

// VerifyState is the last check run, or a detected list waiting on a yes.
export interface VerifyState {
  running?: boolean;
  passed?: boolean;
  stopped?: boolean;
  failed?: string;
  ran: number;
  total: number;
  at?: string;
  narrow?: boolean;
  offer?: VerifyCheck[] | null;
}

// DesignState is where the server put the design documents. A worktree has none
// of its own: they live under the git dir the whole repository shares.
export interface DesignState {
  dir: string;
  file?: string;
  // Absent when a build has nowhere new to go: no document open, or this
  // checkout is already the branch the document names.
  worktree?: { path: string; branch: string } | null;
}

// Tree is one of the repository's linked worktrees and what stands in the way
// of removing it. Scan does not fetch, so `behind` is as of the last one.
export interface Tree {
  path: string;
  name: string;
  branch?: string;
  kind: string;
  serving?: string;
  keep?: string;
  removable: boolean;
  uncommitted?: number;
  unpushed?: number;
  merged?: boolean;
  behind?: number;
  main?: boolean;
  // RFC 3339, from git's own reflog: a list is not a network call.
  lastUsed?: string;
}

// Rev mirrors gitdiff.Rev: one commit of a range. patchId keys it by its diff,
// so a cherry-pick of it elsewhere carries the same value.
export interface Rev {
  sha: string;
  subject: string;
  author: string;
  at: string;
  patchId?: string;
}

// OverlapNode mirrors overlap.Node, which embeds the scan row: the commits a
// worktree holds that the base does not, and whether that is nothing at all.
export interface OverlapNode extends Tree {
  commits: Rev[] | null;
  redundant: boolean;
  // landed means merging it into the base would change nothing, though its
  // commits are not on the base: a squash or a rebase on the forge.
  landed?: boolean;
  // why is the read that failed: commits unknown rather than none.
  why?: string;
}

// OverlapEdge mirrors overlap.Edge. The kinds are "absorbed" (directed: from's
// patches are all on to), "shares" (the same SHAs) and "duplicates".
export interface OverlapEdge {
  from: string;
  to: string;
  kind: string;
  count?: number;
}

// Overlap mirrors the overlap endpoint. base is a resolved commit and baseRef
// what named it; removable is the commit-only verdict, not the scan's.
export interface Overlap {
  base: string;
  baseRef?: string;
  baseBranch?: string;
  nodes: OverlapNode[] | null;
  edges: OverlapEdge[] | null;
  removable: string[] | null;
}

// Marks is the lines of each file that have lineage, keyed by repo-relative path.
export type Marks = { [file: string]: number[] };

// Agent is one named agent the checkout or your home defines.
export interface Agent {
  name: string;
  description?: string;
  reviews: boolean;
  scope: string;
}

export interface Turn {
  session: string;
  messageId: string;
  at: string;
  summary: string;
  commit?: string;
}

export class Client {
  constructor(private server: Server) {}

  get root(): string {
    return this.server.root;
  }

  // events is the server's SSE stream. It carries no payload worth reading: an
  // event name means refetch, which is how the browser treats it too.
  eventsURL(): string {
    return this.url("/api/events");
  }

  state(): Promise<State> {
    return this.get<State>("/api/state");
  }

  async marks(): Promise<Marks> {
    const res = await this.get<{ files: Marks | null }>("/api/lineage/marks");
    return res.files ?? {};
  }

  async lineage(file: string, line: number): Promise<Turn[]> {
    const res = await this.get<{ turns: Turn[] | null }>(
      `/api/lineage?file=${encodeURIComponent(file)}&line=${line}`,
    );
    return res.turns ?? [];
  }

  attach(from: string, text: string): Promise<unknown> {
    return this.post("/api/attach", { from, text });
  }

  // say is a turn typed outside the chat. queue, so a busy agent takes it
  // next rather than refusing it.
  say(message: string): Promise<unknown> {
    return this.post("/api/chat", { message, files: [], queue: true });
  }

  // answer is a question answered by a fork of the conversation, read-only,
  // with the reply handed back rather than posted to the chat.
  answer(question: string): Promise<{ answer: string }> {
    return this.post<{ answer: string }>("/api/answer", { question });
  }

  reply(id: string, body: string): Promise<Thread> {
    return this.post<Thread>(`/api/threads/${encodeURIComponent(id)}/reply`, {
      body,
    });
  }

  resolve(id: string, resolved: boolean): Promise<Thread> {
    return this.post<Thread>(`/api/threads/${encodeURIComponent(id)}/resolve`, {
      resolved,
    });
  }

  // The rewrite runs beside the main agent and answers before the words exist:
  // the text arrives on a later /api/state as the thread's outgoing.
  convert(id: string): Promise<unknown> {
    return this.post(`/api/threads/${encodeURIComponent(id)}/convert`, {});
  }

  // confirm names the pull request: the server refuses a send that does not.
  sendToGitHub(id: string, confirm: number): Promise<unknown> {
    return this.post("/api/github/send", { confirm, thread: id });
  }

  busAnswer(id: string, option: string, text: string): Promise<unknown> {
    return this.post("/api/bus/answer", { id, option, text });
  }

  busClaim(id: string, open: boolean): Promise<{ url: string; path: string }> {
    return this.post<{ url: string; path: string }>("/api/bus/claim", {
      id,
      open,
    });
  }

  busResult(id: string, state: string, summary: string): Promise<unknown> {
    return this.post("/api/bus/result", { id, state, summary });
  }

  busHere(focused: boolean): Promise<unknown> {
    return this.post("/api/bus/here", { focused });
  }

  busWho(): Promise<BusWho> {
    return this.get<BusWho>("/api/bus/who");
  }

  // The id comes straight back; the answer arrives later as a note in the
  // conversation, so nothing here waits for it.
  busAsk(question: string, to: string): Promise<{ id: string; to: string }> {
    return this.post<{ id: string; to: string }>("/api/bus/ask", {
      question,
      to,
    });
  }

  // Empty follows your window again, which is the only way back to automatic.
  busPresence(presence: string): Promise<{ presence: string }> {
    return this.post<{ presence: string }>("/api/bus/presence", { presence });
  }

  // Clearing an answer is what stops it being offered again: the launcher
  // holds it until somebody says they have read it.
  busDismiss(id: string): Promise<unknown> {
    return this.post("/api/bus/dismiss", { id });
  }

  busRequestReview(pr: string, to: string, note: string): Promise<Asked> {
    return this.post<Asked>("/api/bus/request-review", {
      pr,
      to,
      note,
    });
  }

  // force replaces what is on the remote, which an amended commit needs.
  push(force = false): Promise<unknown> {
    return this.post("/api/push", { force });
  }

  deleteThread(id: string): Promise<unknown> {
    return this.del(`/api/threads/${encodeURIComponent(id)}`);
  }

  // Starts the branch at HEAD and moves onto it, uncommitted work and all,
  // leaving the branch behind to be merged onto.
  newBranch(name: string): Promise<unknown> {
    return this.post("/api/branch", { name });
  }

  stage(paths: string[]): Promise<unknown> {
    return this.post("/api/stage", { paths });
  }

  unstage(paths: string[]): Promise<unknown> {
    return this.post("/api/unstage", { paths });
  }

  commit(title: string, body: string, amend: boolean): Promise<unknown> {
    return this.post("/api/commit", { title, body, amend });
  }

  // The agent reads the staged diff and writes a message. It answers before
  // the message exists: the draft arrives on a later /api/state. A busy agent
  // queues it rather than refusing, and says how many are waiting.
  writeCommitMessage(): Promise<{ queued?: number }> {
    return this.post<{ queued?: number }>("/api/commit-message", {});
  }

  // A quiz already written for this diff comes straight back; otherwise this
  // answers before it exists and the questions arrive on a later /api/state.
  quiz(regenerate = false): Promise<{ quiz?: Quiz; queued?: number }> {
    return this.post<{ quiz?: Quiz; queued?: number }>("/api/quiz", {
      regenerate,
    });
  }

  // The verbs that are one POST and no form. Each is a palette entry and
  // nothing more, which is the cheapest parity in the extension.
  cancel(): Promise<unknown> {
    return this.post("/api/cancel", {});
  }

  // Hands the open comments to the agent to act on. An empty list is every
  // one of them, which is what the diff's own Resolve all sends.
  resolveAll(threadIds: string[] = []): Promise<unknown> {
    return this.post("/api/resolve", { threadIds });
  }

  // A cold reviewer files findings on the diff; a named agent answers in the
  // conversation. Neither means the cold one.
  review(level: string, agents: string[] = []): Promise<unknown> {
    return this.post("/api/review", { level, agents });
  }

  setScope(scope: string): Promise<unknown> {
    return this.post("/api/scope", { scope });
  }

  async diff(): Promise<Diff> {
    const res = await this.get<{ version: string; diff: Diff }>("/api/diff");
    return res.diff;
  }

  // One side of one file, as text. The editor diffs the two itself, which is
  // the one thing a framed view could never give it.
  async blob(path: string, side: "left" | "right"): Promise<string> {
    const at = `/api/blob?path=${encodeURIComponent(path)}&side=${side}`;
    const res = await fetch(this.url(at), {
      headers: { "X-Ade-Token": this.server.token },
    });
    if (!res.ok) throw new Error(`${at}: ${res.status} ${await res.text()}`);
    return res.text();
  }

  // The commit one line last came from. The path is relative to the checkout
  // and the line is 1-based, as the editor counts them for a person.
  blame(path: string, line: number): Promise<Blame> {
    return this.get<Blame>(
      `/api/blame?path=${encodeURIComponent(path)}&line=${line}`,
    );
  }

  // The commits that touched a file, newest first, through its renames.
  async history(path: string): Promise<Past[]> {
    const res = await this.get<{ commits: Past[] | null }>(
      `/api/history?path=${encodeURIComponent(path)}`,
    );
    return res.commits ?? [];
  }

  // A file as one commit left it. Text, not JSON: it opens in an editor.
  async fileAt(sha: string, path: string): Promise<string> {
    const at = `/api/file/at?sha=${encodeURIComponent(sha)}&path=${encodeURIComponent(path)}`;
    const res = await fetch(this.url(at), {
      headers: { "X-Ade-Token": this.server.token },
    });
    if (!res.ok) throw new Error(`${at}: ${res.status} ${await res.text()}`);
    return res.text();
  }

  async agents(): Promise<Agent[]> {
    const res = await this.get<{ agents: Agent[] | null }>("/api/agents");
    return res.agents ?? [];
  }

  runQueued(): Promise<unknown> {
    return this.post("/api/queue/run", {});
  }

  dropQueued(id: string): Promise<unknown> {
    return this.del(`/api/queue/${encodeURIComponent(id)}`);
  }

  // Rewrites a waiting prompt, keeping its place in the queue.
  editQueued(id: string, message: string): Promise<unknown> {
    return this.put(`/api/queue/${encodeURIComponent(id)}`, { message });
  }

  // Moves a waiting turn to the front. It does not interrupt the run in flight.
  promoteQueued(id: string): Promise<unknown> {
    return this.post(`/api/queue/${encodeURIComponent(id)}/urgent`, {});
  }

  refreshChecks(): Promise<unknown> {
    return this.post("/api/checks/refresh", {});
  }

  // Sends the failing checks to the agent to read and fix, or queues it behind
  // a run. Refused only when nothing is failing or the PR is not this one's.
  fixChecks(): Promise<{ queued?: number; running?: boolean }> {
    return this.post("/api/checks/fix", {});
  }

  async worktrees(force = false): Promise<Tree[]> {
    const res = await this.get<{ worktrees: Tree[] | null }>(
      `/api/worktrees?force=${force}`,
    );
    return res.worktrees ?? [];
  }

  // How the worktrees relate by the commits they hold. One read answers the
  // whole panel: each node carries its scan row as well as its commits.
  async overlap(): Promise<Overlap> {
    const res = await this.get<Overlap>("/api/worktrees/overlap");
    return {
      ...res,
      nodes: res.nodes ?? [],
      edges: res.edges ?? [],
      removable: res.removable ?? [],
    };
  }

  // The list the switch itself is gated on, so nothing offered is refused.
  async switchable(): Promise<{ worktrees: Tree[]; here: string }> {
    const res = await this.get<{ worktrees: Tree[] | null; here: string }>(
      "/api/worktrees/switch",
    );
    return { worktrees: res.worktrees ?? [], here: res.here };
  }

  switchTo(path: string): Promise<unknown> {
    return this.post("/api/worktrees/switch", { path });
  }

  // The agent writes the description from your commits, pushes the branch,
  // and opens the pull request as a draft. No body: there is nothing to ask.
  createPR(): Promise<unknown> {
    return this.post("/api/pr-create", {});
  }

  // Reading a document is also adopting it: the server has one open document
  // and this is how it is told which. Build acts on whatever that is.
  openDoc(file: string): Promise<{ file: string; title: string }> {
    return this.get(`/api/design?file=${encodeURIComponent(file)}`);
  }

  async estimate(
    provider: string,
    model: string,
  ): Promise<Estimate | undefined> {
    const at = `/api/design/estimate?provider=${encodeURIComponent(provider)}&model=${encodeURIComponent(model)}`;
    const res = await this.get<{ estimate?: Estimate | null }>(at);
    return res.estimate ?? undefined;
  }

  // Clears the conversation and starts the work from the open document. A
  // named machine builds there instead and leaves this session alone.
  build(req: {
    worktree: boolean;
    provider: string;
    model: string;
    machine: string;
  }): Promise<{ url?: string; machine?: string; path?: string }> {
    return this.post("/api/design/go", req);
  }

  // Stops the ade serving this checkout. It answers before it goes, which is
  // the only chance to hear anything from it.
  exit(session: string): Promise<unknown> {
    return this.post("/api/exit", { session });
  }

  // kept is the ones that would not go and why; the scan gates each again, so
  // naming a directory here cannot have it deleted.
  prune(
    paths: string[],
    force: boolean,
  ): Promise<{ removed: string[]; kept: { path: string; why: string }[] }> {
    return this.post("/api/worktrees/prune", { paths, force });
  }

  // The two blockers a panel can clear on your behalf. Uncommitted work is
  // not one of them: it exists nowhere else, so it is never cleared for you.
  pushWorktree(path: string): Promise<{ pushed: string }> {
    return this.post("/api/worktrees/push", { path });
  }

  closeWorktree(path: string): Promise<{ closed: string }> {
    return this.post("/api/worktrees/close", { path });
  }

  // Lands a branch on the one it forked from. A conflict a fast model settles
  // comes back named in resolved; a refusal means nothing was changed.
  mergeWorktree(
    path: string,
    into: string,
  ): Promise<{
    merged: string;
    into: string;
    resolved?: string[];
    by?: string;
  }> {
    return this.post("/api/worktrees/merge", { path, into });
  }

  // A verdict names the pull request it is about. The server refuses one that
  // does not: it is public, posted as you, and the open PR can change.
  approve(confirm: number, body: string): Promise<unknown> {
    return this.post("/api/pr/approve", { confirm, body });
  }

  requestChanges(confirm: number, body: string): Promise<unknown> {
    return this.post("/api/pr/request-changes", { confirm, body });
  }

  merge(method: string): Promise<unknown> {
    return this.post("/api/pr/merge", { method });
  }

  async prs(): Promise<Listed[]> {
    const res = await this.get<{ prs: Listed[] | null }>("/api/prs");
    return res.prs ?? [];
  }

  // One pull request's CI, read when its row is opened. The list does not
  // carry these: a gh call per row on every refresh.
  async checksFor(number: number): Promise<Check[]> {
    const res = await this.get<{ checks: Check[] | null }>(
      `/api/prs/${number}/checks`,
    );
    return res.checks ?? [];
  }

  openPR(number: number): Promise<unknown> {
    return this.post(`/api/prs/${number}`, {});
  }

  async issues(): Promise<Tickets> {
    const res = await this.get<State>("/api/state");
    return res.linear ?? { issues: [] };
  }

  refreshIssues(): Promise<unknown> {
    return this.post("/api/linear/refresh", {});
  }

  // Starts work on a ticket: a branch named the way Linear names it, and a
  // conversation that knows what it is for.
  startIssue(
    identifier: string,
    dir: string,
  ): Promise<{ url?: string; path?: string }> {
    return this.post("/api/linear/start", { identifier, dir });
  }

  // Starting a ticket is gated on this list, so nothing offered is refused.
  async repos(): Promise<Repo[]> {
    const res = await this.get<{ repos: Repo[] | null }>("/api/repos");
    return res.repos ?? [];
  }

  setModel(model: string): Promise<unknown> {
    return this.post("/api/chat/model", { model });
  }

  // Moving to a model the other agent runs is a provider switch: the session
  // is that agent's, so the conversation crosses as context instead.
  switchAgent(provider: string, model: string): Promise<unknown> {
    return this.post("/api/chat/switch", { provider, model });
  }

  // The server refuses an empty one: a session with nothing said is a
  // conversation nobody can tell from the one it replaced.
  newChat(message: string, provider: string, model = ""): Promise<unknown> {
    return this.post("/api/chat/new", { message, provider, model });
  }

  clearChat(): Promise<unknown> {
    return this.post("/api/chat/clear", {});
  }

  create(file: string, line: number, body: string): Promise<Thread> {
    return this.post<Thread>("/api/threads", {
      file,
      line,
      side: "right",
      body,
    });
  }

  // Flipping this kills the agent subprocess, so the server refuses it while
  // a turn is in flight rather than taking the turn down with it.
  sandbox(on: boolean): Promise<unknown> {
    return this.put("/api/sandbox/on", { on });
  }

  // pullBase fetches origin's base branch, so the diff is against main as it
  // is now rather than as it was at the last fetch.
  pullBase(): Promise<{ base: string }> {
    return this.post("/api/base/pull", {});
  }

  // rebaseBase fetches the base and replays this branch on top of it.
  rebaseBase(): Promise<{ base: string; behind: number; replayed: number }> {
    return this.post("/api/base/rebase", {});
  }

  draftReply(id: string): Promise<unknown> {
    return this.post(`/api/threads/${encodeURIComponent(id)}/draft-reply`, {});
  }

  draftReplies(): Promise<{ threads?: number }> {
    return this.post("/api/draft-replies", {});
  }

  sendReply(id: string): Promise<unknown> {
    return this.post(`/api/threads/${encodeURIComponent(id)}/send-reply`, {});
  }

  // confirm names the pull request and threads what was listed: the server
  // posts only those, so a draft that landed since is not sent unread.
  sendReplies(confirm: number, threads: string[]): Promise<unknown> {
    return this.post("/api/threads/send-replies", { confirm, threads });
  }

  stopSide(id: string): Promise<unknown> {
    return this.post(`/api/sides/${encodeURIComponent(id)}/cancel`, {});
  }

  explain(): Promise<unknown> {
    return this.post("/api/explain", {});
  }

  logs(since: number): Promise<Logs> {
    return this.get<Logs>(`/api/logs?since=${since}`);
  }

  // free turns the permission asking off for the rest of the session, or on.
  free(on: boolean): Promise<unknown> {
    return this.post("/api/chat/free", { on });
  }

  // The repository's own checks: what is configured, what ade would suggest,
  // and running them without waiting for a turn to end.
  checks(): Promise<{
    checks: VerifyCheck[];
    asked: boolean;
    detected: VerifyCheck[] | null;
  }> {
    return this.get("/api/verify");
  }

  saveChecks(checks: VerifyCheck[]): Promise<unknown> {
    return this.post("/api/verify", { checks });
  }

  runChecks(): Promise<unknown> {
    return this.post("/api/verify/run", {});
  }

  refreshTeam(): Promise<unknown> {
    return this.post("/api/bus/refresh", {});
  }

  resetThreads(): Promise<{ removed: number }> {
    return this.post("/api/threads/reset", {});
  }

  setOutgoing(id: string, text: string): Promise<Thread> {
    return this.put(`/api/threads/${encodeURIComponent(id)}/outgoing`, {
      text,
    });
  }

  deleteDoc(file: string): Promise<unknown> {
    return this.del(`/api/design/doc?file=${encodeURIComponent(file)}`);
  }

  // archiveDoc distils a finished design onto the wiki and deletes it here.
  // The page is pushed first, so a failure leaves the document alone.
  archiveDoc(file: string): Promise<{ page: string; commit?: string }> {
    return this.post(`/api/design/archive?file=${encodeURIComponent(file)}`, {});
  }

  // headMessage is the last commit's message, which an amend starts from.
  headMessage(): Promise<{ title: string; body: string }> {
    return this.get("/api/commit/head");
  }

  // checkoutPR turns the snapshot this session is reading into a worktree to
  // work in. The server refuses it on anything that is not a snapshot.
  checkoutPR(
    message: string,
  ): Promise<{ url: string; path: string; branch: string }> {
    return this.post("/api/pr/checkout", { message });
  }

  // editPR saves the title and description on the pull request under review.
  editPR(title: string, body: string): Promise<unknown> {
    return this.put("/api/pr", { title, body });
  }

  sandboxFile(): Promise<SandboxFile> {
    return this.get("/api/sandbox");
  }

  // saveSandboxFile writes the whole file: the server has no smaller write.
  saveSandboxFile(body: SandboxBody): Promise<SandboxFile> {
    return this.put("/api/sandbox", body);
  }

  project(): Promise<ProjectView> {
    return this.get("/api/project");
  }

  saveProject(change: {
    instructions?: string;
    remembered?: boolean;
  }): Promise<ProjectView> {
    return this.put("/api/project", change);
  }

  // projectEntry is accept, reject, edit or forget on one entry.
  projectEntry(
    action: string,
    id: string,
    heading = "",
    body = "",
  ): Promise<ProjectView> {
    return this.post("/api/project/entry", { action, id, heading, body });
  }

  distil(): Promise<{ read: number; proposed: number }> {
    return this.post("/api/project/distil", {});
  }

  drift(): Promise<DriftState> {
    return this.get("/api/drift");
  }

  // runDrift scores the diff against the open design document. It answers
  // before the pass has run: the verdicts arrive when the agent goes idle.
  runDrift(): Promise<unknown> {
    return this.post("/api/drift", {});
  }

  dismissDrift(id: string, dismissed: boolean): Promise<unknown> {
    return this.post("/api/drift/dismiss", { id, dismissed });
  }

  diffRoot(root: string): Promise<unknown> {
    return this.post("/api/diff/root", { root });
  }

  diffFolders(
    folders: string[],
  ): Promise<{ roots: DiffRoot[] | null; view: string }> {
    return this.put("/api/diff/folders", { folders });
  }

  seedDesign(): Promise<{ started: boolean; reason?: string }> {
    return this.post("/api/design/seed", {});
  }

  condenseDesign(): Promise<{ started: boolean; reason?: string }> {
    return this.post("/api/design/condense", {});
  }

  preview(): Promise<{ url: string; effective?: string; off?: boolean }> {
    return this.get("/api/preview");
  }

  savePreview(url: string): Promise<unknown> {
    return this.put("/api/preview", { url });
  }

  // port moves a scratch session's work into a repository, on a new worktree.
  port(target: string, branch: string): Promise<{ url: string; path: string }> {
    return this.post("/api/port", { target, branch, remove: false });
  }

  private url(path: string): string {
    const join = path.includes("?") ? "&" : "?";
    return `${this.server.base}${path}${join}t=${encodeURIComponent(this.server.token)}`;
  }

  private async get<T>(path: string): Promise<T> {
    const res = await fetch(this.url(path), {
      headers: { "X-Ade-Token": this.server.token },
    });
    return read<T>(path, res);
  }

  private async del<T>(path: string): Promise<T> {
    const res = await fetch(this.url(path), {
      method: "DELETE",
      headers: { "X-Ade-Token": this.server.token },
    });
    return read<T>(path, res);
  }

  private async put<T>(path: string, body: unknown): Promise<T> {
    const res = await fetch(this.url(path), {
      method: "PUT",
      headers: {
        "X-Ade-Token": this.server.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify(body),
    });
    return read<T>(path, res);
  }

  private async post<T>(path: string, body: unknown): Promise<T> {
    const res = await fetch(this.url(path), {
      method: "POST",
      headers: {
        "X-Ade-Token": this.server.token,
        "Content-Type": "application/json",
      },
      body: JSON.stringify(body),
    });
    return read<T>(path, res);
  }
}

// read turns one answer into a value. Half of ade's verbs answer 204 with no
// body, and JSON.parse("") is a SyntaxError the editor showed as the failure.
export async function read<T>(path: string, res: Response): Promise<T> {
  const said = await res.text();
  if (!res.ok) throw new Error(plainly(path, res.status, said));
  return (said.trim() ? JSON.parse(said) : undefined) as T;
}

// ade's failures carry a sentence meant for a person. Showing it with the
// route and the status around it buried the one part worth reading.
export function plainly(path: string, status: number, said: string): string {
  try {
    const body = JSON.parse(said) as { error?: unknown };
    if (typeof body.error === "string" && body.error.trim()) return body.error;
  } catch {
    // Not JSON, or not ade's shape: the raw answer is all there is.
  }
  return `${path}: ${status} ${said}`;
}
