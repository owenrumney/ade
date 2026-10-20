import { realpath } from "node:fs/promises";
import * as vscode from "vscode";
import type { BranchPR, Check, Client, Listed, Tree } from "./client";
import type { Checkout, Machine, Repo, Session } from "./launcher";
import { find, hostRepos, repos } from "./launcher";
import {
  count as countIn,
  cut,
  elsewhere,
  label,
  labelAll,
  mine,
  only,
  repoContext,
  sessionLine,
  sessionsAcross,
  sessionContext,
  tally,
  title,
  trim,
  workspacesOf,
  type Worktree,
} from "./shape";
import { sessionsIn } from "./counts";
import { failing, rankPR } from "./issues";
import {
  branchItem,
  checkItem,
  listedItem,
  pullsItem,
  sortedChecks,
  type PullNode,
} from "./pulls";
import { context, icon, line } from "./tree";
import { offstage } from "./loading";

export type SessionNode = Node;

// The tree is this checkout's: the repository at the root, its pull requests
// and worktrees under it, and every other repository behind one Elsewhere row.
type Node =
  | { kind: "root"; repo?: Repo; path: string }
  | { kind: "pulls" }
  | PullNode
  | { kind: "worktree"; worktree: Worktree }
  | { kind: "session"; session: Session; here: boolean; machine?: string }
  | { kind: "suggest"; checkout: Checkout }
  | { kind: "elsewhere" }
  | { kind: "machine"; machine: Machine }
  | { kind: "repo"; repo: Repo; machine?: string }
  | { kind: "more"; what: "repos" | "sessions"; count: number };

type Cut = { shown: Session[]; hidden: number };

export class Sessions implements vscode.TreeDataProvider<Node> {
  private changed = new vscode.EventEmitter<void>();
  readonly onDidChangeTreeData = this.changed.event;
  private held: Repo[] = [];
  private machines: Machine[] = [];
  private trees: Tree[] = [];
  private listed: Listed[] = [];
  private branch?: BranchPR;
  private checks = new Map<number, PullNode[]>();
  private client?: Client;
  private loading?: Promise<void>;
  private generation = 0;
  // The root resolved, like every path loaded: the launcher spells a checkout
  // as the roots do and git as it resolves, so a symlink would never join.
  private key?: string;
  // Whether the quiet repositories and the capped sessions are showing. Per
  // window and not saved: it is a look at the full list, not a preference.
  private all = false;

  constructor(private root: string | undefined) {}

  showAll(): void {
    this.all = true;
    this.changed.fire();
  }

  collapse(): void {
    this.all = false;
    this.changed.fire();
  }

  // Nothing is read here. The pull request list is a gh call and the view
  // starts collapsed, so the first getChildren does the load, as Fetch does.
  serving(root: string | undefined, client: Client | undefined): void {
    this.root = root;
    this.client = client;
    this.loading = undefined;
    this.changed.fire();
  }

  refresh(): Promise<void> {
    this.loading = offstage(() => this.load()).then(() => this.changed.fire());
    return this.loading;
  }

  // ready is the load the view would do, once, for a command that has to know
  // what is in the tree before anyone has opened it. It is not a re-read: the
  // promise is kept, so a caller that needs what is true now wants refresh().
  ready(): Promise<void> {
    return this.loading ?? this.refresh();
  }

  private async load(): Promise<void> {
    const gen = ++this.generation;
    const client = this.client;
    const launcher = await find();
    // Scan skips the repository's own checkout; the switch list is the one
    // place the server describes it.
    const [held, machines, trees, main, listed, state, key] = await Promise.all(
      [
        launcher ? repos(launcher).catch(() => []) : [],
        launcher ? hostRepos(launcher).catch(() => []) : [],
        client?.worktrees().catch(() => []) ?? [],
        client
          ?.switchable()
          .then((s) => s.worktrees.find((t) => t.main))
          .catch(() => undefined),
        client?.prs().catch(() => []) ?? [],
        client?.state().catch(() => undefined),
        this.root ? real(this.root) : undefined,
      ],
    );
    const [repos2, trees2] = await Promise.all([
      locatedRepos(held),
      located(main ? [main, ...trees] : trees),
    ]);
    // A slower load that started earlier must not land over this one.
    if (gen !== this.generation) return;
    this.held = repos2;
    this.machines = machines.filter((m) => (m.repos ?? []).length > 0);
    this.trees = trees2;
    this.key = key;
    this.branch = state?.branchPr ?? undefined;
    // The branch's own is drawn from `state`, which carries its checks, so
    // it must not also appear in the list below.
    this.listed = listed
      .filter((pr) => pr.number !== this.branch?.number)
      .sort(rankPR);
    this.checks.clear();
  }

