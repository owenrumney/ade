import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { WHICH, type Reach } from "./panels";

// What a panel's page actually sends. Read off its own `send(...)`
// calls rather than guessed: six panels answered "not wired yet"
// because the host had invented names for their buttons.
function verbsOf(panel: string): string[] {
  const src = readFileSync(join(__dirname, "..", "..", "vscode", "src", panel, "main.ts"), "utf8");
  const sent = [...src.matchAll(/send\("([a-zA-Z-]+)"/g)].map((m) => m[1]);
  const direct = [...src.matchAll(/kind: "do", what: "([a-zA-Z-]+)"/g)].map((m) => m[1]);
  return [...new Set([...sent, ...direct])];
}

// said is what the page would post, and seen is where it went.
function spy(): { reach: Reach; seen: { path: string; body: unknown }[] } {
  const seen: { path: string; body: unknown }[] = [];
  const reach: Reach = {
    get: async () => ({}),
    call: async () => ({ status: 200, ok: true, text: "{}" }),
    send: async (_at, path, body) => {
      seen.push({ path, body });
      return {};
    },
    open: () => {},
    link: () => {},
    run: () => {},
  };
  return { reach, seen };
}

describe("a panel answers every button its page has", () => {
  const cases: [string, string][] = [
    ["ade.worktrees", "worktreepanel"],
    ["ade.commitPanel", "commitpanel"],
    ["ade.purgeWorktrees", "purgepanel"],
    ["ade.purgeDesigns", "designprunepanel"],
    ["ade.showRepo", "repo"],
  ];

  for (const [command, page] of cases) {
    it(`answers ${page}`, async () => {
      const { reach } = spy();
      const panel = WHICH[command](reach);
      for (const what of verbsOf(page)) {
        // `open` and `stop` are the window's own, not a route.
        if (what === "open" || what === "stop") continue;
        const back = (await panel.onMessage({ kind: "do", what })) as { note?: string };
        expect(back?.note ?? "", `${page}: ${what}`).not.toContain("not wired");
      }
    });
  }
});

describe("the verbs that are more than one call", () => {
  it("commits, and pushes when the form asked for both", async () => {
    const { reach, seen } = spy();
    const commit = WHICH["ade.commitPanel"](reach);
    await commit.onMessage({ kind: "do", what: "commit", title: "a change", push: true });
    expect(seen.map((s) => s.path)).toEqual(["/api/commit", "/api/push"]);
  });

  it("does not push when it did not", async () => {
    const { reach, seen } = spy();
    const commit = WHICH["ade.commitPanel"](reach);
    await commit.onMessage({ kind: "do", what: "commit", title: "a change" });
    expect(seen.map((s) => s.path)).toEqual(["/api/commit"]);
  });

  it("clears each worktree's blocker before pruning the lot", async () => {
    const { reach, seen } = spy();
    const trees = WHICH["ade.worktrees"](reach);
    await trees.onMessage({
      kind: "do",
      what: "consolidate",
      paths: ["/a", "/b"],
      clear: { "/a": "push", "/b": "close" },
    });
    expect(seen.map((s) => s.path)).toEqual([
      "/api/worktrees/push",
      "/api/worktrees/close",
      "/api/worktrees/prune",
    ]);
    expect(seen[2].body).toEqual({ paths: ["/a", "/b"] });
  });
});

describe("refresh", () => {
  it("is a read, in every panel that has one", async () => {
    const { reach, seen } = spy();
    const repo = WHICH["ade.showRepo"](reach);
    const back = (await repo.onMessage({ kind: "do", what: "refresh" })) as { note?: string };
    expect(seen).toEqual([]);
    expect(back.note).toBe("");
  });
});

describe("the quiz, which posts rather than reads", () => {
  // A quiz already written for this diff comes straight back.
  const held = (quiz: unknown): Reach => ({
    get: async () => ({}),
    call: async () => ({ status: 200, ok: true, text: "{}" }),
    send: async () => ({ quiz }),
    open: () => {},
    link: () => {},
    run: () => {},
  });

  it("asks on ready and hands the questions straight to the page", async () => {
    const panel = WHICH["ade.quizMe"](held({ questions: [{ ask: "why?" }] }));
    const back = (await panel.onMessage({ kind: "ready" })) as {
      kind?: string;
      quiz?: { questions: unknown[] };
      busy?: boolean;
    };
    expect(back.kind).toBe("state");
    expect(back.quiz?.questions).toHaveLength(1);
    expect(back.busy).toBe(false);
  });

  it("regenerates with the flag the server reads", async () => {
    const seen: unknown[] = [];
    const reach: Reach = {
      ...held({ questions: [] }),
      send: async (_at, _path, body) => {
        seen.push(body);
        return { quiz: { questions: [] } };
      },
    };
    const panel = WHICH["ade.quizMe"](reach);
    await panel.onMessage({ kind: "ready" });
    await panel.onMessage({ kind: "do", what: "regenerate" });
    expect(seen).toEqual([{ regenerate: false }, { regenerate: true }]);
  });

  it("says what went wrong rather than drawing an empty quiz", async () => {
    const reach: Reach = {
      ...held(undefined),
      send: async () => {
        throw new Error("nothing to quiz: the diff is empty");
      },
    };
    const back = (await WHICH["ade.quizMe"](reach).onMessage({ kind: "ready" })) as {
      note?: string;
    };
    expect(back.note).toContain("the diff is empty");
  });

  it("opens a file through the window, since a page has no editor", async () => {
    const opened: string[] = [];
    const reach: Reach = { ...held(undefined), open: (path) => opened.push(path) };
    await WHICH["ade.quizMe"](reach).onMessage({
      kind: "do",
      what: "open",
      file: "internal/web/web.go",
    });
    expect(opened).toEqual(["internal/web/web.go"]);
  });
});

describe("the sandbox, which redraws from state and nothing else", () => {
  const saved = { paths: [{ path: "/r/other", rw: true }], env: {} };

  it("hands back the file the save answered with, so the list moves", async () => {
    const reach: Reach = {
      get: async () => ({ paths: [], env: {} }),
      call: async () => ({ status: 200, ok: true, text: "{}" }),
      send: async () => saved,
      open: () => {},
      link: () => {},
      run: () => {},
    };
    const back = (await WHICH["ade.sandboxPanel"](reach).onMessage({
      kind: "save",
      body: saved,
    })) as { kind?: string; file?: unknown; note?: string };
    expect(back.kind).toBe("state");
    expect(back.file).toEqual(saved);
    expect(back.note).toBe("");
  });

  it("re-reads when the save was refused, so a dead edit does not stay up", async () => {
    const reach: Reach = {
      get: async () => ({ paths: [], env: {} }),
      call: async () => ({ status: 200, ok: true, text: "{}" }),
      send: async () => ({ error: "nowhere is not a directory" }),
      open: () => {},
      link: () => {},
      run: () => {},
    };
    const back = (await WHICH["ade.sandboxPanel"](reach).onMessage({
      kind: "save",
      body: {},
    })) as { file?: { paths: unknown[] }; note?: string };
    expect(back.note).toBe("nowhere is not a directory");
    expect(back.file?.paths).toEqual([]);
  });
});

// A page draws from the one key its host puts the data under. The host
// guessed those keys, and for the commit page it guessed the whole of
// `/api/state` where the page wanted `state.worktree`: no files, no
// error, nothing to see. Read the page instead of guessing.
describe("every panel is handed the key its page reads", () => {
  const pages: Record<string, string> = {
    "ade.commitPanel": "commitpanel",
    "ade.showRepo": "repo",
    "ade.purgeWorktrees": "purgepanel",
    "ade.purgeDesigns": "designprunepanel",
    "ade.worktrees": "worktreepanel",
  };

  // What a page takes off the state it is sent, as it names it.
  function reads(page: string): Set<string> {
    const src = readFileSync(
      join(__dirname, "..", "..", "vscode", "src", page, "main.ts"),
      "utf8",
    );
    const out = new Set<string>();
    for (const m of src.matchAll(/\b(?:said|state)\.([a-zA-Z]+)/g)) out.add(m[1]);
    for (const skip of ["kind", "busy", "note", "path", "id", "error", "text"]) out.delete(skip);
    return out;
  }

  for (const [command, page] of Object.entries(pages)) {
    it(`${page} is sent something it reads`, async () => {
      const { reach } = spy();
      const panel = WHICH[command](reach);
      const sent = (await panel.onMessage({ kind: "ready" })) as Record<string, unknown>;
      const keys = Object.keys(sent).filter(
        (k) => !["kind", "busy", "note"].includes(k),
      );
      // Every field it reads, not merely one of them: the commit page
      // was sent `tree` and read six more, and drew half a screen.
      const missing = [...reads(page)].filter((k) => !keys.includes(k));
      expect(missing, `${page} reads these and the host sends none of them`).toEqual([]);
    });
  }
});

describe("the commit panel, where busy names the verb in flight", () => {
  const state = {
    worktree: { ahead: 2, behind: 0, staged: [], unstaged: [] },
    meta: { base: "main", branch: "feat/x" },
  };
  const reach = (seen: string[] = []): Reach => ({
    get: async () => state,
    call: async () => ({ status: 200, ok: true, text: "{}" }),
    send: async (_at, path) => {
      seen.push(path);
      return {};
    },
    open: () => {},
    link: () => {},
    run: () => {},
  });

  it("hands the page the seven things it draws from", async () => {
    const back = (await WHICH["ade.commitPanel"](reach()).onMessage({ kind: "ready" })) as Record<
      string,
      unknown
    >;
    expect(back.tree).toEqual(state.worktree);
    expect(back.base).toBe("main");
    expect(back.branch).toBe("feat/x");
    expect(back.ahead).toBe(2);
    expect(back.hasPR).toBe(false);
    expect(back.busy).toBe("");
  });

  it("stays busy after a redraft, so the button disables itself", async () => {
    const panel = WHICH["ade.commitPanel"](reach());
    const back = (await panel.onMessage({ kind: "do", what: "redraft" })) as { busy?: string };
    // The draft has not arrived, so the work is still in flight: the
    // page reads this and will not start a second drafting agent.
    expect(back.busy).toBe("redraft");
  });

  it("refuses a second redraft while the first is running", async () => {
    const seen: string[] = [];
    const panel = WHICH["ade.commitPanel"](reach(seen));
    await panel.onMessage({ kind: "do", what: "redraft" });
    await panel.onMessage({ kind: "do", what: "redraft" });
    expect(seen).toEqual(["/api/commit-message"]);
  });

  it("is idle again once the draft it waited for arrives", async () => {
    const seen: string[] = [];
    let draft: unknown;
    const panel = WHICH["ade.commitPanel"]({
      ...reach(seen),
      get: async () => ({ ...state, draft }),
    });
    await panel.onMessage({ kind: "do", what: "redraft" });
    draft = { title: "a change" };
    const back = (await panel.onMessage({ kind: "do", what: "refresh" })) as { busy?: string };
    expect(back.busy).toBe("");
  });

  it("commits and pushes when the form asked for both", async () => {
    const seen: string[] = [];
    await WHICH["ade.commitPanel"](reach(seen)).onMessage({
      kind: "do",
      what: "commit",
      push: true,
    });
    expect(seen).toEqual(["/api/commit", "/api/push"]);
  });
});

describe("a panel hears about work that finished elsewhere", () => {
  it("the commit panel takes a push, because its draft lands outside it", () => {
    const reach: Reach = {
      get: async () => ({}),
      call: async () => ({ status: 200, ok: true, text: "{}" }),
      send: async () => ({}),
      open: () => {},
      link: () => {},
      run: () => {},
    };
    expect(WHICH["ade.commitPanel"](reach).stirs).toBe(true);
    // The sweep is a `gh pr list` per repository and events arrive
    // every few seconds: it is asked for, never pushed at.
    expect(WHICH["ade.purgeDesigns"](reach).stirs).toBeFalsy();
  });
});
