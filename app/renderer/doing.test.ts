import { WHICH } from "./panels";
import { describe, expect, it } from "vitest";
import { live, owed, WIRED, wired } from "./doing";
import { UNLISTED, VOCABULARY } from "./verbs";
import { BAR } from "./model";

// Every verb the omnibox offers. The window's own list: ade is not an
// editor extension and its vocabulary is not an extension manifest.
const offered = VOCABULARY.flatMap((g) => g.verbs.map(([command]) => command));

describe("the table answers for all of ade", () => {
  // The omnibox lists ade's whole vocabulary, so a command with no
  // answer here is one that silently does nothing when picked.
  it("has an entry for every command the box offers", () => {
    const missing = offered.filter((c) => !wired(c));
    expect(missing, `add these to WIRED in doing.ts: ${missing.join(", ")}`).toEqual([]);
  });

  it("names the missing part for one it cannot do yet", () => {
    for (const command of offered) {
      if (live(command)) continue;
      expect(owed(command), command).toMatch(/needs .+/);
    }
  });

  it("says nothing is owed for one that works", () => {
    expect(owed("ade.push")).toBe("");
    expect(live("ade.push")).toBe(true);
  });
});

// Every ask a verb makes, with the command that makes it.
// The other way round: a verb wired here but not in the vocabulary is one
// the omnibox never lists, so ⌘K cannot reach it. Spaces shipped that way
// once and the only symptom was a verb nobody could find.
it("names every verb it wires, so the omnibox can list it", () => {
  const listed = new Set([...offered, ...UNLISTED]);
  const unreachable = Object.keys(WIRED).filter((id) => id.startsWith("ade.") && !listed.has(id));
  expect(unreachable, `add these to verbs.ts: ${unreachable.join(", ")}`).toEqual([]);
});

it("names each verb once, with something to call it", () => {
  const seen = new Set<string>();
  for (const { verbs } of VOCABULARY) {
    for (const [command, name] of verbs) {
      expect(seen.has(command), command).toBe(false);
      seen.add(command);
      expect(name, command).not.toMatch(/^ade:/);
      expect(name.trim(), command).not.toBe("");
    }
  }
});

const asks = Object.entries(WIRED).flatMap(([command, what]) =>
  what.at === "launcher" || what.at === "session"
    ? (what.needs ?? []).map((ask) => [command, what.path, ask] as const)
    : [],
);

describe("a verb that needs something first", () => {
  it("names where the answer goes", () => {
    for (const [command, , ask] of asks) {
      expect(ask.as, command).toMatch(/^[a-z]+$/);
      expect(ask.title, command).toMatch(/\?$|…$/);
    }
  });

  it("asks for each thing once", () => {
    for (const [command, what] of Object.entries(WIRED)) {
      if (what.at !== "launcher" && what.at !== "session") continue;
      const named = (what.needs ?? []).map((a) => a.as);
      expect([...new Set(named)], command).toEqual(named);
    }
  });

  it("offers a choice with more than one thing in it", () => {
    for (const [command, , ask] of asks) {
      if (ask.ask !== "one") continue;
      expect(ask.of.length, command).toBeGreaterThan(1);
    }
  });

  it("picks only from a source the window can fill", async () => {
    const { SOURCES } = await import("./sources");
    // repo, session and scope the window already holds from what it drew;
    // the rest are one read each. Anything else has nowhere to come from
    // and the picker would open empty.
    const held = ["repo", "session", "scope"];
    for (const [command, , ask] of asks) {
      if (ask.ask !== "pick" && ask.ask !== "many") continue;
      expect([...held, ...Object.keys(SOURCES)], command).toContain(ask.from);
    }
  });

  // The two that review everything, so the usual answer is one Enter.
  it("opens the reviewer pick with security and staff-reviewer ticked", () => {
    const ask = (WIRED["ade.reviewWithAgents"].needs ?? [])[0];
    expect(ask.ask).toBe("many");
    expect(ask.ask === "many" && ask.ticks).toEqual(["security", "staff-reviewer"]);
  });

  it("has an answer for every slot in its path", () => {
    for (const [command, what] of Object.entries(WIRED)) {
      if (what.at !== "launcher" && what.at !== "session") continue;
      const slots = [...what.path.matchAll(/\{([a-z]+)\}/g)].map((m) => m[1]);
      const named = (what.needs ?? []).map((a) => a.as);
      for (const slot of slots) expect(named, command).toContain(slot);
    }
  });
});

