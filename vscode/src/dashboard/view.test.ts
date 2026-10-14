import { describe, expect, it } from "vitest";
import type { Session } from "../launcher";
import { ago, inFlight, sections, type Board } from "./view";

const now = Date.parse("2026-10-09T20:00:00Z");
const board: Board = {
  waiting: [{ label: "sam asked you", description: "ade", icon: "bell", command: "ade.omnibox.open", args: [{ kind: "ask" }] }],
  reviews: [
    { repo: "ade", number: 21, title: "Queue edits", author: "kim", url: "https://x/21", checks: "passing", updatedAt: "2026-10-09T19:30:00Z" },
    { repo: "ade", number: 22, title: "Muted one", author: "kim", url: "https://x/22", muted: true },
  ],
  notifications: [
    { id: "n1", repo: "ade", reason: "review_requested", type: "PullRequest", title: "Queue edits", url: "https://x/21", updatedAt: "2026-10-09T19:00:00Z", unread: true },
    { id: "n2", repo: "ade", reason: "mention", type: "Issue", title: "old", url: "https://x/9", updatedAt: "", unread: false, dismissed: true },
  ],
  tickets: [
    { identifier: "ADE-7", title: "Todo groups", url: "https://linear/ADE-7", state: "Todo" },
    { identifier: "ADE-5", title: "Editor fork", url: "https://linear/ADE-5", state: "In Progress", priority: 2 },
  ],
  started: { "ADE-5": { dir: "/r/ade-fork", branch: "feat/vscodium", session: "s1" } },
  mine: [{ number: 16, title: "Concierge", author: "owen", headRef: "feat/concierge", url: "https://x/16", mine: true, checks: "passing" }],
  sessions: [{ session: { id: "s1", agent: "claude", name: "ade", branch: "feat/vscodium", dir: "/r/ade-fork", title: "Editor fork", live: true } as Session, repo: "ade", waiting: false, here: false }],
  todos: [
    { id: "t1", text: "fix the pill #release", meta: "tomorrow · ade", icon: "circle-large-outline" },
    { id: "t2", text: "cut the notes", meta: "in progress", icon: "sync", started: "s1" },
  ],
  agent: "ade · working 4s",
  at: now,
};

describe("the dashboard", () => {
  const got = sections(board);
  const by = (id: string) => got.find((s) => s.id === id)!;

  it("reads top down: waiting, todos, reviews, yours, issues, inbox, sessions", () => {
    expect(got.map((s) => s.id)).toEqual(["waiting", "todos", "reviews", "mine", "tickets", "inbox", "sessions"]);
  });

  it("leaves out what was muted or dismissed", () => {
    expect(by("reviews").lines.map((l) => l.label)).toEqual(["ade#21 · Queue edits"]);
    expect(by("reviews").lines[0].meta).toBe("kim · passing · 30m ago");
    expect(by("inbox").lines).toHaveLength(1);
  });

  it("lists the todos with Done to hand, and the session a started one became", () => {
    const lines = by("todos").lines;
    expect(lines.map((l) => l.label)).toEqual(["fix the pill #release", "cut the notes"]);
    expect(lines[0].do).toEqual({ what: "todo", id: "t1" });
    expect(lines[0].also?.map((a) => a.label)).toEqual(["Done"]);
    expect(lines[1].also?.map((a) => a.do)).toEqual([{ what: "session", id: "s1" }, { what: "todo-done", id: "t2" }]);
    expect(got.map((s) => s.id).indexOf("todos")).toBe(1);
  });

  it("puts issues in flight first and opens their session", () => {
    const lines = by("tickets").lines;
    expect(lines.map((l) => l.label)).toEqual(["ADE-5 · Editor fork", "ADE-7 · Todo groups"]);
    expect(lines[0].do).toEqual({ what: "session", id: "s1" });
    expect(lines[0].meta).toBe("In Progress · P2 · on feat/vscodium");
    expect(lines[1].also?.[0].do).toEqual({ what: "start-ticket", identifier: "ADE-7" });
  });

  it("acts from a line: a review opens, a notification opens on the web", () => {
    expect(by("reviews").lines[0].do).toEqual({ what: "open-review", repo: "ade", number: 21 });
    expect(by("inbox").lines[0].do).toEqual({ what: "open-url", url: "https://x/21" });
    expect(by("waiting").lines[0].do).toEqual({ what: "row", command: "ade.omnibox.open", args: [{ kind: "ask" }] });
  });

  it("knows an issue is in flight by its state, a start, or a flag", () => {
    expect(inFlight({ identifier: "a", title: "", url: "", state: "In Review" }, {})).toBe(true);
    expect(inFlight({ identifier: "a", title: "", url: "", state: "Todo" }, {})).toBe(false);
    expect(ago("2026-10-09T19:59:40Z", now)).toBe("just now");
    expect(ago("2026-10-08T20:00:00Z", now)).toBe("24h ago");
    expect(ago(undefined, now)).toBe("");
  });
});
