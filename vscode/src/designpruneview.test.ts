import * as vscode from "vscode";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DesignPrunePage, type Archived } from "./designpruneview";
import {
  key,
  rows,
  summary,
  ticks,
  touched,
  trouble,
  type Group,
  type Prunable,
  type WikiState,
} from "./designprunepanel/view";

const docs = "/code/ade/.git/ade/docs";

function answer(over: Partial<Prunable> = {}): Prunable {
  return {
    repo: "owenrumney/ade",
    merged: [
      { dir: docs, file: "merged-thing.md", stem: "merged-thing", branch: "merged-thing", number: 11 },
    ],
    abandoned: [{ dir: docs, file: "dropped.md", stem: "dropped", branch: "dropped", number: 12 }],
    unmatched: [
      { dir: docs, file: "old-notes.md", stem: "old-notes", touched: "2026-07-18T09:00:00Z" },
    ],
    wiki: { enabled: true },
    ...over,
  };
}

function groups(...all: Prunable[]): Group[] {
  return all.map((got) => ({ repo: got.repo, pending: false, answer: got }));
}

const now = Date.parse("2026-10-20T09:00:00Z");

describe("the sweep as rows", () => {
  it("draws the three groups in order, per repository", () => {
    const all = rows(groups(answer(), answer({ repo: "owenrumney/coast" })), now);
    expect(all.map((row) => [row.repo, row.file, row.why])).toEqual([
      ["owenrumney/ade", "merged-thing.md", "merged in #11"],
      ["owenrumney/ade", "dropped.md", "branch gone, never merged"],
      ["owenrumney/ade", "old-notes.md", "touched 94 days ago"],
      ["owenrumney/coast", "merged-thing.md", "merged in #11"],
      ["owenrumney/coast", "dropped.md", "branch gone, never merged"],
      ["owenrumney/coast", "old-notes.md", "touched 94 days ago"],
    ]);
  });

  // The pile you have stopped thinking about is mostly these, and the sweep
  // can never see them.
  it("lists the documents no branch ever matched", () => {
    const all = rows(groups(answer({ merged: [], abandoned: [] })), now);
    expect(all.map((row) => row.file)).toEqual(["old-notes.md"]);
    expect(all[0].kind).toBe("unmatched");
  });

  it("ticks what merged and nothing else", () => {
    const all = rows(groups(answer()), now);
    expect([...ticks(all)]).toEqual([key("owenrumney/ade", "merged-thing.md")]);
  });

  it("keys a row by repository, so two repositories never share one", () => {
    const all = rows(groups(answer(), answer({ repo: "owenrumney/coast" })), now);
    expect(new Set(all.map((row) => row.key)).size).toBe(all.length);
  });

  // One `gh` without auth would otherwise hide every other repository.
  it("turns a repository that could not be swept into its own line", () => {
    const bad = groups(answer(), { repo: "owenrumney/coast", error: "gh: not authenticated" });
    expect(trouble(bad)).toEqual(["owenrumney/coast: gh: not authenticated"]);
    expect(rows(bad, now).map((row) => row.repo)).toEqual([
      "owenrumney/ade",
      "owenrumney/ade",
      "owenrumney/ade",
    ]);
  });

  it("says a pending sweep rather than an empty list", () => {
    expect(summary([], [{ repo: "owenrumney/ade", pending: true }])).toBe("Sweeping…");
    expect(summary([], [])).toBe("Nothing here has finished.");
    expect(summary(rows(groups(answer()), now), [])).toBe("3 documents · 1 merged");
  });
});

describe("the age of a document nothing matched", () => {
  it("is the file's mtime in days, and nothing at all for a date Go never read", () => {
    expect(touched("2026-10-19T09:00:00Z", now)).toBe("touched yesterday");
    expect(touched("2026-10-20T08:00:00Z", now)).toBe("touched today");
    expect(touched("0001-01-01T00:00:00Z", now)).toBe("");
    expect(touched(undefined, now)).toBe("");
  });
});

// prune is private, and it is the one method that destroys anything: the
// modal gate, the per-row states and Stop are only testable through it.
interface Innards {
  prune(keys: string[]): Promise<void>;
  stopping: boolean;
  note: string;
  states: Record<string, { state: string; page?: string; why?: string }>;
}

