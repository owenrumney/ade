import { describe, expect, it } from "vitest";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import {
  entries,
  GROUPS,
  hidden,
  listed,
  ROW_ONLY,
  strip,
  TITLE_ONLY,
} from "./menu";
import type { Contributes } from "./menu";
import {
  ADE_EDITOR_MENUS,
  CODE_PROPOSALS,
  KNOWN_MENUS,
  PROPOSED_MENUS,
} from "./proposed";
import { OPEN, RESOLVED, stateOf, threadState } from "./reconcile";
import type { Thread } from "./client";

const pkg = JSON.parse(
  readFileSync(join(__dirname, "..", "package.json"), "utf8"),
) as {
  contributes: Contributes;
  enabledApiProposals?: string[];
};
const c = pkg.contributes;

describe("strip", () => {
  it("drops the prefix every row would otherwise carry", () => {
    expect(strip("ade: Review the diff")).toBe("Review the diff");
    expect(strip("Reply")).toBe("Reply");
  });
});

describe("hidden", () => {
  it("is the commands that need a row to act on", () => {
    expect(hidden().has("ade.reply")).toBe(true);
    expect(hidden().has("ade.openChat")).toBe(false);
  });
});

describe("VS Code's own palette", () => {
  const shown = c.commands
    .map((x) => x.command)
    .filter(
      (id) =>
        !(c.menus?.commandPalette ?? []).some(
          (m) => m.command === id && m.when === "false",
        ),
    );

  // The parent is the door. Fifty entries beside it is the thing it replaces.
  it("shows the parent entry and nothing else", () => {
    expect(shown).toEqual(["ade.menu"]);
  });
});

describe("ROW_ONLY", () => {
  it("names only commands that exist", () => {
    const real = new Set(c.commands.map((x) => x.command));
    for (const id of ROW_ONLY) expect(real, id).toContain(id);
  });

  // A command in neither is unreachable from the palette and has no row.
  it("accounts for every command with the groups, between them", () => {
    const covered = new Set([
      ...ROW_ONLY,
      ...TITLE_ONLY,
      ...GROUPS.flatMap((g) => g.commands),
      "ade.menu",
    ]);
    const orphans = c.commands
      .map((x) => x.command)
      .filter((id) => !covered.has(id));
    expect(orphans, "put these in GROUPS or ROW_ONLY in menu.ts").toEqual([]);
  });

  it("names none that is also in a group", () => {
    const grouped = new Set(GROUPS.flatMap((g) => g.commands));
    expect(ROW_ONLY.filter((id) => grouped.has(id))).toEqual([]);
    expect(TITLE_ONLY.filter((id) => grouped.has(id))).toEqual([]);
  });
});

describe("TITLE_ONLY", () => {
  it("names only commands that exist", () => {
    const real = new Set(c.commands.map((x) => x.command));
    for (const id of TITLE_ONLY) expect(real, id).toContain(id);
  });

  // Commit and Commit… are one keystroke apart and do different things.
  it("keeps the bare Commit out of the sub-palette", () => {
    const offered = listed(c, "ade.menu").map((x) => x.command);
    expect(offered).not.toContain("ade.commit");
    expect(offered).toContain("ade.commitPanel");
  });

});

describe("listed", () => {
  it("leaves out the row commands and the entry that opened it", () => {
    const names = listed(c, "ade.menu").map((x) => x.command);
    expect(names).not.toContain("ade.reply");
    expect(names).not.toContain("ade.menu");
    expect(names).toContain("ade.switchSession");
  });
});

describe("entries", () => {
  const rows = entries(c, "ade.menu");

  it("offers every palette-visible command exactly once", () => {
    const offered = rows.filter((r) => r.command).map((r) => r.command);
    const want = listed(c, "ade.menu").map((x) => x.command);
    expect([...offered].sort()).toEqual([...want].sort());
    expect(new Set(offered).size).toBe(offered.length);
  });

  // A command nobody filed into a group still has to be reachable, or adding
  // one and forgetting the table makes it invisible.
  it("puts anything the groups forgot under Other rather than dropping it", () => {
    const made = entries(
      { commands: [{ command: "ade.brandNew", title: "ade: Brand new" }] },
      "ade.menu",
    );
    expect(made.map((r) => r.label)).toContain("Other");
    expect(made.find((r) => r.command === "ade.brandNew")).toBeTruthy();
  });

  it("does not print a heading for a group with nothing in it", () => {
    const made = entries({ commands: [] }, "ade.menu");
    expect(made).toEqual([]);
  });

  it("puts a heading before each group's rows", () => {
    const first = rows.findIndex((r) => r.separator);
    expect(first).toBe(0);
    expect(rows[1]?.command).toBeTruthy();
  });
});

