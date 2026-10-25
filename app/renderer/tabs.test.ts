import { describe, expect, it } from "vitest";
import {
  held,
  idOf,
  keepable,
  live,
  opened,
  rebuilt,
  shut,
  shutMany,
  sweeping,
  trimmed,
  type Tab,
} from "./tabs";

const tab = (id: string): Tab => ({ id, title: id, icon: "file", what: { kind: "none" } });

describe("what a tab is named by", () => {
  it("is what it holds, so the same file is the same tab", () => {
    expect(idOf({ kind: "code", title: "a/b.go", source: "" })).toBe("code:a/b.go");
    expect(idOf({ panel: "ade.openChat" })).toBe("panel:ade.openChat");
    expect(idOf({ kind: "term", title: "Terminal" })).toBe("term");
  });

  it("names a diff by its file, whichever title it was given", () => {
    const file = { path: "x.go", status: "modified", binary: false, additions: 1, deletions: 0 };
    expect(idOf({ kind: "hunks", title: "anything", file })).toBe("hunks:x.go");
  });
});

describe("opening", () => {
  it("adds one that is not open", () => {
    expect(opened([tab("a")], tab("b")).map((t) => t.id)).toEqual(["a", "b"]);
  });

  it("replaces the one already holding it, in place", () => {
    const again = { ...tab("a"), title: "read again" };
    const out = opened([tab("a"), tab("b")], again);
    expect(out.map((t) => t.id)).toEqual(["a", "b"]);
    expect(out[0].title).toBe("read again");
  });
});

describe("closing", () => {
  const three = [tab("a"), tab("b"), tab("c")];

  it("lands on the one to the right", () => {
    expect(shut(three, "b", "b")).toEqual({ tabs: [tab("a"), tab("c")], active: "c" });
  });

  it("lands on the last when the end one goes", () => {
    expect(shut(three, "c", "c").active).toBe("b");
  });

  it("leaves the active one alone when another closes", () => {
    expect(shut(three, "a", "c").active).toBe("c");
  });

  it("ends with nothing open", () => {
    expect(shut([tab("a")], "a", "a")).toEqual({ tabs: [], active: "" });
  });

  it("ignores one that is not open", () => {
    expect(shut(three, "nope", "a")).toEqual({ tabs: three, active: "a" });
  });
});

describe("closing a sweep of them", () => {
  const four = [tab("a"), tab("b"), tab("c"), tab("d")];

  it("takes the ones to one side of the one picked, in strip order", () => {
    expect(sweeping(four, "b", "right")).toEqual(["c", "d"]);
    expect(sweeping(four, "c", "left")).toEqual(["a", "b"]);
    expect(sweeping(four, "b", "others")).toEqual(["a", "c", "d"]);
    expect(sweeping(four, "b", "all")).toEqual(["a", "b", "c", "d"]);
  });

  it("takes none to the right of the last, or left of the first", () => {
    expect(sweeping(four, "d", "right")).toEqual([]);
    expect(sweeping(four, "a", "left")).toEqual([]);
  });

  // All four is the only sweep that means anything from a tab that went.
  it("takes nothing from a tab that is not open", () => {
    expect(sweeping(four, "nope", "right")).toEqual([]);
    expect(sweeping(four, "nope", "others")).toEqual([]);
    expect(sweeping(four, "nope", "all")).toEqual(["a", "b", "c", "d"]);
  });

  it("keeps the one picked, whichever was active", () => {
    const out = shutMany(four, sweeping(four, "b", "others"), "d");
    expect(out.tabs.map((t) => t.id)).toEqual(["b"]);
    expect(out.active).toBe("b");
    expect(out.gone).toEqual(["a", "c", "d"]);
  });

  it("leaves the active one in front when it survives the sweep", () => {
    const out = shutMany(four, sweeping(four, "b", "right"), "a");
    expect(out.tabs.map((t) => t.id)).toEqual(["a", "b"]);
    expect(out.active).toBe("a");
  });

  // Closing everything to the left of c while a was in front has to land
  // somewhere, and c is the one the sweep was named from.
  it("lands on a survivor when the active one is swept", () => {
    const out = shutMany(four, sweeping(four, "c", "left"), "a");
    expect(out.tabs.map((t) => t.id)).toEqual(["c", "d"]);
    expect(out.active).toBe("c");
  });

  it("ends with nothing open and nothing in front", () => {
    const out = shutMany(four, sweeping(four, "b", "all"), "b");
    expect(out).toEqual({ tabs: [], active: "", gone: ["a", "b", "c", "d"] });
  });

  it("reports only what closed, so no pane is left behind", () => {
    expect(shutMany(four, ["b", "nope"], "a").gone).toEqual(["b"]);
  });
});

