import { afterEach, describe, expect, it, vi } from "vitest";
import {
  mkdirSync,
  mkdtempSync,
  realpathSync,
  rmSync,
  symlinkSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Sessions, type SessionNode } from "./sessions";
import { hostRepos } from "./launcher";
import type { BranchPR, Client, Listed, Tree } from "./client";
import type { Checkout, Repo, Session } from "./launcher";
import { repos } from "./launcher";

vi.mock("./launcher", () => ({
  find: vi.fn(async () => ({ base: "http://launcher", token: "t" })),
  repos: vi.fn(async () => []),
  hostRepos: vi.fn(async () => []),
}));

const base = (path: string) => path.split("/").pop() ?? path;

const session = (id: string, dir: string, over: Partial<Session> = {}) =>
  ({ id, agent: "claude", dir, name: id, branch: "main", ...over }) as Session;

const checkout = (path: string, sessions: Session[] = []): Checkout => ({
  path,
  name: base(path),
  branch: "main",
  sessions,
});

const repo = (path: string, checkouts: Checkout[]): Repo => ({
  path,
  name: base(path),
  owner: "o",
  checkouts,
});

const tree = (path: string, over: Partial<Tree> = {}): Tree => ({
  path,
  name: base(path),
  kind: "session",
  removable: true,
  ...over,
});

const pr = (number: number): Listed => ({
  number,
  title: "t",
  author: "a",
  headRef: "h",
  url: "u",
});

// The server, as far as the tree asks it. The main checkout comes from the
// switch list, which is the one place Scan's omission of it is filled in.
function fake(
  given: {
    trees?: Tree[];
    main?: Tree;
    prs?: Listed[];
    branch?: BranchPR;
    gate?: Promise<void>;
    asked?: () => void;
  } = {},
): Client {
  return {
    worktrees: async () => given.trees ?? [],
    switchable: async () => ({
      worktrees: given.main ? [given.main] : [],
      here: "",
    }),
    prs: async () => {
      given.asked?.();
      await given.gate;
      return given.prs ?? [];
    },
    state: async () => ({
      diffVersion: "",
      threads: null,
      branchPr: given.branch ?? null,
    }),
    checksFor: async () => [],
  } as unknown as Client;
}

function open(
  root: string | undefined,
  held: Repo[],
  client: Client | undefined,
): Sessions {
  vi.mocked(repos).mockResolvedValue(held);
  const s = new Sessions(undefined);
  s.serving(root, client);
  return s;
}

const kinds = (nodes: SessionNode[]) => nodes.map((n) => n.kind);

async function under(s: Sessions): Promise<SessionNode[]> {
  const [root] = await s.getChildren();
  return s.getChildren(root);
}

function worktreeRow(nodes: SessionNode[], path: string) {
  const row = nodes.find(
    (n) => n.kind === "worktree" && n.worktree.path === path,
  );
  if (row?.kind !== "worktree") throw new Error(`no worktree row for ${path}`);
  return row;
}