describe("the group table", () => {
  it("names no command that does not exist", () => {
    const real = new Set(c.commands.map((x) => x.command));
    for (const group of GROUPS) {
      for (const id of group.commands)
        expect(real, `${id} in ${group.title}`).toContain(id);
    }
  });

  it("names none twice", () => {
    const all = GROUPS.flatMap((g) => g.commands);
    expect(new Set(all).size).toBe(all.length);
  });

  // The fallback covers it, but a command in no group reads as an oversight.
  it("covers every command the sub-palette will offer", () => {
    const grouped = new Set(GROUPS.flatMap((g) => g.commands));
    const missing = listed(c, "ade.menu")
      .map((x) => x.command)
      .filter((id) => !grouped.has(id));
    expect(missing, "add these to GROUPS in menu.ts").toEqual([]);
  });
});

// Seven commands were called "Refresh". In a view's title bar the icon says
// which one; in a list of labels nothing does, and the palette showed seven
// identical rows.
describe("every row says which command it is", () => {
  it("offers no two rows under the same label", () => {
    const seen = new Map<string, string[]>();
    for (const row of entries(c, "ade.menu")) {
      if (!row.command) continue;
      seen.set(row.label, [...(seen.get(row.label) ?? []), row.command]);
    }
    const same = [...seen].filter(([, ids]) => ids.length > 1);
    expect(same, "give each of these its own title in package.json").toEqual(
      [],
    );
  });

  it("names what a refresh refreshes", () => {
    for (const cmd of c.commands.filter((x) =>
      x.command.startsWith("ade.refresh"),
    )) {
      expect(
        cmd.title.split(" ").length,
        `${cmd.command} is just "${cmd.title}"`,
      ).toBeGreaterThan(1);
    }
  });
});

describe("where resolving lives", () => {
  // A thread is resolved, not a comment. Putting the pair on comments/comment
  // repeated one button per comment in a thread, all doing the same thing.
  const menus = (
    pkg.contributes as { menus: Record<string, { command: string }[]> }
  ).menus;

  it("is on the thread and never on a comment", () => {
    for (const [id, rows] of Object.entries(menus)) {
      if (!id.startsWith("comments/comment/")) continue;
      for (const row of rows) {
        expect(row.command, `${id} acts on a comment`).not.toMatch(
          /resolveThread|unresolveThread/,
        );
      }
    }
  });

  it("is offered on the thread's title bar", () => {
    const names = (menus["comments/commentThread/title"] ?? []).map(
      (row) => row.command,
    );
    expect(names).toContain("ade.markDone");
    expect(names).toContain("ade.reopenThread");
  });

  // The pane sets the same context keys and passes a node threadOf unwraps.
  it("is offered in the Comments pane exactly as on the thread", () => {
    expect(menus["commentsView/commentThread/context"]).toEqual(
      menus["comments/commentThread/title"],
    );
  });
});

