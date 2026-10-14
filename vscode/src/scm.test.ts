import { describe, expect, it } from "vitest";
import * as vscode from "vscode";
import type { Client, State } from "./client";
import type { GitRepository } from "./gitapi";
import type { Row } from "./commit";
import { GitIndex, type Index, Scm } from "./scm";

const root = "/work/ade";

function repo(at = root) {
  const calls: { add: string[][]; revert: string[][] } = { add: [], revert: [] };
  const r: GitRepository = {
    rootUri: vscode.Uri.file(at),
    inputBox: { value: "" },
    add: async (paths) => void calls.add.push(paths),
    revert: async (paths) => void calls.revert.push(paths),
  };
  return { r, calls };
}

const state = (over: Partial<State> = {}): State =>
  ({
    worktree: {
      staged: ["a.go"],
      unstaged: [{ path: "b.go", status: "modified" }],
      ahead: 0,
    },
    ...over,
  }) as State;

const client = {} as Client;

function other() {
  const seen = { shown: [] as Row[][], staged: [] as string[][], disposed: 0 };
  const index: Index = {
    box: { value: "" },
    show: (staged, changed) => void seen.shown.push([...staged, ...changed]),
    stage: async (paths) => void seen.staged.push(paths),
    unstage: async () => {},
    dispose: () => void seen.disposed++,
  };
  return { index, seen };
}

describe("the changeset on Git's own repository", () => {
  it("puts the agent's draft in Git's box", () => {
    const { r } = repo();
    const scm = new Scm(client, root, new GitIndex(r, root));
    scm.apply(state({ draft: { title: "fix: it", body: "", staged: null } }));
    expect(r.inputBox.value).toBe("fix: it");
  });

  // The message being typed is the person's, not the model's.
  it("leaves a box someone has typed in alone", () => {
    const { r } = repo();
    r.inputBox.value = "mine";
    const scm = new Scm(client, root, new GitIndex(r, root));
    scm.apply(state({ draft: { title: "fix: it", body: "", staged: null } }));
    expect(r.inputBox.value).toBe("mine");
  });

  it("stages and unstages through Git, by absolute path", async () => {
    const { r, calls } = repo();
    const scm = new Scm(client, root, new GitIndex(r, root));
    scm.apply(state());
    await scm.stageAll();
    await scm.unstageAll();
    expect(calls.add).toEqual([[`${root}/b.go`]]);
    expect(calls.revert).toEqual([[`${root}/a.go`]]);
  });
});

describe("moving the changeset to another index", () => {
  it("disposes the one it leaves and draws the rows on the new one", () => {
    const old = other();
    const next = other();
    const scm = new Scm(client, root, old.index);
    scm.apply(state());
    scm.use(next.index, root);
    expect(old.seen.disposed).toBe(1);
    expect(next.seen.shown).toEqual([
      [
        { path: "a.go", status: "" },
        { path: "b.go", status: "modified" },
      ],
    ]);
  });

  // use() resets wrote, so the draft the old box held lands in the new one.
  it("fills the new box with the draft again", () => {
    const old = other();
    const next = other();
    const scm = new Scm(client, root, old.index);
    const drafted = state({
      draft: { title: "fix: it", body: "", staged: null },
    });
    scm.apply(drafted);
    expect(old.index.box.value).toBe("fix: it");
    scm.use(next.index, root);
    scm.apply(drafted);
    expect(next.index.box.value).toBe("fix: it");
  });

  it("moves the root with the index", async () => {
    const mount = "/work/lib";
    const { r, calls } = repo(mount);
    const scm = new Scm(client, root, other().index);
    scm.apply(state());
    scm.use(new GitIndex(r, mount), mount);
    await scm.stageAll();
    expect(calls.add).toEqual([[`${mount}/b.go`]]);
  });
});
