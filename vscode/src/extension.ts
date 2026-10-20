// pickReviewers is who reviews. Several at once is the point — the server
// takes a list and runs one side review each — so this picks many.
async function pickReviewers(
  meta: Meta | undefined,
  named: boolean,
): Promise<string[] | undefined> {
  const all = client ? await client.agents().catch(() => []) : [];
  const offer = reviewers(all);

  if (offer.length === 0) {
    if (named) {
      void vscode.window.showWarningMessage(
        "ade: no named agent can review. A reviewer is a ~/.claude/agents file with reviews: true.",
      );
      return undefined;
    }
    return [];
  }

  // Ticked only where the cold reviewer is not also on offer: a list with
  // both, two of them already chosen, makes the commonest answer an untick.
  const wanted = named
    ? preselect(
        offer,
        vscode.workspace
          .getConfiguration("ade")
          .get<string[]>("defaultReviewers") ?? [],
      )
    : new Set<string>();

  const rows = offer.map((a) => ({
    label: `@${a.name}`,
    description: a.description ?? "",
    name: a.name,
    picked: wanted.has(a.name),
  }));
  // Without the cold reviewer the pick cannot express "file findings on the
  // diff", which is the default and the commonest answer.
  const cold = {
    label: "Cold reviewer",
    description: "files findings as comments on the diff",
    name: "",
    picked: false,
  };

  const picked = await pick(named ? rows : [cold, ...rows], {
    title: `Review ${what(meta)} — pick one or more`,
    canPickMany: true,
  });
  if (!picked || picked.length === 0) return undefined;

  const names = picked.map((p) => p.name).filter(Boolean);
  // Both together would be two reviews of one diff answering in two places.
  if (names.length > 0 && picked.some((p) => !p.name)) {
    void vscode.window.showWarningMessage(
      "ade: pick the cold reviewer or named agents, not both — they file in different places.",
    );
    return undefined;
  }
  return names;
}

import * as vscode from "vscode";
import { homedir } from "node:os";
import { basename, join } from "node:path";
import { readFileSync } from "node:fs";
import { execFile } from "node:child_process";
import { Client } from "./client";
import type { BusOffer, BusWho, State } from "./client";
import {
  type Fresh,
  Inbox,
  PRESENCES,
  replyLine,
  type TeamNode,
  userOf,
} from "./bus";
import type { BusReply } from "./client";
import { ask as askOffer } from "./busask";
import {
  askable as busAskable,
  OTHER,
  reviewers as busReviewers,
  reviewTarget,
  askedLine,
  targetItems,
} from "./busreview";
import { listed } from "./detail";
import {
  type Files,
  gitDir,
  label,
  localFiles,
  nearest,
  probe,
  read,
  recordPath,
  type Server,
} from "./discover";
import { Chat } from "./chat";
import { Settings } from "./settings";
import { Design } from "./design";
import { nameProblem } from "./docs";
import { diagnose, netReport } from "./diagnose";
import {
  type Detail as DiskDetail,
  type Disk as DiskReading,
  page as diskPage,
  type Purgeable,
  size,
  warning as purgeWarning,
} from "./diskrow";
import { Drift } from "./drift";
import { acts as driftActs, rows as driftRows } from "./driftpick";
import type { DriftItem } from "./driftrow";
import { acts as machineActs, rows as machineRows } from "./machinepick";
import { treeOf, Worktrees } from "./worktrees";
import { WorktreeGraph } from "./worktreeview";
import { badge as boxedBadge } from "./boxed";
import { CommitView } from "./commitview";
import { logLine } from "./logline";
import {
  type GraphRepo,
  page as graphPage,
  running,
  state as graphState,
} from "./graphpage";
import { page as jobPage, unread as unreadOutcomes } from "./jobpage";
import { rows as elsewhereRows } from "./elsewhere";
import { prunable } from "./pruneremote";
import { EDIT, Edits, PAGE, Pages } from "./pages";
import {
  folderURI,
  launcherAt,
  machineWide,
  said,
  whyNotHere,
} from "./remote";
import {
  AUTHORITY,
  dispatch as publishRun,
  Failed,
  label as hostLabel,
  noGh,
  noServer,
  published,
  PUBLISH,
  releaseVersion,
  resolve as resolveAuthority,
  serverURL,
  UNVALIDATED,
} from "./resolver";
import { page as prPage } from "./prpage";
import { blameWhen, combinable } from "./shape";
import { choices, detail as ticketDetail } from "./ticketdir";
import { page as usagePage, type WindowSpend } from "./windowpage";
import {
  detail as roleDetail,
  label as roleLabel,
  type Role,
  type RolesView,
  rows as roleRows,
  skeleton as roleSkeleton,
  validName as validRoleName,
} from "./rolerow";
import {
  lines as syncLines,
  page as syncPage,
  type SyncReport,
} from "./syncpage";
import {
  type Entry as MemoryEntry,
  flat as memoryRows,
  label as memoryLabel,
  line as memoryLine,
  page as memoryPage,
  parse as memoryParse,
  type ProjectView,
  type Row as MemoryRow,
} from "./projectrow";
import { page as statsPage, type Stats } from "./statspage";
import { replyable } from "./replies";
import {
  apply as applySandbox,
  type Edit as SandboxEdit,
  type Row as SandboxRow,
  rows as sandboxRows,
  validKey,
} from "./sandboxfile";
import {
  forgetSecret,
  readSecrets,
  secrets,
  setSecret,
  stateOf as secretState,
} from "./secrets";
import {
  addHost,
  ensure as ensureAt,
  expose,
  find as findLauncher,
  ask as askLauncher,
  drop as dropLauncher,
  hostLine,
  hosts,
  job as jobPost,
  run as runJobAt,
  openOn,
  begin as beginSession,
  put as putLauncher,
  target,
  tell as tellLauncher,
  scratch,
  type Hit,
  models,
  waiting as whatIsWaiting,
  say as sayToSession,
  watch as watchLauncherHub,
  events,
  wikiPage,
  wikiSearch,
  dashOf,
  dismissNotification,
  openReview,
  startTicket,
  type Checkout,
  type Host,
  type Job,
  type Repo,
  hosts as hostsOf,
  jobs as jobsOf,
  labelHost,
  usage as usageOf,
  worker,
  type WorkerVerb,
  muteReview,
  open as openSessionAt,
  workspace,
  prune as pruneAt,
  openReview as openReviewAt,
  refreshDash,
  reviews as reviewsOf,
  type Asked,
  type Review,
  reach,
  editorServer,
  removeHost,
  reason,
  tell,
  repos as launcherRepos,
  serving as servingNow,
  type Machine,
  type Session,
  type Up,
} from "./launcher";
import {
  describe as describeJob,
  docPath as jobDocPath,
  editable as editableJob,
  JOB,
  jobNamed,
  line as jobLine,
  rows as jobRows,
  skeleton,
} from "./jobrow";
import { badge as usageBadge } from "./usage";
import { label as countLabel } from "./counts";
import { Machines } from "./machines";
import { Lineage } from "./lineage";
import { Sessions } from "./sessions";
import { Watch } from "./watch";
import { forceable, report, why } from "./tree";
import { blocked, numberOf, label as prLabel, mergeMethods } from "./pr";
import {
  empty,
  LEVELS,
  levelled,
  nothing,
  preselect,
  reviewers,
  SCOPES,
  scopeable,
  what,
} from "./review";
import {
  modelPick,
  pick as wikiPick,
  port as readPort,
  WIKI,
  wikiPath,
} from "./wiki";
import { move, warning } from "./models";
import {
  description,
  detail,
  every as everySession,
  flatten,
  found as foundIn,
  label as sessionLabel,
  sections,
  shortlist,
  type Row,
} from "./switcher";
import {
  badge as runBadge,
  going,
  offersCommit,
  quiet,
  since,
} from "./runstate";
import { acts as sessionActs } from "./sessionacts";
import { DesignEditor } from "./designedit";
import { badge as checksBadge, summary as checksSummary } from "./checks";
import {
  kept as keptChecks,
  parse as parseChecks,
  typed as typedChecks,
} from "./verify";
import { rows as queueRows, type TreeRow } from "./queue";
import { Queue } from "./queuetree";
import {
  detail as cardDetail,
  press as pressCard,
  title as cardTitle,
  waiting as cardsWaiting,
  type Card,
} from "./cards";
import {
  ADD as TODO_ADD,
  DONE as TODO_DONE,
  DROP as TODO_DROP,
  DUE as TODO_DUE,
  Desk,
  REOPEN as TODO_REOPEN,
  rows as todoRows,
  type Row as TodoRow,
} from "./desk";
import {
  add as fileTodo,
  done as doneTodo,
  dueFrom,
  edit as changeTodo,
  remove as dropTodo,
  describe as describeTodo,
  icon as todoIcon,
  todoFrom,
  type Todo,
} from "./todos";
import type {
  DiffFile,
  Listed,
  Meta,
  RunState,
  Tree,
  Worktree,
  VerifyCheck,
  VerifyState,
} from "./client";
import type { BranchPR, Thread } from "./client";
import type { SessionNode } from "./sessions";
import { trimIndent, where } from "./selection";
import { findAde, launcherArgs, serverArgs, start } from "./start";
import { Issues } from "./boards";
import { asideCount, emptyBecause, rows as pendingRows } from "./pending";
import { showReview as showPendingPR } from "./prview";
import { acts as issueActs, rows as issueRows } from "./issuepick";
import { ACTS as DOC_ACTS, rows as docRows } from "./designpick";
import { acts as treeActs, rows as treeRows } from "./worktreepick";
import { offstage } from "./loading";
import { entries, GROUPS } from "./menu";
import { wellCount, wellRows } from "./well";
import { Dashboard } from "./dashview";
import { RepoPage } from "./repoview";
import {
  NewSessionPage,
  nothing as noOffer,
  type Offer,
} from "./newsessionview";
import { plan as sessionPlan } from "./newsession/plan";
import {
  names as sessionNames,
  type Chosen,
  type Doc,
} from "./newsession/view";
import { multiRoot } from "./multiroot";
import { PurgePage } from "./purgeview";
import type { Sweep } from "./purgepanel/view";
import { DesignPrunePage, type Archived } from "./designpruneview";
import type { Prunable, WikiState } from "./designprunepanel/view";
import { QuizPage } from "./quizview";
import type { Repo as RepoStats } from "./repo/view";
import type { Board, Do } from "./dashboard/view";
import { openCount } from "./comments";
import {
  everyRow,
  featuredVerbs,
  type GroupedVerb,
  isQuestion,
  matches,
  plain,
  recentRows,
  remember,
  type Remembered,
  route,
  type Verb,
  verbRowsFor,
} from "./omnibox";
import { at, done, inFile, orphans, type Pinned } from "./here";
import { refused } from "./reconcile";
import { closeAfter, inNewWindow } from "./window";
import { take, want } from "./wanted";
import { AdeIndex, GitIndex, type Index, Scm } from "./scm";
import { gitRepository } from "./gitapi";
import { picked } from "./picked";
import { input, inputBox, pick, quickPick } from "./pick";
import { madeFrom, wouldMake } from "./wouldmake";
import { AGENTS, models as modelsFor, says, wheres } from "./build";
import {
  forceable as canForce,
  here,
  keeping,
  mainOf,
  PARTINGS,
  removable,
} from "./leave";
import { BLOB, Blobs, blobUri, Changes, REV, revUri, Revs } from "./changes";
import { scopeLabel } from "./change";
import { openTerminal } from "./terminal";
import { Threads } from "./threads";
import { Marks } from "./marks";
import { Panels, rulesPage } from "./view";

// retry is how long the extension waits before looking for ade again. ade
// starting after the editor did is the ordinary case, not an error.
const retry = 5000;

// menu is one palette entry standing for all of them, built from what the
// manifest declares so a new command appears here without being added twice.
async function menu(context: vscode.ExtensionContext): Promise<void> {
  const contributes = (
    context.extension.packageJSON as {
      contributes: Parameters<typeof entries>[0];
    }
  ).contributes;
  const rows = entries(contributes, "ade.menu").map((row) =>
    row.separator
      ? { label: row.label, kind: vscode.QuickPickItemKind.Separator }
      : { label: row.label, command: row.command },
  );

  // Named, because a window attaches to one checkout and it is not always the
  // one you are looking at. Every row acts on this one.
  const chosen = await pick(
    rows as (vscode.QuickPickItem & { command?: string })[],
    {
      title: live ? `ade · ${basename(live.root)}` : "ade",
      matchOnDescription: true,
    },
  );
  if (chosen?.command) await vscode.commands.executeCommand(chosen.command);
}

// Asks Linear again rather than redrawing: the board is cached on the
// server, so a refresh that only redraws says nothing.
async function reloadIssues(): Promise<void> {
  if (client)
    await offstage(() => client!.refreshIssues().catch(() => undefined));
  await issues.refresh();
}

// A ticket says what to do and not where, so the repository is asked for.
// pickIssue is the Linear tickets assigned to you. Starting one is what the
// list is for, so it leads; the Linear page is the other half.
async function pickIssue(): Promise<void> {
  if (!client) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  await reloadIssues();
  const list = issueRows(await issues.getChildren());
  if (list.length === 0) {
    void vscode.window.showInformationMessage(
      issues.error
        ? `ade: Linear said: ${issues.error}`
        : "ade: no issues are assigned to you.",
    );
    return;
  }
  const picked = await pick(list, {
    title: "Issues",
    matchOnDescription: true,
  });
  if (!picked) return;

  const act = await pick(
    issueActs(picked.issue).map((a) => ({
      label: a.label,
      detail: a.detail,
      id: a.id,
    })),
    { title: picked.issue.identifier },
  );
  if (!act) return;
  if (act.id === "linear") {
    await vscode.env.openExternal(vscode.Uri.parse(picked.issue.url));
    return;
  }
  await startIssue(picked.issue);
}