  // Beside the title: this repository's conversations when the window is in
  // one, the machine's when it is not and the tree is the Elsewhere list.
  get count(): number {
    const here = mine(this.held, this.key);
    if (here) return countIn(here);
    return this.root ? 0 : sessionsIn(this.held);
  }

  get branchNumber(): number | undefined {
    return this.branch?.number;
  }

  // failing is how many of this branch's checks are red, for the Fix command
  // to hide itself when there is nothing to fix.
  get failing(): number {
    return failing(this.branch?.checks).length;
  }

  getChildren(node?: Node): Node[] | Promise<Node[]> {
    if (!node) return this.ready().then(() => this.top());
    switch (node.kind) {
      case "root":
        return this.under(node.repo);
      case "pulls":
        return this.pulls();
      case "branch":
        return sortedChecks(node.pr.checks, true);
      case "listed":
        return this.checksFor(node.pr.number);
      case "worktree": {
        const w = node.worktree;
        // A loose branch shares the main checkout, whose offer is the main row's.
        return this.sessions(
          cut(w.sessions ?? [], this.cap(), this.all),
          !w.machine && w.path === this.key,
          w.loose ? undefined : w.checkout,
          w.machine,
        );
      }
      case "elsewhere":
        return this.others();
      case "machine":
        return (node.machine.repos ?? []).map(
          (repo) =>
            ({ kind: "repo", repo, machine: node.machine.machine }) as Node,
        );
      case "repo":
        // Its conversations in one list: a checkout level under a repository
        // elsewhere would be the fifth, and each row names its branch anyway.
        return this.sessions(
          sessionsAcross(node.repo, this.cap(), this.all),
          false,
          only(node.repo),
          node.machine,
        );
      default:
        return [];
    }
  }

  // No checkout to root the tree in means everything is elsewhere, and a
  // wrapper row saying so would be the only row.
  private top(): Node[] {
    const elsewhere: Node[] = this.machines.map((machine) => ({
      kind: "machine",
      machine,
    }));
    if (!this.root) return [...this.others(), ...elsewhere];
    const repo = mine(this.held, this.key);
    if (!repo && this.trees.length === 0 && !this.client && !this.rest().length)
      return elsewhere;
    return [{ kind: "root", repo, path: this.root }, ...elsewhere];
  }

  private under(repo: Repo | undefined): Node[] {
    const out: Node[] = [];
    if (this.client) out.push({ kind: "pulls" });
    for (const worktree of workspacesOf(repo, this.trees, this.machines))
      out.push({ kind: "worktree", worktree });
    if (this.away().length > 0) out.push({ kind: "elsewhere" });
    return out;
  }

  private pulls(): Node[] {
    const out: Node[] = [];
    if (this.branch) out.push({ kind: "branch", pr: this.branch });
    out.push(...this.listed.map((pr) => ({ kind: "listed", pr }) as Node));
    return out;
  }

  // Read the first time a row is opened, kept until the list reloads. A failed
  // read is an empty row rather than a dialog: the list itself is still good.
  private async checksFor(number: number): Promise<Node[]> {
    const held = this.checks.get(number);
    if (held) return held;
    const found = await this.client
      ?.checksFor(number)
      .catch(() => [] as Check[]);
    const rows = sortedChecks(found, false);
    this.checks.set(number, rows);
    return rows;
  }

  // rest is every repository but this window's, interesting or not.
  private rest(): Repo[] {
    const here = mine(this.held, this.key);
    return this.held.filter((repo) => repo !== here);
  }

  // away is what Elsewhere holds: the other repositories with a conversation,
  // or every other one once the full list has been asked for.
  private away(): Repo[] {
    return this.all ? this.rest() : elsewhere(this.held, this.key);
  }

  private others(): Node[] {
    const { shown, hidden } = trim(this.rest(), undefined, this.all);
    const out: Node[] = shown.map((repo) => ({ kind: "repo", repo }) as Node);
    if (hidden > 0) out.push({ kind: "more", what: "repos", count: hidden });
    return out;
  }

  private cap(): number {
    return (
      vscode.workspace
        .getConfiguration("ade")
        .get<number>("sessionsPerCheckout") ?? 5
    );
  }

