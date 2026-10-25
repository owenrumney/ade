import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { commit, newSession, WHICH, type Reach } from "./panels";

// What a panel's page actually sends. Read off its own `send(...)`
// calls rather than guessed: six panels answered "not wired yet"
// because the host had invented names for their buttons.
function verbsOf(panel: string): string[] {
  const src = readFileSync(
    join(__dirname, "..", "..", "vscode", "src", panel, "main.ts"),
    "utf8",
  );
  const sent = [...src.matchAll(/send\("([a-zA-Z-]+)"/g)].map((m) => m[1]);
  const direct = [...src.matchAll(/kind: "do", what: "([a-zA-Z-]+)"/g)].map(
    (m) => m[1],
  );
  // send(on ? "stage" : "unstage"). Missing this shape is how Stage all
  // reached a host that had never heard of it.
  const picked = [
    ...src.matchAll(/send\([^)]*?\?\s*"([a-zA-Z-]+)"\s*:\s*"([a-zA-Z-]+)"/g),
  ].flatMap((m) => [m[1], m[2]]);
  return [...new Set([...sent, ...direct, ...picked])];
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
    push: () => {},
    follow: () => {},
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
      for (const what of verbsOf(page)) {
        // `open` and `stop` are the window's own, not a route.
        if (what === "open" || what === "stop") continue;
        // A panel each: a verb that leaves the host busy makes every verb
        // after it answer with the state instead, and the check passes
        // without having checked anything.
        const { reach } = spy();
        const back = (await WHICH[command](reach).onMessage({
          kind: "do",
          what,
        })) as { note?: string };
        expect(back?.note ?? "", `${page}: ${what}`).not.toContain("not wired");
      }
    });
  }
});