async function startIssue(
  issue: { identifier: string; title: string } | undefined,
): Promise<void> {
  const c = client;
  const where = live;
  if (!c || !where || !issue) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  let offered;
  try {
    offered = choices(await c.repos(), where.root);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  if (!offered.length) {
    void vscode.window.showErrorMessage(
      "ade: no repositories under your scan directories to start it in.",
    );
    return;
  }
  const picked =
    offered.length === 1
      ? offered[0]
      : await pick(offered, {
          title: `Start ${issue.identifier} in which repository?`,
          matchOnDetail: true,
        });
  if (!picked) return;
  const said = await vscode.window.showInformationMessage(
    `Start ${issue.identifier} in ${picked.label}?`,
    {
      modal: true,
      detail: ticketDetail(issue.title, picked.path, picked.here),
    },
    "Start",
  );
  if (said !== "Start") return;
  try {
    await c.startIssue(issue.identifier, picked.path);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  await issues.refresh();
  if (picked.here) void chat.reveal(where);
}

// pendingPRs is the account's review requests, not this checkout's. It asks
// GitHub again first: the launcher caches that search for five minutes.
async function pendingPRs(): Promise<void> {
  const at = await findLauncher();
  if (!at) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  // A refresh that failed leaves the launcher's five-minute cache in place, so
  // the list would be however stale it was with nothing to say it.
  let stale = "";
  const found = await vscode.window.withProgress(
    {
      location: vscode.ProgressLocation.Notification,
      title: "ade: asking GitHub what is waiting on you…",
    },
    async () => {
      await refreshDash(at).catch((err: unknown) => {
        stale = String(err);
      });
      return reviewsOf(at).catch(() => ({ all: [], trouble: {} }) as Asked);
    },
  );
  if (stale) void vscode.window.showWarningMessage(`ade: ${stale}`);
  if (found.trouble.error)
    void vscode.window.showWarningMessage(
      `ade: GitHub did not answer, so this list is as old as it was — ${found.trouble.error}`,
    );

  const aside = asideCount(found.all);
  if (pendingRows(found.all).length === 0 && aside === 0) {
    void vscode.window.showInformationMessage(
      `ade: ${emptyBecause(found.trouble)}`,
    );
    return;
  }

  const review = await pickPending(found.all, aside);
  if (!review) return;
  // The page is the default: picking a row shows the request, and what you
  // can do with it is on it.
  const act = await showPendingPR(review);
  if (!act) return;
  if (act === "web") {
    await vscode.env.openExternal(vscode.Uri.parse(review.url));
    return;
  }
  if (act === "ade") {
    await openRequestedReview(review);
    return;
  }
  await mute(review, act === "mute");
}

// pickPending is the list, with one trailing row into what was put aside. A
// heading row would survive the picker's filter while its rows did not.
async function pickPending(
  found: Review[],
  aside: number,
): Promise<Review | undefined> {
  for (let showing = pendingRows(found).length === 0; ;) {
    const rows = pendingRows(found, showing);
    const open: (vscode.QuickPickItem & { review?: Review; more?: boolean })[] =
      rows.map((row) => ({ ...row }));
    if (!showing && aside > 0)
      open.push({
        label: `$(eye-closed) ${aside} put aside`,
        description: "show them",
        more: true,
      });
    const picked = await pick(open, {
      title: showing ? "Put aside" : "Pending pull requests",
      matchOnDescription: true,
    });
    if (!picked) return undefined;
    if (!picked.more) return picked.review;
    showing = true;
  }
}

// reloadSessions asks GitHub about the checks too. They are cached beside the
// pull request, so redrawing alone shows the same red as a minute ago.
async function reloadSessions(): Promise<void> {
  if (client)
    await offstage(() => client!.refreshChecks().catch(() => undefined));
  await sessions.refresh();
}

// openRequestedReview checks the pull request out somewhere this machine has
// the repository, and starts an ade on it. The launcher owns that.
async function openRequestedReview(review: Review): Promise<void> {
  const at = await findLauncher();
  if (!at) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  let where;
  try {
    where = await openReviewAt(at, review.repo, review.number);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  if (!where.path) return;
  // Opening the folder restarts the extension, so the panel cannot be opened
  // from here: the intent is left for the window that comes up to find.
  await want(memory, { path: where.path, number: review.number });
  await switchTo(where.path);
}

async function mute(review: Review, muted: boolean): Promise<void> {
  const at = await findLauncher();
  if (!at) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  try {
    await muteReview(at, review.repo, review.number, muted);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// The server decides whether it may: not with nothing failing, and not on a
// pull request this checkout is not on, where edits land on the wrong branch.
async function fixChecks(): Promise<void> {
  if (!client || !live) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  // Both layers are stale by default: the server polls GitHub, and the tree
  // caches its first load forever. You looked at the checks a second ago.
  await client.refreshChecks().catch(() => undefined);
  await sessions.refresh();
  const bad = sessions.failing;
  if (bad === 0) {
    void vscode.window.showInformationMessage("ade: nothing is failing.");
    return;
  }
  const said = await vscode.window.showWarningMessage(
    `Investigate ${bad} failing check${bad === 1 ? "" : "s"}?`,
    {
      modal: true,
      detail: "The agent reads the runs and edits this checkout to fix them.",
    },
    "Investigate",
  );
  if (said !== "Investigate") return;
  let out: { queued?: number } = {};
  try {
    out = (await client.fixChecks()) as { queued?: number };
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  vscode.window.setStatusBarMessage(
    out.queued
      ? "ade: queued behind what is running"
      : "ade: reading the failing checks",
    3000,
  );
  void chat.reveal(live);
}

// openPR points ade at a pull request, which is what this extension has that
// github.com does not: the diff with its review threads inline.
async function openPR(number: number | undefined): Promise<void> {
  if (!number) return;
  await verb(`#${number} open`, (c) => c.openPR(number));
  await sessions.refresh();
  showReview();
}

async function openBranchPR(): Promise<void> {
  await sessions.ready();
  showReview();
}

// showReview puts the pull request itself in the editor area. It is the
// generated page now; the framed panel went with the app page.
function showReview(): void {
  void showPR();
}

// The pending reconnect, one at a time: a manual Reconnect while the loop is
// armed would otherwise leave two loops running, each arming another.
let waiting: NodeJS.Timeout | undefined;

function again(context: vscode.ExtensionContext): void {
  if (waiting) clearTimeout(waiting);
  waiting = setTimeout(() => void connect(context), retry);
}

// watchRecord connects the moment ade writes its record, rather than up to a
// poll later. On another machine that wait is the whole of the start-up.
function watchRecord(context: vscode.ExtensionContext): vscode.Disposable {
  const seen = vscode.workspace.createFileSystemWatcher(
    "**/ade/server.json",
    false,
    false,
    true,
  );
  const poke = () => {
    if (!live) void connect(context);
  };
  seen.onDidCreate(poke);
  seen.onDidChange(poke);
  return seen;
}

let threads: Threads | undefined;
let scm: Scm | undefined;
let shownView = "";
let lineage: Lineage | undefined;
let stream: AbortController | undefined;
let hubWatch: AbortController | undefined;
let status: vscode.StatusBarItem;
let run: vscode.StatusBarItem;
let plan: vscode.StatusBarItem;
let checks: vscode.StatusBarItem;
let boxed: vscode.StatusBarItem;
let bell: vscode.StatusBarItem;
// offered is the detected check list already put to you in this window, so a
// declined offer is not asked again on the next poll.
let offered = false;
// The last run state, redrawn on a timer so the age on the bar ticks between
// events rather than freezing at whatever it said when the run began.
let runState: RunState | undefined;
let lastWorktree: Worktree | undefined;
let ticker: NodeJS.Timeout | undefined;
let panels: Panels;
let changes: Changes;
const marks = new Marks();
// Where a cross-window intent is left, since the extension restarts when the
// folder changes.
let memory: vscode.Memento | undefined;
let chat: Chat;
let concierge: Chat;
let settingsPage: Settings;
let commitView: CommitView;
let dashboard: Dashboard;
let repoPage: RepoPage;
let newSessionPage: NewSessionPage;
let purgePage: PurgePage;
let designPrunePage: DesignPrunePage;
let quizPage: QuizPage;
let sessions: Sessions;
let design: Design;
let machines: Machines;
let watch: Watch;
// The topmost view's header, where the checkout is named. The container's own
// title is static: VS Code has no api for one.
let diffHeader: { description?: string } | undefined;
let driftTree: Drift;
let worktreeTree: Worktrees;
let worktreeGraph: WorktreeGraph;
const pages = new Pages();
const edits = new Edits();
let queueTree: Queue;
let desk: Desk;
let issues: Issues;
let inbox: Inbox;
let inboxView: vscode.TreeView<TeamNode> | undefined;

export async function activate(
  context: vscode.ExtensionContext,
): Promise<void> {
  status = vscode.window.createStatusBarItem(
    vscode.StatusBarAlignment.Right,
    100,
  );
  status.command = "ade.reconnect";
  // Before anything else: a remote window resolves on the way up.
  registerResolver(context);
  run = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
  run.command = "ade.openChat";
  ticker = setInterval(() => drawRun(), 1000);
  // Left of the run state, and read as often as the launcher itself reads it.
  plan = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 99);
  plan.command = "ade.showUsage";
  void drawPlan();
  const planTicker = setInterval(() => void drawPlan(), 2 * 60 * 1000);
  // Right of the run state: a verdict on the last turn, beside what it did.
  checks = vscode.window.createStatusBarItem(
    vscode.StatusBarAlignment.Left,
    98,
  );
  checks.command = "ade.checks";
  // Right of the checks: where the agent runs is standing, not per-turn.
  boxed = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 97);
  boxed.command = "ade.toggleSandbox";
  // The desk is visible with its sidebar shut, which is most of the time.
  bell = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 99);
  bell.command = "ade.todos";
  watchLauncher();
  context.subscriptions.push(watchRecord(context));
  context.subscriptions.push(status, run, plan, checks, boxed, bell, {
    dispose: () => {
      clearInterval(planTicker);
      if (ticker) clearInterval(ticker);
      if (waiting) clearTimeout(waiting);
      hubWatch?.abort();
      hubWatch = undefined;
      teardown();
    },
  });

  // The window's focus is the nearest thing ade has to knowing you are at the
  // keyboard, and nothing else in the editor reports it.
  context.subscriptions.push(
    vscode.window.onDidChangeWindowState(
      (state) => void tellBusHere(state.focused),
    ),
  );
  void tellBusHere(vscode.window.state.focused);
  context.subscriptions.push(
    vscode.workspace.onDidChangeWorkspaceFolders(() => {
      if (client) void client.diffFolders(windowFolders()).catch(() => {});
    }),
  );

  // A window on another machine never starts ade: the panes have to say that
  // rather than offer a button that does nothing here.
  void vscode.commands.executeCommand(
    "setContext",
    "ade.elsewhere",
    !machineWide(vscode.env.remoteName),
  );

  memory = context.globalState;
  panels = new Panels();
  chat = new Chat(context);
  concierge = new Chat(context, true);
  // Opening the container is not a command, so the view's own showing is what
  // starts the desk. Without this the pane draws before anything serves it.
  concierge.onShown = () => void startDesk();
  boxContext = context;
  dashboard = new Dashboard(context, boardNow, boardAct);
  repoPage = new RepoPage(context, repoNow);
  newSessionPage = new NewSessionPage(
    context,
    sessionOffer,
    sessionDesigns,
    startChosen,
  );
  purgePage = new PurgePage(context, sweepNow, pruneIn);
  designPrunePage = new DesignPrunePage(
    context,
    designRepos,
    prunableIn,
    archiveDesign,
  );
  quizPage = new QuizPage(context, () => live?.root);
  context.subscriptions.push(
    dashboard,
    repoPage,
    newSessionPage,
    purgePage,
    designPrunePage,
    quizPage,
    vscode.window.registerWebviewPanelSerializer("ade.dashboard", {
      deserializeWebviewPanel: async (panel) => dashboard.revive(panel),
    }),
  );
  if (!context.globalState.get<boolean>("ade.arranged")) {
    void context.globalState.update("ade.arranged", true);
    setTimeout(() => void arrange(), 1500);
  }
  settingsPage = new Settings(context);
  commitView = new CommitView(context, async () => {
    if (client) await refresh(client);
  });
  sessions = new Sessions(undefined);
  design = new Design(undefined);
  machines = new Machines();
  driftTree = new Drift();
  worktreeTree = new Worktrees();
  worktreeGraph = new WorktreeGraph(context);
  queueTree = new Queue();
  desk = new Desk(async () => (launcher ??= await findLauncher()));
  issues = new Issues();
  changes = new Changes();
  inbox = new Inbox();
  const blobs = new Blobs(() => client);
  changes.onMoved(() => blobs.stale());
  // Its own container, so it can be dragged to the secondary sidebar as a unit
  // rather than crowding the nine trees in the ADE one.
  context.subscriptions.push(
    vscode.window.registerWebviewViewProvider("ade.chat", chat, {
      webviewOptions: { retainContextWhenHidden: true },
    }),
    vscode.window.registerWebviewPanelSerializer("ade.concierge", {
      deserializeWebviewPanel: async (panel) => concierge.revive(panel),
    }),
    vscode.window.registerWebviewPanelSerializer("ade.settings", {
      deserializeWebviewPanel: async (panel) =>
        settingsPage.revive(panel, live),
    }),
    vscode.window.registerWebviewPanelSerializer("ade.commit", {
      deserializeWebviewPanel: async (panel) => commitView.revive(panel),
    }),
    vscode.window.registerWebviewPanelSerializer("ade.worktreeGraph", {
      deserializeWebviewPanel: async (panel) => worktreeGraph.revive(panel),
    }),
  );

  const diffView = vscode.window.createTreeView("ade.diff", {
    treeDataProvider: changes,
  });
  diffHeader = diffView;
  changes.watching(diffView);
  context.subscriptions.push(
    diffView,
    vscode.window.registerFileDecorationProvider(changes),
    vscode.window.registerFileDecorationProvider(marks),
    vscode.workspace.registerTextDocumentContentProvider(BLOB, blobs),
    vscode.workspace.registerTextDocumentContentProvider(
      REV,
      new Revs(() => client),
    ),
  );
  const queueView = vscode.window.createTreeView("ade.queue", {
    treeDataProvider: queueTree,
  });
  context.subscriptions.push(desk.onDidChange(() => drawDesk()));
  const inboxView2 = vscode.window.createTreeView("ade.inbox", {
    treeDataProvider: inbox,
  });
  inboxView = inboxView2;
  // The twisty's state is ours to remember: a redraw would otherwise reopen
  // a group somebody had just shut.
  inboxView2.onDidCollapseElement((e) => {
    if (e.element.kind === "group") inbox.held(e.element.title, false);
  });
  inboxView2.onDidExpandElement((e) => {
    if (e.element.kind === "group") inbox.held(e.element.title, true);
  });
  // The Diff view has no count: its own description is the file and line
  // tally, which says more than a count would.
  context.subscriptions.push(
    counted(inboxView2, inbox),
    inboxView2,
    counted(queueView, queueTree, "waiting"),
    queueView,
    driftTree,
    worktreeGraph,
    vscode.workspace.registerTextDocumentContentProvider(PAGE, pages),
    vscode.workspace.registerFileSystemProvider(EDIT, edits),
  );
  watch = new Watch();
  context.subscriptions.push(design, panels, chat, watch);

  context.subscriptions.push(
    vscode.commands.registerCommand("ade.menu", () => void menu(context)),
    vscode.commands.registerCommand(
      "ade.reconnect",
      () => void connect(context),
    ),
    vscode.commands.registerCommand(
      "ade.openChat",
      () => void chat.reveal(live),
    ),
    vscode.commands.registerCommand(
      "ade.focusChat",
      () => void chat.focus(live),
    ),
    vscode.commands.registerCommand("ade.dictate", () => {
      chat.dictate();
      concierge.dictate();
    }),
    vscode.commands.registerCommand("ade.ask", () => void ask()),
    vscode.commands.registerCommand(
      "ade.omnibox",
      (text: unknown) => void omnibox(text, context),
    ),
    vscode.commands.registerCommand("ade.omnibox.rows", (text: unknown) =>
      omniboxRows(text),
    ),
    vscode.commands.registerCommand("ade.omnibox.answer", (text: unknown) =>
      omniboxAnswer(text),
    ),
    vscode.commands.registerCommand("ade.omnibox.recent", () => [
      ...featuredVerbs(boxVerbs(context)),
      ...recentRows(recalled(), boxVerbs(context)),
    ]),
    vscode.commands.registerCommand("ade.well", () => void openWell()),
    vscode.commands.registerCommand("ade.stuck", () => void whatIsStuck()),
    vscode.commands.registerCommand(
      "ade.answerSession",
      (row: Row) => void answerSession(row),
    ),
    vscode.commands.registerCommand(
      "ade.dashboard",
      () => void dashboard.show(),
    ),
    vscode.commands.registerCommand("ade.diffTurn", () => void diffTurn()),
    vscode.commands.registerCommand(
      "ade.omnibox.open",
      (what: unknown) => void omniboxOpen(what),
    ),
    vscode.commands.registerCommand(
      "ade.setPresence",
      () => void setPresence(),
    ),
    vscode.commands.registerCommand(
      "ade.dismissReply",
      (id) => void dismissReply(id),
    ),
    vscode.commands.registerCommand(
      "ade.dismissOffer",
      (node?: { offer?: { id?: string } }) =>
        void dismissReply(node?.offer?.id),
    ),
    vscode.commands.registerCommand(
      "ade.messageTeamMember",
      (node) => void messageTeamMember(node),
    ),
    vscode.commands.registerCommand(
      "ade.answerAsk",
      (offer: BusOffer) => void answerAsk(offer),
    ),
    vscode.commands.registerCommand(
      "ade.takeReview",
      (offer: BusOffer) => void takeReview(offer),
    ),
    vscode.commands.registerCommand(
      "ade.askTeamForReview",
      (arg: unknown) => void askTeamForReview(arg),
    ),
    vscode.commands.registerCommand(
      "ade.sendBranchPRTo",
      (node: unknown) => void sendBranchPRTo(node),
    ),
    vscode.commands.registerCommand(
      "ade.notifyWhenBack",
      (node: unknown) => void notifyWhenBack(node),
    ),
    vscode.commands.registerCommand("ade.showInbox", () =>
      vscode.commands.executeCommand("ade.inbox.focus"),
    ),
    vscode.commands.registerCommand(
      "ade.toggleSandbox",
      () => void toggleSandbox(),
    ),
    vscode.commands.registerCommand("ade.openPermissions", () =>
      open("permissions", "ade · Permissions"),
    ),
    vscode.commands.registerCommand(
      "ade.openReview",
      () => void openBranchPR(),
    ),
    vscode.commands.registerCommand(
      "ade.showComments",
      () => void showComments(),
    ),
    vscode.commands.registerCommand(
      "ade.startLauncher",
      () => void startLauncher(),
    ),
    vscode.commands.registerCommand("ade.openTerminal", () => terminal()),
    vscode.commands.registerCommand(
      "ade.stop",
      () => void verb("stopped", (c) => c.cancel()),
    ),
    vscode.commands.registerCommand("ade.newChat", () => void newChat()),
    vscode.commands.registerCommand("ade.clearChat", () => void clearChat()),
    vscode.commands.registerCommand(
      "ade.runQueued",
      () => void verb("queue running", (c) => c.runQueued()),
    ),
    vscode.commands.registerCommand("ade.promoteQueued", (row: TreeRow) =>
      queued(row, "moved to the front", (c, id) => c.promoteQueued(id)),
    ),
    vscode.commands.registerCommand("ade.dropQueued", (row: TreeRow) =>
      queued(row, "taken off the queue", (c, id) => c.dropQueued(id)),
    ),
    vscode.commands.registerCommand("ade.showQueue", () => void showQueue()),
    vscode.commands.registerCommand(
      "ade.refreshChecks",
      () => void verb("checks refreshing", (c) => c.refreshChecks()),
    ),
    vscode.commands.registerCommand("ade.review", () => void review(false)),
    vscode.commands.registerCommand(
      "ade.reviewBranch",
      () => void review(true),
    ),
    vscode.commands.registerCommand(
      "ade.reviewWithAgents",
      () => void review(false, true),
    ),
    vscode.commands.registerCommand("ade.setScope", () => void setScope()),
    vscode.commands.registerCommand(
      "ade.approvePR",
      () => void verdict("approve"),
    ),
    vscode.commands.registerCommand(
      "ade.requestChanges",
      () => void verdict("request-changes"),
    ),
    vscode.commands.registerCommand("ade.mergePR", () => void merge()),
    vscode.commands.registerCommand("ade.searchWiki", () => void searchWiki()),
    vscode.commands.registerCommand("ade.pickModel", () => void pickModel()),
    vscode.commands.registerCommand("ade.exposePort", () => void exposePort()),
    vscode.commands.registerCommand(
      "ade.openSettings",
      () => void openSettings(),
    ),
    vscode.commands.registerCommand("ade.commit", () => void commit(false)),
    vscode.commands.registerCommand("ade.commitAmend", () => void commit(true)),
    vscode.commands.registerCommand("ade.push", () => void scm?.push()),
    vscode.commands.registerCommand(
      "ade.writeCommitMessage",
      () => void scm?.writeMessage(),
    ),
    vscode.commands.registerCommand(
      "ade.newSession",
      (node?: SessionNode) => void newSession(node),
    ),
    vscode.commands.registerCommand("ade.newDesign", () => void newDesign()),
    vscode.commands.registerCommand("ade.addTodo", () => void captureTodo()),
    vscode.commands.registerCommand("ade.todos", () => void pickTodo()),
    vscode.commands.registerCommand("ade.concierge", () => void openDesk()),
    vscode.commands.registerCommand(
      "ade.conciergeFloat",
      () => void floatDesk(),
    ),
    vscode.commands.registerCommand("ade.arrange", () => void arrange()),
    vscode.commands.registerCommand("ade.machines", () => void pickMachine()),
    vscode.commands.registerCommand("ade.addMachine", () => void addMachine()),
    vscode.commands.registerCommand("ade.diagnose", () => void checkNetwork()),
    vscode.commands.registerCommand("ade.keys", () => void keys()),
    vscode.commands.registerCommand("ade.pullBase", () => void pullBase()),
    vscode.commands.registerCommand("ade.draftReply", (t: unknown) =>
      draftReply(t),
    ),
    vscode.commands.registerCommand(
      "ade.draftReplies",
      () => void draftReplies(),
    ),
    vscode.commands.registerCommand("ade.sendReply", (t: unknown) =>
      sendReply(t),
    ),
    vscode.commands.registerCommand(
      "ade.sendReplies",
      () => void sendReplies(),
    ),
    vscode.commands.registerCommand("ade.stopAgent", () => void stopAgent()),
    vscode.commands.registerCommand("ade.explain", () => void explain()),
    vscode.commands.registerCommand("ade.showLogs", () => void showLogs()),
    vscode.commands.registerCommand("ade.toggleFree", () => void toggleFree()),
    vscode.commands.registerCommand("ade.checks", () => void runOrEditChecks()),
    vscode.commands.registerCommand("ade.disk", () => void showDisk()),
    vscode.commands.registerCommand("ade.jobs", () => void pickJob()),
    vscode.commands.registerCommand("ade.memory", () => void pickMemory()),
    vscode.commands.registerCommand("ade.showStats", () => void showStats()),
    vscode.commands.registerCommand("ade.showRepo", () => void repoPage.show()),
    vscode.commands.registerCommand(
      "ade.purgeWorktrees",
      () => void purgePage.show(),
    ),
    vscode.commands.registerCommand(
      "ade.purgeDesigns",
      () => void designPrunePage.show(),
    ),
    vscode.commands.registerCommand("ade.quizMe", () => void quizPage.show()),
    vscode.commands.registerCommand("ade.showGraph", () => void showGraph()),
    vscode.commands.registerCommand("ade.showUsage", () => void showUsage()),
    vscode.commands.registerCommand("ade.showPR", () => void showPR()),
    vscode.commands.registerCommand("ade.editPR", () => void editPR()),
    vscode.commands.registerCommand("ade.checkoutPR", () => void checkoutPR()),
    vscode.commands.registerCommand("ade.combineSessions", (row: unknown) =>
      combineSessions(row),
    ),
    vscode.commands.registerCommand("ade.dismissSuggestion", (row: unknown) =>
      dismissSuggestion(row),
    ),
    vscode.commands.registerCommand("ade.uncombineSession", (row: unknown) =>
      uncombineSession(row),
    ),
    vscode.commands.registerCommand(
      "ade.stopElsewhere",
      () => void stopElsewhere(),
    ),
    vscode.commands.registerCommand("ade.roles", () => void pickRole()),
    vscode.commands.registerCommand("ade.syncModels", () => void syncModels()),
    vscode.commands.registerCommand("ade.showJobReports", (row: unknown) =>
      showJobReports(row),
    ),
    vscode.commands.registerCommand(
      "ade.syncWorktrees",
      () => void syncWorktrees(),
    ),
    vscode.commands.registerCommand("ade.archiveSession", (row: unknown) =>
      archiveSession(row, true),
    ),
    vscode.commands.registerCommand("ade.unarchiveSession", (row: unknown) =>
      archiveSession(row, false),
    ),
    vscode.commands.registerCommand("ade.deleteSession", (row: unknown) =>
      deleteSession(row),
    ),
    vscode.commands.registerCommand("ade.renameSession", (row: unknown) =>
      renameSession(row),
    ),
    vscode.commands.registerCommand("ade.newScratch", () => void newScratch()),
    vscode.commands.registerCommand(
      "ade.pruneMachineSessions",
      (row: unknown) => void pruneMachineSessions(row),
    ),
    vscode.commands.registerCommand(
      "ade.startElsewhere",
      () => void startElsewhere(),
    ),
    vscode.commands.registerCommand("ade.rememberRepo", (row: unknown) =>
      rememberRepo(row, true),
    ),
    vscode.commands.registerCommand("ade.forgetRepo", (row: unknown) =>
      rememberRepo(row, false),
    ),
    vscode.commands.registerCommand("ade.diffRoot", () => void diffRoot()),
    vscode.commands.registerCommand("ade.seedDesign", (node: unknown) =>
      seedDesign(node),
    ),
    vscode.commands.registerCommand("ade.previewURL", () => void previewURL()),
    vscode.commands.registerCommand(
      "ade.portScratch",
      () => void portScratch(),
    ),
    vscode.commands.registerCommand(
      "ade.distilMemory",
      () => void distilMemory(),
    ),
    vscode.commands.registerCommand("ade.drift", () => void pickDrift()),
    vscode.commands.registerCommand("ade.design", () => void pickDesign()),
    vscode.window.registerCustomEditorProvider(
      DesignEditor.viewType,
      new DesignEditor(context.extensionUri),
      { webviewOptions: { retainContextWhenHidden: true } },
    ),
    vscode.commands.registerCommand(
      "ade.grillDesign",
      (node?: unknown) => void grillDesign(node ?? shownDoc()),
    ),
    vscode.commands.registerCommand(
      "ade.condenseDesign",
      (node?: unknown) => void condenseDesign(node ?? shownDoc()),
    ),
    vscode.commands.registerCommand("ade.runDrift", () => void runDrift()),
    vscode.commands.registerCommand(
      "ade.sandboxFile",
      () => void sandboxFile(),
    ),
    vscode.commands.registerCommand(
      "ade.refreshTeam",
      () => void refreshTeam(),
    ),
    vscode.commands.registerCommand(
      "ade.removeAllComments",
      () => void removeAllComments(),
    ),
    vscode.commands.registerCommand("ade.editOutgoing", (t: unknown) =>
      editOutgoing(t),
    ),
    vscode.commands.registerCommand("ade.deleteDoc", (node: unknown) =>
      deleteDoc(node),
    ),
    vscode.commands.registerCommand("ade.archiveDoc", (node: unknown) =>
      archiveDoc(node),
    ),
    vscode.commands.registerCommand("ade.issues", () => void pickIssue()),
    vscode.commands.registerCommand("ade.pendingPRs", () => void pendingPRs()),
    vscode.commands.registerCommand(
      "ade.refreshChanges",
      () => void changes.refresh(),
    ),
    vscode.commands.registerCommand(
      "ade.openChange",
      (file: DiffFile) => void (live && changes.open(file, live.root)),
    ),
    vscode.commands.registerCommand(
      "ade.scopeBranch",
      () => void scopeTo("branch"),
    ),
    vscode.commands.registerCommand(
      "ade.scopeCommitted",
      () => void scopeTo("committed"),
    ),
    vscode.commands.registerCommand(
      "ade.scopeUncommitted",
      () => void scopeTo("uncommitted"),
    ),
    vscode.commands.registerCommand("ade.fixChecks", () => void fixChecks()),
    vscode.commands.registerCommand(
      "ade.startIssue",
      (issue) => void startIssue(issue),
    ),
    vscode.commands.registerCommand(
      "ade.openPR",
      (arg: unknown) => void openPR(numberOf(arg)),
    ),
    vscode.commands.registerCommand(
      "ade.switchWorktree",
      () => void switchWorktree(),
    ),
    vscode.commands.registerCommand(
      "ade.closeSession",
      () => void closeSession(),
    ),
    follow(),
    vscode.commands.registerCommand(
      "ade.build",
      (node?: { label?: string } | string) =>
        void build(typeof node === "string" ? node : undefined),
    ),
    vscode.commands.registerCommand("ade.blame", () => void blameHere()),
    vscode.commands.registerCommand("ade.history", () => void historyHere()),
    vscode.commands.registerCommand("ade.commitPanel", () => commitView.show()),
    vscode.commands.registerCommand("ade.stageAll", () => void stageAll()),
    vscode.commands.registerCommand("ade.unstageAll", () => void unstageAll()),
    vscode.commands.registerCommand(
      "ade.stage",
      (...args: unknown[]) => void scm?.stage(picked(args) as vscode.Uri[]),
    ),
    vscode.commands.registerCommand(
      "ade.unstage",
      (...args: unknown[]) => void scm?.unstage(picked(args) as vscode.Uri[]),
    ),
    vscode.commands.registerCommand("ade.createPR", () => void createPR()),
    vscode.commands.registerCommand(
      "ade.pruneWorktree",
      (node: SessionNode) =>
        void pruneWorktree(Sessions.treeOf(node) ?? treeOf(node)),
    ),
    vscode.commands.registerCommand("ade.openWorktree", (node: SessionNode) => {
      const path = Sessions.checkoutOf(node) ?? treeOf(node)?.path;
      if (path) void switchTo(path);
    }),
    vscode.commands.registerCommand("ade.worktrees", () => void pickWorktree()),
    vscode.commands.registerCommand(
      "ade.worktreeGraph",
      () => void worktreeGraph.show(),
    ),
    vscode.commands.registerCommand("ade.runJob", (row) => void runJob(row)),
    vscode.commands.registerCommand(
      "ade.pauseJob",
      (row) => void pauseJob(row, true),
    ),
    vscode.commands.registerCommand(
      "ade.resumeJob",
      (row) => void pauseJob(row, false),
    ),
    vscode.commands.registerCommand(
      "ade.deleteJob",
      (row) => void deleteJob(row),
    ),
    vscode.commands.registerCommand("ade.newJob", () => void newJob()),
    vscode.commands.registerCommand(
      "ade.refreshSessions",
      () => void reloadSessions(),
    ),
    vscode.commands.registerCommand("ade.showAllSessions", () =>
      sessions.showAll(),
    ),
    vscode.commands.registerCommand(
      "ade.switchSession",
      () => void switchSession(),
    ),
    vscode.commands.registerCommand(
      "ade.switchSessionHere",
      () => void switchSession(repoHere()),
    ),
    vscode.commands.registerCommand(
      "ade.findSession",
      () => void findSession(),
    ),
    vscode.commands.registerCommand("ade.collapseSessions", () =>
      sessions.collapse(),
    ),
    vscode.commands.registerCommand(
      "ade.previewDoc",
      (node: { resourceUri?: vscode.Uri }) =>
        node.resourceUri
          ? vscode.commands.executeCommand(
              "markdown.showPreviewToSide",
              node.resourceUri,
            )
          : undefined,
    ),
    vscode.commands.registerCommand(
      "ade.discussDoc",
      (node: { resourceUri?: vscode.Uri }) =>
        void discussDoc(node?.resourceUri),
    ),
    vscode.commands.registerCommand(
      "ade.openSession",
      (session: Session, machine?: string) => openSession(session, machine),
    ),
    vscode.commands.registerCommand("ade.reply", (reply: vscode.CommentReply) =>
      threads?.reply(reply),
    ),
    vscode.commands.registerCommand("ade.resolveWithAgent", (t: unknown) =>
      resolveWithAgent(t),
    ),
    vscode.commands.registerCommand("ade.markDone", (t: unknown) =>
      threads?.setResolved(t, true),
    ),
    vscode.commands.registerCommand("ade.todoFromThread", (t: unknown) =>
      todoFromThread(t),
    ),
    vscode.commands.registerCommand("ade.unfoldThread", (t: unknown) =>
      threads?.unfold(t, true),
    ),
    vscode.commands.registerCommand("ade.foldThread", (t: unknown) =>
      threads?.unfold(t, false),
    ),
    vscode.commands.registerCommand("ade.reopenThread", (t: unknown) =>
      threads?.setResolved(t, false),
    ),
    vscode.commands.registerCommand("ade.removeComment", (t: unknown) =>
      threads?.remove(t),
    ),
    vscode.commands.registerCommand(
      "ade.refreshComments",
      () => void (client && refresh(client)),
    ),
    vscode.commands.registerCommand("ade.convertForGitHub", (t: unknown) =>
      convertForGitHub(t),
    ),
    vscode.commands.registerCommand("ade.sendToGitHub", (t: unknown) =>
      sendToGitHub(t),
    ),
    vscode.commands.registerCommand("ade.resolveAll", () => void resolveAll()),
    vscode.commands.registerCommand(
      "ade.resolveHere",
      () => void resolveHere(),
    ),
    vscode.commands.registerCommand(
      "ade.pruneOrphans",
      () => void pruneOrphans(),
    ),
    vscode.commands.registerCommand(
      "ade.removeResolved",
      () => void removeThreads("resolved", done, false),
    ),
    vscode.commands.registerCommand(
      "ade.removeDeclined",
      () => void removeThreads("declined", refused, true),
    ),
  );

  // A wiki page is read, not edited: it lives in a clone ade owns, so it opens
  // under a scheme of ours rather than as a file somebody could save over.
  context.subscriptions.push(
    vscode.workspace.registerTextDocumentContentProvider(WIKI, {
      provideTextDocumentContent: async (uri) => {
        const launcher = await findLauncher();
        if (!launcher)
          return "The launcher is not running, so the wiki cannot be read.";
        return wikiPage(launcher, wikiPath(uri)).catch(
          (err) => `Could not read that page.\n\n${String(err)}`,
        );
      },
    }),
  );

  context.subscriptions.push(
    vscode.workspace.registerTextDocumentContentProvider(JOB, {
      provideTextDocumentContent: async (uri) => {
        const at = await findLauncher();
        if (!at)
          return "The launcher is not running, so the job cannot be read.";
        const name = jobNamed(uri.path);
        const got = await jobsOf(at).catch(() => undefined);
        const job = got?.jobs.find((j) => j.name === name);
        return job ? describeJob(job) : `There is no job called ${name} now.`;
      },
    }),
  );

  // A theme change repaints every open pane: the colours are baked into each
  // webview's html, so they only change when it is written again.
  context.subscriptions.push(
    vscode.window.onDidChangeActiveColorTheme(() => panels.serving(urlFor)),
  );

  // The editor runs the installed copy, so a rebuild is invisible until the
  // window reloads. No cli can ask for that; a uri this handler answers can.
  context.subscriptions.push(
    vscode.window.registerUriHandler({
      handleUri: (uri) => {
        if (uri.path === "/reload") void askToReload();
      },
    }),
  );

  watch.start();
  // Not awaited: starting it takes seconds, and nothing in activation needs it.
  void launcherUp();
  await connect(context);
}

// A uri is reachable from any page the browser is pointed at, and a reload
// throws away a typed chat draft and the images held with it. So it is asked.
async function askToReload(): Promise<void> {
  const said = await vscode.window.showInformationMessage(
    "ade: a new build is installed. Reload this window to run it?",
    "Reload",
  );
  if (said === "Reload") {
    await vscode.commands.executeCommand("workbench.action.reloadWindow");
  }
}

// live is the server the views point at, kept because a webview can be opened
// long after the connection was made.
let live: Server | undefined;
let launcher: Server | undefined;
let client: Client | undefined;
let logChannel: vscode.OutputChannel | undefined;
let logSeen = 0;

// The box remembers what it was asked, per workspace, so an empty box can
// offer it back and /again can rerun the last verb.
let boxContext: vscode.ExtensionContext | undefined;

function recalled(): Remembered[] {
  return boxContext?.workspaceState.get<Remembered[]>("ade.box.recent") ?? [];
}

function noteBox(one: Omit<Remembered, "at">): void {
  void boxContext?.workspaceState.update(
    "ade.box.recent",
    remember(recalled(), { ...one, at: Date.now() }),
  );
}

function boxVerbs(context: vscode.ExtensionContext): GroupedVerb[] {
  const manifest = context.extension.packageJSON as {
    contributes: { commands: (Verb & { icon?: string | { light: string } })[] };
  };
  const titled = new Map(
    manifest.contributes.commands.map((c) => [c.command, c]),
  );
  const out: GroupedVerb[] = [];
  for (const g of GROUPS) {
    for (const command of g.commands) {
      const c = titled.get(command);
      if (c)
        out.push({
          command,
          title: c.title,
          group: g.title,
          icon: typeof c.icon === "string" ? c.icon : undefined,
        });
    }
  }
  return out;
}

function repoHere(): string | undefined {
  const id = runState?.session;
  const row = id
    ? sessionsKept?.rows.find((r) => r.session.id === id)
    : undefined;
  return row?.repo ?? (live ? basename(live.root) : undefined);
}

// omnibox is what the editor's quick open hands over: a verb by name, or a
// turn for the agent. The verbs are the menu's, named as the palette does.
async function omnibox(
  text: unknown,
  context: vscode.ExtensionContext,
): Promise<void> {
  const verbs = boxVerbs(context);
  const went = route(typeof text === "string" ? text : "", verbs);
  if (went.kind === "focus") {
    await vscode.commands.executeCommand("ade.focusChat");
  } else if (went.kind === "run") {
    noteBox({ kind: "verb", text: went.command });
    await vscode.commands.executeCommand(went.command);
  } else if (went.kind === "note") {
    await noteDesign(went.text);
  } else if (went.kind === "go") {
    await goTo(went.name);
  } else if (went.kind === "again") {
    const last = recalled().find((r) => r.kind === "verb");
    if (last) await vscode.commands.executeCommand(last.text);
    else
      void vscode.window.showInformationMessage(
        "ade: nothing to run again yet.",
      );
  } else if (went.kind === "new") {
    noteBox({ kind: "turn", text: went.message });
    await newSession(undefined, went.message);
  } else if (went.kind === "pick") {
    const chosen = await pick(
      went.commands.map((command) => ({
        label: plain(
          verbs.find((v) => v.command === command)?.title ?? command,
        ),
        command,
      })),
      { title: "ade" },
    );
    if (chosen) {
      noteBox({ kind: "verb", text: chosen.command });
      await vscode.commands.executeCommand(chosen.command);
    }
  } else if (!client || !live) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
  } else if (isQuestion(went.message)) {
    await quickAnswer(went.message);
  } else {
    noteBox({ kind: "turn", text: went.message });
    try {
      await client.say(went.message);
    } catch (err) {
      void vscode.window.showErrorMessage(`ade: ${String(err)}`);
      return;
    }
    void chat.reveal(live);
  }
}

// quickAnswer is a question answered without a turn: a fork reads and
// replies, and the reply is a notification with the chat one click away.
async function quickAnswer(question: string): Promise<void> {
  if (!client || !live) return;
  const here = client;
  const got = await vscode.window.withProgress(
    { location: vscode.ProgressLocation.Notification, title: "ade: thinking…" },
    () =>
      here
        .answer(question)
        .catch((err) => ({ answer: `could not answer: ${oneLine(err)}` })),
  );
  if (!got.answer.startsWith("could not answer"))
    noteBox({ kind: "ask", text: question, answer: got.answer });
  const said = await vscode.window.showInformationMessage(
    got.answer,
    "Ask in the chat",
  );
  if (said && live) {
    await here.say(question).catch(() => undefined);
    void chat.reveal(live);
  }
}

function oneLine(err: unknown): string {
  return String(err)
    .replace(/^Error:\s*/, "")
    .split("\n")[0]
    .trim();
}

async function omniboxAnswer(text: unknown): Promise<{ answer: string }> {
  const question = typeof text === "string" ? text.trim() : "";
  if (!question) return { answer: "" };
  if (!client) return { answer: "nothing is running for this folder yet" };
  const got = await client
    .answer(question)
    .catch((err) => ({ answer: `could not answer: ${oneLine(err)}` }));
  if (!got.answer.startsWith("could not answer"))
    noteBox({ kind: "ask", text: question, answer: got.answer });
  return got;
}

// noteDesign drops a line at the end of the session's design document, dated,
// without leaving the box. Nothing is open: say so, rather than invent a doc.
async function noteDesign(text: string): Promise<void> {
  const file = driftTree.file;
  if (!design.docs || !file) {
    void vscode.window.showInformationMessage(
      "ade: no design document is open for this session, so there is nowhere to note that.",
    );
    return;
  }
  const uri = vscode.Uri.file(join(design.docs, file));
  const had = Buffer.from(await vscode.workspace.fs.readFile(uri)).toString(
    "utf8",
  );
  const line = `- ${new Date().toISOString().slice(0, 10)}, from the box: ${text}\n`;
  const next =
    had.endsWith("\n") || had === "" ? had + line : had + "\n" + line;
  await vscode.workspace.fs.writeFile(uri, Buffer.from(next, "utf8"));
  vscode.window.setStatusBarMessage(`ade: noted in ${file}`, 4000);
}

// goTo switches to the one session a name picks out, or asks among the few.
async function goTo(name: string): Promise<void> {
  if (!sessionsKept) await keepSessions();
  const rows = (sessionsKept?.rows ?? []).filter(
    (r) =>
      r.session.id !== runState?.session &&
      matches(
        name,
        `${r.session.title ?? ""} ${r.session.branch} ${r.session.name} ${r.repo}`,
      ),
  );
  if (rows.length === 1) {
    await openSession(rows[0].session);
    return;
  }
  if (rows.length === 0) {
    void vscode.window.showInformationMessage(
      `ade: no session matches "${name}".`,
    );
    return;
  }
  const picked = await pick(
    rows.map((r) => ({ label: sessionLabel(r), description: foundIn(r), r })),
    { title: `Which "${name}"?` },
  );
  if (picked) await openSession(picked.r.session);
}

function pending() {
  return {
    sessions: sessionsKept?.rows ?? [],
    offers: inbox.all,
    waiting: runState?.waiting ?? [],
    asking: runState?.asking?.length ?? 0,
    hereId: runState?.session,
  };
}

// openWell is the bell: the desk first, then everything waiting on you.
async function openWell(): Promise<void> {
  const rows = wellRows(pending());
  const picked = await pick(
    [
      ...rows.map((r) => ({
        label: `$(${r.icon ?? "circle-outline"}) ${r.label}`,
        description: r.description,
        command: r.command,
        args: r.args ?? [],
      })),
      // Under a separator: the title counts what is waiting, and an action
      // listed among those rows is counted by the eye but not by the title.
      {
        label: "",
        kind: vscode.QuickPickItemKind.Separator,
        command: "",
        args: [] as unknown[],
      },
      {
        label: "$(bell) Open the concierge",
        description: "the front desk",
        command: "ade.concierge",
        args: [] as unknown[],
      },
    ],
    {
      title: rows.length
        ? `${rows.length} waiting on you`
        : "Nothing is waiting on you",
    },
  );
  if (picked)
    await vscode.commands.executeCommand(picked.command, ...picked.args);
}

let wellShown = -1;

function updateWell(): void {
  const n = live ? wellCount(pending()) : 0;
  if (n === wellShown) return;
  wellShown = n;
  void vscode.commands.executeCommand("setContext", "ade.wellCount", n);
  if (titleRefused) return;
  void vscode.commands
    .executeCommand("workbench.titleBar.setStatus", {
      id: "ade.well",
      text: n ? `$(bell-dot) ${n}` : undefined,
      tooltip:
        n === 1
          ? "one thing is waiting on you"
          : `${n} things are waiting on you`,
      warn: true,
      command: "ade.well",
    })
    .then(undefined, () => (titleRefused = true));
}

// boardNow gathers the dashboard: the launcher's dash, this repository's pull
// requests, the sessions, the well, and what the agent is doing.
async function boardNow(): Promise<Board> {
  const at = launcher ?? (await ensureLauncher());
  const dash = at ? await dashOf(at).catch(() => undefined) : undefined;
  if (!sessionsKept) await keepSessions();
  const prs = client ? await client.prs().catch(() => [] as Listed[]) : [];
  await desk.refresh().catch(() => undefined);
  const now = new Date();
  const badge = live
    ? runBadge(
        runState,
        since(runState, Date.now()),
        quiet(runState, Date.now()),
      )
    : undefined;
  return {
    waiting: wellRows(pending()),
    reviews: dash?.reviews ?? [],
    notifications: dash?.notifications ?? [],
    tickets: dash?.linear?.issues ?? [],
    ticketsError: dash?.linear?.error,
    started: dash?.started ?? {},
    mine: prs.filter((p) => p.mine),
    sessions: shortlist(sessionsKept?.rows ?? [], [], live?.root).slice(0, 8),
    todos: desk.list
      .filter((t) => !doneTodo(t))
      .slice(0, 10)
      .map((t) => ({
        id: t.id,
        text: t.text,
        meta: describeTodo(t, now),
        icon: todoIcon(t, now),
        started: t.started,
      })),
    agent: badge?.text.replace(/\$\([^)]*\)\s*/g, ""),
    at: Date.now(),
  };
}

async function boardAct(what: Do): Promise<string | void> {
  const at = launcher ?? (await ensureLauncher());
  switch (what.what) {
    case "refresh":
      if (at) await refreshDash(at);
      return;
    case "row":
      await vscode.commands.executeCommand(
        String(what.command),
        ...((what.args as unknown[]) ?? []),
      );
      return;
    case "open-url":
      await vscode.env.openExternal(vscode.Uri.parse(String(what.url)));
      return;
    case "open-pr":
      await openPR(Number(what.number));
      return;
    case "session": {
      const row = sessionsKept?.rows.find((r) => r.session.id === what.id);
      if (row) await openSession(row.session);
      return;
    }
    case "mute":
      if (at)
        await muteReview(at, String(what.repo), Number(what.number), true);
      return `muted ${what.repo}#${what.number}`;
    case "dismiss":
      if (at) await dismissNotification(at, String(what.id));
      return;
    case "open-review": {
      if (!at) return "the launcher is not running";
      const got = await openReview(at, String(what.repo), Number(what.number));
      if (got.path) {
        await vscode.commands.executeCommand(
          "vscode.openFolder",
          vscode.Uri.file(got.path),
          { forceNewWindow: true },
        );
      }
      return;
    }
    case "todo": {
      const item = desk.list.find((t) => t.id === what.id);
      if (item) await todoActions(item);
      return;
    }
    case "todo-done": {
      const item = desk.list.find((t) => t.id === what.id);
      if (!item) return;
      await patchTodo(item, { done: true });
      return `done · ${item.text}`;
    }
    case "start-ticket": {
      if (!at || !live) return "open a checkout first";
      const got = await startTicket(at, String(what.identifier), live.root);
      if (got.path) await switchTo(got.path);
      return `started ${what.identifier}`;
    }
  }
}

async function reopen(one: Remembered | undefined): Promise<void> {
  if (!one) return;
  if (one.kind === "verb") {
    await vscode.commands.executeCommand(one.text);
  } else if (one.kind === "ask") {
    const said = await vscode.window.showInformationMessage(
      one.answer ?? one.text,
      "Ask again",
      "Ask in the chat",
    );
    if (said === "Ask again") await quickAnswer(one.text);
    else if (said && client && live) {
      await client.say(one.text).catch(() => undefined);
      void chat.reveal(live);
    }
  } else {
    const said = await vscode.window.showInformationMessage(
      one.text,
      "Send again",
    );
    if (said && client && live) {
      await client.say(one.text).catch(() => undefined);
      void chat.reveal(live);
    }
  }
}

// diffTurn opens what the agent changed this turn, every file at once; with
// nothing noted (a Bash edit says no file), it is what is uncommitted.
async function diffTurn(): Promise<void> {
  const noted = runState?.touched ?? [];
  const touched = noted.length
    ? noted
    : [
        ...new Set([
          ...(lastWorktree?.staged ?? []),
          ...(lastWorktree?.unstaged ?? []).map((c) => c.path),
        ]),
      ];
  if (!live || touched.length === 0) {
    void vscode.window.showInformationMessage(
      "ade: nothing has changed since the last commit.",
    );
    return;
  }
  const root = live.root;
  await vscode.commands.executeCommand(
    "vscode.changes",
    "What the agent changed",
    touched.map((file) => {
      const uri = vscode.Uri.file(join(root, file));
      return [uri, blobUri(file, "left"), uri];
    }),
  );
}

// omniboxRows is what quick open shows above the files while you type. The
// session list is a launcher round trip, so it is kept between keystrokes.
let sessionsKept: { at: number; rows: Row[] } | undefined;
let keepingSessions: Promise<void> | undefined;
let repoKept: { at: number; trees: Tree[]; pulls: Listed[] } | undefined;

// keepRepo is the same for what the session's own server knows: its
// worktrees and the repository's pull requests.
async function keepRepo(): Promise<void> {
  if (!client) return;
  repoKept = {
    at: Date.now(),
    trees: repoKept?.trees ?? [],
    pulls: repoKept?.pulls ?? [],
  };
  const kept = repoKept;
  await Promise.all([
    worktreeTree.refresh().then(
      (found) => {
        kept.trees = [...(found.main ? [found.main] : []), ...found.linked];
      },
      () => undefined,
    ),
    client.prs().then(
      (pulls) => (kept.pulls = pulls),
      () => undefined,
    ),
  ]);
}

function keepSessions(): Promise<void> {
  keepingSessions =
    keepingSessions ??
    sessionRows()
      .then((got) => {
        sessionsKept = { at: Date.now(), rows: got?.all ?? [] };
        titleSession();
        updateWell();
      })
      .catch(() => undefined)
      .finally(() => (keepingSessions = undefined));
  return keepingSessions;
}

async function omniboxRows(text: unknown) {
  const query = typeof text === "string" ? text.trim() : "";
  if (!query || query.startsWith("/")) return [];
  // Never waited for: the editor gives these rows a short budget, and a
  // launcher round trip or a worktree scan would blow it on the first key.
  if (!sessionsKept || Date.now() - sessionsKept.at > 10_000)
    void keepSessions();
  if (!repoKept || Date.now() - repoKept.at > 30_000) void keepRepo();
  if (Date.now() - designsAt > 30_000) {
    designsAt = Date.now();
    void design.refresh().catch(() => undefined);
  }
  const verbs = boxContext ? verbRowsFor(query, boxVerbs(boxContext)) : [];
  return [
    ...verbs,
    ...everyRow(query, {
      sessions: sessionsKept?.rows ?? [],
      threads: threads?.all ?? [],
      trees: repoKept?.trees ?? [],
      pulls: repoKept?.pulls ?? [],
      offers: inbox.all,
      waiting: runState?.waiting ?? [],
      designs: design.files,
      wiki: await wikiWithin(query, 300),
      hereId: runState?.session,
      herePath: live?.root,
    }),
  ];
}

let designsAt = 0;
let wikiKept: { q: string; hits: Hit[] } = { q: "", hits: [] };

// wikiWithin is the wiki's answer for this query if it comes within the
// budget, else the last one that did: the box must not wait on a search.
async function wikiWithin(q: string, ms: number): Promise<Hit[]> {
  if (!launcher || q.length < 3) return [];
  if (wikiKept.q === q) return wikiKept.hits;
  const search = wikiSearch(launcher, q)
    .then((hits) => {
      wikiKept = { q, hits };
      return hits;
    })
    .catch(() => [] as Hit[]);
  const late = new Promise<Hit[]>((ok) =>
    setTimeout(() => ok(wikiKept.hits), ms),
  );
  return Promise.race([search, late]);
}

async function omniboxOpen(what: unknown): Promise<void> {
  const it = (what ?? {}) as {
    kind?: string;
    session?: Session;
    file?: string;
    line?: number;
    path?: string;
    number?: number;
    offer?: BusOffer;
    index?: number;
  };
  if (it.kind === "session" && it.session) {
    await openSession(it.session);
  } else if (it.kind === "worktree" && it.path) {
    await switchTo(it.path);
  } else if (it.kind === "pr" && it.number) {
    await openPR(it.number);
  } else if (it.kind === "ask" && it.offer) {
    await answerAsk(it.offer);
  } else if (it.kind === "review" && it.offer) {
    await takeReview(it.offer);
  } else if (it.kind === "queue") {
    await vscode.commands.executeCommand("ade.showQueue");
  } else if (it.kind === "recent") {
    await reopen(recalled()[Number(it.index ?? -1)]);
  } else if (it.kind === "design" && it.file && design.docs) {
    await vscode.commands.executeCommand(
      "vscode.open",
      vscode.Uri.file(join(design.docs, it.file)),
    );
  } else if (it.kind === "wiki" && it.path) {
    const doc = await vscode.workspace.openTextDocument(
      vscode.Uri.from({ scheme: WIKI, path: "/" + it.path }),
    );
    const at = new vscode.Position(Math.max(0, (it.line ?? 1) - 1), 0);
    await vscode.window.showTextDocument(doc, {
      selection: new vscode.Range(at, at),
    });
  } else if (it.kind === "thread" && it.file && live) {
    const at = new vscode.Position(Math.max(0, (it.line ?? 1) - 1), 0);
    await vscode.window.showTextDocument(
      vscode.Uri.file(join(live.root, it.file)),
      { selection: new vscode.Range(at, at) },
    );
  }
}

// ask puts the selection on your next message rather than sending it. The
// question is still yours to type; this only saves retyping where it is.
async function ask(): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  if (!editor || !client || !live) return;
  const at = editor.selection;
  const text = editor.document.getText(
    at.isEmpty ? editor.document.lineAt(at.start).range : at,
  );
  if (!text.trim()) return;

  try {
    await client.attach(
      where(
        live.root,
        editor.document.uri.fsPath,
        at.start.line + 1,
        at.end.line + 1,
      ),
      trimIndent(text),
    );
  } catch (err) {
    void vscode.window.showErrorMessage(
      `ade: could not attach that (${String(err)})`,
    );
    return;
  }
  void chat.reveal(live);
}

