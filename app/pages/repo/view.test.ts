import { describe, expect, it } from "vitest";
import {
  against,
  ago,
  count,
  heat,
  kinds,
  months,
  percent,
  spark,
  topLanguages,
  weeks,
  type Day,
  type Repo,
} from "./view";

function days(from: string, counts: number[]): Day[] {
  const start = new Date(`${from}T00:00:00Z`);
  return counts.map((commits, i) => {
    const at = new Date(start.getTime() + i * 86400000);
    return { date: at.toISOString().slice(0, 10), commits };
  });
}

const base: Repo = {
  repo: "owenrumney/ade",
  dir: "/src/ade",
  branch: "main",
  ahead: 0,
  behind: 0,
  dirty: 0,
  languages: [],
  lines: 0,
  files: 0,
  days: [],
  commits: 0,
  contributors: [],
  agent: {
    cost: 0,
    turns: 0,
    sessions: 0,
    tokens: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, reasoning: 0 },
    byDay: [],
    tools: [],
    models: [],
  },
};

describe("languages", () => {
  it("folds the tail into Other so the bar keeps its shape", () => {
    const many = Array.from({ length: 9 }, (_, i) => ({
      name: `L${i}`,
      lines: 100 - i * 10,
      files: 1,
    }));
    const got = topLanguages(many, 3);
    expect(got.map((l) => l.name)).toEqual(["L0", "L1", "L2", "Other"]);
    expect(got[3].lines).toBe(70 + 60 + 50 + 40 + 30 + 20);
    expect(got[3].files).toBe(6);
  });

  it("leaves a short list alone", () => {
    const two = [
      { name: "Go", lines: 10, files: 1 },
      { name: "CSS", lines: 5, files: 1 },
    ];
    expect(topLanguages(two, 5)).toHaveLength(2);
  });

  it("never rounds a language with lines down to nothing", () => {
    expect(percent(1, 10000)).toBe("<1%");
    expect(percent(0, 10000)).toBe("0%");
    expect(percent(760, 1000)).toBe("76%");
    expect(percent(5, 0)).toBe("0%");
  });
});

describe("the commit heatmap", () => {
  it("starts a column on the weekday the first day falls on", () => {
    // 2026-10-14 is a Wednesday, so three blanks come before it.
    const cols = weeks(days("2026-10-14", [1, 0, 0, 0]));
    expect(cols[0].slice(0, 3)).toEqual([null, null, null]);
    expect(cols[0][3]?.date).toBe("2026-10-14");
  });

  it("pads the last column so every week has seven cells", () => {
    const cols = weeks(days("2026-10-11", [1, 1, 1]));
    expect(cols).toHaveLength(1);
    expect(cols[0]).toHaveLength(7);
    expect(cols[0][3]).toBeNull();
  });

  it("is empty with no days rather than one blank column", () => {
    expect(weeks([])).toEqual([]);
  });

  it("scales the shade against the busiest day", () => {
    expect(heat(0, 10)).toBe(0);
    expect(heat(10, 10)).toBe(4);
    expect(heat(1, 10)).toBe(1);
    expect(heat(6, 10)).toBe(3);
  });

  it("gives a one-commit repository its full shade", () => {
    expect(heat(1, 1)).toBe(4);
  });

  it("names each month once, at the week it starts in", () => {
    const named = months(weeks(days("2026-09-27", new Array(40).fill(0))));
    expect(named.filter(Boolean)).toEqual(["Sep", "Oct", "Nov"]);
  });
});

describe("the branch line", () => {
  it("says so when there is no upstream", () => {
    expect(against(base)).toBe("main · no upstream");
  });

  it("says matching when it matches", () => {
    expect(against({ ...base, upstream: "origin/main" })).toBe(
      "main matches origin/main",
    );
  });

  it("counts both ways", () => {
    expect(against({ ...base, upstream: "origin/main", ahead: 2, behind: 3 })).toBe(
      "main 2 ahead, 3 behind of origin/main",
    );
    expect(against({ ...base, upstream: "origin/main", ahead: 1 })).toBe(
      "main 1 ahead of origin/main",
    );
  });
});

describe("the agent numbers", () => {
  it("drops a token kind that is not there, biggest first", () => {
    const agent = {
      ...base.agent,
      tokens: { input: 10, output: 5, cacheRead: 100, cacheWrite: 0, reasoning: 0 },
    };
    expect(kinds(agent).map((k) => k.name)).toEqual(["cache read", "input", "output"]);
  });

  it("scales the sparkline against the busiest day", () => {
    const got = spark([
      { key: "a", cost: 0, tokens: 0, sessions: 0 },
      { key: "b", cost: 5, tokens: 0, sessions: 0 },
      { key: "c", cost: 10, tokens: 0, sessions: 0 },
    ]);
    expect(got.map((d) => d.height)).toEqual([0, 0.5, 1]);
  });

  it("has no height at all on a quiet month, rather than dividing by zero", () => {
    const got = spark([{ key: "a", cost: 0, tokens: 0, sessions: 0 }]);
    expect(got[0].height).toBe(0);
  });

  it("abbreviates counts the way the dashboard does", () => {
    expect(count(950)).toBe("950");
    expect(count(4_680_000_000)).toBe("4.7 B");
    expect(count(92_200_000)).toBe("92.2 M");
  });
});

describe("ago", () => {
  const now = new Date("2026-10-14T12:00:00Z");

  it("reads in the units a person would say", () => {
    expect(ago("2026-10-14T11:30:00Z", now)).toBe("30m ago");
    expect(ago("2026-10-14T07:00:00Z", now)).toBe("5h ago");
    expect(ago("2026-10-04T12:00:00Z", now)).toBe("10d ago");
    expect(ago("2026-04-14T12:00:00Z", now)).toBe("6mo ago");
  });

  it("says nothing for a date it cannot read", () => {
    expect(ago("not a date", now)).toBe("");
  });
});
