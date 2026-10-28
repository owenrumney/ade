import { describe, expect, it } from "vitest";
import type { BranchPR } from "../pages/client";
import { prActs } from "./prpage";

const url = "https://github.com/o/r/pull/7";
const ids = (acts: { id: string }[]) => acts.map((a) => a.id);

describe("what a pull request's page offers", () => {
  it("offers a review request to ade first, and to put it aside", () => {
    expect(ids(prActs({ url, requested: {} }))).toEqual(["ade", "web", "mute"]);
    expect(prActs({ url, requested: {} })[0].primary).toBe(true);
  });

  it("offers one put aside back", () => {
    expect(ids(prActs({ url, requested: { muted: true } }))).toEqual(["ade", "web", "unmute"]);
  });

  it("gives the session's own pull request its verdicts, with GitHub's reason when blocked", () => {
    const branch = { number: 7, approveBlocked: "you opened it", mergeBlocked: "" } as BranchPR;
    const acts = prActs({ url, branch });
    expect(ids(acts)).toEqual(["approve", "request-changes", "merge", "web"]);
    expect(acts[0].blocked).toBe("you opened it");
    expect(acts[2].blocked).toBeUndefined();
  });

  it("offers only the web when ade knows nothing else about it", () => {
    expect(ids(prActs({ url }))).toEqual(["web"]);
  });
});