describe("the checkout's tree", () => {
  const here = repo("/r/ade", [
    checkout("/r/ade", [session("s1", "/r/ade")]),
    checkout("/r/ade-fix", [session("s2", "/r/ade-fix")]),
  ]);
  const busy = repo("/r/coast", [
    checkout("/r/coast", [session("c1", "/r/coast")]),
  ]);
  const quiet = repo("/r/quiet", [checkout("/r/quiet")]);

  it("roots in the repository: pull requests, its worktrees, then Elsewhere", async () => {
    const s = open("/r/ade", [here, busy], fake());
    const top = await s.getChildren();
    expect(kinds(top)).toEqual(["root"]);
    expect(kinds(await s.getChildren(top[0]))).toEqual([
      "pulls",
      "worktree",
      "worktree",
      "elsewhere",
    ]);
  });

  it("gives the main checkout the server's tree, last and folded", async () => {
    const s = open(
      "/r/ade",
      [here],
      fake({
        trees: [tree("/r/ade-fix", { branch: "fix" })],
        main: tree("/r/ade", {
          main: true,
          branch: "main",
          uncommitted: 2,
          removable: false,
        }),
      }),
    );
    const rows = (await under(s)).filter((n) => n.kind === "worktree");
    const [linked, main] = rows.map((n) => s.getTreeItem(n));
    expect(main.label).toBe("main");
    expect(main.contextValue).toBe("ade.tree.main");
    expect(main.description).toBe(
      "1 conversation · 2 uncommitted · this window",
    );
    // 1 is Collapsed: the catch-all is one row even in its own window.
    expect(main.collapsibleState).toBe(1);
    expect(linked.label).toBe("fix");
    expect(linked.contextValue).toBe("ade.tree.removable");
  });

  it("nests a conversation under the branch it was had on", async () => {
    const mixed = repo("/r/ade", [
      checkout("/r/ade", [
        session("scratch", "/r/ade"),
        session("left", "/r/ade", { branch: "old-idea" }),
      ]),
    ]);
    const s = open("/r/ade", [mixed], fake());
    const rows = (await under(s)).filter((n) => n.kind === "worktree");
    expect(rows.map((n) => s.getTreeItem(n).label)).toEqual([
      "old-idea",
      "main",
    ]);
    const [left] = await s.getChildren(rows[0]);
    expect(left.kind === "session" && left.session.id).toBe("left");
    expect(s.getTreeItem(left).contextValue).toBe("ade.session.here");
    expect(s.getTreeItem(rows[0]).description).toBe("1 conversation");
  });

  it("draws this repository's clone on a machine as a branch row", async () => {
    vi.mocked(hostRepos).mockResolvedValueOnce([
      {
        machine: "bigboy",
        repos: [
          repo("/srv/ade/0123/ade", [
            {
              ...checkout("/srv/ade/0123/ade", [
                session("w", "/srv/ade/0123/ade", { branch: "worker" }),
              ]),
              branch: "worker",
            },
          ]),
        ],
      },
    ]);
    const s = open("/r/ade", [here], fake());
    const rows = (await under(s)).filter((n) => n.kind === "worktree");
    const far = rows.find((n) => Sessions.machineOf(n) === "bigboy")!;
    const item = s.getTreeItem(far);
    expect(item.label).toBe("worker");
    expect(item.description).toBe("1 conversation · bigboy");
    expect(item.contextValue).toBe("ade.workspace.away");
    const [row] = await s.getChildren(far);
    expect(Sessions.machineOf(row)).toBe("bigboy");
    expect(s.getTreeItem(row).contextValue).toBe("ade.session.elsewhere");
  });

  // Elsewhere is for work in a repository this window is not open on. A
  // quiet one is not work, until the full list is asked for.
  it("draws Elsewhere only when another repository has a conversation", async () => {
    const s = open("/r/ade", [here, quiet], fake());
    expect(kinds(await under(s))).not.toContain("elsewhere");
    s.showAll();
    expect(kinds(await under(s))).toContain("elsewhere");
  });

  it("lists a repository elsewhere flat, whatever checkout each is in", async () => {
    const coast = repo("/r/coast", [
      checkout("/r/coast", [session("c1", "/r/coast")]),
      checkout("/r/coast-fix", [session("c2", "/r/coast-fix")]),
    ]);
    const s = open("/r/ade", [here, coast], fake());
    const away = (await under(s)).find((n) => n.kind === "elsewhere");
    const [row] = await s.getChildren(away);
    expect(row.kind).toBe("repo");
    expect(kinds(await s.getChildren(row))).toEqual(["session", "session"]);
  });

  it("marks a conversation in this window's checkout as here", async () => {
    const s = open("/r/ade", [here], fake());
    const rows = await under(s);
    const [mine] = await s.getChildren(worktreeRow(rows, "/r/ade"));
    const [theirs] = await s.getChildren(worktreeRow(rows, "/r/ade-fix"));
    expect(s.getTreeItem(mine).contextValue).toBe("ade.session.here");
    expect(s.getTreeItem(theirs).contextValue).toBe("ade.session.elsewhere");
  });

  it("counts this repository's conversations, not the machine's", async () => {
    const s = open("/r/ade", [here, busy], fake());
    await s.ready();
    expect(s.count).toBe(2);
  });

  it("is empty when nothing knows this folder and nothing else exists", async () => {
    const s = open("/r/fresh", [], undefined);
    expect(await s.getChildren()).toEqual([]);
  });

  it("is the Elsewhere list flat when no folder is open", async () => {
    const s = open(undefined, [busy, quiet], undefined);
    expect(kinds(await s.getChildren())).toEqual(["repo", "more"]);
  });
});

