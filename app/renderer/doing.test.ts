import { describe, expect, it } from "vitest";
import manifest from "../../vscode/package.json";
import { entries, type Contributes } from "../../vscode/src/menu";
import { live, owed, WIRED, wired } from "./doing";

// Every command the omnibox offers, as menu.ts decides them.
const offered = entries(manifest.contributes as unknown as Contributes, "ade.omnibox")
  .filter((e) => !e.separator && e.command)
  .map((e) => e.command!);

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
const asks = Object.entries(WIRED).flatMap(([command, what]) =>
  (what.at === "launcher" || what.at === "session" ? what.needs ?? [] : []).map(
    (ask) => [command, what.path, ask] as const,
  ),
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
    const places = [
      "waiting", "prs", "designs", "sessions",
      "todos", "jobs", "issues", "inbox", "team", "review", "code",
    ];
    for (const [command, what] of Object.entries(WIRED)) {
      if (what.at !== "window" || !what.place) continue;
      expect(places, command).toContain(what.place);
    }
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