describe("the contextValue the menus match on", () => {
  const menus = (
    pkg.contributes as { menus: Record<string, { when?: string }[]> }
  ).menus;

  // Every value a drawn thread can carry, from the states threadState knows.
  const drawn = new Set<string>();
  for (const over of [
    {},
    { resolved: true },
    { orphaned: true },
    { scope: "repo" },
    { outgoing: "please fix" },
    { externalId: "r1" },
    { externalId: "r1", comments: [] },
    {
      comments: [
        { id: "1", author: "a", body: "x", at: "" },
        { id: "2", author: "b", body: "y", at: "" },
        { id: "3", author: "c", body: "z", at: "" },
        { id: "4", author: "d", body: "w", at: "" },
      ],
    },
    { sentAt: "2026-10-05T10:00:00Z" },
  ]) {
    const t = {
      id: "1",
      file: "a.go",
      side: "right",
      line: 4,
      comments: [{ id: "c", author: "you", body: "x", at: "" }],
      ...over,
    } as Thread;
    for (const hasPR of [true, false])
      for (const open of [true, false]) drawn.add(threadState(t, hasPR, open));
  }

  function fits(when: string, value: string): boolean {
    const re = /commentThread =~ \/(.+?)\//.exec(when);
    if (re) return new RegExp(re[1]).test(value);
    const eq = /commentThread == ([\w-]+)/.exec(when);
    return eq ? eq[1] === value : false;
  }

  // A when-clause naming a value threads.ts never sets is a button that never
  // appears, which is indistinguishable from not having contributed it.
  it("is only what a drawn thread actually sets", () => {
    for (const [id, rows] of Object.entries(menus)) {
      if (!id.startsWith("comments/")) continue;
      for (const row of rows) {
        const when = row.when ?? "";
        if (!when.includes("commentThread")) continue;
        expect(
          [...drawn].some((value) => fits(when, value)),
          `${id}: nothing a thread carries matches ${when}`,
        ).toBe(true);
      }
    }
  });

  // Two entries in one slot is one icon: they are mutually exclusive only
  // when no thread can satisfy both when-clauses at once.
  it("gives each slot to one verb at a time", () => {
    for (const [id, rows] of Object.entries(menus)) {
      if (!id.startsWith("comments/")) continue;
      const bySlot = new Map<string, string[]>();
      for (const row of rows) {
        const group = (row as { group?: string }).group ?? "";
        if (!group.startsWith("inline")) continue;
        bySlot.set(group, [...(bySlot.get(group) ?? []), row.when ?? ""]);
      }
      for (const [slot, whens] of bySlot) {
        for (const value of drawn) {
          const fit = whens.filter((when) => fits(when, value));
          expect(
            fit.length,
            `${id} ${slot}: ${value} matches ${fit.length}`,
          ).toBeLessThan(2);
        }
      }
    }
  });

  it("is set by stateOf for both states", () => {
    expect(stateOf(false)).toBe(OPEN);
    expect(stateOf(true)).toBe(RESOLVED);
  });

  // The verbs act on the thread, so they have to sit where the thread is the
  // argument: a comment menu hands back the comment, which cannot be looked up.
  it("keeps the github verbs on the thread's own menu", () => {
    const onComment = (menus["comments/comment/title"] ?? []).length;
    expect(onComment).toBe(0);
    const titles = (menus["comments/commentThread/title"] ?? []).map(
      (row) => (row as { command?: string }).command,
    );
    expect(titles).toContain("ade.convertForGitHub");
    expect(titles).toContain("ade.sendToGitHub");
  });
});

describe("the menus this extension contributes to", () => {
  const ids = Object.keys(
    (pkg.contributes as { menus: Record<string, unknown> }).menus,
  );
  const declared = pkg.enabledApiProposals ?? [];

  // A gated menu renders nothing unless the manifest declares its proposal
  // and the editor grants it: product.json in the ade editor, argv.json in
  // VS Code. The entry is in the manifest and the button is nowhere otherwise.
  it("declares the proposal behind every gated menu it uses", () => {
    const missing = ids
      .map((id) => PROPOSED_MENUS[id] ?? ADE_EDITOR_MENUS[id])
      .filter((p): p is string => Boolean(p) && !declared.includes(p));
    expect(missing, "gated menus need their proposal declared").toEqual([]);
  });

  it("declares no proposal that nothing uses", () => {
    const used = new Set([
      ...ids.map((id) => PROPOSED_MENUS[id] ?? ADE_EDITOR_MENUS[id]),
      ...CODE_PROPOSALS,
    ]);
    expect(declared.filter((name) => !used.has(name))).toEqual([]);
  });

  it("has the typings for every proposal the code calls", () => {
    for (const name of CODE_PROPOSALS) {
      const dts = join(__dirname, `vscode.proposed.${name}.d.ts`);
      expect(existsSync(dts), dts).toBe(true);
    }
  });

  // A typo'd id is an error nowhere: VS Code reads the menus it knows and
  // ignores the rest, so the entry simply never appears.
  it("is never one VS Code has no menu for at all", () => {
    const bad = ids.filter(
      (id) => !KNOWN_MENUS.includes(id) && !(id in ADE_EDITOR_MENUS),
    );
    expect(bad, "nothing reads these menu ids").toEqual([]);
  });
});