  // The suggestion goes first: it is an offer about the rows under it, and a
  // row nobody scrolls to is one nobody takes.
  private sessions(
    cut: Cut,
    here: boolean,
    checkout?: Checkout,
    machine?: string,
  ): Node[] {
    const out: Node[] = [];
    if (checkout?.suggestion) out.push({ kind: "suggest", checkout });
    out.push(
      ...cut.shown.map(
        (session) => ({ kind: "session", session, here, machine }) as Node,
      ),
    );
    if (cut.hidden > 0)
      out.push({ kind: "more", what: "sessions", count: cut.hidden });
    return out;
  }

  // machineOf is which machine a row's work is on, or nothing for this one.
  static machineOf(node: Node | undefined): string | undefined {
    if (!node) return undefined;
    if (node.kind === "machine") return node.machine.machine;
    if (node.kind === "worktree") return node.worktree.machine;
    return "machine" in node ? node.machine : undefined;
  }

  // checkoutOf is the folder a node stands for. The root is the checkout this
  // window is in; a repository elsewhere is its first, which is the main one.
  static checkoutOf(node: Node | undefined): string | undefined {
    if (!node) return undefined;
    switch (node.kind) {
      case "root":
        return node.path;
      case "worktree":
        return node.worktree.path;
      case "repo":
        return (node.repo.checkouts ?? [])[0]?.path ?? node.repo.path;
      case "session":
        return node.session.dir;
      default:
        return undefined;
    }
  }

  // treeOf is the server's view of a worktree row, which the removal verbs
  // need. Nothing on a row the server has not described.
  static treeOf(node: Node | undefined): Tree | undefined {
    return node?.kind === "worktree" ? node.worktree.tree : undefined;
  }

  getTreeItem(node: Node): vscode.TreeItem {
    switch (node.kind) {
      case "root":
        return this.rootItem(node.repo);
      case "pulls":
        return pullsItem(
          this.listed.length + (this.branch ? 1 : 0),
          Boolean(this.branch),
        );
      case "branch":
        return branchItem(node.pr);
      case "listed":
        return listedItem(node.pr);
      case "check":
        return checkItem(node.check, node.own);
      case "worktree":
        return this.worktreeItem(node.worktree);
      case "session":
        return sessionItem(node.session, node.here, node.machine);
      case "elsewhere":
        return elsewhereItem(
          labelAll(elsewhere(this.held, this.key)),
          this.rest().length,
        );
      case "repo":
        return repoItem(node.repo);
      case "more":
        return moreItem(node.what, node.count);
      case "suggest":
        return suggestItem(node.checkout);
      case "machine":
        return machineItem(node.machine);
    }
  }

  // The repository this window is in. Named from the launcher when it knows
  // the checkout, from the folder when it does not.
  private rootItem(repo: Repo | undefined): vscode.TreeItem {
    const item = new vscode.TreeItem(
      repo?.name ?? base(this.root ?? ""),
      vscode.TreeItemCollapsibleState.Expanded,
    );
    item.description = repo
      ? [labelAll([repo]), repo.owner].filter(Boolean).join(" · ")
      : undefined;
    item.iconPath = new vscode.ThemeIcon("repo");
    item.contextValue = repoContext(repo);
    item.tooltip = repo?.path ?? this.root;
    return item;
  }

  // A worktree row says what stands in the way of removing it, which only the
  // server knows; with no server it says its branch and nothing more.
  private worktreeItem(w: Worktree): vscode.TreeItem {
    const here = !w.machine && !w.loose && w.path === this.key;
    // The catch-all stays shut: scratch work is one row until it is wanted.
    const item = new vscode.TreeItem(
      title(w),
      here && !w.catchAll
        ? vscode.TreeItemCollapsibleState.Expanded
        : vscode.TreeItemCollapsibleState.Collapsed,
    );
    // The count first: a narrow view cuts the end, and it is why you open one.
    item.description = [
      tally(w.sessions ?? []),
      w.tree && line({ ...w.tree, branch: undefined }),
      w.machine,
      here && "this window",
    ]
      .filter(Boolean)
      .join(" · ");
    if (w.machine) {
      item.iconPath = new vscode.ThemeIcon("vm");
      item.contextValue = "ade.workspace.away";
      item.tooltip = `${w.path} on ${w.machine}`;
      return item;
    }
    item.iconPath = new vscode.ThemeIcon(
      w.tree ? icon(w.tree) : here ? "folder-active" : "source-control",
    );
    item.contextValue = w.tree
      ? context(w.tree)
      : here
        ? "ade.checkout.here"
        : "ade.checkout";
    item.tooltip = w.tree?.keep ? `${w.path}\n${w.tree.keep}` : w.path;
    if (!w.loose) item.resourceUri = vscode.Uri.file(w.path);
    return item;
  }
}

