import { describe, expect, it } from "vitest";
import { blocked, label, mergeMethods, numberOf } from "./pr";
import type { BranchPR } from "./client";

const pr = (over: Partial<BranchPR> = {}): BranchPR => ({
  number: 12,
  title: "feat: the thing",
  url: "https://github.com/o/r/pull/12",
  state: "OPEN",
  ...over,
});

describe("blocked", () => {
  it("is empty when the verdict can be given", () => {
    expect(blocked(pr(), "approve")).toBe("");
  });

  it("repeats the server's reason for each verdict separately", () => {
    const one = pr({
      approveBlocked: "it is yours",
      mergeBlocked: "checks are red",
    });
    expect(blocked(one, "approve")).toBe("it is yours");
    expect(blocked(one, "merge")).toBe("checks are red");
    expect(blocked(one, "request-changes")).toBe("");
  });
});

describe("label", () => {
  it("names the pull request the way GitHub does", () => {
    expect(label(pr())).toBe("#12 feat: the thing");
  });
});

describe("mergeMethods", () => {
  // The server refuses anything but these two, so offering a third here
  // would only turn a pick into a worse error message.
  it("is the two the server accepts", () => {
    expect([...mergeMethods]).toEqual(["rebase", "merge"]);
  });
});

// A context menu hands the command the tree node; a row's own click hands it
// the pull request. Reading only one of the two opened nothing.
describe("finding the pull request a command was handed", () => {
  it("reads a row's own click argument", () => {
    expect(numberOf({ number: 412, title: "x" })).toBe(412);
  });

  it("reads a tree node from the context menu", () => {
    expect(numberOf({ kind: "listed", pr: { number: 412 } })).toBe(412);
    expect(numberOf({ kind: "branch", pr: { number: 7 } })).toBe(7);
  });

  it("is undefined for anything else, so nothing is opened", () => {
    expect(numberOf(undefined)).toBeUndefined();
    expect(numberOf(null)).toBeUndefined();
    expect(
      numberOf({ kind: "check", check: { name: "build" } }),
    ).toBeUndefined();
    expect(numberOf({ number: 0 })).toBeUndefined();
  });
});
