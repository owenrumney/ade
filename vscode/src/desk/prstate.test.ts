import { describe, expect, it } from "vitest";
import type { Review, ReviewState } from "../launcher";
import { icon, needsYou, runnable, says } from "./prstate";

const pr = (review?: ReviewState, muted?: boolean): Review => ({
  repo: "ade",
  number: 1,
  title: "x",
  author: "kim",
  url: "https://x/1",
  muted,
  review,
});

describe("what a pull request row says", () => {
  it("counts the drafts, because the number is what sends you back", () => {
    expect(says(pr({ state: "drafted", unsent: 2 }))).toBe("2 unsent");
    expect(says(pr({ state: "drafted", unsent: 1 }))).toBe("1 unsent");
  });

  it("says nothing extra for one merely asked for", () => {
    expect(says(pr())).toBe("");
    expect(icon(pr())).toBe("git-pull-request");
  });

  it("spins while a review is going and ticks once it is sent", () => {
    expect(icon(pr({ state: "running" }))).toBe("loading~spin");
    expect(says(pr({ state: "sent" }))).toBe("sent");
  });
});

describe("which rows want you", () => {
  it("wants you for a finished review and an unsent draft", () => {
    expect(needsYou(pr({ state: "ready" }))).toBe(true);
    expect(needsYou(pr({ state: "drafted", unsent: 1 }))).toBe(true);
  });

  it("leaves you alone while it runs, once it is sent, and when muted", () => {
    expect(needsYou(pr({ state: "running" }))).toBe(false);
    expect(needsYou(pr({ state: "sent" }))).toBe(false);
    expect(needsYou(pr({ state: "ready" }, true))).toBe(false);
  });
});

describe("which rows Review it would act on", () => {
  it("only one nothing has been done to", () => {
    expect(runnable(pr())).toBe(true);
    expect(runnable(pr({ state: "requested" }))).toBe(true);
    expect(runnable(pr({ state: "running" }))).toBe(false);
    expect(runnable(pr({ state: "ready" }))).toBe(false);
  });
});