function inner(view: DesignPrunePage): Innards {
  return view as never as Innards;
}

function page(
  archive: (repo: string, file: string) => Promise<Archived>,
  held: Prunable[] = [answer()],
  wiki: WikiState = { enabled: true },
): DesignPrunePage {
  const context = { extensionUri: { scheme: "file", fsPath: "/ext" } } as never;
  return new DesignPrunePage(
    context,
    async () => ({ repos: held.map((got) => ({ repo: got.repo })), wiki }),
    async (repo) => held.find((got) => got.repo === repo) ?? { repo },
    archive,
  );
}

function says(reply: string | undefined) {
  return vi.spyOn(vscode.window, "showWarningMessage").mockResolvedValue(reply as never);
}

const merged = key("owenrumney/ade", "merged-thing.md");
const dropped = key("owenrumney/ade", "dropped.md");

afterEach(() => {
  vi.restoreAllMocks();
});

describe("the run", () => {
  it("archives nothing until the one confirm is answered", async () => {
    says(undefined);
    const archive = vi.fn(async () => ({ page: "repos/ade/designs/x.md" }));
    const view = page(archive);
    await view.show();

    await inner(view).prune([merged]);
    expect(archive).not.toHaveBeenCalled();
  });

  it("publishes one document at a time and names the page on the row", async () => {
    says("Archive");
    const archive = vi.fn(async (_repo: string, file: string) => ({
      page: `repos/ade/designs/${file}`,
    }));
    const view = page(archive);
    await view.show();

    await inner(view).prune([merged, dropped]);
    expect(archive.mock.calls).toEqual([
      ["owenrumney/ade", "merged-thing.md"],
      ["owenrumney/ade", "dropped.md"],
    ]);
    expect(inner(view).states[merged]).toEqual({
      state: "published",
      page: "repos/ade/designs/merged-thing.md",
    });
    expect(inner(view).note).toBe("Archived 2.");
  });

  // A failed document leaves its file alone, as `ade doc archive` does.
  it("carries on past a document that failed, and keeps its reason", async () => {
    says("Archive");
    const view = page(async (_repo, file) => {
      if (file === "merged-thing.md") throw new Error("the model wrote no page");
      return { page: "repos/ade/designs/dropped.md" };
    });
    await view.show();

    await inner(view).prune([merged, dropped]);
    expect(inner(view).states[merged].state).toBe("failed");
    expect(inner(view).states[merged].why).toContain("the model wrote no page");
    expect(inner(view).states[dropped].state).toBe("published");
    expect(inner(view).note).toBe("Archived 1. 1 failed; the reason is on the row.");
  });

  // Stop lets the document in flight finish and skips the rest.
  it("stops after the document in flight", async () => {
    says("Archive");
    const seen: string[] = [];
    const view = page(async (_repo, file) => {
      seen.push(file);
      inner(view).stopping = true;
      return { page: "repos/ade/designs/x.md" };
    });
    await view.show();

    await inner(view).prune([merged, dropped]);
    expect(seen).toEqual(["merged-thing.md"]);
    expect(inner(view).note).toContain("Stopped");
    expect(inner(view).states[dropped].state).toBe("queued");
  });

  it("closes a tab open on an archived document", async () => {
    says("Archive");
    const close = vi.fn(async () => true);
    const tabs = vscode.window.tabGroups as unknown as { all: unknown; close: unknown };
    tabs.all = [{ tabs: [{ input: { uri: { fsPath: `${docs}/merged-thing.md` } } }] }];
    tabs.close = close;
    const view = page(async () => ({ page: "repos/ade/designs/x.md" }));
    await view.show();

    await inner(view).prune([merged]);
    expect(close).toHaveBeenCalledOnce();
    tabs.all = [];
  });

  it("says the launcher is unreachable rather than an empty list", async () => {
    const view = new DesignPrunePage(
      { extensionUri: { scheme: "file", fsPath: "/ext" } } as never,
      async () => {
        throw new Error("the launcher is not running");
      },
      async (repo) => ({ repo }),
      async () => ({}),
    );
    await view.show();
    expect(inner(view).note).toContain("the launcher is not running");
  });
});