// verb is a palette entry that is one POST: do it, or say why not. Nothing
// here reads the reply — the event stream redraws whatever it changed.
// toggleSandbox moves the session in or out of the container. The server
// refuses while a turn is in flight: the flip would take the turn with it.
async function toggleSandbox(): Promise<void> {
  if (!client) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  const on = !runState?.sandbox;
  try {
    await client.sandbox(on);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${reason(err)}`);
    return;
  }
  vscode.window.setStatusBarMessage(
    on ? "ade: sandbox on" : "ade: sandbox off",
    3000,
  );
  await refresh(client);
}

async function verb(
  said: string,
  call: (client: Client) => Promise<unknown>,
): Promise<void> {
  if (!client) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  try {
    await call(client);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  vscode.window.setStatusBarMessage(`ade: ${said}`, 3000);
}

// discussDoc puts the document in one column and the conversation in the next.
// A design document carries no comments, so the chat is where it is discussed.
async function discussDoc(uri: vscode.Uri | undefined): Promise<void> {
  if (uri) {
    await vscode.commands.executeCommand("vscode.open", uri, {
      viewColumn: vscode.ViewColumn.One,
      preview: false,
    });
  }
  await chat.reveal(live);
}

// searchWiki asks the launcher and opens the Markdown of what you pick. The
// search itself is a pick, not a page: a list of hits is what a pick is for.
async function searchWiki(): Promise<void> {
  const launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const q = await input({
    title: "Search the shared wiki",
    placeHolder: "local network, worker, sandbox…",
  });
  if (!q?.trim()) return;

  let hits: Hit[];
  try {
    hits = await wikiSearch(launcher, q);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  if (hits.length === 0) {
    void vscode.window.showInformationMessage(
      `ade: nothing in the wiki matches "${q}".`,
    );
    return;
  }

  const picked = await pick(
    hits.map((hit) => ({ ...wikiPick(hit), hit })),
    { title: `${hits.length} in the wiki`, matchOnDetail: true },
  );
  if (!picked) return;

  const doc = await vscode.workspace.openTextDocument(
    vscode.Uri.from({ scheme: WIKI, path: "/" + picked.hit.path }),
  );
  await vscode.languages.setTextDocumentLanguage(doc, "markdown");
  const editor = await vscode.window.showTextDocument(doc, { preview: true });
  // The hit named a line, and opening the page at the top hides why it matched.
  const at = new vscode.Position(Math.max(0, picked.hit.line - 1), 0);
  editor.revealRange(
    new vscode.Range(at, at),
    vscode.TextEditorRevealType.InCenter,
  );
  editor.selection = new vscode.Selection(at, at);
}

// pickModel changes what the conversation runs on. The catalogue is the
// server's, so the list is the same one the web picker offers.
async function pickModel(): Promise<void> {
  if (!live || !client) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  let list;
  let now: RunState = {} as RunState;
  try {
    list = await models(live);
    now = (await client.state()).agent ?? now;
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  const picked = await pick(
    list.map((m) => ({ ...modelPick(m), id: m.id })),
    { title: "Run this conversation on which model?" },
  );
  if (!picked) return;
  const going = move(list, now, picked.id);
  if (going.kind === "none") return;
  if (going.kind === "model") {
    await verb(`model: ${picked.label}`, (c) => c.setModel(going.model));
    return;
  }
  const said = await vscode.window.showWarningMessage(
    warning(going.provider),
    { modal: true },
    "Continue there",
  );
  if (said !== "Continue there") return;
  await verb(`model: ${picked.label}`, (c) =>
    c.switchAgent(going.provider, going.model),
  );
}

// The first message is required: without one there is nothing to tell the
// new session from the one it replaced.
async function newChat(): Promise<void> {
  if (!client) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  const message = await firstMessage();
  if (!message) return;
  await newChatHere(message);
}

// clearChat empties the conversation in place. Unlike newChat it asks for
// nothing: the agent and model stay as they are.
async function clearChat(): Promise<void> {
  await verb("conversation cleared", (c) => c.clearChat());
}

function firstMessage(): Thenable<string | undefined> {
  return input({
    title: "New session",
    prompt: "What do you want to do?",
    placeHolder:
      "Add rate limiting to the webhook handler, with a test for the 429 path.",
  });
}

// exposePort forwards a port from the machine a session runs on to this one.
// Not VS Code's own forwarding: that is for a remote window, this is a worker.
async function exposePort(): Promise<void> {
  const launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const said = await input({
    title: "Expose which port?",
    placeHolder: "5173",
    validateInput: (value) =>
      readPort(value) ? undefined : "a port between 1 and 65535",
  });
  const which = said ? readPort(said) : undefined;
  if (!which) return;
  try {
    await expose(launcher, which);
    vscode.window.setStatusBarMessage(`ade: ${which} exposed`, 3000);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

async function openSettings(): Promise<void> {
  if (!live) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  await settingsPage.show(live);
}

// pr is the pull request for this branch, or nothing with a reason said.
async function pr(): Promise<BranchPR | undefined> {
  if (!client) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return undefined;
  }
  const found = (await client.state().catch(() => undefined))?.branchPr;
  if (!found) {
    void vscode.window.showWarningMessage(
      "ade: this branch has no pull request.",
    );
    return undefined;
  }
  return found;
}

// verdict posts a review as you. Public, not quietly undoable and in your
// name, so it is confirmed here as well as being confirmed by the server.
async function verdict(kind: "approve" | "request-changes"): Promise<void> {
  const found = await pr();
  if (!found || !client) return;
  const why = blocked(found, kind);
  if (why) {
    void vscode.window.showWarningMessage(
      `ade: #${found.number} cannot be ${kind}d: ${why}`,
    );
    return;
  }

  // GitHub will not take changes-requested without a body, and an approval
  // reads better with one.
  const body = await input({
    title: `${kind === "approve" ? "Approve" : "Request changes on"} ${prLabel(found)}`,
    prompt:
      kind === "approve" ? "A note, if you want one" : "What needs changing?",
    validateInput: (value) =>
      kind === "approve" || value.trim()
        ? undefined
        : "GitHub needs a reason for this one",
  });
  if (body === undefined) return;

  const said = await vscode.window.showWarningMessage(
    `Post this on ${prLabel(found)} as you?`,
    { modal: true, detail: found.url },
    kind === "approve" ? "Approve" : "Request changes",
  );
  if (!said) return;

  const call =
    kind === "approve"
      ? (c: Client) => c.approve(found.number, body)
      : (c: Client) => c.requestChanges(found.number, body);
  await verb(
    `#${found.number} ${kind === "approve" ? "approved" : "sent back"}`,
    call,
  );
}

async function merge(): Promise<void> {
  const found = await pr();
  if (!found) return;
  const why = blocked(found, "merge");
  if (why) {
    void vscode.window.showWarningMessage(
      `ade: #${found.number} cannot be merged: ${why}`,
    );
    return;
  }
  const method = await pick([...mergeMethods], {
    title: `Merge ${prLabel(found)} how?`,
  });
  if (!method) return;
  const said = await vscode.window.showWarningMessage(
    `Merge ${prLabel(found)}?`,
    { modal: true, detail: found.url },
    "Merge",
  );
  if (said !== "Merge") return;
  await verb(`#${found.number} merged`, (c) => c.merge(method));
}

// The scope is what a review reads, and it was invisible from the editor:
// "review the diff" could quietly mean the uncommitted half of one.
async function setScope(): Promise<void> {
  if (!client) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  const meta = (await client.state().catch(() => undefined))?.meta;
  if (!scopeable(meta)) {
    void vscode.window.showWarningMessage(
      "ade: a fetched snapshot has only the one view.",
    );
    return;
  }
  const picked = await pick(
    SCOPES.map((s) => ({
      label: s.label,
      description: s.detail,
      id: s.id,
      picked: s.id === meta?.scope,
    })),
    { title: `Diff ${meta?.branch ?? "this session"} at what scope?` },
  );
  if (!picked) return;
  await verb(`scope: ${picked.label.toLowerCase()}`, (c) =>
    c.setScope(picked.id),
  );
}

// scopeTo is the Diff view's own switch: three buttons rather than a pick,
// because switching between them is the thing done most often here.
async function scopeTo(scope: string): Promise<void> {
  const meta = (await client?.state().catch(() => undefined))?.meta;
  if (!scopeable(meta)) {
    void vscode.window.showWarningMessage(
      "ade: a fetched snapshot has only the one view.",
    );
    return;
  }
  await verb(`scope: ${scopeLabel(scope).toLowerCase()}`, (c) =>
    c.setScope(scope),
  );
  await drawScope(scope);
  await changes.refresh();
}

// A context key, so the button for the scope you are on hides itself rather
// than sitting there doing nothing.
async function drawScope(scope: string | undefined): Promise<void> {
  await vscode.commands.executeCommand(
    "setContext",
    "ade.scope",
    scope ?? "branch",
  );
}

// Whether an ade is serving this folder, for the menu entries whose only answer
// without one is an error message.
async function drawLive(on: boolean): Promise<void> {
  await vscode.commands.executeCommand("setContext", "ade.live", on);
}

