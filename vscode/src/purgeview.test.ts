import * as vscode from "vscode";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Tree } from "./client";
import type { Sweep } from "./purgepanel/view";
import { brief, PurgePage, type Pruned } from "./purgeview";

function tree(over: Partial<Tree> = {}): Tree {
  return { path: "/code/ade-old", name: "ade-old", kind: "session", removable: true, ...over };
}

const sweep: Sweep = {
  repos: [
    { path: "/code/ade", name: "ade", worktrees: [tree()] },
    {
      path: "/code/coast",
      name: "coast",
      worktrees: [
        tree({
          path: "/code/coast-wip",
          name: "coast-wip",
          removable: false,
          keep: "unpushed",
          unpushed: 2,
        }),
      ],
    },
  ],
};

// remove is private, and it is the one method here that destroys anything: the
// modal gate and what is said afterwards are only testable through it.
interface Innards {
  remove(paths: string[], force: boolean): Promise<void>;
  note: string;
}

function inner(view: PurgePage): Innards {
  return view as never as Innards;
}

function page(
  prune: (repo: string, paths: string[], force: boolean) => Promise<Pruned>,
  feed: (fresh: boolean) => Promise<Sweep> = async () => sweep,
): PurgePage {
  const context = { extensionUri: { scheme: "file", fsPath: "/ext" } } as never;
  return new PurgePage(context, feed, prune);
}

function says(answer: string | undefined) {
  return vi.spyOn(vscode.window, "showWarningMessage").mockResolvedValue(answer as never);
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("removing worktrees", () => {
  it("removes nothing when the modal is not answered with Remove", async () => {
    says(undefined);
    const prune = vi.fn(async () => ({ removed: [] }));
    const view = page(prune);
    await view.show();

    await inner(view).remove(["/code/ade-old"], false);
    expect(prune).not.toHaveBeenCalled();
  });

  it("prunes one repository at a time", async () => {
    says("Remove");
    const prune = vi.fn(async (_repo: string, paths: string[], _force: boolean) => ({
      removed: paths,
    }));
    const view = page(prune);
    await view.show();

    await inner(view).remove(["/code/ade-old", "/code/coast-wip"], true);
    expect(prune.mock.calls).toEqual([
      ["/code/ade", ["/code/ade-old"], true],
      ["/code/coast", ["/code/coast-wip"], true],
    ]);
    expect(inner(view).note).toBe("Removed 2.");
  });

  it("names every worktree the server kept back, with its own reason", async () => {
    says("Remove");
    // One call per repository, so the fixture answers for the paths it was
    // asked about rather than for both every time.
    const why: Record<string, string> = {
      "/code/ade-old": "uncommitted",
      "/code/coast-wip": "serving",
    };
    const view = page(async (_repo, paths) => ({
      removed: [],
      kept: paths.map((path) => ({ path, why: why[path] })),
    }));
    await view.show();

    await inner(view).remove(["/code/ade-old", "/code/coast-wip"], true);
    expect(inner(view).note).toBe("Removed 0. Kept ade-old: uncommitted; coast-wip: serving");
  });

  it("keeps the failed re-read on screen beside what was removed", async () => {
    says("Remove");
    let reads = 0;
    const view = page(
      async (_repo: string, paths: string[]) => ({ removed: paths }),
      async () => {
        reads += 1;
        if (reads > 1) throw new Error("the launcher is not running");
        return sweep;
      },
    );
    await view.show();

    await inner(view).remove(["/code/ade-old"], false);
    expect(inner(view).note).toContain("Removed 1.");
    expect(inner(view).note).toContain("the launcher is not running");
  });

  it("turns a failed request into the reason that worktree stayed", async () => {
    says("Remove");
    const view = page(async () => {
      throw new Error("502 from the launcher");
    });
    await view.show();

    await inner(view).remove(["/code/ade-old"], false);
    expect(inner(view).note).toContain("Kept ade-old: Error: 502 from the launcher");
  });
});

describe("brief", () => {
  it("is one line, short enough to read as a note", () => {
    expect(brief(new Error(`<!doctype html>\n${"x".repeat(5000)}`))).toBe(
      "Error: <!doctype html>",
    );
    expect(brief(new Error("x".repeat(5000))).length).toBe(201);
  });
});