describe("when the tree reads", () => {
  const here = repo("/r/ade", [checkout("/r/ade")]);

  it("reads nothing until the view is opened", async () => {
    const asked = vi.fn();
    const s = open("/r/ade", [here], fake({ asked }));
    await new Promise((r) => setTimeout(r, 0));
    expect(asked).not.toHaveBeenCalled();
    await s.getChildren();
    expect(asked).toHaveBeenCalledTimes(1);
  });

  it("lets a newer load win over a slower, older one", async () => {
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    const s = open("/r/ade", [here], fake({ prs: [pr(7)], gate }));
    const slow = s.ready();
    s.serving("/r/ade", fake({ prs: [pr(9)] }));
    await s.ready();
    release();
    await slow;
    const pulls = await s.getChildren({ kind: "pulls" });
    expect(pulls.map((n) => n.kind === "listed" && n.pr.number)).toEqual([9]);
  });
});

describe("paths spelled two ways", () => {
  const dirs: string[] = [];
  afterEach(() => {
    for (const dir of dirs.splice(0))
      rmSync(dir, { recursive: true, force: true });
  });

  // The launcher reports a checkout as the roots spell it and git as it
  // resolves; one directory must be one row.
  it("joins a checkout reached through a symlink to the server's real path", async () => {
    const dir = mkdtempSync(join(tmpdir(), "ade-sessions-"));
    dirs.push(dir);
    const real = realpathSync(dir);
    mkdirSync(join(real, "ade"));
    symlinkSync(real, join(dir, "link"));
    const spelled = join(dir, "link", "ade");

    const s = open(
      spelled,
      [repo(spelled, [checkout(spelled, [session("s", spelled)])])],
      fake({ main: tree(join(real, "ade"), { main: true }) }),
    );
    const rows = await under(s);
    expect(kinds(rows)).toEqual(["pulls", "worktree"]);
    const [row] = rows.filter((n) => n.kind === "worktree");
    if (row.kind !== "worktree") throw new Error("no worktree row");
    expect(row.worktree.tree).toBeDefined();
    expect(row.worktree.checkout?.sessions).toHaveLength(1);
    expect(s.getTreeItem(row).description).toContain("this window");
  });
});

describe("another machine's checkouts", () => {
  it("get a row of their own, after this machine's", async () => {
    vi.mocked(hostRepos).mockResolvedValueOnce([
      {
        machine: "pi@pi5.local",
        repos: [repo("/home/pi/code/ade", [checkout("/home/pi/code/ade")])],
      },
      { machine: "empty", repos: [] },
    ]);
    const tree = new Sessions(undefined);
    const top = await tree.getChildren();

    const rows = top.map((n) => n.kind);
    expect(rows).toContain("machine");
    // A machine with nothing checked out is not news.
    expect(rows.filter((k) => k === "machine")).toHaveLength(1);

    const machine = top.find((n) => n.kind === "machine")!;
    expect(tree.getTreeItem(machine).label).toBe("pi@pi5.local");
    expect(tree.getTreeItem(machine).description).toBe("1 repo");
    expect((await tree.getChildren(machine)).map((n) => n.kind)).toEqual([
      "repo",
    ]);
  });
});