// review reads whatever the session is scoped to, so the pick says what that
// is. branch forces the whole branch first, which is the usual intent.
// resolveAll hands every open comment to the agent, as the diff's own button
// does. Confirmed with the count: it starts a run that edits the checkout.
async function resolveAll(): Promise<void> {
  if (!client) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  let open = 0;
  try {
    open = openCount((await client.state()).threads ?? []);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  if (open === 0) {
    void vscode.window.showInformationMessage(
      "ade: no open comments to resolve.",
    );
    return;
  }
  const yes = await vscode.window.showWarningMessage(
    `Send ${open} open comment(s) to the agent?`,
    { modal: true, detail: "It reads each one and changes the code." },
    "Resolve all",
  );
  if (yes !== "Resolve all") return;
  await verb(`resolving ${open} comment(s)`, (c) => c.resolveAll());
}

// rel is the path ade names the open file by, or undefined when it is outside
// the checkout being served.
function rel(uri: vscode.Uri): string | undefined {
  if (uri.scheme !== "file") return undefined;
  const path = vscode.workspace.asRelativePath(uri, false);
  return path.startsWith("..") ? undefined : path;
}

// blameHere is the commit the cursor's line last came from. The cursor is the
// argument, as it is for resolveHere and ask.
async function blameHere(): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  const file = editor && rel(editor.document.uri);
  if (!client || !editor || !file) {
    void vscode.window.showErrorMessage(
      "ade: open a file in this checkout first.",
    );
    return;
  }

  const line = editor.selection.active.line + 1;
  let blame;
  try {
    blame = await client.blame(file, line);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  if (!blame.committed) {
    void vscode.window.showInformationMessage(
      `ade: ${file}:${line} is not committed yet.`,
    );
    return;
  }

  const when = blameWhen(blame.at);
  const picked = await pick(
    [
      {
        label: `Copy ${blame.short}`,
        description: [blame.author, when].filter(Boolean).join(" · "),
        detail: blame.summary,
        id: "copy",
      },
      { label: `Open ${file} at ${blame.short}`, id: "open" },
      { label: `Ask about ${blame.short}`, id: "ask" },
    ],
    { title: `${file}:${line}` },
  );
  if (!picked) return;

  if (picked.id === "copy") {
    await vscode.env.clipboard.writeText(blame.sha);
    void vscode.window.showInformationMessage(`ade: copied ${blame.short}.`);
    return;
  }
  if (picked.id === "open") {
    await openAt(blame.short, file, line);
    return;
  }
  try {
    await client.say(
      `Tell me about commit ${blame.sha} ("${blame.summary}"), which last touched ${file}:${line}.`,
    );
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  if (live) void chat.reveal(live);
}

// historyHere is the commits that touched the open file, newest first.
// Choosing one opens the file as that commit left it.
async function historyHere(): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  const file = editor && rel(editor.document.uri);
  if (!client || !editor || !file) {
    void vscode.window.showErrorMessage(
      "ade: open a file in this checkout first.",
    );
    return;
  }

  let past;
  try {
    past = await client.history(file);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  if (past.length === 0) {
    void vscode.window.showInformationMessage(
      `ade: nothing in the history has touched ${file}.`,
    );
    return;
  }

  const picked = await pick(
    past.map((commit) => ({
      label: commit.subject,
      // The rename is the thing you would not otherwise see, so it is said.
      description: [
        commit.sha.slice(0, 7),
        commit.author,
        blameWhen(commit.at),
        commit.path === file ? "" : `as ${commit.path}`,
      ]
        .filter(Boolean)
        .join(" · "),
      commit,
    })),
    { title: `History of ${file}`, matchOnDescription: true },
  );
  if (!picked) return;
  await openAt(picked.commit.sha.slice(0, 7), picked.commit.path);
}

// openAt shows a file as one commit left it, read-only because a commit is.
// The line is the cursor's, so the same place is on screen in both.
async function openAt(sha: string, path: string, line?: number): Promise<void> {
  const uri = vscode.Uri.from({ scheme: REV, ...revUri(sha, path) });
  try {
    const doc = await vscode.workspace.openTextDocument(uri);
    const at = Math.min((line ?? 1) - 1, Math.max(doc.lineCount - 1, 0));
    await vscode.window.showTextDocument(doc, {
      preview: true,
      selection: new vscode.Range(at, 0, at, 0),
    });
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// resolveHere resolves the comments under the cursor, falling back to the
// file's. It takes no argument: the editor is the argument.
async function resolveHere(): Promise<void> {
  const editor = vscode.window.activeTextEditor;
  const file = editor && rel(editor.document.uri);
  if (!client || !editor || !file) {
    void vscode.window.showErrorMessage(
      "ade: open a file in this checkout first.",
    );
    return;
  }

  let threads: Pinned[] = [];
  try {
    threads = ((await client.state()).threads ?? []) as Pinned[];
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }

  const line = editor.selection.active.line + 1;
  let wanted = at(threads, file, line);
  let what = `${file}:${line}`;
  if (wanted.length === 0) {
    wanted = inFile(threads, file);
    what = file;
  }
  if (wanted.length === 0) {
    void vscode.window.showInformationMessage(
      `ade: no open comments in ${file}.`,
    );
    return;
  }

  await verb(`resolved ${wanted.length} comment(s) on ${what}`, async (c) => {
    for (const thread of wanted) await c.resolve(thread.id, true);
  });
  await refresh(client);
}

const pruneOrphans = () => removeThreads("orphaned", orphans, true);

// removeThreads deletes a named set of comments, now rather than waiting on
// whatever would clear them by itself. ask is for the sets you might still
// want: a resolved comment has been dealt with, an orphan has not.
async function removeThreads(
  what: string,
  pick: (threads: Thread[]) => Pinned[],
  ask: boolean,
): Promise<void> {
  if (!client) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }

  let gone: Pinned[] = [];
  try {
    gone = pick((await client.state()).threads ?? []);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  if (gone.length === 0) {
    void vscode.window.showInformationMessage(
      `ade: no ${what} comments of ours to remove.`,
    );
    return;
  }

  if (ask) {
    const yes = await vscode.window.showWarningMessage(
      `Delete ${gone.length} ${what} comment(s)?`,
      {
        modal: true,
        detail: `This cannot be undone.\n\n${listed(
          gone.map((t) => `${t.file}:${t.line}`),
        )}`,
      },
      "Delete",
    );
    if (yes !== "Delete") return;
  }

  await verb(`deleted ${gone.length} ${what} comment(s)`, async (c) => {
    for (const thread of gone) await c.deleteThread(thread.id);
  });
  await refresh(client);
}

// resolveWithAgent hands one thread to the agent to act on, which is what
// "resolve" means in ade. Mark done is the other thing: closing it yourself.
async function resolveWithAgent(arg: unknown): Promise<void> {
  const id = threads?.threadId(arg);
  if (!id) {
    void vscode.window.showErrorMessage("ade: no comment to resolve.");
    return;
  }
  await verb("sent it to the agent", (c) => c.resolveAll([id]));
  void chat.reveal(live);
}

// convertForGitHub rewrites one finding as something you would say to the
// author. Nothing is sent; the words arrive on a later state as the outgoing.
async function convertForGitHub(arg: unknown): Promise<void> {
  const id = threads?.threadId(arg);
  if (!id) {
    void vscode.window.showErrorMessage("ade: no comment to convert.");
    return;
  }
  await verb("rewriting it for the author", (c) => c.convert(id));
}

// sendToGitHub posts the rewritten words on the pull request. Confirmed: it
// leaves a review comment under your name and cannot be taken back.
async function sendToGitHub(arg: unknown): Promise<void> {
  const id = threads?.threadId(arg);
  const pr = threads?.branchPR;
  if (!id || !pr) {
    void vscode.window.showErrorMessage(
      "ade: nothing to send, or no pull request for this branch.",
    );
    return;
  }
  const yes = await vscode.window.showWarningMessage(
    `Post this on #${pr.number}?`,
    { modal: true, detail: "It goes up under your name and cannot be undone." },
    "Post",
  );
  if (yes !== "Post") return;
  await verb(`sent to #${pr.number}`, (c) => c.sendToGitHub(id, pr.number));
  if (client) await refresh(client);
}

async function review(branch: boolean, named = false): Promise<void> {
  if (!client) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  let meta = (await client.state().catch(() => undefined))?.meta;

  if (branch && meta?.scope !== "branch") {
    if (!scopeable(meta)) {
      void vscode.window.showWarningMessage(
        "ade: a fetched snapshot has only the one view.",
      );
      return;
    }
    try {
      await client.setScope("branch");
      meta = (await client.state().catch(() => undefined))?.meta;
    } catch (err) {
      void vscode.window.showErrorMessage(`ade: ${String(err)}`);
      return;
    }
  }

  if (empty(meta)) {
    void vscode.window.showWarningMessage(nothing(meta));
    return;
  }

  const who = await pickReviewers(meta, named);
  if (!who) return;

  // A level is only read when no agent is named, so it is only asked for then.
  if (!levelled(who)) {
    const said = who.map((n) => `@${n}`).join(" ");
    await verb(`${said} reviewing ${what(meta)}`, (c) => c.review("", who));
    return;
  }

  const level = await pick(
    LEVELS.map((l) => ({ label: l.id, description: l.detail })),
    { title: `Review ${what(meta)} at what level?` },
  );
  if (!level) return;

  await verb(`reviewing ${what(meta)}`, (c) => c.review(level.label, []));
}

async function commit(amend: boolean): Promise<void> {
  if (!scm) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  if (await scm.commit(amend))
    vscode.window.setStatusBarMessage("ade: committed", 3000);
}

// ade.gitRoots puts ade's buttons on Git's row for the session's root.
async function indexFor(
  c: Client,
  root: string,
  elsewhere = false,
): Promise<Index> {
  const repo = elsewhere ? undefined : await gitRepository(root);
  const roots = repo ? [repo.rootUri.toString()] : [];
  await vscode.commands.executeCommand("setContext", "ade.gitRoots", roots);
  return repo ? new GitIndex(repo, root) : new AdeIndex(c, root);
}

// The two buttons the Diff view needs beside the scope. Everything or
// nothing: a single file is a gesture on its row, which the SCM view has.
async function stageAll(): Promise<void> {
  if (!scm || scm.changedCount === 0) {
    void vscode.window.showInformationMessage("ade: nothing left to stage.");
    return;
  }
  await scm.stageAll();
}

async function unstageAll(): Promise<void> {
  if (!scm || scm.stagedCount === 0) {
    void vscode.window.showInformationMessage("ade: nothing is staged.");
    return;
  }
  await scm.unstageAll();
}

// createPR is one POST and a confirmation, because it pushes and posts.
async function createPR(): Promise<void> {
  const said = await vscode.window.showWarningMessage(
    "Open a draft pull request for this branch?",
    {
      modal: true,
      detail:
        "The agent writes the description from your commits, pushes the branch, and opens it as a draft.",
    },
    "Create draft PR",
  );
  if (said !== "Create draft PR") return;
  await verb("opening a draft pull request", (c) => c.createPR());
}

// Clears the conversation and builds from the open design document. Picks
// rather than a form: three answers, each a list the server will accept.
async function build(name?: string): Promise<void> {
  if (!client || !live) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  const file = name ?? (await pickDoc());
  if (!file) return;
  const state = await client.openDoc(file).catch(() => undefined);
  if (!state) {
    void vscode.window.showErrorMessage(
      `ade: ${file} is not a design document here.`,
    );
    return;
  }

  const at = await findLauncher();
  const hosts = at ? await hostsOf(at).catch(() => []) : [];
  const now = await client.state().catch(() => undefined);
  const meta = now?.meta;
  const where = await pick(
    wheres(
      meta?.branch,
      hosts,
      meta?.mode === "pr snapshot",
      now?.design?.worktree,
    ).map((w) => ({
      label: w.label,
      description: w.detail,
      id: w.id,
      machine: w.machine,
    })),
    { title: `Build ${state.title || file} where?` },
  );
  if (!where) return;

  const agent = await pick(
    AGENTS.map((a) => ({ label: a.label, id: a.id })),
    { title: "Which agent?" },
  );
  if (!agent) return;

  const offered = await models(live).catch(() => []);
  const model = await pick(
    modelsFor(offered, agent.id).map((m) => ({ label: m.label, id: m.id })),
    { title: `Which ${agent.label} model?` },
  );
  if (!model) return;

  const said = says(
    await client.estimate(agent.id, model.id).catch(() => undefined),
  );
  const go = await vscode.window.showWarningMessage(
    `Build ${state.title || file} on ${where.label}?`,
    {
      modal: true,
      detail: [said, "The conversation is cleared. The document is the brief."]
        .filter(Boolean)
        .join("\n\n"),
    },
    "Build",
  );
  if (go !== "Build") return;

  let started: { path?: string };
  try {
    started = await client.build({
      worktree: where.id === "worktree",
      provider: agent.id,
      model: model.id,
      machine: where.machine,
    });
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  if (where.machine) {
    // The build runs over there, so the window to watch it in is over there.
    if (started.path) {
      await openThere(where.machine, where.label, started.path);
      return;
    }
    void vscode.window.showInformationMessage(
      `ade: building ${file} on ${where.machine}.`,
    );
    return;
  }
  // A worktree build relocated the session, so this folder is no longer the
  // one being built in.
  if (started.path) {
    await switchTo(started.path);
    return;
  }
  void chat.reveal(live);
}

// sessionOn gives the document a session of its own: the screen's Design row,
// with the document already picked and every default still to be looked at.
async function sessionOn(file: string): Promise<void> {
  const dir = live?.root ?? openFolder();
  if (!dir) {
    void vscode.window.showErrorMessage(
      "ade: no checkout is being served, so there is nowhere to branch from.",
    );
    return;
  }
  await newSessionPage.show({
    mode: "design",
    design: file,
    repos: [await madeFrom(dir)],
  });
}

// Which document, when the command did not come off a row. The tree's list is
// the directory's, which is the same list the server would offer.
// pickDesign is the design directory and what you can do with a document.
// Open leads, because that is what the tree row did on a click.
async function pickDesign(): Promise<void> {
  await design.refresh();
  const list = docRows(design.files);
  if (list.length === 0) {
    void vscode.window.showInformationMessage(
      "ade: there are no design documents in this checkout.",
    );
    return;
  }
  const picked = await pick(list, {
    title: "Design documents",
    matchOnDescription: true,
  });
  if (!picked) return;

  const act = await pick(
    DOC_ACTS.map((a) => ({ label: a.label, detail: a.detail, id: a.id })),
    { title: picked.label },
  );
  if (!act) return;
  const dir = design.docs;
  const uri = dir ? vscode.Uri.file(join(dir, picked.file)) : undefined;
  switch (act.id) {
    case "open":
      // Named, so it is the text editor however the rendered one is
      // registered: a design document opens rendered by default.
      if (uri)
        await vscode.commands.executeCommand("vscode.openWith", uri, "default");
      return;
    case "rendered":
      if (uri)
        await vscode.commands.executeCommand(
          "vscode.openWith",
          uri,
          DesignEditor.viewType,
        );
      return;
    case "preview":
      if (uri)
        await vscode.commands.executeCommand("markdown.showPreviewToSide", uri);
      return;
    case "discuss":
      return discussDoc(uri);
    case "build":
      return build(picked.file);
    case "session":
      return sessionOn(picked.file);
    case "seed":
      return seedDesign(picked.file);
    case "grill":
      return grillDesign(picked.file);
    case "condense":
      return condenseDesign(picked.file);
    case "archive":
      return archiveDoc(picked.file);
    case "delete":
      return deleteDoc(picked.file);
  }
}

async function pickDoc(): Promise<string | undefined> {
  const open = vscode.window.activeTextEditor?.document.uri.fsPath;
  const dir = design.docs;
  if (dir && open?.startsWith(`${dir}/`)) return open.slice(dir.length + 1);
  await design.refresh();
  const found = [...design.files];
  if (found.length === 0) {
    void vscode.window.showWarningMessage(
      "ade: there are no design documents in this checkout.",
    );
    return undefined;
  }
  return pick(found, {
    title: "Build which design document?",
  });
}

// The document in front of you is the one the session is working on, which is
// what Build acts on. Reading it through the server is how that is said.
function follow(): vscode.Disposable {
  return vscode.window.onDidChangeActiveTextEditor((editor) => {
    const dir = design.docs;
    const at = editor?.document.uri.fsPath;
    if (!client || !dir || !at?.startsWith(`${dir}/`) || !at.endsWith(".md"))
      return;
    void client.openDoc(at.slice(dir.length + 1)).catch(() => undefined);
  });
}

// terminal is the checkout's shell in VS Code's own terminal rather than an
// xterm in a frame: the editor already has scrollback, find and splits.
function terminal(): void {
  if (!live) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  openTerminal(live);
}

// open frames ade's one remaining page. /permissions is plain html served by
// the checkout's own server, and is the last thing with no native shape.
function open(key: string, title: string): void {
  panels.show(key, title, urlFor(key));
}

function urlFor(_key: string): string | undefined {
  return live ? rulesPage(live) : undefined;
}

// startLauncher brings up the one launcher this machine has. It used to open
// its page; the trees are that page now, so this only has to make it exist.
async function startLauncher(): Promise<void> {
  launcher = await ensureLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage(noLauncher("surface"));
    return;
  }
  await machines.refresh();
  void vscode.window.showInformationMessage("ade: the launcher is running.");
}

// ensureLauncher is a launcher to talk to, started if there was not one. Every
// surface that is not this checkout's own server goes through it.
async function ensureLauncher(): Promise<Server | undefined> {
  // On another machine the launcher is the one this session was reached
  // through: on a worker that is in the container beside it. See the doc.
  if (!machineWide(vscode.env.remoteName)) return workerLauncher();
  return ensureAt(
    findLauncher,
    () => start(launcherArgs, undefined, noAde),
    waitFor,
  );
}

function workerLauncher(): Server | undefined {
  if (!live) return undefined;
  const at = launcherAt(live.base, live.token);
  return at ? { ...at, root: live.root } : undefined;
}

// The launcher backs the machines, jobs and plan surfaces, so a window that
// comes up without one shows them empty until it is started.
async function launcherUp(): Promise<void> {
  launcher = await ensureLauncher();
  if (!launcher) return;
  // Dropped, not read: a read now lands before the side bar is open, so the
  // view that is eventually looked at never shows that it was loading.
  machines.stale();
  void desk.refresh();
  await drawPlan();
}

// waitFor polls until a server has written its record and answers. Starting one
// is a spawn and a listen, not an install: a few seconds is generous.
async function waitFor<T>(
  look: () => Promise<T | undefined>,
): Promise<T | undefined> {
  for (let tries = 0; tries < 20; tries++) {
    await new Promise((r) => setTimeout(r, 400));
    const got = await look();
    if (got) return got;
  }
  return undefined;
}

// Everything the launcher has seen, for the two pickers that go looking
// through it. Undefined means it said nothing and has already said why.
async function sessionRows(
  everything = false,
): Promise<{ all: Row[]; up: Up[] } | undefined> {
  const found = await findLauncher();
  if (!found) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return undefined;
  }
  launcher = found;

  const [repos, asked, up] = await Promise.all([
    launcherRepos(found, everything).catch(() => []),
    // A launcher that cannot say what is waiting still has the sessions.
    whatIsWaiting(found).catch(() => []),
    servingNow(found).catch(() => []),
  ]);
  return { all: flatten(repos, asked, live?.root, up), up };
}

// Find is a search where Switch is a shortlist: every session, flat, with the
// repository in the text so a name you half remember finds it.
async function findSession(): Promise<void> {
  const got = await sessionRows(true);
  if (!got) return;
  const rows = everySession(got.all, got.up, live?.root);
  if (rows.length === 0) {
    void vscode.window.showInformationMessage(
      "ade: no sessions yet. Start one with New Session.",
    );
    return;
  }

  const picked = await pick(
    rows.map((row) => ({
      label: sessionLabel(row),
      description: foundIn(row),
      detail: detail(row),
      row,
    })),
    {
      title: `Find a session — ${rows.length} to search`,
      placeHolder: "repository, branch, or what it was about",
      matchOnDescription: true,
      matchOnDetail: true,
    },
  );
  if (!picked) return;

  const session = picked.row.session;
  const act = await pick(
    sessionActs(session).map((a) => ({
      label: a.label,
      detail: a.detail,
      id: a.id,
    })),
    { title: sessionLabel(picked.row) },
  );
  if (!act) return;
  switch (act.id) {
    case "open":
      return openSession(session);
    case "rename":
      return renameSession(session);
    case "archive":
      return archiveSession(session, true);
    case "unarchive":
      return archiveSession(session, false);
    case "delete":
      return deleteSession(session);
  }
}

// The tree is a place to look; this is a place to go. Flat, and ordered by
// what you are most likely to want rather than by where it lives.
async function switchSession(onlyRepo?: string): Promise<void> {
  const got = await sessionRows();
  if (!got) return;
  const { all, up } = got;
  if (all.length === 0 && up.length === 0) {
    void vscode.window.showInformationMessage(
      "ade: no sessions yet. Start one with New Session.",
    );
    return;
  }
  const every = shortlist(all, up, live?.root);
  const rows = onlyRepo ? every.filter((row) => row.repo === onlyRepo) : every;

  const items: (vscode.QuickPickItem & { row?: (typeof rows)[number] })[] = [];
  for (const g of sections(rows)) {
    items.push({ label: g.repo, kind: vscode.QuickPickItemKind.Separator });
    for (const row of g.rows) {
      items.push({
        label: sessionLabel(row),
        // Under Waiting for you the heading is not a repository, so the row
        // has to carry its own.
        description: row.waiting ? foundIn(row) : description(row),
        detail: detail(row),
        row,
      });
    }
  }

  const picked = await pick(items, {
    title: rows.some((row) => row.session.live)
      ? "Switch to which session?"
      : "Nothing is running — recent sessions",
    matchOnDescription: true,
    matchOnDetail: true,
  });
  if (!picked?.row) return;
  await openSession(picked.row.session);
}

// openSession resumes a conversation in this checkout, or offers to open the
// folder of one elsewhere.
// goingRound is whether the badge is the stuck one, which is the only state
// the run item offers an action on.
function goingRound(state: RunState | undefined): boolean {
  return Boolean(state && going(state, Date.now()));
}

// whatIsStuck offers the two things worth doing about a run repeating itself.
// Leaving it alone is the third and needs no command.
async function whatIsStuck(): Promise<void> {
  if (!live) return;
  const doing = runState?.doing ?? "the same call";
  const picked = await pick(
    [
      { label: "$(stop-circle) Stop it", description: doing, id: "stop" },
      {
        label: "$(comment) Ask what is happening",
        description: "the agent answers in the chat",
        id: "ask",
      },
    ],
    { title: `Running ${runState?.repeats ?? 0}× over` },
  );
  if (picked?.id === "stop") {
    await client?.cancel().catch(() => undefined);
    return;
  }
  if (picked?.id === "ask") {
    await client
      ?.say(
        "You have run the same thing several times over. What are you waiting on, and what would unblock it?",
      )
      .catch(() => undefined);
    void chat.reveal(live);
  }
}

// answerSession replies to another session from here. The question it asked
// is the prompt, so you answer what was put to you rather than recalling it.
async function answerSession(row: Row): Promise<void> {
  const at = await findLauncher();
  if (!at) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const who = row.session.title || row.session.branch || row.session.name;
  const answer = await input({
    prompt: row.asked || `Answer ${who}`,
    placeHolder: `your answer to ${who}`,
  });
  if (!answer?.trim()) return;
  try {
    await sayToSession(at, row.session.id, answer);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  vscode.window.setStatusBarMessage(`ade: answered ${who}`, 3000);
  await keepSessions();
}

async function openSession(session: Session, machine?: string): Promise<void> {
  if (live && session.dir === live.root) {
    await resume(live, session.id, session.agent, session.model ?? "");
    void chat.reveal(live);
    return;
  }
  // That path is on the other machine, and this one has no such folder.
  if (machine) {
    await openThere(machine, machine, session.dir, "that conversation is");
    return;
  }

  // Opening the folder restarts the extension, so the conversation is left for
  // the window that comes up: without it the switch lands on no tabs at all.
  await want(memory, {
    path: session.dir,
    chat: true,
    session: session.id,
    agent: session.agent,
    model: session.model,
  });
  await switchTo(session.dir);
}

// resume points a server's conversation at a session. A failure is said and
// swallowed: the chat is still worth opening on whatever it is showing.
async function resume(
  server: Server,
  id: string,
  provider: string,
  model: string,
): Promise<void> {
  try {
    await fetch(
      `${server.base}/api/chat/resume?t=${encodeURIComponent(server.token)}`,
      {
        method: "POST",
        headers: {
          "X-Ade-Token": server.token,
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ id, provider, model }),
      },
    );
  } catch (err) {
    void vscode.window.showErrorMessage(
      `ade: could not resume that session (${String(err)})`,
    );
  }
}

// switchTo moves this window to a checkout. No confirmation: clicking a session
// somewhere else is the instruction, and being asked every time is the cost.
async function switchTo(dir: string): Promise<void> {
  const newWindow = inNewWindow(
    vscode.workspace.getConfiguration("ade").get<string>("openSessionIn"),
    (vscode.workspace.workspaceFolders ?? []).length,
  );
  await vscode.commands.executeCommand(
    "vscode.openFolder",
    vscode.Uri.file(dir),
    {
      forceNewWindow: newWindow,
    },
  );
}

// keys sets or forgets one of the secrets ade keeps. The value goes in through
// a password box, so it is never on screen and never in the command history.
async function keys(): Promise<void> {
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const at = launcher;
  try {
    const rows = secrets(await readSecrets(at));
    const picked = await pick(
      rows.map((secret) => ({
        label: secret.label,
        description: secretState(secret),
        detail: secret.detail,
        secret,
      })),
      { title: "ade: keys and tokens" },
    );
    if (!picked) return;
    const { secret } = picked;

    const SET = secret.set ? "Replace…" : "Set…";
    const FORGET = "Forget";
    const what = secret.set
      ? await pick([SET, FORGET], {
          title: `${secret.label} · ${secretState(secret)}`,
        })
      : SET;
    if (!what) return;

    if (what === FORGET) {
      await forgetSecret(at, secret.route);
      vscode.window.setStatusBarMessage(
        `ade: forgot the ${secret.label}`,
        3000,
      );
      return;
    }
    const value = await input({
      title: secret.label,
      prompt: "Stored in ~/.ade/settings.json, where the agents read it.",
      password: true,
      ignoreFocusOut: true,
    });
    // Cancelled or left blank changes nothing: Forget is the way to clear one.
    if (!value?.trim()) return;
    await setSecret(at, secret.route, value.trim());
    vscode.window.setStatusBarMessage(`ade: saved the ${secret.label}`, 3000);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// pullBase fetches origin's base branch, so the diff stops showing main's own
// commits as if this branch had made them.
async function pullBase(): Promise<void> {
  await verb("pulled the base branch", (c) => c.pullBase());
  if (client) await refresh(client);
}

// draftReply has the agent write an answer to one review comment. Nothing is
// sent: the draft lands on the thread to be edited, then sent.
async function draftReply(arg: unknown): Promise<void> {
  const id = threads?.threadId(arg);
  if (!id) {
    void vscode.window.showErrorMessage("ade: no comment to answer.");
    return;
  }
  await verb("drafting a reply", (c) => c.draftReply(id));
}

async function draftReplies(): Promise<void> {
  await verb("drafting replies to the review comments", (c) =>
    c.draftReplies(),
  );
}

// sendReply posts one thread's waiting reply into the reviewer's own thread.
// Confirmed: it goes up under your name and cannot be taken back.
async function sendReply(arg: unknown): Promise<void> {
  const id = threads?.threadId(arg);
  const pr = threads?.branchPR;
  if (!id || !pr) {
    void vscode.window.showErrorMessage(
      "ade: nothing to send, or no pull request for this branch.",
    );
    return;
  }
  const yes = await vscode.window.showWarningMessage(
    `Post this reply on #${pr.number}?`,
    { modal: true, detail: "It goes up under your name and cannot be undone." },
    "Post",
  );
  if (yes !== "Post") return;
  await verb(`replied on #${pr.number}`, (c) => c.sendReply(id));
  if (client) await refresh(client);
}

// sendReplies posts every waiting reply. The confirmation lists them, and only
// the listed ones go: a draft that lands while it is open was never read.
async function sendReplies(): Promise<void> {
  const c = client;
  if (!c) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  let state: State;
  try {
    state = await c.state();
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  const pr = state.branchPr;
  const ready = replyable(state.threads ?? []);
  if (!pr || ready.length === 0) {
    void vscode.window.showInformationMessage(
      pr
        ? "ade: no replies are waiting to be sent."
        : "ade: there is no pull request to reply on.",
    );
    return;
  }
  const yes = await vscode.window.showWarningMessage(
    `Post ${ready.length} repl${ready.length === 1 ? "y" : "ies"} on #${pr.number}?`,
    {
      modal: true,
      detail: `They go up under your name and cannot be undone.\n\n${listed(
        ready.map((t) => `${t.file}:${t.line}`),
      )}`,
    },
    "Post",
  );
  if (yes !== "Post") return;
  await verb(`replied on #${pr.number}`, (c) =>
    c.sendReplies(
      pr.number,
      ready.map((t) => t.id),
    ),
  );
  await refresh(c);
}

// stopAgent stops one named agent and leaves the rest running. Stop, beside
// it, stops everything.
async function stopAgent(): Promise<void> {
  const c = client;
  if (!c) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  try {
    const runs = (await c.state()).agent?.sideRuns ?? [];
    if (runs.length === 0) {
      void vscode.window.showInformationMessage(
        "ade: no named agent is running.",
      );
      return;
    }
    const picked = await pick(
      runs.map((run) => ({
        label: `@${run.name}`,
        description: run.startedAt
          ? `since ${new Date(run.startedAt).toLocaleTimeString()}`
          : "",
        id: run.id,
      })),
      { title: "ade: stop one agent" },
    );
    if (!picked) return;
    await c.stopSide(picked.id);
    vscode.window.setStatusBarMessage(`ade: stopped ${picked.label}`, 3000);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// explain has a read-only agent say the diff back in plain words, in the chat.
async function explain(): Promise<void> {
  await verb("explaining the diff", (c) => c.explain());
  void chat.reveal(live);
}

// showLogs writes what this checkout's ade logged into an output channel. Each
// call appends only what is new, so the channel reads as one log.
async function showLogs(): Promise<void> {
  const c = client;
  if (!c) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  logChannel ??= vscode.window.createOutputChannel("ade");
  logChannel.show(true);
  try {
    const found = await c.logs(logSeen);
    for (const record of found.records ?? [])
      logChannel.appendLine(logLine(record));
    logSeen = found.latest;
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// toggleFree stops ade asking before each tool call for the rest of this
// session, or starts it asking again. Confirmed on the way off, not on.
async function toggleFree(): Promise<void> {
  const on = !runState?.free;
  if (on) {
    const yes = await vscode.window.showWarningMessage(
      "Stop asking for permission in this session?",
      {
        modal: true,
        detail:
          "The agent runs every tool call without asking until you turn this back on or the session ends.",
      },
      "Stop asking",
    );
    if (yes !== "Stop asking") return;
  }
  await verb(on ? "running without asking" : "asking again", (c) => c.free(on));
  if (client) await refresh(client);
}

async function refreshTeam(): Promise<void> {
  await verb("asked the team roster again", (c) => c.refreshTeam());
}

// removeAllComments empties this checkout's comment store. What came from the
// pull request is fetched again; what was written here is gone for good.
async function removeAllComments(): Promise<void> {
  const yes = await vscode.window.showWarningMessage(
    "Delete every comment in this checkout?",
    {
      modal: true,
      detail:
        "Open and resolved alike. Review comments from the pull request come back on the next fetch; everything else cannot be recovered.",
    },
    "Delete all",
  );
  if (yes !== "Delete all") return;
  await verb("deleted every comment", (c) => c.resetThreads());
  if (client) await refresh(client);
}

// editOutgoing changes the words Convert wrote before they are sent: the
// agent drafted them, and reading them over is the point of the two steps.
async function editOutgoing(arg: unknown): Promise<void> {
  const c = client;
  const id = threads?.threadId(arg);
  if (!c || !id) {
    void vscode.window.showErrorMessage("ade: no comment to edit.");
    return;
  }
  try {
    const thread = (await c.state()).threads?.find((t) => t.id === id);
    const text = await input({
      title: "What goes to the author",
      value: thread?.outgoing ?? "",
      ignoreFocusOut: true,
    });
    if (!text?.trim()) return;
    await c.setOutgoing(id, text.trim());
    await refresh(c);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// archiveDoc publishes a finished design to the wiki and deletes it here. It
// takes a model call and a push, so the progress note is not optional.
async function archiveDoc(node: unknown): Promise<void> {
  const name = docName(node);
  const c = client;
  if (!name || !c) {
    void vscode.window.showErrorMessage("ade: no document to archive.");
    return;
  }
  const yes = await vscode.window.showWarningMessage(
    `Archive ${name}?`,
    {
      modal: true,
      detail:
        "It is distilled onto the shared wiki and deleted here. The page is pushed first, so nothing is lost if that fails.",
    },
    "Archive",
  );
  if (yes !== "Archive") return;
  try {
    const out = await vscode.window.withProgress(
      {
        location: vscode.ProgressLocation.Notification,
        title: `ade: archiving ${name}`,
      },
      () => c.archiveDoc(name),
    );
    vscode.window.setStatusBarMessage(
      `ade: archived ${name} to ${out.page}`,
      5000,
    );
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// deleteDoc removes one design document, asked first: it lives under the git
// directory, where no commit holds a copy.
async function deleteDoc(node: unknown): Promise<void> {
  const name = docName(node);
  if (!name) {
    void vscode.window.showErrorMessage("ade: no document to delete.");
    return;
  }
  const yes = await vscode.window.showWarningMessage(
    `Delete ${name}?`,
    {
      modal: true,
      detail: "It is not in any commit, so this cannot be undone.",
    },
    "Delete",
  );
  if (yes !== "Delete") return;
  await verb(`deleted ${name}`, (c) => c.deleteDoc(name));
}

// docName is which document a Design row meant: the tree's element is the
// file name, and a menu on the open editor hands over a uri instead.
function docName(node: unknown): string | undefined {
  if (typeof node === "string") return node;
  const uri =
    node instanceof vscode.Uri
      ? node
      : (node as { resourceUri?: vscode.Uri } | undefined)?.resourceUri;
  if (!uri) return undefined;
  const dir = design.docs;
  if (dir && uri.fsPath.startsWith(`${dir}/`))
    return uri.fsPath.slice(dir.length + 1);
  return basename(uri.fsPath);
}

// sandboxFile edits what the repository's sandbox mounts and sets. A run of
// picks rather than the file: the file holds secrets the server never shows.
async function sandboxFile(): Promise<void> {
  const c = client;
  if (!c) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  try {
    for (;;) {
      const file = await c.sandboxFile();
      const picked = await pick(
        sandboxRows(file).map((row) => ({
          label: row.label,
          description: row.description,
          row,
        })),
        {
          title: "ade: sandbox mounts and environment",
          placeHolder:
            "A change drops the running container; the next turn starts a new one",
        },
      );
      if (!picked) return;
      const edit = await sandboxEdit(picked.row);
      if (edit) await c.saveSandboxFile(applySandbox(file, edit));
    }
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// sandboxEdit asks what to do with one row. Nothing chosen is no edit, and
// the list comes back as it was.
async function sandboxEdit(row: SandboxRow): Promise<SandboxEdit | undefined> {
  switch (row.kind) {
    case "mount": {
      const flip = row.mount.rw ? "Make read-only" : "Make read-write";
      const what = await pick([flip, "Stop mounting"], {
        title: row.mount.path,
      });
      if (what === flip)
        return { kind: "mount", path: row.mount.path, rw: !row.mount.rw };
      if (what) return { kind: "unmount", path: row.mount.path };
      return undefined;
    }
    case "env": {
      const what = await pick(["Replace the value…", "Remove"], {
        title: row.key,
      });
      if (what === "Remove") return { kind: "unset", key: row.key };
      if (!what) return undefined;
      return sandboxValue(row.key);
    }
    case "add-mount": {
      const [folder] =
        (await vscode.window.showOpenDialog({
          canSelectFiles: true,
          canSelectFolders: true,
          canSelectMany: false,
          openLabel: "Mount",
        })) ?? [];
      if (!folder) return undefined;
      const how = await pick(["Read-only", "Read-write"], {
        title: folder.fsPath,
      });
      if (!how) return undefined;
      return { kind: "mount", path: folder.fsPath, rw: how === "Read-write" };
    }
    default: {
      const key = await input({
        title: "Name of the variable",
        validateInput: (value) =>
          validKey(value.trim())
            ? undefined
            : "letters, digits and _, not starting with a digit",
      });
      if (!key?.trim()) return undefined;
      return sandboxValue(key.trim());
    }
  }
}

async function sandboxValue(key: string): Promise<SandboxEdit | undefined> {
  const value = await input({
    title: key,
    prompt: "Kept in a file under .git that only you can read.",
    password: true,
    ignoreFocusOut: true,
  });
  if (value === undefined || value === "") return undefined;
  return { kind: "set", key, value };
}

// distilMemory has the agent read the transcripts it has not seen and propose
// what is worth keeping. Nothing is remembered until a proposal is accepted.
async function distilMemory(): Promise<void> {
  const c = client;
  if (!c) return;
  try {
    const found = await vscode.window.withProgress(
      {
        location: { viewId: "ade.memory" },
        title: "reading the new transcripts",
      },
      () => c.distil(),
    );
    vscode.window.setStatusBarMessage(
      `ade: read ${found.read}, proposed ${found.proposed}`,
      5000,
    );
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// runDrift scores the change against the open design document. The verdicts
// arrive when the agent goes idle, as squiggles and in the Drift view.
async function runDrift(): Promise<void> {
  await verb("checking the change against the design", (c) => c.runDrift());
  if (client) await refresh(client);
}

async function dismissDrift(
  item: DriftItem,
  dismissed: boolean,
): Promise<void> {
  await verb(dismissed ? "dismissed" : "brought back", (c) =>
    c.dismissDrift(item.id, dismissed),
  );
  await driftTree.refresh();
}

// pickDrift is the checklist on demand. The squiggles are the ambient half and
// stay put: this is for reading a verdict in full, or waving a requirement off.
async function pickDrift(): Promise<void> {
  if (!client) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  await driftTree.refresh();
  const list = driftRows(await driftTree.state());
  if (list.length === 0) {
    void vscode.window.showInformationMessage(
      "ade: no design document is being checked against this change.",
    );
    return;
  }
  const picked = await pick(list, {
    title: driftTree.file ?? "Drift",
    matchOnDescription: true,
  });
  if (!picked) return;

  const act = await pick(
    driftActs(picked.item).map((a) => ({
      label: a.label,
      detail: a.detail,
      id: a.id,
    })),
    { title: picked.item.text },
  );
  if (!act) return;
  if (act.id === "open") {
    const at = driftTree.at(picked.item);
    if (at) await vscode.commands.executeCommand(at.command, ...at.arguments!);
    return;
  }
  await dismissDrift(picked.item, act.id === "dismiss");
}

async function diffRoot(): Promise<void> {
  const c = client;
  if (!c) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  try {
    const { roots, view } = await c.diffFolders(windowFolders());
    if (!roots?.length) {
      void vscode.window.showInformationMessage(
        "ade: this checkout is the only repository the Diff view can show.",
      );
      return;
    }
    const picked = await pick(
      roots.map((r) => ({
        label:
          (r.root === view || (r.home && !view) ? "$(check) " : "") + r.name,
        description: [
          r.home ? "this checkout" : "",
          r.branch,
          r.changes ? `${r.changes} changed` : "",
        ]
          .filter(Boolean)
          .join(" · "),
        detail: r.root,
        root: r.home ? "" : r.root,
      })),
      { title: "ade: which repository the Diff view shows" },
    );
    if (!picked) return;
    await c.diffRoot(picked.root);
    await useView(c, picked.root);
    await refresh(c);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// seedDesign opens the conversation from a first draft: the agent reads the
// document and says what it has missed. Adopted first, so it is this one.
// condenseDesign runs the condenser beside the conversation. It rewrites the
// document in place, so the confirmation is the point rather than politeness.
async function condenseDesign(node: unknown): Promise<void> {
  const c = client;
  const name = docName(node);
  if (!c || !name) {
    void vscode.window.showErrorMessage("ade: no document to condense.");
    return;
  }
  const said = await vscode.window.showWarningMessage(
    `Condense ${name}? It is rewritten in place.`,
    { modal: true },
    "Condense",
  );
  if (said !== "Condense") return;
  try {
    await c.openDoc(name);
    const got = await c.condenseDesign();
    if (!got.started) {
      void vscode.window.showInformationMessage(
        `ade: ${got.reason ?? "the condenser did not start"}.`,
      );
      return;
    }
    void chat.reveal(live);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// grillDesign hands the document to the grill-me skill, which interviews you
// about it until nothing is left assumed. A slash command goes through as typed.
async function grillDesign(node: unknown): Promise<void> {
  const c = client;
  const name = docName(node);
  const dir = design.docs;
  if (!c || !name || !dir) {
    void vscode.window.showErrorMessage("ade: no document to grill you on.");
    return;
  }
  try {
    await c.openDoc(name);
    await c.say(`/grill-me the design in ${join(dir, name)}`);
    void chat.reveal(live);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// shownDoc is the document in the active editor, for a command run from the
// palette with nothing to point at.
function shownDoc(): vscode.Uri | undefined {
  return vscode.window.activeTextEditor?.document.uri;
}

async function seedDesign(node: unknown): Promise<void> {
  const c = client;
  const name = docName(node);
  if (!c || !name) {
    void vscode.window.showErrorMessage("ade: no document to ask about.");
    return;
  }
  try {
    await c.openDoc(name);
    const got = await c.seedDesign();
    if (!got.started) {
      void vscode.window.showInformationMessage(
        `ade: ${got.reason ?? "there is nothing in the document to react to yet"}.`,
      );
      return;
    }
    void chat.reveal(live);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// previewURL sets the page ade photographs after a turn. Empty clears it.
async function previewURL(): Promise<void> {
  const c = client;
  if (!c) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  try {
    const now = await c.preview();
    const url = await input({
      title: "The page to capture after each turn",
      prompt: now.off
        ? "Preview capture is turned off in settings."
        : "A local dev server's address. Leave empty to capture nothing.",
      value: now.url,
      placeHolder: now.effective || "http://localhost:3000",
    });
    if (url === undefined) return;
    await c.savePreview(url.trim());
    vscode.window.setStatusBarMessage("ade: preview saved", 3000);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// portScratch moves a scratch session's work into a repository. The server
// refuses a checkout that is not a scratch one.
async function portScratch(): Promise<void> {
  const c = client;
  if (!c) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  const [target] =
    (await vscode.window.showOpenDialog({
      canSelectFiles: false,
      canSelectFolders: true,
      canSelectMany: false,
      openLabel: "Move the work here",
      title: "The repository this work belongs in",
    })) ?? [];
  if (!target) return;
  const branch = await input({
    title: "Branch name",
    prompt: "Leave empty to let ade name it.",
  });
  if (branch === undefined) return;
  try {
    const moved = await c.port(target.fsPath, branch.trim());
    const open = await vscode.window.showInformationMessage(
      `ade: the work is now at ${moved.path}.`,
      "Open it",
    );
    if (open)
      await vscode.commands.executeCommand(
        "vscode.openFolder",
        vscode.Uri.file(moved.path),
        { forceNewWindow: true },
      );
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// sessionOf is which session a row's menu meant: the row hands over its node,
// and the row's own click hands over the session.
function sessionOf(arg: unknown): Session | undefined {
  const node = arg as { session?: Session; id?: string } | undefined;
  if (node?.session) return node.session;
  return node?.id ? (node as Session) : undefined;
}

// sessionVerb posts one of the launcher's session verbs and redraws the tree.
async function sessionVerb(
  said: string,
  path: string,
  body: unknown,
): Promise<void> {
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  try {
    await jobPost(launcher, path, body);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  vscode.window.setStatusBarMessage(`ade: ${said}`, 3000);
  await sessions.refresh();
}

async function archiveSession(arg: unknown, archived: boolean): Promise<void> {
  const session = sessionOf(arg);
  if (!session) return;
  await sessionVerb(archived ? "archived" : "brought back", "/api/archive", {
    id: session.id,
    archived,
    machine: Sessions.machineOf(arg as SessionNode),
  });
}

// deleteSession takes a session out of every list for good. Asked first:
// archive is the one that can be undone.
// renameSession gives it your name, which stops ade renaming it.
async function renameSession(arg: unknown): Promise<void> {
  // No row means the palette asked, which is about this window's own.
  const session = sessionOf(arg);
  const id = session?.id ?? runState?.session ?? "";
  if (!id) {
    void vscode.window.showWarningMessage(
      "ade: no conversation here to rename.",
    );
    return;
  }
  const now = session?.title ?? runState?.title ?? "";
  const title = await input({
    title: now ? `Rename “${now}”` : "Name this session",
    value: now,
    valueSelection: [0, now.length],
    prompt: "ade stops renaming it once you have",
  });
  if (title === undefined) return;
  await sessionVerb("renamed", "/api/sessions/rename", {
    id,
    title: title.trim(),
  });
  if (client) await refresh(client);
}

async function deleteSession(arg: unknown): Promise<void> {
  const session = sessionOf(arg);
  if (!session) return;
  const machine = Sessions.machineOf(arg as SessionNode);
  const yes = await vscode.window.showWarningMessage(
    `Delete “${session.title || session.id.slice(0, 8)}”?`,
    {
      modal: true,
      detail:
        "It leaves every list for good. Archive puts it away and can be undone.",
    },
    "Delete",
  );
  if (yes !== "Delete") return;
  await sessionVerb("deleted the session", "/api/sessions/delete", {
    ids: [session.id],
    machine,
  });
}

// pruneMachineSessions clears conversations piled up on another machine. The
// rows come from that machine, so only it can forget them.
async function pruneMachineSessions(arg: unknown): Promise<void> {
  const node = arg as { machine?: Machine } | undefined;
  const machine = node?.machine;
  if (!machine?.machine) return;
  const rows = prunable(machine);
  if (rows.length === 0) {
    void vscode.window.showInformationMessage(
      `ade: ${machine.machine} is holding no finished conversations.`,
    );
    return;
  }
  const picked = await pick(
    rows.map((row) => ({ ...row, picked: false })),
    {
      title: `Delete which conversations on ${machine.machine}?`,
      canPickMany: true,
      placeHolder: "Oldest first. Running conversations are not listed.",
    },
  );
  if (!picked?.length) return;
  const yes = await vscode.window.showWarningMessage(
    `Delete ${picked.length} conversation${picked.length === 1 ? "" : "s"} on ${machine.machine}?`,
    {
      modal: true,
      detail: "They leave every list for good. The checkouts are left alone.",
    },
    "Delete",
  );
  if (yes !== "Delete") return;
  await sessionVerb(
    `deleted ${picked.length} on ${machine.machine}`,
    "/api/sessions/delete",
    { ids: picked.map((row) => row.session.id), machine: machine.machine },
  );
}

// newScratch starts work that has no repository yet. ade makes the checkout,
// so there is no folder to pick: only what the work is about.
async function newScratch(): Promise<void> {
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const name = await input({
    title: "Start on something with no home yet",
    prompt:
      "What it is about. It names the branch; leave empty for a dated one.",
  });
  if (name === undefined) return;
  try {
    const made = await scratch(launcher, { name: name.trim() });
    await want(memory, { path: made.path, chat: true });
    await switchTo(made.path);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// repoNow is this window's folder, read by the launcher: what the repository
// is, and what ade has done in it.
async function repoNow(fresh: boolean): Promise<RepoStats> {
  const folder = (vscode.workspace.workspaceFolders ?? [])[0];
  if (!folder) throw new Error("this window has no folder open");
  launcher = await findLauncher();
  if (!launcher) throw new Error("the launcher is not running");
  return askLauncher<RepoStats>(
    launcher,
    `/api/repo?dir=${encodeURIComponent(folder.uri.fsPath)}${fresh ? "&fresh=1" : ""}`,
  );
}

// sweepNow is every worktree on this machine. The launcher is the only
// server that can sweep: an ade serving a checkout knows only its own.
async function sweepNow(fresh: boolean): Promise<Sweep> {
  launcher = await findLauncher();
  if (!launcher) throw new Error("the launcher is not running");
  return askLauncher<Sweep>(
    launcher,
    `/api/worktrees/all${fresh ? "?fresh=1" : ""}`,
  );
}

// designRepos is every repository the launcher holds designs for, and the
// wiki state. A session server only knows the checkout it started in.
async function designRepos(): Promise<{
  repos: { repo: string }[];
  wiki?: WikiState;
}> {
  launcher = await findLauncher();
  if (!launcher) throw new Error("the launcher is not running");
  return askLauncher<{ repos: { repo: string }[]; wiki?: WikiState }>(
    launcher,
    "/api/designs/repos",
  );
}

// prunableIn is one repository's read-only sweep. One call per repository,
// because each is a `gh pr list`: a repo that hangs hangs its own row.
async function prunableIn(repo: string): Promise<Prunable> {
  launcher = await findLauncher();
  if (!launcher) throw new Error("the launcher is not running");
  return askLauncher<Prunable>(
    launcher,
    `/api/designs/prunable?repo=${encodeURIComponent(repo)}`,
  );
}

// archiveDesign publishes one document and deletes the local file.
async function archiveDesign(repo: string, file: string): Promise<Archived> {
  launcher = await findLauncher();
  if (!launcher) throw new Error("the launcher is not running");
  return tellLauncher<Archived>(launcher, "/api/designs/archive", {
    repo,
    file,
  });
}

// pruneIn removes one repository's worktrees, through the launcher: a server
// cannot remove the checkout it is serving from.
async function pruneIn(
  repo: string,
  paths: string[],
  force: boolean,
): Promise<{ removed?: string[]; kept?: { path: string; why: string }[] }> {
  launcher = await findLauncher();
  if (!launcher) throw new Error("the launcher is not running");
  return pruneAt(launcher, repo, paths, force);
}

// showStats opens what the transcripts say was spent, as a page. Generated
// each time: it is a reading, not a file.
async function showStats(): Promise<void> {
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const at = launcher;
  try {
    const stats = await vscode.window.withProgress(
      {
        location: vscode.ProgressLocation.Notification,
        title: "ade: reading the transcripts…",
      },
      () => askLauncher<Stats>(at, "/api/stats"),
    );
    await pages.show("spent.md", statsPage(stats));
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// showGraph opens the machine right now as a page: every checkout with a
// server or a session, drawn and listed.
async function showGraph(): Promise<void> {
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  try {
    const got = await askLauncher<{ repos: GraphRepo[] | null }>(
      launcher,
      "/api/graph",
    );
    await pages.show("running.md", graphPage(got.repos ?? []));
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// showJobReports opens what a job's runs had to say, newest first, and puts
// them away: reading the reports is the whole of reviewing them.
async function showJobReports(row: unknown): Promise<void> {
  const job = jobOf(row as never);
  if (!job) return;
  const outcomes = job.outcomes ?? [];
  await pages.show(`${job.name}.md`, jobPage(job.name, outcomes));
  // No time named marks every outcome the job holds, which is what was shown.
  if (unreadOutcomes(outcomes).length === 0) return;
  await jobVerb("/api/jobs/seen", { name: job.name });
}

// syncWorktrees brings a repository's checkouts up to its base. Asked first,
// with what each one would have done to it, from a dry run.
async function syncWorktrees(): Promise<void> {
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const at = launcher;
  try {
    const all = await launcherRepos(at);
    const mine = all.filter((repo) => (repo.checkouts ?? []).length > 1);
    if (mine.length === 0) {
      void vscode.window.showInformationMessage(
        "ade: no repository here has a worktree to sync.",
      );
      return;
    }
    const picked =
      mine.length === 1
        ? { repo: mine[0] }
        : await pick(
            mine.map((repo) => ({
              label: repo.name,
              description: repo.path,
              repo,
            })),
            { title: "ade: which repository's worktrees to sync" },
          );
    if (!picked) return;
    const repo = picked.repo;

    const plan = await vscode.window.withProgress(
      {
        location: vscode.ProgressLocation.Notification,
        title: `ade: fetching ${repo.name}'s base…`,
      },
      () =>
        tellLauncher<SyncReport>(at, "/api/sync", {
          repo: repo.path,
          dryRun: true,
        }),
    );
    const yes = await vscode.window.showWarningMessage(
      `Sync ${repo.name}'s worktrees?`,
      { modal: true, detail: syncLines(plan) },
      "Sync",
    );
    if (yes !== "Sync") return;

    const done = await vscode.window.withProgress(
      {
        location: vscode.ProgressLocation.Notification,
        title: `ade: syncing ${repo.name}…`,
      },
      () =>
        tellLauncher<SyncReport>(at, "/api/sync", {
          repo: repo.path,
          dryRun: false,
        }),
    );
    await pages.show("sync.md", syncPage(repo.name, done));
    await sessions.refresh();
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// pickRole is the verbs around ~/.ade/roles: which prompt new main chats start
// with, and getting at the file, which the editor is what edits.
async function pickRole(): Promise<void> {
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const at = launcher;
  try {
    const view = await askLauncher<RolesView>(at, "/api/roles");
    const picked = await pick(
      roleRows(view).map((row) => ({
        label: roleLabel(row),
        detail: roleDetail(row),
        row,
      })),
      { title: "ade: the role new chats start with" },
    );
    if (!picked) return;

    switch (picked.row.kind) {
      case "none":
        await putLauncher(at, "/api/roles/default", { name: "" });
        vscode.window.setStatusBarMessage("ade: no role", 3000);
        return;
      case "install":
        installRoles();
        return;
      case "new":
        await newRole(at);
        return;
      case "role":
        await roleVerb(at, picked.row.role, picked.row.selected);
    }
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// roleVerb is what to do with the one that was picked. Selecting it is the
// common case, so it is first and is what Enter does.
async function roleVerb(
  at: Server,
  role: Role,
  selected: boolean,
): Promise<void> {
  const USE = "Use it for new chats";
  const EDIT = "Edit…";
  const DELETE = "Delete";
  const what = await pick(selected ? [EDIT, DELETE] : [USE, EDIT, DELETE], {
    title: `${role.name}${selected ? " · in use" : ""}`,
  });
  if (!what) return;
  if (what === EDIT) {
    await openRole(role.name);
    return;
  }
  if (what === USE) {
    await putLauncher(at, "/api/roles/default", { name: role.name });
    vscode.window.setStatusBarMessage(`ade: new chats use ${role.name}`, 3000);
    return;
  }
  const yes = await vscode.window.showWarningMessage(
    `Delete the ${role.name} role?`,
    {
      modal: true,
      detail:
        "Its file goes for good. Sessions already running keep what they were given.",
    },
    "Delete",
  );
  if (yes !== "Delete") return;
  await dropLauncher(at, `/api/roles/${encodeURIComponent(role.name)}`);
  vscode.window.setStatusBarMessage(`ade: deleted ${role.name}`, 3000);
}

// newRole writes the file through the launcher, which is what checks the name
// and the cap, then opens it to be written properly.
async function newRole(at: Server): Promise<void> {
  const name = await input({
    title: "Name of the role",
    prompt: "It becomes a file in ~/.ade/roles.",
    validateInput: (value) =>
      validRoleName(value.trim())
        ? undefined
        : "letters, digits, - and _, not starting with - or _",
  });
  if (!name?.trim()) return;
  await tellLauncher(at, "/api/roles", {
    name: name.trim(),
    prompt: roleSkeleton(name.trim()),
  });
  await openRole(name.trim());
}

async function openRole(name: string): Promise<void> {
  const uri = vscode.Uri.file(join(homedir(), ".ade", "roles", `${name}.md`));
  await vscode.window.showTextDocument(uri);
}

// installRoles runs the cli, which is the only thing that has the defaults.
// In a terminal because it writes files and should be seen doing it.
function installRoles(): void {
  const term = vscode.window.createTerminal({ name: "ade roles" });
  term.sendText(`${findAde()} roles install`);
  term.show();
}

// syncModels refetches each provider's model catalogue, which is what the
// model picks are drawn from.
async function syncModels(): Promise<void> {
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const at = launcher;
  try {
    await vscode.window.withProgress(
      {
        location: vscode.ProgressLocation.Notification,
        title: "ade: refetching the models…",
      },
      () => tellLauncher(at, "/api/models/sync", {}),
    );
    vscode.window.setStatusBarMessage("ade: models refetched", 3000);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// showUsage opens the plan windows and what spent the current one: the bar has
// room for the percentages and nothing else.
async function showUsage(): Promise<void> {
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const at = launcher;
  try {
    const [read, spend] = await Promise.all([
      usageOf(at),
      askLauncher<WindowSpend>(at, "/api/limits/window").catch(() => ({})),
    ]);
    await pages.show("usage.md", usagePage(read, spend, Date.now()));
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// stopElsewhere stops an ade in another checkout, or the agent inside it. The
// Stop beside it is this session's; this is every other one on the machine.
async function stopElsewhere(): Promise<void> {
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const at = launcher;
  try {
    const got = await askLauncher<{ repos: GraphRepo[] | null }>(
      at,
      "/api/graph",
    );
    const rows = running(got.repos ?? []);
    if (rows.length === 0) {
      void vscode.window.showInformationMessage("ade: nothing is running.");
      return;
    }
    const picked = await pick(
      rows.map((row) => ({
        label: row.checkout.branch || row.checkout.name,
        description: row.repo.name,
        detail: `${graphState(row.checkout)} · ${row.checkout.path}`,
        row,
      })),
      { title: "ade: stop one that is running" },
    );
    if (!picked) return;

    const AGENT = "Stop the agent, leave ade running";
    const SERVER = "Stop ade in that checkout";
    const what = await pick([AGENT, SERVER], {
      title: picked.row.checkout.path,
    });
    if (!what) return;
    const path = what === AGENT ? "/api/graph/cancel" : "/api/graph/stop";
    await tellLauncher(at, path, { dir: picked.row.checkout.path });
    vscode.window.setStatusBarMessage(
      what === AGENT ? "ade: agent stopped" : "ade: stopped",
      3000,
    );
    await sessions.refresh();
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// combineSessions folds a checkout's finished conversations into one card, so
// a pile of small ones reads as the piece of work it was.
async function combineSessions(from?: unknown): Promise<void> {
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const at = launcher;
  try {
    // Off the suggestion row the checkout is already chosen, and from the
    // menu it is not.
    const named = (from as Checkout | undefined)?.sessions
      ? (from as Checkout)
      : undefined;
    const all = named ? [] : await launcherRepos(at);
    const checkouts = all
      .flatMap((repo) => repo.checkouts ?? [])
      .filter((c) => combinable(c.sessions ?? []).length > 1);
    if (!named && checkouts.length === 0) {
      void vscode.window.showInformationMessage(
        "ade: no checkout has two finished conversations to combine.",
      );
      return;
    }
    const where = named
      ? { checkout: named }
      : checkouts.length === 1
        ? { checkout: checkouts[0] }
        : await pick(
            checkouts.map((checkout) => ({
              label: checkout.name,
              description: checkout.branch,
              detail: checkout.path,
              checkout,
            })),
            { title: "ade: which checkout's conversations" },
          );
    if (!where) return;

    const picked = await pick(
      combinable(where.checkout.sessions ?? []).map((session) => ({
        label: session.title || session.id.slice(0, 8),
        description: session.branch,
        session,
      })),
      {
        title: `Combine in ${where.checkout.name}`,
        placeHolder: "Pick two or more; they become one card",
        canPickMany: true,
      },
    );
    if (!picked || picked.length < 2) return;
    await tellLauncher(at, "/api/sessions/combine", {
      dir: where.checkout.path,
      ids: picked.map((p) => p.session.id),
    });
    vscode.window.setStatusBarMessage(
      `ade: combined ${picked.length} conversations`,
      3000,
    );
    await sessions.refresh();
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// dismissSuggestion puts this exact set aside. The fingerprint is of the
// membership, so finishing another conversation brings the offer back.
async function dismissSuggestion(arg: unknown): Promise<void> {
  const checkout = arg as Checkout | undefined;
  if (!checkout?.suggestion) return;
  await sessionVerb(
    "suggestion dismissed",
    "/api/sessions/suggestion/dismiss",
    {
      dir: checkout.path,
      fingerprint: checkout.suggestion.fingerprint,
    },
  );
}

// uncombineSession takes one conversation back out of its card. The launcher
// drops a card that is left with nothing.
async function uncombineSession(arg: unknown): Promise<void> {
  const session = sessionOf(arg);
  if (!session) return;
  await sessionVerb("taken out of its card", "/api/sessions/uncombine", {
    id: session.id,
  });
}

// showPR opens the pull request under review as a page: what it is for, where
// it is going, who reviewed it and what the checks say.
async function showPR(): Promise<void> {
  const pr = threads?.branchPR;
  if (!pr) {
    void vscode.window.showInformationMessage(
      "ade: this branch has no pull request.",
    );
    return;
  }
  await pages.show(`pr-${pr.number}.md`, prPage(pr));
}

// editPR changes the title and the description on GitHub. The title is a line
// and goes in a box; the description is prose and gets a page to write in.
async function editPR(): Promise<void> {
  const c = client;
  const pr = threads?.branchPR;
  if (!c || !pr) {
    void vscode.window.showInformationMessage(
      "ade: this branch has no pull request.",
    );
    return;
  }
  const title = await input({
    title: `#${pr.number} on GitHub`,
    prompt: "The title. Press enter to go on to the description.",
    value: pr.title,
  });
  if (title === undefined || !title.trim()) return;
  await edits.show(
    `pr-${pr.number}-description.md`,
    pr.body ?? "",
    async (body) => {
      await c.editPR(title.trim(), body);
      await refresh(c);
      vscode.window.setStatusBarMessage(`ade: saved #${pr.number}`, 3000);
    },
  );
}

// checkoutPR turns the snapshot this session is reading into a worktree. The
// server refuses it on anything that is not one. See the design doc.
async function checkoutPR(): Promise<void> {
  const c = client;
  const pr = threads?.branchPR;
  if (!c || !pr) {
    void vscode.window.showInformationMessage(
      "ade: no pull request is open here.",
    );
    return;
  }
  const message = await input({
    title: `Work on #${pr.number}`,
    prompt: "What to start on, or leave empty to just check it out.",
    placeHolder: pr.title,
  });
  if (message === undefined) return;
  try {
    const made = await vscode.window.withProgress(
      {
        location: vscode.ProgressLocation.Notification,
        title: `ade: checking out #${pr.number}…`,
      },
      () => c.checkoutPR(message.trim()),
    );
    await want(memory, { path: made.path, chat: true });
    await switchTo(made.path);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// startElsewhere starts a session on another machine. Asking a machine for its
// repositories connects it, so this is also how one gets woken up.
async function startElsewhere(): Promise<void> {
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const at = launcher;
  try {
    const machines = (await hosts(at)).filter((host) => !host.local);
    if (machines.length === 0) {
      void vscode.window.showInformationMessage(
        "ade: no other machines are bookmarked.",
      );
      return;
    }
    const machine = await pick(
      machines.map((host) => ({
        label: host.label || host.name,
        description: hostLine(host),
        name: host.name,
      })),
      { title: "ade: which machine" },
    );
    if (!machine) return;

    const found = await vscode.window.withProgress(
      {
        location: vscode.ProgressLocation.Notification,
        title: `ade: asking ${machine.label} what it has…`,
      },
      () => target(at, machine.name),
    );
    // The launcher adds this machine's repositories that one has not got,
    // with no checkouts: opening one of those clones it there first.
    const places = elsewhereRows(found.repos ?? []);
    if (places.length === 0) {
      void vscode.window.showInformationMessage(
        `ade: ${machine.label} has nothing to work in, and nothing here to clone there.`,
      );
      return;
    }
    const where = await pick(
      places.map((row) => ({ ...row, row })),
      { title: `ade: where on ${machine.label}` },
    );
    if (!where) return;

    const prompt = await input({
      title: `Start on ${machine.label}`,
      prompt: "What to start on, or leave empty to just open a session.",
    });
    if (prompt === undefined) return;

    const started = await vscode.window.withProgress(
      {
        location: vscode.ProgressLocation.Notification,
        title: where.row.clone
          ? `ade: cloning ${where.row.label} on ${machine.label}…`
          : `ade: starting on ${machine.label}…`,
      },
      () => openOn(at, machine.name, where.row.dir, prompt.trim()),
    );
    // A clone lands where only the machine knows, so the window opens on what
    // it says rather than on what was asked for.
    await openThere(machine.name, machine.label, started.path ?? where.row.dir);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// product is the editor's own: the build's commit and its server download
// url, neither of which the extension API reports.
interface Product {
  commit?: string;
  serverDownloadUrlTemplate?: string;
}

function product(): Product {
  try {
    return JSON.parse(
      readFileSync(join(vscode.env.appRoot, "product.json"), "utf8"),
    ) as Product;
  } catch {
    return {};
  }
}

// Cached for the session: a 404 does not become a 200 without a run.
let serverPublished: boolean | undefined;

// openThere offers a window where the work's files are. The only thing in
// the way is a pin nothing was built a server for.
async function openThere(
  name: string,
  label: string,
  dir: string,
  verb?: string,
): Promise<void> {
  const template = product().serverDownloadUrlTemplate ?? "";
  if (serverPublished === undefined)
    serverPublished = await published(
      serverURL(template),
      async (at) => (await fetch(at, { method: "HEAD" })).status,
    );
  if (!serverPublished) {
    await noServerYet(template, name, dir);
    return;
  }
  const open = await vscode.window.showInformationMessage(
    said(label, verb),
    "Open a window there",
  );
  if (!open) return;
  await openWindowOn(name, dir);
}

async function openWindowOn(name: string, dir: string): Promise<void> {
  await vscode.commands.executeCommand(
    "vscode.openFolder",
    vscode.Uri.parse(folderURI(name, dir)),
    { forceNewWindow: true },
  );
}

// noServerYet offers the remedy rather than the diagnosis. No window opens:
// publishing is 25 to 40 minutes. Ask again when the run is green.
async function noServerYet(
  template: string,
  name: string,
  dir: string,
): Promise<void> {
  const version = releaseVersion(template);
  const press = await vscode.window.showWarningMessage(
    noServer(version),
    PUBLISH,
    UNVALIDATED,
  );
  if (press === PUBLISH) {
    await publishServer(version);
    return;
  }
  if (press !== UNVALIDATED) return;
  // One resolve, not a setting: a persisted flag is how a month goes by on
  // an unvalidated server without anyone noticing.
  await boxContext?.globalState.update(UNVALIDATED_ONCE, name);
  await openWindowOn(name, dir);
}

async function publishServer(version: string): Promise<void> {
  try {
    const run = await gh(publishRun(version));
    const url = await gh([
      "run",
      "list",
      "--workflow=editor-reh.yml",
      "--limit",
      "1",
      "--json",
      "url",
      "-q",
      ".[0].url",
    ]).catch(() => "");
    void vscode.window.showInformationMessage(
      `ade: building the editor server for ${version}. ${url.trim() || run.trim() || "25 to 40 minutes."}`,
    );
  } catch {
    void vscode.window.showErrorMessage(noGh(version));
  }
}

function gh(args: string[]): Promise<string> {
  return new Promise((done, fail) => {
    execFile("gh", args, (err, out) => (err ? fail(err) : done(out)));
  });
}

// The resolve runs in the new window's extension host, not this one.
const UNVALIDATED_ONCE = "ade.unvalidatedOnce";

// remoteLog is each step of a resolve. resolve() has no progress channel of
// its own, so a minutes-long install says so here and in a notification.
let remoteLog: vscode.OutputChannel | undefined;

function remoteOutput(): vscode.OutputChannel {
  remoteLog ??= vscode.window.createOutputChannel("Remote - ade");
  return remoteLog;
}

// registerResolver claims ade's own authority. Without the resolvers
// proposal granted there is nothing to register.
function registerResolver(context: vscode.ExtensionContext): void {
  if (!vscode.workspace.registerRemoteAuthorityResolver) return;
  context.subscriptions.push(
    vscode.workspace.registerRemoteAuthorityResolver(AUTHORITY, {
      resolve: (authority: string) => resolveHost(authority, context),
    }),
  );
}

async function resolveHost(
  authority: string,
  context: vscode.ExtensionContext,
): Promise<vscode.ResolvedAuthority> {
  const log = remoteOutput();
  try {
    const got = await vscode.window.withProgress(
      {
        location: vscode.ProgressLocation.Notification,
        title:
          "ade: starting the editor server (a first install downloads 120 MiB)",
      },
      () =>
        resolveAuthority(authority, {
          launcher: findLauncher,
          editorServer: (server, host, unvalidated) => {
            const info = product();
            return editorServer(
              server,
              host,
              info.commit ?? "",
              info.serverDownloadUrlTemplate ?? "",
              unvalidated,
            );
          },
          unvalidated: async (host) => {
            const asked = context.globalState.get<string>(UNVALIDATED_ONCE);
            if (asked === undefined) return false;
            await context.globalState.update(UNVALIDATED_ONCE, undefined);
            return asked === host;
          },
          log: (line) => log.appendLine(line),
        }),
    );
    nameTheHost(context, authority, got.host);
    return new vscode.ResolvedAuthority("127.0.0.1", got.port, got.token);
  } catch (err) {
    log.appendLine(String(err));
    const message = err instanceof Failed ? err.message : String(err);
    // A hard error is for what retrying cannot fix. Everything else heals
    // itself, and the workbench retries on its own backoff.
    if (err instanceof Failed && !err.retry)
      throw vscode.RemoteAuthorityResolverError.NotAvailable(message, true);
    throw vscode.RemoteAuthorityResolverError.TemporarilyNotAvailable(message);
  }
}

// nameTheHost makes the remote indicator read "ade: pi@pi5.local" rather
// than the raw authority.
function nameTheHost(
  context: vscode.ExtensionContext,
  authority: string,
  host: string,
): void {
  if (!vscode.workspace.registerResourceLabelFormatter) return;
  context.subscriptions.push(
    vscode.workspace.registerResourceLabelFormatter({
      scheme: "vscode-remote",
      authority,
      formatting: {
        label: "${path}",
        separator: "/",
        tildify: true,
        workspaceSuffix: hostLabel(host),
      },
    }),
  );
}

// rememberRepo turns a repository's project memory on or off from outside its
// checkout. Inside one the Project Memory view's own switch does it.
async function rememberRepo(arg: unknown, remembered: boolean): Promise<void> {
  const repo = arg as Repo | undefined;
  if (!repo?.path) return;
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  try {
    await jobPost(launcher, "/api/remember", {
      path: repo.path,
      remembered,
    });
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  vscode.window.setStatusBarMessage(
    remembered
      ? `ade: remembering ${repo.name}`
      : `ade: ${repo.name} forgotten`,
    3000,
  );
  await sessions.refresh();
}

// noLauncher is why a machine-wide surface is not available: on another
// machine there is no launcher to have, and here one should have started.
function noLauncher(what: "session" | "surface"): string {
  return machineWide(vscode.env.remoteName)
    ? "ade: could not start the launcher. Is `ade` on your PATH?"
    : whyNotHere(what);
}

// noAde is said once per window. Otherwise a failed spawn is an unhandled
// error nobody sees. Only this machine's: a remote window starts nothing.
let saidNoAde = false;

function noAde(): void {
  if (saidNoAde) return;
  saidNoAde = true;
  void vscode.window.showWarningMessage(
    "ade: no `ade` on PATH, so none could be started. Install it with `make install`.",
  );
}

// showDisk is what ade is costing in space: a reading you scan, so a page
// rather than a pick. Every place is walked, which is why it says it is.
async function showDisk(): Promise<void> {
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const at = launcher;
  try {
    const reading = await vscode.window.withProgress(
      {
        location: vscode.ProgressLocation.Notification,
        title: "ade: measuring…",
      },
      async () => {
        const disk = await askLauncher<DiskReading>(at, "/api/disk");
        const details = new Map<string, DiskDetail>();
        for (const store of disk.stores ?? []) {
          const one = await askLauncher<DiskDetail>(
            at,
            `/api/disk/detail?path=${encodeURIComponent(store.path)}`,
          ).catch(() => undefined);
          if (one) details.set(store.path, one);
        }
        return { disk, details };
      },
    );
    await pages.show("disk.md", diskPage(reading.disk, reading.details));
    const trees = [...reading.details.values()].flatMap(
      (d) => d.purgeable ?? [],
    );
    if (trees.length === 0) return;
    const empty = await vscode.window.showInformationMessage(
      `ade: ${trees.length} thing${trees.length === 1 ? "" : "s"} here can be emptied.`,
      "Empty one…",
    );
    if (empty) await emptyOne(at, trees);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// emptyOne is the verb the page does not carry: pick what to empty, and say
// whether it comes back before it goes.
async function emptyOne(at: Server, trees: Purgeable[]): Promise<void> {
  const picked = await pick(
    trees.map((tree) => ({
      label: tree.label,
      description: size(tree.bytes),
      detail: tree.path,
      tree,
    })),
    { title: "ade: what to empty" },
  );
  if (!picked) return;
  const yes = await vscode.window.showWarningMessage(
    `Empty ${picked.tree.label}?`,
    { modal: true, detail: purgeWarning(picked.tree) },
    "Empty",
  );
  if (yes !== "Empty") return;
  await jobPost(
    at,
    `/api/disk/purge?path=${encodeURIComponent(picked.tree.path)}`,
    {},
  );
  vscode.window.setStatusBarMessage(`ade: emptied ${picked.tree.label}`, 3000);
}

// jobVerb posts one of a job's verbs. Nothing is refreshed: the pick is read
// fresh each time it is opened.
async function jobVerb(path: string, body: unknown): Promise<void> {
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  try {
    await jobPost(launcher, path, body);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// jobOf is which job a pick or a command meant.
function jobOf(row: unknown): Job | undefined {
  const held = row as { job?: Job } | undefined;
  return held?.job;
}

// pickJob is the scheduled work: a list looked at when something has fired or
// needs changing, which is a pick rather than a row you walk past all day.
async function pickJob(): Promise<void> {
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const at = launcher;
  try {
    const got = await jobsOf(at, live?.root);
    const rows = jobRows(got.jobs, got.skipped);
    if (rows.length === 0) {
      void vscode.window.showInformationMessage("ade: no jobs defined.");
      return;
    }
    const picked = await pick(
      rows.map((row) => ({
        label: "job" in row ? row.job.name : fileName(row.skipped.path),
        description: "job" in row ? jobLine(row.job) : row.skipped.error,
        row,
      })),
      { title: "ade: scheduled jobs" },
    );
    if (!picked) return;
    if (!("job" in picked.row)) {
      await vscode.commands.executeCommand(
        "vscode.open",
        vscode.Uri.file(picked.row.skipped.path),
      );
      return;
    }
    await jobVerbs(picked.row.job);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// jobVerbs is what one job offers. Open is first: a job is a file, and reading
// it is what you came for more often than running it.
async function jobVerbs(job: Job): Promise<void> {
  const OPEN = "Open the job";
  const REPORTS = "What its runs reported";
  const RUN = "Run it now";
  const PAUSE = job.paused ? "Resume it" : "Pause it";
  const DELETE = "Delete it";
  const offered = [OPEN, REPORTS, RUN, PAUSE];
  if (editableJob(job)) offered.push(DELETE);

  const what = await pick(offered, {
    title: `${job.name} · ${jobLine(job)}`,
  });
  if (!what) return;
  switch (what) {
    case OPEN:
      await openJob(job);
      return;
    case REPORTS:
      await showJobReports({ job });
      return;
    case RUN:
      await runJob({ job });
      return;
    case DELETE:
      await deleteJob({ job });
      return;
    default:
      await pauseJob({ job }, !job.paused);
  }
}

// openJob opens its file, or the generated page for a built-in, which is Go in
// the launcher and has no file to read.
async function openJob(job: Job): Promise<void> {
  const uri = editableJob(job)
    ? vscode.Uri.file(job.path)
    : vscode.Uri.from({ scheme: JOB, path: jobDocPath(job.name) });
  await vscode.window.showTextDocument(uri);
}

// pickMemory is what ade keeps about this repository between sessions: the
// instructions, the proposals waiting on you, and what has been accepted.
async function pickMemory(): Promise<void> {
  const c = client;
  if (!c) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  try {
    const view = await c.project();
    const picked = await pick(
      memoryRows(view).map((row) => ({
        label: memoryLabel(row),
        description: memoryLine(row),
        row,
      })),
      { title: `ade: what ade remembers about ${view.name}` },
    );
    if (!picked) return;
    await memoryVerbs(c, view, picked.row);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

async function memoryVerbs(
  c: Client,
  view: ProjectView,
  row: MemoryRow,
): Promise<void> {
  switch (row.kind) {
    case "instructions":
      await edits.show(
        "project-instructions.md",
        view.instructions,
        async (text) => {
          await c.saveProject({ instructions: text.trim() });
          vscode.window.setStatusBarMessage("ade: instructions saved", 3000);
        },
      );
      return;
    case "off":
      await verb("remembering this project", (x) =>
        x.saveProject({ remembered: true }),
      );
      return;
    case "distil":
      await distilMemory();
      return;
    case "entry":
      await entryVerbs(c, row.entry, row.proposed);
  }
}

// entryVerbs is what one remembered thing offers. A proposal is decided; one
// already kept is read, rewritten, or dropped.
async function entryVerbs(
  c: Client,
  entry: MemoryEntry,
  proposed: boolean,
): Promise<void> {
  const KEEP = "Remember this";
  const REJECT = "Reject, and do not propose it again";
  const EDIT = "Edit…";
  const FORGET = "Forget it";
  const what = await pick(proposed ? [KEEP, REJECT] : [EDIT, FORGET], {
    title: entry.heading,
    placeHolder: entry.body.split("\n")[0],
  });
  if (!what) return;
  if (what === EDIT) {
    await edits.show(
      `memory-${entry.id}.md`,
      memoryPage(entry),
      async (text) => {
        const said = memoryParse(text);
        if (!said)
          throw new Error("the first line has to be `# ` and the heading");
        await c.projectEntry("edit", entry.id, said.heading, said.body);
      },
    );
    return;
  }
  const action = { [KEEP]: "accept", [REJECT]: "reject", [FORGET]: "forget" }[
    what
  ] as string;
  if (action !== "accept") {
    const word = action === "reject" ? "Reject" : "Forget";
    const yes = await vscode.window.showWarningMessage(
      `${word} “${entry.heading}”?`,
      {
        modal: true,
        detail:
          action === "reject"
            ? "It will not be proposed again."
            : "It stops being told to the agent.",
      },
      word,
    );
    if (yes !== word) return;
  }
  await verb(`${action}ed “${entry.heading}”`, (x) =>
    x.projectEntry(action, entry.id, entry.heading, entry.body),
  );
}

// checkNetwork asks the launcher what it can reach and opens the answer as a
// page: a refused LAN dial comes with the Settings row to turn on.
async function checkNetwork(): Promise<void> {
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const at = launcher;
  try {
    const found = await vscode.window.withProgress(
      {
        location: vscode.ProgressLocation.Notification,
        title: "ade: checking the network…",
      },
      () => diagnose(at),
    );
    await pages.show("network.md", netReport(found));
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// addMachine bookmarks a machine. The name goes to ssh, so an alias from your
// ssh config is as good as a host name and usually better.
async function addMachine(): Promise<void> {
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const name = await input({
    title: "Add a machine",
    prompt: "An ssh alias, a host name, or user@host — whatever `ssh` takes",
    placeHolder: "pi@pi5.local",
    validateInput: (value) =>
      /^[A-Za-z0-9][A-Za-z0-9._@-]*$/.test(value.trim())
        ? undefined
        : "letters, digits and . _ @ - , not starting with a dash",
  });
  if (!name?.trim()) return;

  const label = await input({
    title: `What to call ${name.trim()}`,
    prompt: "A name for the row. Leave blank to use the host name.",
  });
  if (label === undefined) return;

  try {
    await addHost(launcher, name.trim(), label.trim());
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  await machines.refresh();
}

// pickMachine is the machine list and its verbs. A machine mid-connect offers
// none: the picker is a moment's snapshot, and both verbs would be wrong.
async function pickMachine(): Promise<void> {
  const list = machineRows(await machines.refresh(), machines.doing);
  if (list.length === 0) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const picked = await pick(list, {
    title: "Machines",
    matchOnDescription: true,
  });
  if (!picked) return;

  const offered = machineActs(picked.host);
  if (offered.length === 0) {
    void vscode.window.showInformationMessage(
      picked.host.local
        ? "ade: this Mac is always here."
        : `ade: ${picked.label} is ${picked.description}.`,
    );
    return;
  }
  const act = await pick(
    offered.map((a) => ({ label: a.label, detail: a.detail, id: a.id })),
    { title: picked.label },
  );
  if (!act) return;
  switch (act.id) {
    case "connect":
      return wake(picked.host, true);
    case "disconnect":
      return wake(picked.host, false);
    case "credentials":
      return tellWorker(picked.host, "credentials");
    case "update":
      return tellWorker(picked.host, "update");
    case "stop":
      return stopWorker(picked.host);
    case "rename":
      return renameMachine(picked.host);
    case "forget":
      return forgetMachine(picked.host);
  }
}

// forgetMachine drops the bookmark. The connection goes with it on the server,
// so this is asked for rather than done on a click.
async function forgetMachine(host: Host): Promise<void> {
  launcher = await findLauncher();
  if (!launcher || !host) return;
  const said = await vscode.window.showWarningMessage(
    `Forget ${host.label || host.name}?`,
    {
      modal: true,
      detail:
        "ade stops listing it and drops the connection. Nothing on that machine changes.",
    },
    "Forget",
  );
  if (said !== "Forget") return;
  try {
    await removeHost(launcher, host.name);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
  await machines.refresh();
}

// renameMachine changes what the row is called. The host name is what ssh
// dials, so it is not offered: a different one is a different machine.
async function renameMachine(host: Host): Promise<void> {
  launcher = await findLauncher();
  if (!launcher || !host) return;
  const label = await input({
    title: `What to call ${host.name}`,
    prompt: "A name for the row. Leave blank to use the host name.",
    value: host.label ?? "",
  });
  if (label === undefined) return;
  try {
    await labelHost(launcher, host.name, label.trim());
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
  await machines.refresh();
}

const WORKER: Record<WorkerVerb, { doing: string; done: string }> = {
  credentials: { doing: "syncing", done: "has this Mac's agent logins" },
  update: { doing: "updating", done: "is on the latest images" },
  stop: { doing: "stopping", done: "has no worker running" },
};

async function tellWorker(host: Host, verb: WorkerVerb): Promise<void> {
  launcher = await findLauncher();
  if (!launcher || !host) return;
  const at = launcher;
  const said = host.label || host.name;
  machines.begin(host.name, WORKER[verb].doing);
  try {
    // The outcome reaches the row later, through the hosts list.
    await offstage(() => worker(at, verb, host.name));
    vscode.window.setStatusBarMessage(
      `ade: ${said}, ${WORKER[verb].doing}`,
      3000,
    );
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${said}: ${String(err)}`);
  } finally {
    machines.end(host.name);
  }
  await machines.refresh();
}

// Stop is not Disconnect, and the two read alike, so this one is asked for
// with the difference spelt out.
async function stopWorker(host: Host): Promise<void> {
  if (!host) return;
  const said = await vscode.window.showWarningMessage(
    `Stop the worker on ${host.label || host.name}?`,
    {
      modal: true,
      detail:
        "The container on that machine is removed and whatever is running in it ends. The machine stays in the list. Disconnect only drops the ssh connection and leaves the worker running.",
    },
    "Stop the worker",
  );
  if (said !== "Stop the worker") return;
  await tellWorker(host, "stop");
}

// wake connects a machine or lets it go, then redraws: ssh takes a moment and
// the row has to stop saying "off" once it is not.
async function wake(host: Host, connect: boolean): Promise<void> {
  launcher = await findLauncher();
  if (!launcher) return;
  const at = launcher;
  machines.begin(host.name, connect ? "connecting" : "disconnecting");
  try {
    await offstage(() => reach(at, host.name, connect));
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${host.name}: ${String(err)}`);
  } finally {
    machines.end(host.name);
  }
  await machines.refresh();
}

// Stops the ade serving this checkout and offers to take the worktree with
// it: reviewing a pull request leaves one of each behind.
async function closeSession(): Promise<void> {
  if (!client || !live) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  const root = live.root;
  // /api/worktrees lists the linked checkouts only; the switch list is the
  // one that carries the repository's own, which the launcher prunes against.
  const [trees, switchable] = await Promise.all([
    client.worktrees().catch(() => []),
    client.switchable().catch(() => ({ worktrees: [] as Tree[], here: "" })),
  ]);
  const mine = here(trees, root);
  const repo = mainOf(switchable.worktrees)?.path;

  const parting = await pick(
    PARTINGS.map((p) => ({ label: p.label, description: p.detail, id: p.id })),
    { title: "Exit this session. What happens to the conversation?" },
  );
  if (!parting) return;

  const alsoRemove =
    removable(trees, root) && repo ? await askRemove(mine) : "no";
  if (alsoRemove === "cancelled") return;

  try {
    await client.exit(parting.id);
  } catch (err) {
    // A server that cannot be reached has already gone; only a refusal it
    // answered is a reason to stay attached to it.
    if (!(err instanceof TypeError)) {
      void vscode.window.showErrorMessage(`ade: ${String(err)}`);
      return;
    }
    void vscode.window.showWarningMessage(
      "ade: the server for this folder had already stopped.",
    );
  }
  // Said before the server goes: only the chat knows it was asked for.
  chat.closing();
  teardown();

  const removed =
    alsoRemove === "no" || !repo
      ? undefined
      : await removeHere(repo, root, alsoRemove === "force");
  if (
    closeAfter(
      vscode.workspace.getConfiguration("ade").get<string>("afterCloseSession"),
      removed === false,
    )
  ) {
    await vscode.commands.executeCommand("workbench.action.closeWindow");
    return;
  }
  // The folder this window is open on has just gone, so it cannot stay here.
  if (removed && repo) {
    await switchTo(repo);
    return;
  }
  vscode.window.setStatusBarMessage("ade: session closed", 3000);
}

// The worktree goes through the launcher, once the server in it has gone: a
// server cannot remove the checkout it is serving from. False means it stayed
// and said why, which the window has to live long enough to show.
async function removeHere(
  repo: string,
  root: string,
  force: boolean,
): Promise<boolean> {
  const at = await waitFor(findLauncher);
  if (!at) {
    void vscode.window.showWarningMessage(
      "ade: the session is closed; the launcher is not running, so the worktree stays.",
    );
    return false;
  }
  const out = await pruneAt(at, repo, [root], force).catch((err: unknown) => {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return undefined;
  });
  if (!out) return false;
  if (out.kept?.length) {
    void vscode.window.showWarningMessage(`ade: ${out.kept[0].why}`);
    return false;
  }
  return true;
}

// What removing the worktree would cost, asked before anything is stopped.
async function askRemove(
  mine: Parameters<typeof keeping>[0],
): Promise<"no" | "yes" | "force" | "cancelled"> {
  const why = keeping(mine);
  if (!why) {
    const said = await vscode.window.showWarningMessage(
      `Remove the worktree ${mine?.name} as well?`,
      {
        modal: true,
        detail: `${mine?.path}\n\nThe branch stays. Only the checkout goes.`,
      },
      "Close and remove",
      "Close only",
    );
    if (!said) return "cancelled";
    return said === "Close and remove" ? "yes" : "no";
  }
  if (!canForce(mine)) {
    const said = await vscode.window.showWarningMessage(
      `${mine?.name} has ${why}, so the worktree stays.`,
      {
        modal: true,
        detail: "Closing the session stops ade here. Nothing is removed.",
      },
      "Close only",
    );
    return said ? "no" : "cancelled";
  }
  const said = await vscode.window.showWarningMessage(
    `${mine?.name} has ${why}.`,
    {
      modal: true,
      detail:
        "Removing the worktree leaves the branch. The commits stay on it.",
    },
    "Close and remove anyway",
    "Close only",
  );
  if (!said) return "cancelled";
  return said === "Close and remove anyway" ? "force" : "no";
}

// switchWorktree moves ade itself into another checkout of the repository,
// taking the agent. Not the same as opening the folder: the session goes too.
async function switchWorktree(): Promise<void> {
  if (!client) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  let offered;
  try {
    offered = await client.switchable();
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    return;
  }
  const picked = await pick(
    offered.worktrees
      .filter((t) => t.path !== offered.here)
      .map((t) => ({
        label: t.name,
        description: t.branch ?? t.path,
        path: t.path,
      })),
    { title: "Move this session to which worktree?" },
  );
  if (!picked) return;
  await verb(`moved to ${picked.label}`, (c) => c.switchTo(picked.path));
  await sessions.refresh();
}

// pruneWorktree removes one, asking first. Force is offered only where it
// would help: unpushed work can be let go on purpose, uncommitted never.
// pickWorktree is every checkout of this repository. The graph is its own
// command: a shape is not a list, and a picker cannot draw one.
async function pickWorktree(): Promise<void> {
  if (!client) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  worktreeGraph.stale();
  const { main, linked } = await worktreeTree.refresh();
  const list = treeRows(main, linked);
  if (list.length === 0) {
    void vscode.window.showInformationMessage(
      "ade: this repository has no worktrees.",
    );
    return;
  }
  const picked = await pick(list, {
    title: "Worktrees",
    matchOnDescription: true,
  });
  if (!picked) return;

  const act = await pick(
    treeActs(picked.tree).map((a) => ({
      label: a.label,
      detail: a.detail,
      id: a.id,
    })),
    { title: picked.label },
  );
  if (!act) return;
  switch (act.id) {
    case "open":
      return switchTo(picked.tree.path);
    case "session":
      return newSession(picked.tree as never);
    case "remove":
      return pruneWorktree(picked.tree);
  }
}

async function pruneWorktree(t: Tree | undefined): Promise<void> {
  if (!client || !t) return;
  let force = false;
  if (!t.removable) {
    if (!forceable(t)) {
      void vscode.window.showWarningMessage(`ade: ${why(t)}`);
      return;
    }
    const said = await vscode.window.showWarningMessage(
      `${t.name} has unpushed commits.`,
      {
        modal: true,
        detail:
          "Removing the worktree leaves the branch. The commits stay on it.",
      },
      "Remove anyway",
    );
    if (said !== "Remove anyway") return;
    force = true;
  } else {
    const said = await vscode.window.showWarningMessage(
      `Remove the worktree ${t.name}?`,
      { modal: true, detail: t.path },
      "Remove",
    );
    if (said !== "Remove") return;
  }

  try {
    const out = await client.prune([t.path], force);
    vscode.window.setStatusBarMessage(
      `ade: ${report(out.removed, out.kept)}`,
      5000,
    );
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
  await Promise.all([sessions.refresh(), worktreeTree.refresh()]);
  worktreeGraph.stale();
}

// A fire waits for the run, which is the point: "Run now" wants the report.
// A writes job comes back at once with where it is working instead.
async function runJob(row: unknown): Promise<void> {
  const job = jobOf(row as never);
  if (!job) return;
  const dir = job.allow === "writes" ? await whereToWork() : undefined;
  if (job.allow === "writes" && !dir) return;
  launcher = await findLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage("ade: the launcher is not running.");
    return;
  }
  const at = launcher;
  try {
    const out = await vscode.window.withProgress(
      {
        location: vscode.ProgressLocation.Notification,
        title: `ade: running ${job.name}`,
      },
      () => runJobAt(at, job.name, dir),
    );
    if (out.error) {
      void vscode.window.showErrorMessage(`ade: ${out.error}`);
      return;
    }
    if (job.allow !== "writes" || !out.dir) return;
    const open = await vscode.window.showInformationMessage(
      out.report ?? "ade: started",
      "Open the worktree",
    );
    if (open) await switchTo(out.dir);
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
}

// whereToWork is the checkout a writes job branches beside: this one, or any
// worktree of the repository. The fork lives on a branch, so main is wrong.
async function whereToWork(): Promise<string | undefined> {
  const here = live?.root;
  if (!here) {
    void vscode.window.showWarningMessage(
      "ade: open a checkout first; a writes job branches beside one.",
    );
    return undefined;
  }
  const trees = (repoKept?.trees ?? []).filter((t) => t.path !== here);
  if (trees.length === 0) return here;
  const picked = await pick(
    [
      { label: basename(here), description: "this checkout", path: here },
      ...trees.map((t) => ({
        label: t.branch || basename(t.path),
        description: t.path,
        path: t.path,
      })),
    ],
    { title: "Branch from which checkout?" },
  );
  return picked?.path;
}

async function pauseJob(row: unknown, paused: boolean): Promise<void> {
  const job = jobOf(row as never);
  if (!job) return;
  await jobVerb("/api/jobs/pause", { name: job.name, paused });
}

// Deleting takes the definition and the state with it, so it is asked for.
async function deleteJob(row: unknown): Promise<void> {
  const job = jobOf(row as never);
  if (!job) return;
  const said = await vscode.window.showWarningMessage(
    `Delete the job ${job.name}?`,
    {
      modal: true,
      detail: "Its file and the record of when it last fired both go.",
    },
    "Delete",
  );
  if (said !== "Delete") return;
  await jobVerb("/api/jobs/delete", { name: job.name });
}

// A new job is a file, as a new design document is. The launcher's watcher
// picks it up, so nothing is posted.
async function newJob(): Promise<void> {
  const asked = await input({
    prompt: "What is the job called?",
    placeHolder: "morning-brief",
    validateInput: (value) =>
      /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(value.trim())
        ? undefined
        : "letters, digits, - and _ only",
  });
  if (!asked) return;

  const name = asked.trim();
  const uri = vscode.Uri.file(join(homedir(), ".ade", "jobs", `${name}.md`));
  try {
    await vscode.workspace.fs.stat(uri);
  } catch {
    await vscode.workspace.fs.createDirectory(vscode.Uri.joinPath(uri, ".."));
    await vscode.workspace.fs.writeFile(
      uri,
      Buffer.from(skeleton(name), "utf8"),
    );
  }
  await vscode.window.showTextDocument(uri);
}

// newDesign writes an empty document where the agent looks for them. Named
// after what it designs, lower case with dashes, as the repository's rule says.
async function newDesign(): Promise<void> {
  const dir = design.docs;
  if (!dir) {
    void vscode.window.showErrorMessage(
      "ade: no checkout is being served, so there is nowhere to put it.",
    );
    return;
  }
  const asked = await input({
    prompt: "What does it design?",
    placeHolder: "project-memory",
    validateInput: (value) =>
      /^[a-z0-9][a-z0-9-]*$/.test(value.trim().replace(/\.md$/, ""))
        ? undefined
        : "lower case, digits and dashes",
  });
  if (!asked) return;

  const name = asked.trim().replace(/\.md$/, "") + ".md";
  const uri = vscode.Uri.file(join(dir, name));
  try {
    await vscode.workspace.fs.stat(uri);
  } catch {
    const heading = name.replace(/\.md$/, "").split("-").join(" ");
    const title = heading.charAt(0).toUpperCase() + heading.slice(1);
    await vscode.workspace.fs.createDirectory(vscode.Uri.file(dir));
    await vscode.workspace.fs.writeFile(
      uri,
      Buffer.from(`# ${title}\n\n`, "utf8"),
    );
  }
  await vscode.window.showTextDocument(uri);
}

function windowFolders(): string[] {
  return (vscode.workspace.workspaceFolders ?? []).map((f) => f.uri.path);
}

async function useView(c: Client, view: string): Promise<void> {
  shownView = view;
  const root = view || c.root;
  scm?.use(await indexFor(c, root, view !== ""), root);
}

// openFolder is the checkout this window is on: the nearest to what you are
// reading, as the discovery loop orders them.
function openFolder(): string | undefined {
  const folders = (vscode.workspace.workspaceFolders ?? []).map(
    (f) => f.uri.fsPath,
  );
  if (folders.length === 0) return undefined;
  return nearest(
    folders,
    vscode.window.activeTextEditor?.document.uri.fsPath,
  )[0];
}

// newSession starts a conversation in a checkout, from the tree. The launcher
// owns this: it is the only thing that can start a server ade is not already in.
async function newSession(node?: SessionNode, said?: string): Promise<void> {
  // Nothing named and nothing said: the screen, where the defaults are on
  // show before they are accepted. A tree row or /new already named one.
  if (!node && said === undefined) return showNewSession();

  // The folder this window has open, when nothing is serving it yet: it is
  // the checkout you are looking at, and the one you meant.
  const dir =
    Sessions.checkoutOf(node) ??
    treeOf(node)?.path ??
    live?.root ??
    openFolder();
  if (!dir) {
    void vscode.window.showErrorMessage(
      "ade: pick a checkout to start a session in.",
    );
    return;
  }
  launcher = await ensureLauncher();
  if (!launcher) {
    void vscode.window.showErrorMessage(noLauncher("session"));
    return;
  }
  const name = await workspaceName(dir);
  if (name === undefined) return;
  const message = said ?? (await firstMessage());
  if (!message) return;
  if (name) {
    await newWorkspace(launcher, dir, name, message);
    return;
  }
  // A server already serving this checkout owns its conversation: asking the
  // launcher to open it again reuses that server and changes nothing.
  if (dir === live?.root && client) {
    await newChatHere(message);
    return;
  }
  try {
    await openSessionAt(launcher, dir, true, "", message);
  } catch (err) {
    void vscode.window.showErrorMessage(
      `ade: could not start a session (${String(err)})`,
    );
    return;
  }
  // Same as newChatHere, which ends on the conversation: a session started in
  // another folder should land there too.
  await want(memory, { path: dir, chat: true });
  await switchTo(dir);
}

async function showNewSession(): Promise<void> {
  const dir = live?.root ?? openFolder();
  const home = dir ? await madeFrom(dir) : undefined;
  await newSessionPage.show(home ? { repos: [home] } : {});
}

// sessionOffer is everything the new-session screen can pick from. Asking a
// machine for its repositories connects it, which is how one gets woken up.
async function sessionOffer(machine: string): Promise<Offer> {
  const at = await ensureLauncher();
  if (!at) throw new Error(noLauncher("session"));
  launcher = at;
  const [machines, named] = await Promise.all([
    hosts(at)
      .then((list) => list.filter((host) => !host.local))
      .catch(() => [] as Host[]),
    askLauncher<RolesView>(at, "/api/roles")
      .then((view) => (view.roles ?? []).map((role) => role.name))
      .catch(() => [] as string[]),
  ]);
  const found = machine ? await target(at, machine) : undefined;
  // Both lists when a machine is chosen: the Design row carries a document
  // from this machine's checkout to that machine's. A failed read is not none.
  localRepos = await launcherRepos(at, true);
  remoteRepos = found?.repos ?? [];
  const list = found ? remoteRepos : localRepos;
  const offered = found?.models ?? (await models(at).catch(() => []));
  return {
    ...noOffer,
    machines: machines.map((host) => ({
      name: host.name,
      label: host.label || host.name,
    })),
    repos: list.map(mainCheckout),
    agents: [
      { id: "claude", label: "claude" },
      { id: "codex", label: "codex" },
    ],
    models: offered.map((model) => ({ id: model.id, label: model.label })),
    roles: named,
    recent: recalled()
      .filter((one) => one.kind === "turn")
      .map((one) => one.text),
  };
}

// sessionDesigns is what the Design row lists: the documents on the machine the
// session runs on, and, when that is not this one, this machine's to carry over.
async function sessionDesigns(machine: string, dir: string): Promise<Doc[]> {
  const at = launcher ?? (await findLauncher());
  if (!at) throw new Error(noLauncher("session"));
  const there = (await documentsIn(at, machine, dir)).map((file) => ({
    file,
    ship: false,
  }));
  if (!machine) return there;
  const here = localCounterpart(dir);
  if (!here) return there;
  // Not caught: an empty Here group has to mean there is nothing to carry.
  const ship = (await documentsIn(at, "", here)).map((file) => ({
    file,
    ship: true,
  }));
  return [...ship, ...there];
}

async function documentsIn(
  at: Server,
  machine: string,
  dir: string,
): Promise<string[]> {
  const query = machine ? `&machine=${encodeURIComponent(machine)}` : "";
  const got = await askLauncher<{ designs: string[] | null }>(
    at,
    `/api/designs?dir=${encodeURIComponent(dir)}${query}`,
  );
  return got.designs ?? [];
}

// What each machine has, kept from the last offer so the Design row can match
// a repository on another machine to this machine's checkout of it.
let localRepos: Repo[] = [];
let remoteRepos: Repo[] = [];

// localCounterpart is this machine's checkout of the repository a path on
// another machine names. Matched on owner and name: the disks differ.
function localCounterpart(dir: string): string | undefined {
  const there = remoteRepos.find((repo) => mainCheckout(repo).path === dir);
  if (!there) return undefined;
  const key = (repo: Repo) =>
    `${repo.owner ?? ""}/${repo.name}`.toLowerCase();
  const here = localRepos.find((repo) => key(repo) === key(there));
  return here ? mainCheckout(here).path : undefined;
}

// The repository as one row: its main checkout, which is what a worktree is
// made beside. A machine that has not got it yet names the clone instead.
function mainCheckout(repo: Repo): Offer["repos"][number] {
  const main = (repo.checkouts ?? [])[0];
  return {
    path: main?.path ?? repo.path,
    name: repo.name,
    owner: repo.owner,
    branch: main?.branch,
  };
}

// startChosen routes the screen to the way in it names. Nothing here is new
// behaviour: every row is a door ade already had.
async function startChosen(chosen: Chosen): Promise<void> {
  const at = await ensureLauncher();
  if (!at) throw new Error(noLauncher("session"));
  launcher = at;

  if (chosen.mode === "scratch") {
    // Every default the row showed, not just the name: a choice on screen
    // that Start does not send is a choice accepted without being made.
    const made = await scratch(at, {
      name: chosen.name.trim(),
      agent: chosen.agent || undefined,
      model: chosen.model || undefined,
      role: chosen.role || undefined,
      prompt: chosen.message.trim(),
      machine: chosen.machine || undefined,
      sandbox: chosen.sandbox,
    });
    // The path is that machine's, so it is a window over there and not a
    // folder this disk has never had.
    if (chosen.machine) {
      await openThere(chosen.machine, await labelOf(at, chosen.machine), made.path);
      return;
    }
    await want(memory, { path: made.path, chat: true });
    await switchTo(made.path);
    return;
  }

  const [home, ...also] = chosen.repos;
  if (!home) throw new Error("tick a repository to start in");
  // The document's own path is the name: ade create reuses a document that is
  // already there, so a design row carries on with it rather than making one.
  const name = sessionNames(chosen);
  // Read before the session is asked for: a document that cannot be read is a
  // plain error, not a session open somewhere with the wrong brief.
  const brief = await carried(at, chosen, home);
  const started = await vscode.window.withProgress(
    {
      location: vscode.ProgressLocation.Notification,
      title: `ade: starting ${name || basename(home)}…`,
    },
    () =>
      beginSession(at, {
        dir: home,
        name: name || undefined,
        brief,
        also: also.length > 0 ? also : undefined,
        agent: chosen.agent || undefined,
        model: chosen.model || undefined,
        role: chosen.role || undefined,
        prompt: chosen.message.trim(),
        machine: chosen.machine || undefined,
        sandbox: chosen.sandbox,
      }),
  );

  // begin reports a failure that still opened the session rather than throwing
  // it: the window goes where it got to, and what failed is said.
  if (started.error)
    void vscode.window.showErrorMessage(`ade: ${started.error}`);

  if (chosen.machine) {
    await openThere(
      chosen.machine,
      await labelOf(at, chosen.machine),
      started.path ?? home,
    );
    return;
  }

  // Plan and Design are the document first: the window lands on the worktree
  // with the design open rather than on the conversation.
  const path = started.path ?? home;
  await want(memory, {
    path,
    chat: chosen.mode !== "plan" && chosen.mode !== "design",
  });
  await switchTo(await spanning(path, also));
}

// labelOf is what the machine is called on screen, or its name.
async function labelOf(at: Server, machine: string): Promise<string> {
  const found = (await hosts(at).catch(() => [] as Host[])).find(
    (host) => host.name === machine,
  );
  return found?.label ?? machine;
}

// carried is the document to take with the session, read here and written in
// once it is serving. Only when the machine it runs on has not got it already.
async function carried(
  at: Server,
  chosen: Chosen,
  home: string,
): Promise<{ name: string; body: string } | undefined> {
  // The plan decides, not the field: a document picked on the Design row and
  // left behind by a switch to Work must not be shipped into a named session.
  if (!sessionPlan(chosen).carried) return undefined;
  const here = chosen.machine ? localCounterpart(home) : home;
  if (!here) throw new Error(`no checkout here to read ${chosen.design} from`);
  const got = await askLauncher<{ body: string }>(
    at,
    `/api/design?dir=${encodeURIComponent(here)}&file=${encodeURIComponent(chosen.design)}`,
  );
  return { name: chosen.design, body: got.body };
}

// spanning is what the window opens: the worktree, or a workspace file over
// every repository the session was started across, home first.
async function spanning(path: string, also: string[]): Promise<string> {
  if (also.length === 0) return path;
  try {
    const dir = await gitDir(path);
    if (!dir) return path;
    // Where ade create put them, not where the screen predicted: a branch
    // already checked out elsewhere is worked in where it is.
    const made = await sessionMounts(join(dir, "ade", "session.json"));
    if (made.length === 0) return path;
    const file = join(dir, "ade", "session.code-workspace");
    await vscode.workspace.fs.createDirectory(vscode.Uri.file(join(dir, "ade")));
    await vscode.workspace.fs.writeFile(
      vscode.Uri.file(file),
      Buffer.from(multiRoot(path, made), "utf8"),
    );
    return file;
  } catch {
    return path;
  }
}

// sessionMounts is the other checkouts this session may edit, as the session's
// own mounts file records them.
async function sessionMounts(file: string): Promise<string[]> {
  const raw = await localFiles.text(file);
  if (raw === undefined) return [];
  const held = JSON.parse(raw) as { paths?: { path: string; rw?: boolean }[] };
  return (held.paths ?? []).filter((one) => one.rw).map((one) => one.path);
}

// A name is a branch: the session gets a document and a worktree of its own.
// Empty is a conversation on the branch the checkout is already on.
async function workspaceName(dir: string): Promise<string | undefined> {
  const root = await madeFrom(dir);
  const value = await input({
    title: "New session",
    prompt:
      "Name the work to give it a branch and a worktree, or leave empty to talk on this branch.",
    placeHolder: "cancel-running-process",
    validateInput: (value) => nameProblem(value) ?? wouldMake(value, root),
  });
  return value?.trim();
}

async function newWorkspace(
  at: NonNullable<typeof launcher>,
  dir: string,
  name: string,
  message: string,
): Promise<void> {
  try {
    const made = await vscode.window.withProgress(
      {
        location: vscode.ProgressLocation.Notification,
        title: `ade: making ${name}…`,
      },
      () => workspace(at, dir, name, message),
    );
    await want(memory, { path: made.path, chat: true });
    await switchTo(made.path);
  } catch (err) {
    void vscode.window.showErrorMessage(
      `ade: could not start ${name} (${String(err)})`,
    );
  }
}

// The provider is carried over, not defaulted: a codex session asked to
// start again on claude is a different conversation from the one asked for.
async function newChatHere(message: string): Promise<void> {
  let now: RunState = {} as RunState;
  try {
    now = (await client!.state()).agent ?? now;
  } catch {
    now = {} as RunState;
  }
  await verb("new conversation", (c) =>
    c.newChat(message, now.provider ?? "claude"),
  );
  void chat.reveal(live);
}

export function deactivate(): void {
  teardown();
}

// The attachment goes with it: a client kept past its server makes the next
// thing the window is asked to do fail as a fetch error.
function teardown(): void {
  live = undefined;
  client = undefined;
  stream?.abort();
  stream = undefined;
  threads?.dispose();
  threads = undefined;
  scm?.dispose();
  scm = undefined;
  void vscode.commands.executeCommand("setContext", "ade.gitRoots", []);
  lineage?.dispose();
  lineage = undefined;
}

async function connect(context: vscode.ExtensionContext): Promise<void> {
  if (waiting) {
    clearTimeout(waiting);
    waiting = undefined;
  }
  teardown();

  let found = await find();

  // Start one rather than wait to be told. Once per folder: the retry loop
  // comes back every few seconds, and a spawn per tick is a fork bomb.
  // Never on another machine: there is no cli there to start one with.
  if (
    machineWide(vscode.env.remoteName) &&
    "why" in found &&
    found.checkout &&
    !started.has(found.checkout)
  ) {
    started.add(found.checkout);
    const checkout = found.checkout;
    start(serverArgs, checkout, noAde, (why) => {
      void vscode.window.showErrorMessage(
        `ade could not start in ${basename(checkout)}: ${why}`,
      );
    });
    const came = await waitFor(async () => {
      const again = await find();
      return "server" in again ? again : undefined;
    });
    if (came) found = came;
  }

  if ("why" in found) {
    live = undefined;
    client = undefined;
    runState = undefined;
    marks.apply(undefined, [], [], false);
    await drawLive(false);
    if (diffHeader) diffHeader.description = undefined;
    drawRun();
    drawBoxed();
    panels.serving(urlFor);
    chat.serving(undefined);
    settingsPage.serving(undefined);
    commitView.serving(undefined);
    quizPage.serving(undefined);
    sessions.serving(found.checkout, undefined);
    design.serving(undefined);
    queueTree.serving(undefined);
    issues.serving(undefined);
    changes.serving(undefined);
    driftTree.serving(undefined);
    worktreeTree.serving(undefined);
    worktreeGraph.serving(undefined);
    offline(found.why);
    again(context);
    return;
  }
  const server = found.server;

  live = server;
  desk.scope(server.root);
  panels.serving(urlFor);
  chat.serving(server);
  settingsPage.serving(server);
  machines.stale();

  client = new Client(server);
  await drawLive(true);
  void keepSessions();
  void keepRepo();
  threads = new Threads(client);
  lineage = new Lineage(client);
  scm = new Scm(client, server.root, await indexFor(client, server.root));
  shownView = "";
  void client.diffFolders(windowFolders()).catch(() => {});
  sessions.serving(server.root, client);
  queueTree.serving(client);
  commitView.serving(client);
  quizPage.serving(client);
  issues.serving(client);
  changes.serving(client);
  driftTree.serving(client);
  worktreeTree.serving(client);
  worktreeGraph.serving(client);

  status.text = "$(comment-discussion) ade";
  status.tooltip = `ade is serving ${server.root}`;
  status.show();

  await refresh(client);
  const asked = await take(memory, server.root);
  if (asked?.number) showReview();
  if (asked?.chat) {
    if (asked.session)
      await resume(server, asked.session, asked.agent ?? "", asked.model ?? "");
    void chat.reveal(server);
  }
  listen(client, context);
}

// started is the folders this window has already tried to start a server in.
const started = new Set<string>();

// find is the first folder with a live ade, or why none had one; checkout names
// one that is a checkout without a server, which is worth starting.
async function find(): Promise<
  { server: Server } | { why: string; checkout?: string }
> {
  const folders = vscode.workspace.workspaceFolders ?? [];
  if (folders.length === 0) {
    return {
      why: "this window has no folder open, so there is no checkout to serve",
    };
  }

  const tried: string[] = [];
  let checkout: string | undefined;
  const where = new Map(folders.map((f) => [f.uri.path, f.uri]));
  const roots = nearest(
    folders.map((f) => f.uri.path),
    vscode.window.activeTextEditor?.document.uri.path,
  );
  for (const root of roots) {
    const files = filesOf(where.get(root));
    const dir = await gitDir(root, files);
    if (!dir) {
      tried.push(`${root}: not a git checkout`);
      continue;
    }
    const rec = await read(recordPath(dir), files);
    if (!rec) {
      tried.push(`${root}: no ade running`);
      checkout ??= root;
      continue;
    }
    const server = await probe(rec);
    if (server) return { server };
    tried.push(`${root}: the recorded ade has gone`);
    checkout ??= root;
  }
  return { why: tried.join("\n"), checkout };
}

// filesOf is the disk the folder is on. A ui extension runs here in a window
// on another machine, so a host path goes through the window's file service.
function filesOf(uri: vscode.Uri | undefined): Files {
  if (!uri || uri.scheme === "file") return localFiles;
  const at = (path: string) => uri.with({ path });
  return {
    directory: async (path) => {
      try {
        const info = await vscode.workspace.fs.stat(at(path));
        return (info.type & vscode.FileType.Directory) !== 0;
      } catch {
        return undefined;
      }
    },
    text: async (path) => {
      try {
        return new TextDecoder().decode(
          await vscode.workspace.fs.readFile(at(path)),
        );
      } catch {
        return undefined;
      }
    },
  };
}

// queued runs a row's verb and redraws, so the view shows the new order rather
// than waiting for the next poll to say the click landed.
async function queued(
  row: TreeRow | undefined,
  said: string,
  call: (c: Client, id: string) => Promise<unknown>,
): Promise<void> {
  if (!row?.id) return;
  await verb(said, (c) => call(c, row.id));
  await queueTree.refresh();
}

// showCards puts up what the concierge proposed, one at a time and modal: a
// card starts work, which is not a thing to notice later in a corner.
let showing = false;

async function showCards(): Promise<void> {
  if (showing) return;
  const server = launcher;
  if (!server) return;
  showing = true;
  try {
    for (const card of await cardsWaiting(server).catch(() => [])) {
      await offerCard(server, card);
    }
  } finally {
    showing = false;
  }
}

// offerCard is the press. Start sends exactly what is shown; an edit has the
// desk write the card again, once, and the new one is offered the same way.
async function offerCard(server: Server, card: Card): Promise<void> {
  const pressed = await vscode.window.showInformationMessage(
    cardTitle(card),
    { modal: true, detail: cardDetail(card) },
    "Start",
    "Edit",
  );
  if (pressed === "Start") {
    await pressCard(server, card.id, "start").catch((e: unknown) =>
      vscode.window.showErrorMessage(`ade: ${reason(e)}`),
    );
    return;
  }
  if (pressed !== "Edit") {
    await pressCard(server, card.id, "cancel").catch(() => undefined);
    return;
  }
  const said = await input({
    title: "What should it say instead?",
    placeHolder: "start from the design, not the branch",
  });
  if (said === undefined || !said.trim()) {
    await pressCard(server, card.id, "cancel").catch(() => undefined);
    return;
  }
  const again = await pressCard(server, card.id, "edit", said).catch(
    (e: unknown) => {
      void vscode.window.showErrorMessage(`ade: ${reason(e)}`);
      return undefined;
    },
  );
  if (again) await offerCard(server, again);
}

// openDesk shows the concierge and makes sure its session is up. The launcher
// owns that session: it outlives this window, and every window gets the same.
async function openDesk(): Promise<void> {
  await floatDesk();
}

// COMMENTS_PANEL is how the Comments view is opened, best first.
const COMMENTS_PANEL = [
  "workbench.action.focusCommentsPanel",
  "workbench.panel.comments.focus",
];

// The view names its own focus command, so the id every other view answers to
// is not registered. It is also hideIfEmpty, hence the word when nothing shows.
async function showComments(): Promise<void> {
  for (const command of COMMENTS_PANEL) {
    try {
      await vscode.commands.executeCommand(command);
      return;
    } catch {
      // Try the next; the last one failing is what is reported.
    }
  }
  void vscode.window.showInformationMessage(
    "ade: no comments to show. The panel appears once a review leaves one.",
  );
}

// arrange lays the window out for a session: ade's views on the left, the
// Comments panel below, the concierge on the right, the editor in the middle.
async function arrange(): Promise<void> {
  for (const command of [
    "workbench.view.extension.ade",
    ...COMMENTS_PANEL,
    "workbench.action.focusActiveEditorGroup",
  ]) {
    try {
      await vscode.commands.executeCommand(command);
    } catch {
      // A view that is not there yet is not worth stopping for.
    }
  }
  await dashboard.show();
}

// floatDesk puts the concierge in its own small window that stays on top. The
// ade editor has a command for that; VS Code only has a plain new window.
async function floatDesk(): Promise<void> {
  await startDesk();
  concierge.show(concierge.served);
  try {
    await vscode.commands.executeCommand(
      "workbench.action.moveEditorToFloatingWindow",
    );
  } catch {
    await vscode.commands.executeCommand(
      "workbench.action.moveEditorToNewWindow",
    );
  }
}

// startDesk makes sure the desk's session is up and points the view at it.
// One at a time: showing the view asks for this, and so does the command.
let starting: Promise<void> | undefined;

function startDesk(): Promise<void> {
  starting = starting ?? deskUp().finally(() => (starting = undefined));
  return starting;
}

async function deskUp(): Promise<void> {
  const server = await deskServer();
  if (!server) return;
  try {
    const at = await tell<{ url: string; path: string }>(
      server,
      "/api/concierge",
      {},
    );
    concierge.serving(sessionAt(at.url, at.path));
    // A card drawn while this window was shut is still waiting to be pressed.
    await showCards();
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${reason(err)}`);
  }
}

// sessionAt reads a session's own address out of the url the launcher prints.
// The token rides in the query, which is the only place it is ever written.
function sessionAt(url: string, root: string): Server | undefined {
  try {
    const at = new URL(url);
    const token = at.searchParams.get("t");
    if (!token) return undefined;
    return { base: at.origin, token, root };
  } catch {
    return undefined;
  }
}

// drawDesk keeps the bell in the status bar counting what is open, since the
// secondary sidebar is shut most of the time. Nothing open, nothing shown.
function drawDesk(): void {
  const open = desk.count;
  const late = desk.late;
  if (open === 0) {
    bell.hide();
    return;
  }
  bell.text = late ? `$(bell-dot) ${open}` : `$(bell) ${open}`;
  bell.tooltip = late ? `${late} of ${open} overdue` : `${open} on the list`;
  bell.show();
}

// deskServer is the launcher the todos live in, started if there is not one.
async function deskServer(): Promise<Server | undefined> {
  launcher = launcher ?? (await findLauncher()) ?? (await ensureLauncher());
  if (!launcher) void vscode.window.showErrorMessage(noLauncher("surface"));
  return launcher;
}

// here is the checkout this window is in, which is the default scope for a
// capture. Without one there is only the machine's list.
function hereDir(): string | undefined {
  return live?.root;
}

// todoFromThread files a review thread as a todo: a finding that is right but
// not for this turn becomes a row in the list instead of a comment left open.
async function todoFromThread(arg: unknown): Promise<void> {
  const id = threads?.threadId(arg);
  const thread = threads?.all.find((t) => t.id === id);
  if (!thread) return;
  const server = await deskServer();
  if (!server) return;
  const made = todoFrom(thread);
  try {
    await fileTodo(server, { ...made, dir: hereDir() });
  } catch (e) {
    void vscode.window.showErrorMessage(`ade: ${reason(e)}`);
    return;
  }
  await desk.refresh();
  vscode.window.setStatusBarMessage(`ade: todo filed · ${made.text}`, 4000);
}

// captureTodo is the whole capture: a line of text, and a button to file it
// for the machine rather than this checkout. Everything else waits.
async function captureTodo(): Promise<void> {
  const server = await deskServer();
  if (!server) return;
  const checkout = hereDir();
  const box = inputBox();
  let dir = checkout;
  const scope: vscode.QuickInputButton = {
    iconPath: new vscode.ThemeIcon("globe"),
    tooltip: "File it for this machine instead",
  };
  const draw = () => {
    box.title = dir ? `Todo for ${basename(dir)}` : "Todo for this machine";
    box.buttons = checkout ? [scope] : [];
  };
  box.placeholder = "What do you want to get to? A #word groups it";
  draw();
  box.onDidTriggerButton(() => {
    dir = dir ? undefined : checkout;
    draw();
  });
  box.onDidAccept(() => {
    const text = box.value.trim();
    box.hide();
    if (!text) return;
    void (async () => {
      await fileTodo(server, { text, dir }).catch((e: unknown) =>
        vscode.window.showErrorMessage(`ade: ${reason(e)}`),
      );
      await desk.refresh();
    })();
  });
  box.onDidHide(() => box.dispose());
  box.show();
}

// pickTodo is the list. A quick pick rather than a pane: the todos are read in
// bursts and acted on one at a time, which is what a keystroke list is for.
async function pickTodo(): Promise<void> {
  const server = await deskServer();
  if (!server) return;
  await desk.refresh();

  const pick = quickPick<TodoRow>();
  pick.title = "ade: Todo";
  pick.placeholder = "Filter, #group, or type something new and press Enter";
  pick.matchOnDescription = true;
  const draw = () => {
    pick.items = todoRows(desk.list, new Date());
  };
  draw();
  pick.buttons = [TODO_ADD];
  pick.onDidTriggerButton(() => {
    pick.hide();
    void captureTodo();
  });
  pick.onDidTriggerItemButton(async (e) => {
    const item = e.item.todo;
    if (!item) return;
    if (e.button === TODO_DONE || e.button === TODO_REOPEN)
      await patchTodo(item, { done: !doneTodo(item) });
    if (e.button === TODO_DUE) {
      pick.hide();
      await dateTodo(item);
      return;
    }
    if (e.button === TODO_DROP) await deleteTodo(item);
    draw();
  });
  // Nothing matched means the words are new: Enter files them rather than
  // closing an empty list, which is the capture that costs least of all.
  pick.onDidAccept(() => {
    const chosen = pick.selectedItems[0];
    const typed = pick.value.trim();
    pick.hide();
    if (chosen?.todo) {
      void todoActions(chosen.todo);
      return;
    }
    if (typed) void fileTyped(server, typed);
  });
  pick.onDidHide(() => pick.dispose());
  pick.show();
}

async function fileTyped(server: Server, text: string): Promise<void> {
  await fileTodo(server, { text, dir: hereDir() }).catch((e: unknown) =>
    vscode.window.showErrorMessage(`ade: ${reason(e)}`),
  );
  await desk.refresh();
}

// todoActions is what a row opens onto, the two that turn a note into work
// first. A finished todo is offered neither of them.
async function todoActions(item: Todo): Promise<void> {
  const open = !doneTodo(item);
  const picked = await pick(
    [
      ...(open
        ? [
            { label: "$(comment-discussion) Start a conversation", id: "talk" },
            { label: "$(new-file) Start a design", id: "design" },
          ]
        : []),
      {
        label: open ? "$(check) Done" : "$(issue-reopened) Not done after all",
        id: "done",
      },
      { label: "$(calendar) Set a due date", id: "due" },
      { label: "$(trash) Delete", id: "drop" },
    ],
    { title: item.text },
  );
  if (!picked) return;
  if (picked.id === "talk") await startTodo(item);
  if (picked.id === "design") await designTodo(item);
  if (picked.id === "done") await patchTodo(item, { done: !doneTodo(item) });
  if (picked.id === "due") await dateTodo(item);
  if (picked.id === "drop") await deleteTodo(item);
}

// startTodo turns the note into work: a new conversation in this window's
// checkout, with the todo as its first turn.
async function startTodo(item: Todo): Promise<void> {
  const server = await deskServer();
  const dir = hereDir();
  if (!server) return;
  if (!dir) {
    void vscode.window.showErrorMessage(
      "ade: no checkout is open here, so there is nowhere to start it.",
    );
    return;
  }
  try {
    await openSessionAt(server, dir, true, "", item.text);
    await patchTodo(item, { started: basename(dir) });
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${reason(err)}`);
  }
}

// designTodo writes the note into a design document and opens it. The todo is
// then in progress: the document is where the thinking goes from here.
async function designTodo(item: Todo): Promise<void> {
  const dir = design.docs;
  if (!dir) {
    void vscode.window.showErrorMessage(
      "ade: no checkout is being served, so there is nowhere to put it.",
    );
    return;
  }
  const asked = await designName(item.text);
  const name = (asked ?? "").trim().replace(/\.md$/, "");
  if (!name) return;
  const uri = vscode.Uri.file(join(dir, name + ".md"));
  try {
    await vscode.workspace.fs.stat(uri);
  } catch {
    const heading = name.split("-").join(" ");
    await vscode.workspace.fs.createDirectory(vscode.Uri.file(dir));
    await vscode.workspace.fs.writeFile(
      uri,
      Buffer.from(
        `# ${heading.charAt(0).toUpperCase() + heading.slice(1)}\n\n${item.text}\n`,
        "utf8",
      ),
    );
  }
  await vscode.window.showTextDocument(uri);
  await patchTodo(item, { started: name + ".md" });
}

// designName offers the todo's own words as the file name, which is right
// often enough to be one keystroke.
function designName(text: string): Thenable<string | undefined> {
  return input({
    title: "What does it design?",
    value: slug(text),
    validateInput: (value) =>
      /^[a-z0-9][a-z0-9-]*$/.test(value.trim().replace(/\.md$/, ""))
        ? undefined
        : "lower case, digits and dashes",
  });
}

function slug(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "")
    .split("-")
    .filter(Boolean)
    .slice(0, 5)
    .join("-");
}

// dateTodo takes a date in the words you would say, not a picker.
async function dateTodo(item: Todo): Promise<void> {
  const said = await input({
    title: "Due when?",
    placeHolder: "today · tomorrow · in 3 days · 2026-12-01 · none",
    value: "",
  });
  if (said === undefined) return;
  const due = dueFrom(said, new Date());
  if (due === undefined) {
    void vscode.window.showErrorMessage(`ade: "${said}" is not a date I read.`);
    return;
  }
  await patchTodo(item, { due });
}

async function deleteTodo(item: Todo): Promise<void> {
  const server = await deskServer();
  if (!server || !item?.id) return;
  await dropTodo(server, item.id, desk.where(item)).catch((e: unknown) =>
    vscode.window.showErrorMessage(`ade: ${reason(e)}`),
  );
  await desk.refresh();
}

async function patchTodo(
  item: Todo,
  change: { done?: boolean; due?: string; started?: string },
): Promise<void> {
  const server = await deskServer();
  if (!server || !item?.id) return;
  await changeTodo(server, item.id, {
    ...change,
    dir: desk.where(item),
  }).catch((e: unknown) => vscode.window.showErrorMessage(`ade: ${reason(e)}`));
  await desk.refresh();
}

// counted keeps a view's title counting what it holds. The provider's change
// event is the signal: reading the count fetches nothing.
function counted(
  view: vscode.TreeView<unknown>,
  provider: { onDidChangeTreeData: vscode.Event<void>; count: number },
  word = "",
): vscode.Disposable {
  const draw = () => {
    view.description = countLabel(provider.count, word);
  };
  draw();
  return provider.onDidChangeTreeData(draw);
}

const PROMOTE: vscode.QuickInputButton = {
  iconPath: new vscode.ThemeIcon("arrow-up"),
  tooltip: "Run this one next",
};
const REWORD: vscode.QuickInputButton = {
  iconPath: new vscode.ThemeIcon("edit"),
  tooltip: "Change what it asks",
};
const REMOVE: vscode.QuickInputButton = {
  iconPath: new vscode.ThemeIcon("trash"),
  tooltip: "Remove from the queue",
};

// showQueue lists the waiting turns, each with a promote and a remove button.
// It stays open across an action, so several can go in one visit.
async function showQueue(): Promise<void> {
  const c = client;
  if (!c) {
    void vscode.window.showErrorMessage(
      "ade: nothing is running for this folder yet.",
    );
    return;
  }
  type Item = vscode.QuickPickItem & { id?: string; message?: string };
  const pick = quickPick<Item>();
  pick.title = "ade: queued turns";
  pick.placeholder = "Nothing is queued";
  const load = async () => {
    pick.busy = true;
    try {
      const agent = (await c.state()).agent;
      pick.items = queueRows(agent?.waiting ?? [], Boolean(agent?.running)).map(
        // Only where there is wording: the rest are buttons someone pressed.
        (row) => ({
          ...row,
          buttons: row.id
            ? row.message
              ? [REWORD, PROMOTE, REMOVE]
              : [PROMOTE, REMOVE]
            : [],
        }),
      );
    } catch (err) {
      void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    }
    pick.busy = false;
  };
  let rewording = false;
  pick.onDidTriggerItemButton(async ({ item, button }) => {
    if (!item.id) return;
    try {
      if (button === REWORD) {
        // One quick input shows at a time: the list steps aside, then returns.
        rewording = true;
        pick.hide();
        const said = await input({
          title: "Change what this queued turn asks",
          value: item.message ?? "",
          prompt: "It keeps its place in the queue",
        }).finally(() => {
          rewording = false;
        });
        if (said !== undefined && said.trim())
          await c.editQueued(item.id, said);
        pick.show();
      } else if (button === REMOVE) await c.dropQueued(item.id);
      else await c.promoteQueued(item.id);
    } catch (err) {
      void vscode.window.showErrorMessage(`ade: ${String(err)}`);
    }
    await load();
  });
  pick.onDidAccept(() => {
    const [chosen] = pick.selectedItems;
    if (!chosen || chosen.id) return;
    pick.hide();
    void verb("queue running", (c) => c.runQueued());
  });
  pick.onDidHide(() => {
    if (!rewording) pick.dispose();
  });
  pick.show();
  await load();
}

// nudge badges the view and says once that a question has arrived. The toast
// carries its first line; the question itself stays in the view.
function nudge(fresh: Fresh): void {
  if (inboxView) {
    inboxView.badge =
      inbox.count > 0
        ? {
            value: inbox.count,
            tooltip: `${inbox.count} waiting from the team`,
          }
        : undefined;
  }
  for (const who of fresh.back) {
    if (!watchedBack.delete(who)) continue;
    void vscode.window
      .showInformationMessage(`${who} is back online`, "Message them")
      .then((pressed) => {
        if (pressed === "Message them")
          void messageTeamMember({ kind: "member", member: { user: who } });
      });
  }
  for (const reply of fresh.replies) void announceReply(reply);
  for (const offer of fresh.offers.filter((one) => one.kind === "ask")) {
    const line = (offer.question ?? "").split("\n")[0] ?? "";
    void vscode.window
      .showInformationMessage(
        `${offer.from} asks: ${line.length > 80 ? line.slice(0, 79) + "…" : line}`,
        "Answer",
        "Later",
      )
      .then((pressed) => {
        if (pressed === "Answer") void answerAsk(offer);
      });
  }
}

// answerAsk puts the question the way its shape deserves — a quick pick for a
// short one, a panel for a long one — and sends what you said back.
async function answerAsk(offer: BusOffer): Promise<void> {
  const c = client;
  if (!c) return;
  const answer = await askOffer(offer);
  if (!answer) return;
  try {
    await c.busAnswer(offer.id, answer.option ?? "", answer.text ?? "");
    vscode.window.setStatusBarMessage(`answered ${offer.from}`, 4000);
  } catch (err) {
    void vscode.window.showWarningMessage(`ade: ${String(err)}`);
  }
  await refresh(c);
}

// takeReview claims the request and checks the pull request out, which is a
// worktree and a ade of its own.
async function takeReview(offer: BusOffer): Promise<void> {
  const c = client;
  if (!c) return;
  const name = `${offer.repo}#${offer.number}`;
  try {
    const got = await vscode.window.withProgress(
      {
        location: vscode.ProgressLocation.Window,
        title: `ade: checking out ${name}`,
      },
      () => c.busClaim(offer.id, true),
    );
    await vscode.commands.executeCommand(
      "vscode.openFolder",
      vscode.Uri.file(got.path),
      {
        forceNewWindow: true,
      },
    );
  } catch (err) {
    void vscode.window.showWarningMessage(`ade: ${reason(err)}`);
  }
  await refresh(c);
}

// tellBusHere passes the window's focus to ade, which turns it into presence
// on agentbus. Quiet when there is no server: this is a nicety, not a feature.
async function tellBusHere(focused: boolean): Promise<void> {
  try {
    await client?.busHere(focused);
  } catch (err) {
    console.debug("ade: focus", err);
  }
}

// setPresence fixes how you look to the team. The window cannot know you have
// stepped out, so this is the one thing it cannot work out for itself.
async function setPresence(): Promise<void> {
  const c = client;
  if (!c) return;
  const picked = await pick(
    PRESENCES.map((one) => ({
      label: one.label,
      detail: one.detail,
      to: one.to,
    })),
    { title: "How should the team see you?" },
  );
  if (!picked) return;
  try {
    const now = await c.busPresence(picked.to);
    vscode.window.setStatusBarMessage(`ade: showing as ${now.presence}`, 4000);
  } catch (err) {
    void vscode.window.showWarningMessage(`ade: ${reason(err)}`);
  }
}

// watchedBack is who you asked to be told about. One shot each: the message
// clears it, so a person who comes and goes does not nag.
const watchedBack = new Set<string>();

// notifyWhenBack watches one person, from their row. Nothing is sent to them.
async function notifyWhenBack(node: unknown): Promise<void> {
  const who = userOf(node);
  if (!who) return;
  if (watchedBack.has(who)) {
    watchedBack.delete(who);
    vscode.window.setStatusBarMessage(`ade: no longer watching ${who}`, 4000);
    return;
  }
  watchedBack.add(who);
  vscode.window.setStatusBarMessage(`ade: will say when ${who} is back`, 4000);
}

// sendBranchPRTo offers this branch's pull request to one person, skipping the
// picker: their row already named them.
async function sendBranchPRTo(node: unknown): Promise<void> {
  const c = client;
  if (!c) return;
  const to = userOf(node);
  if (!to) return;
  const url = (await pr())?.url;
  if (!url) {
    void vscode.window.showWarningMessage(
      "ade: this branch has no pull request to send",
    );
    return;
  }
  const note = await input({
    title: `Ask ${to} to review ${url.split("/").slice(-3).join("/")}`,
    prompt: "optional — what to look at, or why it matters",
  });
  if (note === undefined) return;
  try {
    const sent = await c.busRequestReview(url, to, note.trim());
    vscode.window.setStatusBarMessage(`ade: ${askedLine(sent)}`, 6000);
  } catch (err) {
    void vscode.window.showWarningMessage(`ade: ${reason(err)}`);
  }
}

// messageTeamMember puts a question to one person, from their row. The answer
// comes back as a note in this conversation, so nothing waits here.
async function messageTeamMember(node: unknown): Promise<void> {
  const c = client;
  if (!c) return;
  // No row means the palette asked, so this is where you choose who.
  const to = userOf(node) || (await pickSomebody(c));
  if (!to) return;
  const question = await input({
    title: `Message ${to}`,
    prompt:
      "they see it with your name; the answer comes back as a notification",
    placeHolder: "what do you want to ask them?",
  });
  if (!question?.trim()) return;
  try {
    const sent = await c.busAsk(question.trim(), to);
    vscode.window.setStatusBarMessage(`ade: asked ${sent.to}`, 5000);
  } catch (err) {
    void vscode.window.showWarningMessage(`ade: ${reason(err)}`);
  }
}

// pickSomebody is who to ask, when no row named one.
async function pickSomebody(c: Client): Promise<string | undefined> {
  const who = await c.busWho().catch((): BusWho => ({ online: false }));
  if (!who.online) {
    void vscode.window.showWarningMessage(
      "ade: this session is not on agentbus",
    );
    return undefined;
  }
  const rows = busAskable(who.instances, inbox.user);
  const picked = await pick(targetItems(rows), {
    title: "Who do you want to message?",
    placeHolder: rows.length ? "who to ask" : "nobody is online",
  });
  if (!picked) return undefined;
  if (picked.label !== OTHER) return picked.to;
  const typed = await input({
    title: "Who do you want to message?",
    prompt: "an email, a group name, or user@host/instance-id",
  });
  return typed?.trim() || undefined;
}

// dismissReply clears one, here and at the launcher. The launcher holds an
// answer until somebody says they have read it.
async function dismissReply(id: unknown): Promise<void> {
  if (typeof id !== "string" || !id) return;
  await client?.busDismiss(id).catch(() => undefined);
  if (client) await refresh(client);
}

// announceReply is the answer coming back. It is also in the conversation as
// a note; this is for when you are not looking at it.
async function announceReply(reply: BusReply): Promise<void> {
  const picked = await vscode.window.showInformationMessage(
    `ade · ${replyLine(reply)}`,
    "Open chat",
    "Dismiss",
  );
  if (picked === "Open chat") void chat.reveal(live);
  if (picked === "Dismiss") await dismissReply(reply.id);
}

// askTeamForReview offers a pull request to the team over agentbus. Whoever
// claims it checks it out in their own ade; no row means this branch's own.
async function askTeamForReview(arg: unknown): Promise<void> {
  const c = client;
  if (!c) return;
  const url = await reviewTarget(arg, pr);
  if (!url) return;

  const who = await c.busWho().catch((): BusWho => ({ online: false }));
  if (!who.online) {
    void vscode.window.showWarningMessage(
      "ade: this session is not on agentbus",
    );
    return;
  }

  const rows = busReviewers(who.instances, inbox.user);
  const picked = await pick(targetItems(rows), {
    title: "Ask for a review of " + url.split("/").slice(-3).join("/"),
    placeHolder: rows.length
      ? "who should review it"
      : "nobody is online who takes reviews",
  });
  if (!picked) return;

  let to = picked.to ?? "";
  if (picked.label === OTHER) {
    const typed = await input({
      title: "Who should review it",
      prompt: "an email, a group name, or user@host/instance-id",
    });
    if (!typed?.trim()) return;
    to = typed.trim();
  }

  const note = await input({
    title: "Anything they should know",
    prompt: "optional — what to look at, or why it matters",
  });
  if (note === undefined) return;

  try {
    const sent = await c.busRequestReview(url, to, note.trim());
    vscode.window.setStatusBarMessage(`ade: ${askedLine(sent)}`, 6000);
  } catch (err) {
    void vscode.window.showWarningMessage(`ade: ${String(err)}`);
  }
}

// drawBoxed says whether the agent is in a container, and is the way out.
function drawBoxed(): void {
  const badge = boxedBadge(runState?.sandbox, runState?.running);
  if (!badge || !live) {
    boxed.hide();
    return;
  }
  boxed.text = badge.text;
  boxed.tooltip = badge.tooltip;
  // Filled, so a sandboxed session is visible without reading the words.
  boxed.backgroundColor = new vscode.ThemeColor(badge.background);
  boxed.show();
}

// drawRun puts what the agent is doing on the status bar. Clicking it opens
// the conversation, which is the only place the detail is.
function drawRun(): void {
  const badge = live
    ? runBadge(
        runState,
        since(runState, Date.now()),
        quiet(runState, Date.now()),
      )
    : undefined;
  if (!badge) {
    run.hide();
    titleStatus(undefined);
    return;
  }
  run.text = badge.text;
  run.tooltip = badge.tooltip;
  // An idle agent with a queue has nothing in the chat to look at, and a run
  // going round is worth acting on rather than only reading.
  run.command = goingRound(runState)
    ? "ade.stuck"
    : offersCommit(runState)
      ? "ade.writeCommitMessage"
      : !runState?.running && runState?.queued
        ? "ade.showQueue"
        : "ade.openChat";
  titleStatus(badge);
  run.backgroundColor = badge.warn
    ? new vscode.ThemeColor("statusBarItem.warningBackground")
    : undefined;
  run.show();
}

// titleStatus mirrors the run badge into the ade editor's title bar, once per
// change. VS Code has no such command, and after one refusal is not asked again.
let titleShown = "";
let titleRefused = false;

// titleSession names this window's session beside the run badge, so a glance
// at any window says which conversation it is. Clicking it switches.
let sessionShown = "";

function titleSession(): void {
  const id = runState?.session;
  const row = id
    ? sessionsKept?.rows.find((r) => r.session.id === id)
    : undefined;
  const title = live
    ? runState?.title || row?.session.title || row?.session.branch || ""
    : "";
  if (titleRefused || title === sessionShown) return;
  sessionShown = title;
  void vscode.commands
    .executeCommand("workbench.titleBar.setStatus", {
      id: "ade.session",
      text: title ? `$(comment-discussion) ${title}` : undefined,
      tooltip: row ? `${row.repo} · ${row.session.branch}` : undefined,
      command: "ade.switchSessionHere",
    })
    .then(undefined, () => (titleRefused = true));
}

function titleStatus(
  badge: { text: string; tooltip?: string; warn?: boolean } | undefined,
): void {
  const key = badge ? `${badge.text}|${badge.warn ? 1 : 0}|${run.command}` : "";
  if (titleRefused || key === titleShown) return;
  titleShown = key;
  vscode.commands
    .executeCommand("workbench.titleBar.setStatus", {
      id: "ade",
      text: badge?.text,
      tooltip: [
        typeof badge?.tooltip === "string" ? badge.tooltip : "",
        [runState?.model, runState?.sandbox ? "sandboxed" : ""]
          .filter(Boolean)
          .join(" · "),
      ]
        .filter(Boolean)
        .join("\n"),
      command: typeof run.command === "string" ? run.command : undefined,
      warn: badge?.warn,
    })
    .then(undefined, () => (titleRefused = true));
}

function drawChecks(state: VerifyState | null | undefined): void {
  const badge = live ? checksBadge(state) : undefined;
  if (!badge) {
    checks.hide();
    return;
  }
  checks.text = badge.text;
  checks.tooltip = badge.tooltip;
  checks.backgroundColor = badge.warn
    ? new vscode.ThemeColor("statusBarItem.errorBackground")
    : undefined;
  checks.show();
}

// offerChecks puts a detected list to you once per window. Declining saves an
// empty list, which is what stops the server offering again.
async function offerChecks(found: VerifyCheck[]): Promise<void> {
  if (offered || !client) return;
  offered = true;
  const list = found.map((check) => check.run).join("\n");
  const yes = await vscode.window.showInformationMessage(
    "Run this repository's checks after each turn?",
    {
      modal: true,
      detail: `${list}\n\nThey run after a turn that changed files, and a failure is handed to the agent with your next message.`,
    },
    "Run these",
  );
  await verb(yes ? "checks set up" : "checks off", (c) =>
    c.saveChecks(yes ? found : []),
  );
  if (client) await refresh(client);
}

// pickChecks is the status bar's click: run them, or edit the list.
async function runOrEditChecks(): Promise<void> {
  if (!client) return;
  try {
    const have = await client.checks();
    const RUN = "Run them now";
    const EDIT = "Edit the list…";
    const picked = await pick(have.checks.length ? [RUN, EDIT] : [EDIT], {
      title: `ade checks: ${checksSummary(have.checks)}`,
    });
    if (picked === RUN) {
      await verb("running the checks", (c) => c.runChecks());
      return;
    }
    if (picked !== EDIT) return;
    const edited = await input({
      title: "The checks, as name=command separated by |",
      value: typedChecks(
        have.checks.length ? have.checks : (have.detected ?? []),
      ),
      prompt: "Empty turns the checks off.",
    });
    if (edited === undefined) return;
    await verb("checks saved", (c) =>
      c.saveChecks(keptChecks(parseChecks(edited), have.checks)),
    );
  } catch (err) {
    void vscode.window.showErrorMessage(`ade: ${String(err)}`);
  }
  if (client) await refresh(client);
}

// drawPlan puts the plan windows on the bar. No launcher or no reading hides
// it: a stale number reads as a current one.
async function drawPlan(): Promise<void> {
  const at = await findLauncher();
  const read = at ? await usageOf(at).catch(() => undefined) : undefined;
  const badge = usageBadge(read, Date.now());
  if (!badge) {
    plan.hide();
    return;
  }
  plan.text = badge.text;
  plan.tooltip = badge.tooltip;
  plan.backgroundColor = badge.warn
    ? new vscode.ThemeColor("statusBarItem.warningBackground")
    : undefined;
  plan.show();
}

function offline(why: string): void {
  status.text = "$(circle-slash) ade";
  status.tooltip = new vscode.MarkdownString(
    `**ade is not connected**\n\n${machineWide(vscode.env.remoteName) ? why : whyNotHere("session")}`,
  );
  status.show();
}

async function refresh(client: Client): Promise<void> {
  try {
    const state = await client.state();
    if (!sessionsKept || Date.now() - sessionsKept.at > 30_000)
      void keepSessions();
    threads?.apply(state.threads ?? [], state.branchPr ?? undefined);
    queueTree.show(state.agent?.waiting ?? []);
    scm?.apply(state);
    const idle = Boolean(runState?.running) && !state.agent?.running;
    const moved = state.design?.file !== driftTree.file;
    driftTree.place(live?.root, state.design?.dir);
    if (idle || moved) void driftTree.refresh();
    if (idle) {
      worktreeTree.stale();
      worktreeGraph.stale();
    }
    runState = state.agent ?? undefined;
    lastWorktree = state.worktree ?? undefined;
    marks.apply(
      live?.root,
      runState?.touched ?? [],
      state.threads ?? [],
      Boolean(runState?.running),
    );
    titleSession();
    updateWell();
    dashboard.stale();
    nudge(inbox.apply(state.bus));
    if (diffHeader)
      diffHeader.description = state.meta?.view
        ? basename(state.meta.view)
        : label(state.meta?.project, live?.root);
    design.serving(state.design?.dir);
    drawRun();
    drawBoxed();
    commitView.apply(state);
    quizPage.apply(state);
    drawChecks(state.verify);
    if (state.verify?.offer?.length) void offerChecks(state.verify.offer);
    await drawScope(state.meta?.scope);
    void vscode.commands.executeCommand(
      "setContext",
      "ade.diffRoots",
      (state.meta?.roots?.length ?? 0) > 1,
    );
    // The server takes the diff home when its folder leaves the window.
    if ((state.meta?.view ?? "") !== shownView)
      await useView(client, state.meta?.view ?? "");
    await changes.at(state.diffVersion);
    await lineage?.refresh();
  } catch (err) {
    console.error("ade: refresh", err);
  }
}

// Keyed by the word the row showed while the action ran.
const SETTLED: Record<string, string> = {
  syncing: "has this Mac's agent logins",
  updating: "is on the latest images",
  stopping: "has no worker running",
};

// The launcher outlives any one session, so teardown does not drop this.
function watchLauncher(): void {
  if (hubWatch) return;
  const control = new AbortController();
  hubWatch = control;
  void (async () => {
    while (!control.signal.aborted) {
      const at = await findLauncher();
      if (at)
        await watchLauncherHub(
          at,
          (event) => void fromLauncher(event),
          control.signal,
        )
          // A dropped stream is the launcher going away; the wait is the retry.
          .catch(() => undefined);
      if (control.signal.aborted) return;
      await new Promise((done) => setTimeout(done, retry));
    }
  })();
}

async function fromLauncher(event: string): Promise<void> {
  if (event === "todos") {
    await desk.refresh();
    return;
  }
  if (event === "cards") {
    await showCards();
    return;
  }
  if (event !== "hosts") return;
  await machines.refresh();
  for (const done of machines.settled()) {
    if (done.failed) {
      void vscode.window.showErrorMessage(`ade: ${done.label}: ${done.failed}`);
      continue;
    }
    vscode.window.setStatusBarMessage(
      `ade: ${done.label} ${SETTLED[done.doing] ?? "is done"}`,
      3000,
    );
  }
}

const REDRAWN = new Set(["threads", "state", "lineage", "agent", "resync"]);

// listen holds the event stream open. An event name means refetch; the payload
// is not read, which is how the browser treats it too.
function listen(client: Client, context: vscode.ExtensionContext): void {
  const control = new AbortController();
  stream = control;

  void (async () => {
    try {
      const res = await fetch(client.eventsURL(), { signal: control.signal });
      // resync is the server saying this stream dropped frames.
      if (res.body)
        await events(res.body, (name) => {
          if (REDRAWN.has(name)) void refresh(client);
        });
    } catch {
      // An aborted stream is a teardown, and a dropped one is ade going away.
    }
    if (!control.signal.aborted) again(context);
  })();
}

// fileName is the last part of a path, for a file that would not parse as a
// job and so has no name of its own.
function fileName(path: string): string {
  return path.split("/").pop() ?? path;
}
