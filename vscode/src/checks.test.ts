import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { badge, summary } from "./checks";

describe("the checks badge", () => {
  const at = "2026-10-04T12:00:00Z";

  it("says nothing until there is something to say", () => {
    expect(badge(undefined)).toBeUndefined();
    expect(badge(null)).toBeUndefined();
    expect(badge({ ran: 0, total: 0 })).toBeUndefined();
  });

  it("spins while they run", () => {
    const shown = badge({ running: true, ran: 0, total: 3 });
    expect(shown?.text).toContain("spin");
    expect(shown?.warn).toBe(false);
  });

  it("counts a pass and warns on a failure", () => {
    expect(badge({ passed: true, ran: 3, total: 3, at })?.text).toBe(
      "$(check) 3",
    );

    const failed = badge({ failed: "vet", ran: 2, total: 3, at });
    expect(failed?.text).toBe("$(error) vet");
    expect(failed?.tooltip).toContain("2 of 3 ran");
    expect(failed?.warn).toBe(true);
  });

  // A narrow pass proves less than the list, and is never drawn as a tick.
  it("says narrow pass while the full list runs", () => {
    const shown = badge({ running: true, narrow: true, ran: 1, total: 1 });
    expect(shown?.text).toContain("spin");
    expect(shown?.tooltip).toContain("Narrow pass: 1 passed");
    expect(
      badge({ passed: true, narrow: true, ran: 1, total: 1 }),
    ).toBeUndefined();

    const failed = badge({ failed: "test", narrow: true, ran: 1, total: 1 });
    expect(failed?.tooltip).toContain("in the narrow pass");
  });

  // Stopping them yourself is not a failure, and must not light up red.
  it("does not warn when you stopped them", () => {
    const shown = badge({ stopped: true, ran: 1, total: 3 });
    expect(shown?.warn).toBe(false);
    expect(shown?.tooltip).toContain("stopped");
  });

  it("asks when there is a list to set up", () => {
    const shown = badge({
      ran: 0,
      total: 0,
      offer: [{ name: "build", run: "go build ./..." }],
    });
    expect(shown?.text).toContain("checks");
    expect(shown?.tooltip).toContain("Set them up?");
  });

  // A bad timestamp must not put "Invalid Date" on the bar.
  it("leaves out a time it cannot read", () => {
    expect(
      badge({ passed: true, ran: 1, total: 1, at: "nonsense" })?.tooltip,
    ).toBe("All 1 checks passed");
  });
});

describe("the one-line summary", () => {
  it("names the checks, or says there are none", () => {
    expect(summary([])).toBe("no checks configured");
    expect(summary([{ name: "build" }, { name: "vet" }])).toBe("build, vet");
  });
});

// GitHub went red, the server polls, and the tree keeps its first load for the
// life of the window. Asked "is anything failing", both have to be re-read.
describe("asking whether anything is failing", () => {
  const extension = readFileSync(join(__dirname, "extension.ts"), "utf8");
  const body = (() => {
    const fn = extension.slice(extension.indexOf("async function fixChecks"));
    return fn.slice(0, fn.indexOf("\n}\n"));
  })();

  it("asks the server to re-read the pull request first", () => {
    expect(body).toContain("client.refreshChecks()");
  });

  it("re-loads the tree rather than taking whatever it loaded once", () => {
    expect(body).toContain("sessions.refresh()");
    expect(
      body,
      "ready() keeps its promise, so it answers with the load that already happened",
    ).not.toContain("sessions.ready()");
  });

  it("re-reads before it decides, not after", () => {
    expect(body.indexOf("sessions.refresh()")).toBeLessThan(
      body.indexOf("sessions.failing"),
    );
  });
});
