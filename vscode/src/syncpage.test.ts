import { describe, expect, it } from "vitest";
import { lines, page, type SyncReport } from "./syncpage";

const report: SyncReport = {
  base: "origin/main",
  fetched: 3,
  results: [
    {
      path: "/r",
      name: "ade",
      branch: "main",
      main: true,
      outcome: "would fast-forward",
    },
    {
      path: "/r-7",
      name: "ade-pr-7",
      branch: "fix/x",
      outcome: "would rebase",
      detail: "2 commits",
    },
  ],
};

describe("a sync report", () => {
  it("is a line per checkout before it runs", () => {
    expect(lines(report)).toBe(
      "origin/main moved 3 commits.\n\nade: would fast-forward\nade-pr-7: would rebase (2 commits)",
    );
  });

  it("carries the note when there is one", () => {
    expect(lines({ ...report, fetched: 1, note: "Nothing to do." })).toContain(
      "origin/main moved 1 commit.\nNothing to do.",
    );
  });

  it("is a table once it has", () => {
    const text = page("ade", report);
    expect(text).toContain("| ade (main) | main | would fast-forward |");
    expect(text).toContain("| ade-pr-7 | fix/x | would rebase — 2 commits |");
  });

  it("survives a report with no checkouts", () => {
    expect(
      page("ade", { base: "origin/main", fetched: 0, results: null }),
    ).toContain("| Checkout |");
  });
});