describe("the verbs that are more than one call", () => {
  it("commits, and pushes when the form asked for both", async () => {
    const { reach, seen } = spy();
    const commit = WHICH["ade.commitPanel"](reach);
    await commit.onMessage({
      kind: "do",
      what: "commit",
      message: "a change\n\nThe body.",
      push: true,
    });
    expect(seen.map((s) => s.path)).toEqual(["/api/commit", "/api/push"]);
    expect(seen[0].body).toEqual({ title: "a change", body: "The body." });
  });

  // The server clears its draft when the commit lands, but the page keeps
  // what you typed across every other read, so the host has to say which
  // state is the one that followed a commit.
  it("says the commit landed, once", async () => {
    const { reach } = spy();
    const commit = WHICH["ade.commitPanel"](reach);
    const after = (await commit.onMessage({
      kind: "do",
      what: "commit",
      title: "a change",
    })) as { committed?: boolean };
    expect(after.committed).toBe(true);
    const next = (await commit.onMessage({ kind: "do", what: "refresh" })) as {
      committed?: boolean;
    };
    expect(next.committed).toBe(false);
  });

  it("does not push when it did not", async () => {
    const { reach, seen } = spy();
    const commit = WHICH["ade.commitPanel"](reach);
    await commit.onMessage({ kind: "do", what: "commit", message: "a change" });
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
    const back = (await repo.onMessage({ kind: "do", what: "refresh" })) as {
      note?: string;
    };
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
    push: () => {},
    follow: () => {},
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
    const back = (await WHICH["ade.quizMe"](reach).onMessage({
      kind: "ready",
    })) as {
      note?: string;
    };
    expect(back.note).toContain("the diff is empty");
  });

  it("opens a file through the window, since a page has no editor", async () => {
    const opened: string[] = [];
    const reach: Reach = {
      ...held(undefined),
      open: (path) => opened.push(path),
    };
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
      push: () => {},
      follow: () => {},
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
      push: () => {},
      follow: () => {},
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
    for (const m of src.matchAll(/\b(?:said|state)\.([a-zA-Z]+)/g))
      out.add(m[1]);
    for (const skip of ["kind", "busy", "note", "path", "id", "error", "text"])
      out.delete(skip);
    return out;
  }

  for (const [command, page] of Object.entries(pages)) {
    it(`${page} is sent something it reads`, async () => {
      const { reach } = spy();
      const panel = WHICH[command](reach);
      const sent = (await panel.onMessage({ kind: "ready" })) as Record<
        string,
        unknown
      >;
      const keys = Object.keys(sent).filter(
        (k) => !["kind", "busy", "note"].includes(k),
      );
      // Every field it reads, not merely one of them: the commit page
      // was sent `tree` and read six more, and drew half a screen.
      const missing = [...reads(page)].filter((k) => !keys.includes(k));
      expect(
        missing,
        `${page} reads these and the host sends none of them`,
      ).toEqual([]);
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
    push: () => {},
    follow: () => {},
  });

  it("hands the page the seven things it draws from", async () => {
    const back = (await WHICH["ade.commitPanel"](reach()).onMessage({
      kind: "ready",
    })) as Record<string, unknown>;
    expect(back.tree).toEqual(state.worktree);
    expect(back.base).toBe("main");
    expect(back.branch).toBe("feat/x");
    expect(back.ahead).toBe(2);
    expect(back.hasPR).toBe(false);
    expect(back.busy).toBe("");
  });

  it("stays busy after a redraft, so the button disables itself", async () => {
    const panel = WHICH["ade.commitPanel"](reach());
    const back = (await panel.onMessage({ kind: "do", what: "redraft" })) as {
      busy?: string;
    };
    // The draft has not arrived, so the work is still in flight: the
    // page reads this and will not start a second drafting agent.
    expect(back.busy).toBe("redraft");
  });

  // /api/pr-create answers 202 and an agent writes the pull request, so the
  // POST returning is not the work finishing.
  it("stays busy while the agent writes the pull request", async () => {
    const running = { ...state, agent: { running: true, kind: "pr-create" } };
    const seen: string[] = [];
    const panel = WHICH["ade.commitPanel"]({ ...reach(seen), get: async () => running });
    const back = (await panel.onMessage({ kind: "do", what: "createPR" })) as { busy?: string };
    expect(back.busy).toBe("createPR");
    await panel.onMessage({ kind: "do", what: "createPR" });
    expect(seen).toEqual(["/api/pr-create"]);
  });

  it("is idle again once that run is over", async () => {
    let agent: unknown = { running: true, kind: "pr-create" };
    const panel = WHICH["ade.commitPanel"]({ ...reach(), get: async () => ({ ...state, agent }) });
    await panel.onMessage({ kind: "do", what: "createPR" });
    agent = { running: false };
    const back = (await panel.onMessage({ kind: "do", what: "refresh" })) as { busy?: string };
    expect(back.busy).toBe("");
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
    const back = (await panel.onMessage({ kind: "do", what: "refresh" })) as {
      busy?: string;
    };
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
      push: () => {},
      follow: () => {},
    };
    expect(WHICH["ade.commitPanel"](reach).stirs).toBe(true);
    // The sweep is a `gh pr list` per repository and events arrive
    // every few seconds: it is asked for, never pushed at.
    expect(WHICH["ade.purgeDesigns"](reach).stirs).toBeFalsy();
  });
});

// One page, two hosts. The window's half is driven above; the editor's is
// the same page's other host, and a verb only one of them answers is a
// button that does nothing and says nothing.
describe("the commit page's verbs, in the editor's host too", () => {
  it("is answered by commitview.ts", () => {
    const host = readFileSync(
      join(__dirname, "..", "..", "vscode", "src", "commitview.ts"),
      "utf8",
    );
    const missing = verbsOf("commitpanel").filter(
      (what) => !host.includes(`case "${what}"`),
    );
    expect(missing).toEqual([]);
  });
});

// The page disables its buttons on `busy`. The host used to say so only in
// its answer, which lands when the work is already over — so Create PR could
// be pressed four times while the first press was still running.
describe("the commit panel while work is in flight", () => {
  const stateReply = {
    worktree: { ahead: 0, behind: 0, staged: ["a.go"], unstaged: [] },
    meta: { base: "main", branch: "feature" },
  };

  function panelWithSlowSend() {
    const pushed: Record<string, unknown>[] = [];
    let finish = () => {};
    const waited = new Promise<void>((done) => {
      finish = done;
    });
    const reach: Reach = {
      get: async () => stateReply,
      call: async () => ({ status: 200, ok: true, text: "{}" }),
      send: async () => {
        await waited;
        return {};
      },
      open: () => {},
      link: () => {},
      run: () => {},
      push: (id, said) => pushed.push({ id, ...(said as Record<string, unknown>) }),
      follow: () => {},
    };
    return { panel: commit(reach), pushed, finish };
  }

  it("says it is busy before the work is done, not after", async () => {
    const { panel, pushed, finish } = panelWithSlowSend();
    await panel.onMessage({ kind: "ready" });

    const running = panel.onMessage({ kind: "do", what: "createPR" });
    await Promise.resolve();
    expect(pushed).toHaveLength(1);
    expect(pushed[0].id).toBe("commit");
    expect(pushed[0].busy).toBe("createPR");

    // Still busy when the POST comes back: 202 means the agent has started
    // writing the pull request, not that it has written one.
    finish();
    const done = (await running) as { busy?: string };
    expect(done.busy).toBe("createPR");
  });

  // Two presses must not be two pull requests.
  it("ignores a second press while the first is running", async () => {
    const { panel, pushed, finish } = panelWithSlowSend();
    await panel.onMessage({ kind: "ready" });

    const first = panel.onMessage({ kind: "do", what: "createPR" });
    const second = (await panel.onMessage({ kind: "do", what: "createPR" })) as { busy?: string };
    expect(second.busy).toBe("createPR");
    expect(pushed, "the second press started nothing").toHaveLength(1);

    finish();
    await first;
  });
});

// Stage all sends `stage` with the loose paths. Checking the case exists in
// the source is not the same as the verb reaching the server.
describe("staging from the commit panel", () => {
  it("sends the paths to the staging routes", async () => {
    const { reach, seen } = spy();
    const panel = commit(reach);
    await panel.onMessage({ kind: "ready" });

    await panel.onMessage({ kind: "do", what: "stage", paths: ["a.go", "b.go"] });
    await panel.onMessage({ kind: "do", what: "unstage", paths: ["a.go"] });

    expect(seen.map((one: { path: string }) => one.path)).toEqual(["/api/stage", "/api/unstage"]);
    expect((seen[0].body as { paths: string[] }).paths).toEqual(["a.go", "b.go"]);
  });

  // A rebase leaves the branch diverged, so the tickbox has to reach git.
  it("passes the lease through to the push", async () => {
    const { reach, seen } = spy();
    await commit(reach).onMessage({ kind: "do", what: "push", force: true });
    expect(seen[0]).toEqual({ path: "/api/push", body: { force: true } });
  });

  it("pushes without the lease when it is not ticked", async () => {
    const { reach, seen } = spy();
    await commit(reach).onMessage({ kind: "do", what: "push" });
    expect(seen[0]).toEqual({ path: "/api/push", body: { force: false } });
  });

  it("says so rather than going quiet when a verb really is unknown", async () => {
    const { reach } = spy();
    const said = (await commit(reach).onMessage({ kind: "do", what: "fly" })) as { note?: string };
    expect(said.note).toContain("fly is not wired yet");
  });
});

// The window's host listed no documents at all — `designs: []`, hard-coded —
// so the Design row said "No design documents in coast yet" over a repository
// with forty of them.
describe("the New session panel's Design row", () => {
  function reachWith(designs: unknown, opened: Record<string, unknown>[] = []): Reach {
    return {
      get: async (_at, path) => {
        if (path.startsWith("/api/designs?dir=")) {
          if (designs instanceof Error) throw designs;
          return designs;
        }
        if (path === "/api/repos") return { repos: [{ name: "coast", path: "/r/coast" }] };
        return {};
      },
      call: async () => ({ status: 200, ok: true, text: "{}" }),
      send: async (_at, path, body) => {
        opened.push({ path, ...body });
        return { path: "/r/coast-x" };
      },
      open: () => {},
      link: () => {},
      run: () => {},
      push: () => {},
      follow: () => {},
    };
  }

  const offerOf = async (reach: Reach, seed: Record<string, unknown>) => {
    const got = (await newSession(reach, seed).onMessage({ kind: "ready" })) as {
      offer: { designs: { file: string }[]; designNote?: string };
    };
    return got.offer;
  };

  it("lists home's documents", async () => {
    const offer = await offerOf(reachWith({ designs: ["FIX-818.md", "TRK-1065.md"] }), {
      mode: "design",
      repos: ["/r/coast"],
    });
    expect(offer.designs.map((d) => d.file)).toEqual(["FIX-818.md", "TRK-1065.md"]);
  });

  it("says a failed read failed, rather than that there are none", async () => {
    const offer = await offerOf(reachWith(new Error("404 Not Found")), {
      mode: "design",
      repos: ["/r/coast"],
    });
    expect(offer.designs).toEqual([]);
    expect(offer.designNote).toContain("Could not list them");
  });

  // Picking a document and pressing Start has to start on that document.
  it("starts on the document that was picked", async () => {
    const opened: Record<string, unknown>[] = [];
    const panel = newSession(reachWith({ designs: ["FIX-818.md"] }, opened), {});
    await panel.onMessage({
      kind: "start",
      chosen: {
        mode: "design",
        repos: ["/r/coast"],
        design: "FIX-818.md",
        name: "",
        message: "",
        machine: "",
        agent: "",
        model: "",
        role: "",
        sandbox: true,
        ship: false,
      },
    });
    expect(opened[0].path).toBe("/api/open");
    expect(opened[0].name).toBe("FIX-818.md");
  });

  const typed = (name: string) => ({
    mode: "work",
    repos: ["/r/coast"],
    design: "",
    name,
    message: "",
    machine: "",
    agent: "",
    model: "",
    role: "",
    sandbox: true,
    ship: false,
  });

  // A state back is a redraw, and a redraw takes the cursor out of the box.
  it("answers a keystroke with nothing", async () => {
    const panel = newSession(reachWith({ designs: [] }), { repos: ["/r/coast"] });
    expect(await panel.onMessage({ kind: "change", chosen: typed("c") })).toBeUndefined();
    expect(await panel.onMessage({ kind: "change", chosen: typed("ca") })).toBeUndefined();
  });

  it("answers a new home with the documents it has", async () => {
    const panel = newSession(reachWith({ designs: ["FIX-818.md"] }), {});
    const said = (await panel.onMessage({ kind: "change", chosen: typed("c") })) as {
      offer: { designs: { file: string }[] };
    };
    expect(said.offer.designs.map((d) => d.file)).toEqual(["FIX-818.md"]);
  });

  it("sends what was pasted into the first message", async () => {
    const opened: Record<string, unknown>[] = [];
    const panel = newSession(reachWith({ designs: [] }, opened), {});
    const file = { name: "shot.png", mediaType: "image/png", data: "AAAA" };
    await panel.onMessage({ kind: "start", chosen: typed("cancel"), files: [file] });
    expect(opened[0].files).toEqual([file]);
  });

  // Start used to leave a red "started" banner and no conversation.
  it("follows the session it started", async () => {
    const went: string[] = [];
    const reach = { ...reachWith({ designs: [] }), follow: (dir: string) => went.push(dir) };
    const said = (await newSession(reach, {}).onMessage({
      kind: "start",
      chosen: typed("cancel"),
    })) as { note: string };
    expect(went).toEqual(["/r/coast-x"]);
    expect(said.note).toBe("");
  });

  it("stays open on a failure, and says it", async () => {
    const went: string[] = [];
    const reach = {
      ...reachWith({ designs: [] }),
      send: async () => ({ error: "that branch is checked out elsewhere" }),
      follow: (dir: string) => went.push(dir),
    };
    const said = (await newSession(reach, {}).onMessage({
      kind: "start",
      chosen: typed("cancel"),
    })) as { note: string };
    expect(went).toEqual([]);
    expect(said.note).toContain("checked out elsewhere");
  });
});

// Remove used to post the page's own message to /api/prune, which names no
// repository: the launcher refused, the window swallowed the refusal, and
// the page sat there with the same table and nothing said.
describe("removing worktrees from the purge page", () => {
  const sweep = {
    repos: [
      {
        path: "/code/ade",
        name: "ade",
        worktrees: [
          { path: "/code/ade-old", name: "ade-old", branch: "old", kind: "session", removable: true },
        ],
      },
      {
        path: "/code/coast",
        name: "coast",
        worktrees: [
          { path: "/code/coast-wip", name: "coast-wip", branch: "wip", kind: "session", removable: true },
          { path: "/code/coast-live", name: "coast-live", branch: "live", kind: "session", removable: false, keep: "serving" },
        ],
      },
    ],
  };

  function purge(
    answer: (body: Record<string, unknown>) => unknown,
    asked: string[] = [],
    seen: Record<string, unknown>[] = [],
  ): { reach: Reach; seen: Record<string, unknown>[]; asked: string[] } {
    const reach: Reach = {
      get: async () => sweep,
      call: async () => ({ status: 200, ok: true, text: "{}" }),
      send: async (_at, _path, body) => {
        seen.push(body);
        return answer(body);
      },
      open: () => {},
      link: () => {},
      run: () => {},
      push: () => {},
      follow: () => {},
      ask: async (title) => {
        asked.push(title);
        return "yes";
      },
    };
    return { reach, seen, asked };
  }

  async function remove(reach: Reach, paths: string[]): Promise<{ note: string }> {
    return (await WHICH["ade.purgeWorktrees"](reach).onMessage({
      kind: "do",
      what: "remove",
      paths,
    })) as { note: string };
  }

  it("prunes one repository at a time, each named", async () => {
    const { reach, seen, asked } = purge((body) => ({ removed: body.paths, kept: [] }));
    const said = await remove(reach, ["/code/ade-old", "/code/coast-wip"]);
    expect(seen).toEqual([
      { repo: "/code/ade", paths: ["/code/ade-old"], force: false },
      { repo: "/code/coast", paths: ["/code/coast-wip"], force: false },
    ]);
    expect(asked).toEqual(["Remove 2 worktrees?"]);
    expect(said.note).toBe("Removed 2.");
  });

  it("says why, when the launcher refuses", async () => {
    const { reach } = purge(() => ({ error: "that is not one of the repositories on this machine" }));
    const said = await remove(reach, ["/code/ade-old"]);
    expect(said.note).toContain("not one of the repositories");
  });

  it("removes nothing when the answer is no", async () => {
    const seen: Record<string, unknown>[] = [];
    const reach: Reach = {
      ...purge(() => ({}), [], seen).reach,
      ask: async () => undefined,
    };
    const said = await remove(reach, ["/code/ade-old"]);
    expect(seen).toEqual([]);
    expect(said.note).toBe("");
  });

  it("drops a tick the sweep no longer frees", async () => {
    const { reach, seen } = purge(() => ({ removed: [], kept: [] }));
    const said = await remove(reach, ["/code/coast-live"]);
    expect(seen).toEqual([]);
    expect(said.note).toContain("moved on");
  });
});

// Refresh read the same warm sweep the launcher had already served, so the
// button did nothing twice: no rescan, and no sign it had been pressed.
describe("refreshing the purge page", () => {
  function watched(): { reach: Reach; read: string[]; pushed: unknown[] } {
    const read: string[] = [];
    const pushed: unknown[] = [];
    const reach: Reach = {
      get: async (_at, path) => {
        read.push(path);
        return { repos: [] };
      },
      call: async () => ({ status: 200, ok: true, text: "{}" }),
      send: async () => ({}),
      open: () => {},
      link: () => {},
      run: () => {},
      push: (_id, said) => pushed.push(said),
      follow: () => {},
    };
    return { reach, read, pushed };
  }

  it("asks the launcher to rescan", async () => {
    const { reach, read } = watched();
    const panel = WHICH["ade.purgeWorktrees"](reach);
    await panel.onMessage({ kind: "ready" });
    await panel.onMessage({ kind: "do", what: "refresh" });
    expect(read).toEqual(["/api/worktrees/all", "/api/worktrees/all?fresh=1"]);
  });

  it("says it is scanning, with the table it already drew", async () => {
    const { reach, pushed } = watched();
    const panel = WHICH["ade.purgeWorktrees"](reach);
    await panel.onMessage({ kind: "ready" });
    await panel.onMessage({ kind: "do", what: "refresh" });
    expect(pushed).toEqual([{ kind: "state", sweep: { repos: [] }, busy: true, note: "" }]);
  });

  it("leaves a page with no rescan path reading its own", async () => {
    const { reach, read } = watched();
    const panel = WHICH["ade.showRepo"](reach);
    await panel.onMessage({ kind: "do", what: "refresh" });
    expect(read).toEqual(["/api/repo"]);
  });
});
