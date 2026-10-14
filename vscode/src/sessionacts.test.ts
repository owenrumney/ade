import { describe, expect, it } from "vitest";
import type { Session } from "./launcher";
import { acts } from "./sessionacts";

const session = (over: Partial<Session> = {}): Session => ({
  id: "abc12345",
  agent: "claude",
  dir: "/code/ade",
  name: "ade",
  branch: "main",
  ...over,
});

describe("what a session offers once you have found it", () => {
  const ids = (s: Session) => acts(s).map((a) => a.id);

  // Opening it is what the list is for, so it leads whatever else is offered.
  it("leads with opening it", () => {
    expect(acts(session())[0].id).toBe("open");
  });

  it("says where it would open, unless it is this window's own", () => {
    expect(acts(session())[0].detail).toBe("/code/ade");
    expect(acts(session({ here: true }))[0].detail).toBe("in this window");
  });

  // Only the one that would change anything, as everywhere else.
  it("offers archive on a live one and bring back on an archived one", () => {
    expect(ids(session())).toEqual(["open", "rename", "archive", "delete"]);
    expect(ids(session({ archived: true }))).toEqual([
      "open",
      "rename",
      "unarchive",
      "delete",
    ]);
  });

  // Delete is last on purpose: archive is the one that can be undone, so it
  // is the one your hand lands on first.
  it("puts delete below archive", () => {
    const order = ids(session());
    expect(order.indexOf("delete")).toBeGreaterThan(order.indexOf("archive"));
  });
});
