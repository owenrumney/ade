import { describe, expect, it } from "vitest";
import { models, says, span, wheres } from "./build";
import type { Host, Model } from "./launcher";

const hosts: Host[] = [
  { name: "local", local: true, connected: true },
  { name: "bigboy", label: "Big Boy", connected: true },
  { name: "pi5", connected: false },
];

const tree = { branch: "fix-726" };

describe("where a build can go", () => {
  it("offers here, a worktree, and every machine that is actually connected", () => {
    const got = wheres("feature", hosts, false, tree);
    expect(got.map((w) => w.label)).toEqual([
      "Here",
      "In a new worktree",
      "Big Boy",
    ]);
    expect(got[2].machine).toBe("bigboy");
  });

  it("names the branch, because Here clears the conversation on it", () => {
    expect(wheres("feature", [], false, tree)[0].detail).toContain("feature");
  });

  it("names the worktree's own branch, which is not this one", () => {
    expect(wheres("feature", [], false, tree)[1].detail).toContain("fix-726");
  });

  // Already on the branch the document names: the server refuses a second
  // worktree off it, so offering one is a dead end.
  it("drops the worktree when the server proposes none", () => {
    expect(wheres("fix-726", hosts, false, null).map((w) => w.id)).toEqual([
      "here",
      "machine",
    ]);
  });

  // A fetched snapshot has no local checkout, so only a machine is left.
  it("drops the local answers on a snapshot", () => {
    expect(wheres("feature", hosts, true, tree).map((w) => w.id)).toEqual([
      "machine",
    ]);
  });
});

describe("which models", () => {
  const all: Model[] = [
    { id: "opus", label: "opus", agent: "claude" },
    { id: "gpt-5.6-terra", label: "terra", agent: "codex" },
  ];

  it("is the picked agent's, with its own default first", () => {
    expect(models(all, "codex").map((m) => m.id)).toEqual([
      "",
      "gpt-5.6-terra",
    ]);
    expect(models(all, "claude")[0].label).toContain("claude");
  });
});

describe("what the estimate says", () => {
  it("is nothing at all when nothing like it has run", () => {
    expect(says(undefined)).toBe("");
    expect(
      says({
        lowSeconds: 60,
        highSeconds: 120,
        runs: 0,
        kind: "build",
        group: "here",
      }),
    ).toBe("");
  });

  it("names the window and how many runs it is built from", () => {
    const got = says({
      lowSeconds: 1200,
      highSeconds: 2400,
      runs: 7,
      kind: "build",
      group: "in this repo",
      window: { name: "5h", used: 31.4, about: 8.2, high: 12 },
    });
    expect(got).toBe(
      "usually 20–40 min · about 8% of your 5h window (31% used) · from 7 builds in this repo",
    );
  });

  it("counts one run as one", () => {
    expect(
      says({
        lowSeconds: 60,
        highSeconds: 60,
        runs: 1,
        kind: "build",
        group: "here",
      }),
    ).toContain("from 1 build here");
  });

  it("reads long spans in hours", () => {
    expect(span(3600, 9000)).toBe("1h–2h 30m");
  });
});
