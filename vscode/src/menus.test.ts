import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// A `when` clause is a regex against a string chosen somewhere else, and a
// wrong one is an invisible button that nothing else catches.
const pkg = JSON.parse(
  readFileSync(join(__dirname, "..", "package.json"), "utf8"),
) as {
  contributes: {
    menus: Record<string, { command: string; when?: string }[]>;
    commands: { command: string }[];
  };
};

const items = pkg.contributes.menus["view/item/context"] ?? [];

const produced: Record<string, string[]> = {
  "ade.inbox": [
    "ade.bus.you",
    "ade.bus.member",
    "ade.bus.member.ask",
    "ade.bus.member.reviews",
    "ade.bus.member.both",
    "ade.bus.group",
    "ade.bus.reply",
    "ade.bus.ask",
    "ade.bus.review",
  ],
  "ade.diff": ["ade.change"],
  "ade.queue": ["adeQueued", "adeQueuedNext"],
};

// matcher reads `view == X && viewItem =~ /re/` or `viewItem == X` out of a
// when clause, which is the only shape this extension uses.
function matches(when: string, view: string, value: string): boolean {
  if (!when.includes(`view == ${view}`)) return false;
  const re = /viewItem =~ \/(.+?)\/(?=\s|$|\))/.exec(when);
  if (re) return new RegExp(re[1].replace(/\\\\/g, "\\")).test(value);
  const eq = /viewItem == ([\w.]+)/.exec(when);
  if (eq) return eq[1] === value;
  return true;
}

describe("view/item/context clauses", () => {
  it("every clause matches at least one row the tree can draw", () => {
    for (const item of items) {
      const when = item.when ?? "";
      const view = /view == ([\w.]+)/.exec(when)?.[1];
      expect(view, `${item.command} has no view in its when`).toBeTruthy();
      const values = produced[view!] ?? [];
      expect(
        values.length,
        `${view} produces no known contextValue`,
      ).toBeGreaterThan(0);
      expect(
        values.some((v) => matches(when, view!, v)),
        `${item.command}: nothing in ${view} matches ${when}`,
      ).toBe(true);
    }
  });

  it("every command a menu names is a declared command", () => {
    const declared = new Set(pkg.contributes.commands.map((c) => c.command));
    for (const item of items) expect(declared).toContain(item.command);
  });
});

describe("the Diff view's title", () => {
  const strip = (pkg.contributes.menus["view/title"] ?? [])
    .filter((i) => /view == ade\.diff\b/.test(i.when ?? ""))
    .map((i) => ({ ...i, group: (i as { group?: string }).group ?? "" }))
    .filter((i) => i.group.startsWith("navigation"))
    .sort((a, b) => a.group.localeCompare(b.group));

  it("leads with Review with named agents", () => {
    expect(strip[0]?.command).toBe("ade.reviewWithAgents");
  });

  it("gives every button its own place, so the order is not left to chance", () => {
    const places = strip.map((i) => i.group);
    expect(new Set(places).size).toBe(places.length);
  });

  // The toolbar reads left to right in group order, so the order is the test.
  it("reads review, then the scopes, then refresh and commit", () => {
    expect(strip.map((i) => i.command)).toEqual([
      "ade.reviewWithAgents",
      "ade.scopeBranch",
      "ade.scopeCommitted",
      "ade.scopeUncommitted",
      "ade.refreshChanges",
      "ade.writeCommitMessage",
    ]);
  });
});
