import { describe, expect, it } from "vitest";
import { held, idOf, keepable, live, opened, shut, type Tab } from "./tabs";

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
  const doc: Tab = { id: "doc:#412", title: "#412", icon: "pr", what: { kind: "doc", title: "#412", body: "..." } };

  it("is a tab the window can open from its id alone", () => {
    expect(keepable([file, chat, doc])).toEqual(["code:a/b.go", "panel:ade.openChat"]);
  });

  it("leaves out a document that came from a one-off read", () => {
    expect(keepable([doc])).toEqual([]);
  });
});