describe("a pane that may not be rebuilt", () => {
  // An iframe reloads when it is moved and a terminal reconnects, so
  // those stay in the page and are hidden instead.
  it("is a panel or the terminal", () => {
    expect(live({ ...tab("x"), what: { panel: "ade.openChat" } })).toBe(true);
    expect(live({ ...tab("x"), what: { kind: "term", title: "Terminal" } })).toBe(true);
    expect(live({ ...tab("x"), what: { kind: "doc", title: "d", body: "" } })).toBe(false);
  });
});

describe("what is showing", () => {
  it("is the active tab, or nothing", () => {
    expect(held([tab("a"), tab("b")], "b")?.id).toBe("b");
    expect(held([tab("a")], "gone")).toBeUndefined();
  });
});

describe("what is picked up again next time", () => {
  const file: Tab = {
    id: "code:a/b.go",
    title: "b.go",
    icon: "file",
    what: { kind: "code", title: "a/b.go", source: "x" },
  };
  const chat: Tab = { id: "panel:ade.openChat", title: "Chat", icon: "session", what: { panel: "ade.openChat" } };
  const panel: Tab = { id: "panel:ade.worktrees", title: "Worktrees", icon: "branch", what: { panel: "ade.worktrees" } };
  const doc: Tab = { id: "doc:#412", title: "#412", icon: "pr", what: { kind: "doc", title: "#412", body: "..." } };

  it("is a tab the window can open from its id alone", () => {
    expect(keepable([file, panel, doc])).toEqual(["code:a/b.go", "panel:ade.worktrees"]);
  });

  it("leaves out the chat, which is the dock and not a tab", () => {
    expect(keepable([file, chat])).toEqual(["code:a/b.go"]);
  });

  it("leaves out a document that came from a one-off read", () => {
    expect(keepable([doc])).toEqual([]);
  });
});

describe("when a pane is built again", () => {
  const pane = (key: string, waiting = false) => ({ key, waiting });

  it("is when the session or the address it was built with has moved on", () => {
    expect(rebuilt(pane("s1:http://x"), "s2:http://x", false)).toBe(true);
    expect(rebuilt(pane("s1:http://x"), "s1:http://y", false)).toBe(true);
    expect(rebuilt(pane("s1:http://x"), "s1:http://x", false)).toBe(false);
  });

  // The bug this is here for: the pane said "starting ade…", the session
  // came up, and nothing in the key had changed — so it said it until the
  // tab was closed and opened again.
  it("is when one that could only say “starting ade…” can now draw the page", () => {
    expect(rebuilt(pane("s1:http://x", true), "s1:http://x", false)).toBe(true);
  });

  it("is not when a drawn page is asked to wait again: it would lose what was typed", () => {
    expect(rebuilt(pane("s1:http://x", false), "s1:http://x", true)).toBe(false);
  });

  it("is not while it is still waiting", () => {
    expect(rebuilt(pane("s1:http://x", true), "s1:http://x", true)).toBe(false);
  });
});

describe("the tab sets written down", () => {
  const set = (n: string) => ({ tabs: [`code:${n}.go`], active: `code:${n}.go` });

  it("puts the conversation just used first, so the oldest is what goes", () => {
    const kept = new Map([["a", set("a")], ["b", set("b")], ["c", set("c")]]);
    expect(trimmed(kept, "c", 2).map(([s]) => s)).toEqual(["c", "a"]);
    expect(trimmed(kept, "a").map(([s]) => s)).toEqual(["a", "b", "c"]);
  });

  it("writes the others down even when the one just used has nothing kept", () => {
    const kept = new Map([["a", set("a")]]);
    expect(trimmed(kept, "unknown").map(([s]) => s)).toEqual(["a"]);
    expect(trimmed(new Map(), "a")).toEqual([]);
  });
});