describe("a report", () => {
  it("has a renderer for the name it gives", async () => {
    const { REPORTS } = await import("./reports");
    for (const [command, what] of Object.entries(WIRED)) {
      if (what.at !== "report") continue;
      expect(Object.keys(REPORTS), command).toContain(what.render);
      expect(what.path, command).toMatch(/^\/api\//);
      if (what.also) expect(what.also, command).toMatch(/^\/api\//);
    }
  });
});

describe("a search", () => {
  it("reads a list and then one of it", () => {
    for (const [command, what] of Object.entries(WIRED)) {
      if (what.at !== "find") continue;
      expect(what.path, command).toMatch(/^\/api\//);
      expect(what.open, command).toMatch(/^\/api\/.*\/$/);
      expect(what.title, command).not.toBe("");
    }
  });
});

describe("what each kind of entry promises", () => {
  it("sends a session verb to a path the session server serves", () => {
    for (const [command, what] of Object.entries(WIRED)) {
      if (what.at !== "session") continue;
      expect(what.path, command).toMatch(/^\/api\//);
    }
  });

  it("sends a launcher verb to a path the launcher serves", () => {
    for (const [command, what] of Object.entries(WIRED)) {
      if (what.at !== "launcher") continue;
      expect(what.path, command).toMatch(/^\/api\//);
    }
  });

  it("points a window verb at a place that exists", () => {
    const places = BAR;
    for (const [command, what] of Object.entries(WIRED)) {
      if (what.at !== "window" || !what.place) continue;
      expect(places, command).toContain(what.place);
    }
  });
});

// /api/pr/approve and /api/pr/request-changes refuse a body that does not
// name the pull request, and the table cannot know its number. Wired as a
// path and a body, both answered 400 and the omnibox looked inert.
describe("a verdict on a pull request", () => {
  it("goes through the window, which knows the number", () => {
    for (const command of ["ade.approvePR", "ade.requestChanges", "ade.mergePR"]) {
      expect(wired(command), command).toMatchObject({ at: "verdict" });
    }
  });

  it("is never wired as a bare post to a verdict route", () => {
    const bare = Object.entries(WIRED).filter(
      ([, what]) =>
        (what.at === "session" || what.at === "launcher") &&
        /^\/api\/pr\/(approve|request-changes)$/.test(what.path),
    );
    expect(bare.map(([c]) => c)).toEqual([]);
  });
});

// Not a threshold to defend, a number to watch: it should go up.
describe("how much of ade the window has caught up with", () => {
  it("reports the split", () => {
    const working = offered.filter(live).length;
    expect(working).toBeGreaterThan(0);
    console.log(
      `  ${working} of ${offered.length} commands run; ` +
        `${offered.length - working} name what they are waiting for`,
    );
  });
});

describe("what is left", () => {
  it("has nothing owed, or says what each one waits for", () => {
    const owing = offered.filter((c) => !live(c)).map((c) => owed(c));
    expect(owing.filter((why) => !/ needs /.test(why))).toEqual([]);
  });
});

describe("a new conversation", () => {
  // Posted bare, /api/chat/new answers "say what the new session is for".
  it("is the window's verb, so it can ask what it is for", () => {
    expect(wired("ade.newChat")).toMatchObject({ at: "window", verb: "new-chat" });
  });
});

describe("what the status bar clicks", () => {
  // The bar shows these two as facts and offers them as verbs. Unwire
  // either and the click becomes a tooltip promising nothing.
  it("can do the verbs behind the model and the sandbox", () => {
    expect(wired("ade.pickModel")).toBeTruthy();
    expect(wired("ade.sandboxPanel")).toBeTruthy();
  });
});

describe("drift and machines, now that they have verbs", () => {
  it("fills the host name into the path rather than the body", () => {
    const what = wired("ade.removeMachine");
    expect(what).toMatchObject({ at: "launcher", path: "/api/hosts/{name}", how: "DELETE" });
    expect((what as { needs?: { as: string }[] }).needs?.[0].as).toBe("name");
  });

  it("dismisses and restores from the two halves of the list", () => {
    expect(wired("ade.driftDismiss")).toMatchObject({
      path: "/api/drift/dismiss",
      body: { dismissed: true },
    });
    expect(wired("ade.driftRestore")).toMatchObject({
      path: "/api/drift/dismiss",
      body: { dismissed: false },
    });
  });
});

// A verb that opens a panel names it, and panels.ts has to know how to
// build that name: when it does not, the tab draws "no panel called X"
// and the only clue is the name itself.
describe("every verb that opens a panel can open it", () => {
  it("names a panel panels.ts knows how to build", () => {
    const named = Object.entries(WIRED)
      .filter(([, what]) => what.at === "panel")
      .map(([command, what]) => [command, (what as { panel: string }).panel] as const);
    expect(named.length).toBeGreaterThan(5);
    expect(named.filter(([, panel]) => !WHICH[panel]).map(([c, p]) => `${c} -> ${p}`)).toEqual([]);
  });
});