// A session in this window's checkout is resumed in place; one elsewhere
// needs the window to be looking at that folder, on whichever machine it is on.
function sessionItem(
  session: Session,
  here: boolean,
  machine?: string,
): vscode.TreeItem {
  const item = new vscode.TreeItem(
    session.title || session.id.slice(0, 8),
    vscode.TreeItemCollapsibleState.None,
  );
  item.description = sessionLine(session);
  item.iconPath = new vscode.ThemeIcon(
    session.live ? "circle-filled" : "comment-discussion",
  );
  item.tooltip = new vscode.MarkdownString(
    [
      `**${session.title || "untitled"}**`,
      `${session.agent} · ${session.branch || "no branch"}`,
      session.dir,
    ].join("\n\n"),
  );
  item.contextValue = sessionContext(session, here);
  if (session.archived) item.iconPath = new vscode.ThemeIcon("archive");
  item.command = {
    command: "ade.openSession",
    title: "Open",
    arguments: [session, machine],
  };
  return item;
}

// A named, collapsed parent rather than a separator: it can be acted on, it
// stays one line while you do not care, and its count says whether to open it.
function elsewhereItem(count: string, repos: number): vscode.TreeItem {
  const item = new vscode.TreeItem(
    "Elsewhere",
    vscode.TreeItemCollapsibleState.Collapsed,
  );
  item.description = count || `${repos} quiet`;
  item.iconPath = new vscode.ThemeIcon("globe");
  item.contextValue = "ade.elsewhere";
  item.tooltip = "Conversations in repositories this window is not open on";
  return item;
}

// A repository is named by owner and repository, not by path: two checkouts of
// the same repository differ by branch, and the path says that below.
function repoItem(repo: Repo): vscode.TreeItem {
  const item = new vscode.TreeItem(
    repo.name,
    vscode.TreeItemCollapsibleState.Collapsed,
  );
  item.description = [label(repo), repo.owner].filter(Boolean).join(" · ");
  item.iconPath = new vscode.ThemeIcon("repo");
  item.contextValue = repoContext(repo);
  item.tooltip = repo.path;
  return item;
}

// The row that stands for what is not shown. It is a row rather than a
// setting so the list can be short without anything being unreachable.
function moreItem(what: "repos" | "sessions", count: number): vscode.TreeItem {
  const item = new vscode.TreeItem(
    what === "repos" ? `${count} more repositories` : `${count} older`,
    vscode.TreeItemCollapsibleState.None,
  );
  item.iconPath = new vscode.ThemeIcon("ellipsis");
  item.command = { command: "ade.showAllSessions", title: "Show all" };
  item.contextValue = "ade.more";
  return item;
}

function base(path: string): string {
  return path.split("/").pop() ?? path;
}

// One spelling per directory. A path that does not resolve, or is on another
// machine, keeps the one it came with.
function real(path: string): Promise<string> {
  return realpath(path).catch(() => path);
}

function located<T extends { path: string }>(rows: T[]): Promise<T[]> {
  return Promise.all(
    rows.map(async (row) => ({ ...row, path: await real(row.path) })),
  );
}

function locatedRepos(held: Repo[]): Promise<Repo[]> {
  return Promise.all(
    held.map(async (repo) => ({
      ...repo,
      path: await real(repo.path),
      checkouts: await located(repo.checkouts ?? []),
    })),
  );
}

// suggestItem is the offer to fold a checkout's finished conversations into
// one card. Clicking it combines; the row's X dismisses this exact set.
function suggestItem(checkout: Checkout): vscode.TreeItem {
  const n = checkout.suggestion?.ids.length ?? 0;
  const item = new vscode.TreeItem(`Combine ${n} finished conversations`);
  item.description = "they look like one piece of work";
  item.iconPath = new vscode.ThemeIcon("merge");
  item.contextValue = "ade.suggest";
  item.command = {
    command: "ade.combineSessions",
    title: "Combine",
    arguments: [checkout],
  };
  return item;
}

// machineItem is another machine's checkouts, under its own row: the tree is
// this machine's, and work running elsewhere is otherwise invisible here.
function machineItem(machine: Machine): vscode.TreeItem {
  const repos = machine.repos ?? [];
  const item = new vscode.TreeItem(
    machine.machine,
    vscode.TreeItemCollapsibleState.Collapsed,
  );
  item.description = `${repos.length} repo${repos.length === 1 ? "" : "s"}`;
  item.iconPath = new vscode.ThemeIcon("vm-active");
  item.contextValue = "ade.machine.checkouts";
  return item;
}
