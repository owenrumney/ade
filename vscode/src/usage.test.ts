import { describe, expect, it } from "vitest";
import { badge, span, type Usage } from "./usage";

const now = Date.parse("2026-10-01T12:00:00Z");
const claude: Usage = {
  claude: {
    provider: "claude",
    windows: [
      { name: "5h", percent: 12.4, resetsAt: "2026-10-01T14:30:00Z" },
      { name: "7d", percent: 41, resetsAt: "2026-10-04T15:00:00Z" },
    ],
  },
};

describe("the plan usage on the status bar", () => {
  it("is every window of the plan, rounded", () => {
    expect(badge(claude, now)?.text).toBe("$(dashboard) 5h 12% · 7d 41%");
  });

  it("says when each window resets", () => {
    const tip = badge(claude, now)?.tooltip ?? "";
    expect(tip).toContain("claude 5h: 12%, resets in 2h 30m");
    expect(tip).toContain("claude 7d: 41%, resets in 3d 3h");
  });

  // Nothing read is nothing to say, not "you have used none of it".
  it("is not there without a reading", () => {
    expect(badge(undefined, now)).toBeUndefined();
    expect(badge({ error: "no login" }, now)).toBeUndefined();
    expect(
      badge({ claude: { provider: "claude", windows: null } }, now),
    ).toBeUndefined();
  });

  it("falls back to codex when that is the only reading", () => {
    const got = badge(
      { codex: { provider: "codex", windows: [{ name: "5h", percent: 9 }] } },
      now,
    );
    expect(got?.text).toBe("$(dashboard) 5h 9%");
  });

  it("puts both providers in the tooltip", () => {
    const got = badge(
      {
        ...claude,
        codex: { provider: "codex", windows: [{ name: "5h", percent: 9 }] },
      },
      now,
    );
    expect(got?.tooltip).toContain("codex 5h: 9%");
  });

  it("warns once a window is nearly spent", () => {
    expect(badge(claude, now)?.warn).toBe(false);
    const near: Usage = {
      claude: { provider: "claude", windows: [{ name: "5h", percent: 80 }] },
    };
    expect(badge(near, now)?.warn).toBe(true);
  });

  it("says so when the reading is hours old", () => {
    const old: Usage = {
      codex: {
        provider: "codex",
        checkedAt: "2026-09-28T12:00:00Z",
        windows: [{ name: "5h", percent: 9 }],
      },
    };
    const got = badge(old, now);
    expect(got?.text).toBe("$(dashboard) ~5h 9%");
    expect(got?.tooltip).toContain("codex: last read 3d 0h ago");
  });

  it("takes a fresh reading as current", () => {
    const fresh: Usage = {
      claude: {
        provider: "claude",
        checkedAt: "2026-10-01T11:50:00Z",
        windows: [{ name: "5h", percent: 12 }],
      },
    };
    const got = badge(fresh, now);
    expect(got?.text).toBe("$(dashboard) 5h 12%");
    expect(got?.tooltip).not.toContain("last read");
  });

  // Stale says the numbers are old; this says why they stopped.
  it("says why the last read failed, beside the numbers it left standing", () => {
    const got = badge({ ...claude, error: "usage answered HTTP 401" }, now);
    expect(got?.tooltip).toContain(
      "The last read failed: usage answered HTTP 401",
    );
    expect(got?.text).toContain("5h 12%");
  });

  it("says nothing about an error there was not", () => {
    expect(badge(claude, now)?.tooltip).not.toContain("failed");
  });

  it("leaves out a reset the server did not give", () => {
    const zero: Usage = {
      claude: {
        provider: "claude",
        windows: [{ name: "5h", percent: 5, resetsAt: "0001-01-01T00:00:00Z" }],
      },
    };
    expect(badge(zero, now)?.tooltip).toContain("claude 5h: 5%\n");
  });
});

describe("span", () => {
  it("is minutes, then hours, then days", () => {
    expect(span(5 * 60000)).toBe("5m");
    expect(span(90 * 60000)).toBe("1h 30m");
    expect(span(50 * 3600000)).toBe("2d 2h");
  });
});
