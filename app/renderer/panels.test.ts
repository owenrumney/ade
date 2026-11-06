import { describe, expect, it } from "vitest";
import { newSession, WHICH, type Reach } from "./panels";

describe("the panels the window frames", () => {
  it("are New session and Settings, and nothing that was killed", () => {
    expect(Object.keys(WHICH).sort()).toEqual(["ade.newSession", "ade.openSettings"]);
  });
});

describe("starting a session from the New session panel", () => {
  function reachWith(sent: Record<string, unknown>[], followed: string[] = []): Reach {
    return {
      get: async () => ({}),
      call: async () => ({ status: 200, ok: true, text: "{}" }),
      send: async (_at, path, body) => {
        sent.push({ path, ...body });
        return { path: "/r/coast-fix" };
      },
      link: () => {},
      push: () => {},
      follow: (dir) => void followed.push(dir),
    };
  }
  const work = { mode: "work", name: "fix", ticket: "", message: " go ", agent: "codex", repos: ["/r/coast", "/r/web"], branch: "", at: "" };

  it("sends the agent, the first message and every ticked repository", async () => {
    const sent: Record<string, unknown>[] = [];
    await newSession(reachWith(sent)).onMessage({ kind: "start", chosen: work });
    expect(sent).toEqual([
      { path: "/api/open", dir: "/r/coast", name: "fix", ticket: undefined, also: ["/r/web"], fresh: true, direct: true, agent: "codex", prompt: "go", terminal: true },
    ]);
  });

  it("sends a ticket in place of a name, so the server names the branch", async () => {
    const sent: Record<string, unknown>[] = [];
    await newSession(reachWith(sent)).onMessage({ kind: "start", chosen: { ...work, mode: "ticket", ticket: "FIX-623" } });
    expect(sent[0]).toMatchObject({ ticket: "FIX-623", name: undefined });
  });

  it("follows the checkout the launcher answered with", async () => {
    const followed: string[] = [];
    await newSession(reachWith([], followed)).onMessage({ kind: "start", chosen: work });
    expect(followed).toEqual(["/r/coast-fix"]);
  });

  it("gives scratch its own route and no repository", async () => {
    const sent: Record<string, unknown>[] = [];
    await newSession(reachWith(sent)).onMessage({ kind: "start", chosen: { ...work, mode: "scratch", repos: [] } });
    expect(sent[0]).toMatchObject({ path: "/api/scratch", name: "fix", agent: "codex", terminal: true });
  });
});

describe("the New session panel's Open branch row", () => {
  const BRANCHES = {
    worktrees: [
      { path: "/r/coast", name: "coast", branch: "main", main: true },
      { path: "/r/coast-fix-pricing", name: "coast-fix-pricing", branch: "fix-pricing" },
      { path: "/r/coast-detached", name: "coast-detached" },
    ],
    branches: [{ name: "feat/ADE-211", at: "3 days ago" }],
  };

  function reachWith(answer: unknown = BRANCHES, sent: Record<string, unknown>[] = []): Reach {
    return {
      get: async (_at, path) => {
        if (path.startsWith("/api/branches?repo=")) {
          if (answer instanceof Error) throw answer;
          return answer;
        }
        if (path === "/api/repos") return { repos: [{ name: "coast", path: "/r/coast" }] };
        return {};
      },
      call: async () => ({ status: 200, ok: true, text: "{}" }),
      send: async (_at, path, body) => {
        sent.push({ path, ...body });
        return path === "/api/branches/worktree" ? { path: "/r/coast-feat-ade-211" } : { url: "u" };
      },
      link: () => {},
      push: () => {},
      follow: () => {},
    };
  }

  const offerOf = async (reach: Reach, seed: Record<string, unknown>) => {
    const got = (await newSession(reach, seed).onMessage({ kind: "ready" })) as {
      offer: { checkouts?: { name: string; at: string }[]; checkoutNote?: string };
    };
    return got.offer;
  };

  const chose = (branch: string, at: string) => ({
    mode: "branch",
    repos: ["/r/coast"],
    branch,
    at,
    name: "",
    message: "",
    agent: "claude",
    ticket: "",
  });

  it("offers the worktrees first, then the branches with none", async () => {
    const offer = await offerOf(reachWith(), { mode: "branch", repos: ["/r/coast"] });
    expect(offer.checkouts).toEqual([
      { name: "main", at: "/r/coast", note: "main checkout" },
      { name: "fix-pricing", at: "/r/coast-fix-pricing", note: "coast-fix-pricing" },
      { name: "feat/ADE-211", at: "", note: "3 days ago" },
    ]);
  });

  // Nothing names it, so it cannot be opened by name.
  it("leaves out a worktree on no branch", async () => {
    const offer = await offerOf(reachWith(), { mode: "branch", repos: ["/r/coast"] });
    expect(offer.checkouts?.map((one) => one.at)).not.toContain("/r/coast-detached");
  });

  it("does not read the branches for another row", async () => {
    const offer = await offerOf(reachWith(), { mode: "work", repos: ["/r/coast"] });
    expect(offer.checkouts).toBeUndefined();
  });

  it("says a failed read failed, rather than that there are none", async () => {
    const offer = await offerOf(reachWith(new Error("not a git repository")), {
      mode: "branch",
      repos: ["/r/coast"],
    });
    expect(offer.checkoutNote).toContain("not a git repository");
    expect(offer.checkouts).toBeUndefined();
  });

  // The checkout is already there, so nothing is made: it opens as it is.
  it("opens a worktree it already has without making one", async () => {
    const sent: Record<string, unknown>[] = [];
    const panel = newSession(reachWith(BRANCHES, sent), {});
    await panel.onMessage({ kind: "start", chosen: chose("fix-pricing", "/r/coast-fix-pricing") });
    expect(sent.map((one) => one.path)).toEqual(["/api/open"]);
    expect(sent[0].dir).toBe("/r/coast-fix-pricing");
    expect(sent[0].name).toBeUndefined();
  });

  // A branch with no checkout gets one, and the session opens in that.
  it("checks a branch out first when it has no worktree", async () => {
    const sent: Record<string, unknown>[] = [];
    const panel = newSession(reachWith(BRANCHES, sent), {});
    await panel.onMessage({ kind: "start", chosen: chose("feat/ADE-211", "") });
    expect(sent.map((one) => one.path)).toEqual(["/api/branches/worktree", "/api/open"]);
    expect(sent[0]).toMatchObject({ repo: "/r/coast", branch: "feat/ADE-211" });
    expect(sent[1].dir).toBe("/r/coast-feat-ade-211");
  });
});