// A title bar has no row under it, so the command runs with no argument. One
// that needs a thread does nothing at all there, and nothing says so.
describe("the title bars", () => {
  const menus = c.menus as unknown as Record<
    string,
    { command: string }[] | undefined
  >;
  const titled = ["view/title", "scm/title"].flatMap((id) => menus[id] ?? []);

  it("offer no command that needs a row to act on", () => {
    const bad = titled
      .map((i) => i.command)
      .filter((id) => ROW_ONLY.includes(id));
    expect(bad, "these take an argument a title bar cannot give").toEqual([]);
  });
});

// ade's verbs sit on git's header for the session's root and the draft lands in
// its input box. ade's own provider stands in only where Git cannot see the root.
describe("ade on the git source control header", () => {
  const menus = c.menus as unknown as Record<
    string,
    { command: string; group?: string; when?: string }[] | undefined
  >;

  const onGit = (i: { when?: string }) =>
    (i.when ?? "").includes("scmProvider == git");

  it("puts commit, amend and push in the overflow, not beside git's own", () => {
    const title = (menus["scm/title"] ?? []).filter(onGit);
    for (const cmd of ["ade.commit", "ade.commitAmend", "ade.push"]) {
      const entry = title.find((i) => i.command === cmd);
      expect(entry, `scm/title has no ${cmd}`).toBeDefined();
      expect(entry?.group).not.toBe("navigation");
    }
  });

  it("puts the message button inside git's input box", () => {
    const box = (menus["scm/inputBox"] ?? []).map((i) => i.command);
    expect(box).toContain("ade.writeCommitMessage");
  });

  it("scopes every entry to ade's provider or to Git on ade's root", () => {
    const loose = [
      ...(menus["scm/title"] ?? []),
      ...(menus["scm/inputBox"] ?? []),
    ].filter(
      (i) =>
        !(i.when ?? "").includes("scmProvider == ade") &&
        !(i.when ?? "").includes("scmProviderRootUri in ade.gitRoots"),
    );
    expect(loose, "an unscoped entry lands on every repository").toEqual([]);
  });
});

// The concierge is an action, not one of the things waiting on you: it sits
// below a separator so the rows above it are exactly what the title counts.
it("keeps the concierge out of the bell's counted rows", () => {
  const src = readFileSync(join(__dirname, "extension.ts"), "utf8");
  const well = src.slice(src.indexOf("async function openWell"), src.indexOf("let wellShown"));
  expect(well.indexOf("QuickPickItemKind.Separator")).toBeLessThan(well.indexOf("Open the concierge"));
  expect(well.indexOf("rows.map")).toBeLessThan(well.indexOf("Open the concierge"));
});

// The chord is the only way to dictate without holding the mouse down.
it("gives dictation a command, a group and a chord", () => {
  expect(c.commands.map((x) => x.command)).toContain("ade.dictate");
  expect(GROUPS.find((g) => g.title === "Conversation")?.commands).toContain("ade.dictate");
  const keys = (c as unknown as { keybindings: { command: string; mac?: string }[] }).keybindings;
  expect(keys.find((k) => k.command === "ade.dictate")?.mac).toBe("cmd+alt+v");
});

// A link in the chat is handed to openExternal, which the editor guards with
// its trusted-domains prompt. The fork trusts where ade's own links point.
describe("the domains a chat link opens without asking", () => {
  const product = JSON.parse(
    readFileSync(join(__dirname, "..", "..", "editor", "product.json"), "utf8"),
  ) as { linkProtectionTrustedDomains?: string[] };
  const trusted = product.linkProtectionTrustedDomains ?? [];

  it("trusts github, which is where a PR or issue row goes", () => {
    expect(trusted).toContain("https://github.com");
  });

  // The agent writes the chat, so trusting every domain would hand it the
  // browser. The prompt is the protection for anywhere it invents.
  it("trusts no wildcard that would cover the whole web", () => {
    for (const domain of trusted) {
      expect(domain).toMatch(/^https:\/\//);
      expect(domain).not.toBe("*");
      expect(domain).not.toMatch(/^https:\/\/\*$/);
      expect(domain).not.toMatch(/^https:\/\/\*\.[a-z]+$/);
    }
  });
});
