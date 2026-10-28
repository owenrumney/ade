import { describe, expect, it } from "vitest";
import type { BranchPR } from "./client";
import { page } from "./prpage";

const pr = (over: Partial<BranchPR> = {}): BranchPR => ({
  number: 41,
  title: "Delete the shell",
  url: "https://github.com/owenrumney/ade/pull/41",
  state: "OPEN",
  author: "owenrumney",
  baseRef: "main",
  headRef: "feat/no-shell",
  body: "The Electron shell goes.",
  ...over,
});

describe("the pull request page", () => {
  it("leads with what it is and where it is going", () => {
    const text = page(pr());
    expect(text).toContain("# #41 Delete the shell");
    expect(text).toContain("open · by owenrumney · feat/no-shell → main");
    expect(text).toContain("The Electron shell goes.");
  });

  it("says draft rather than open when it is one", () => {
    expect(page(pr({ draft: true }))).toContain("draft · by owenrumney");
  });

  it("says so when nothing has been written", () => {
    expect(page(pr({ body: "  " }))).toContain("_Nothing written yet._");
  });

  it("puts failing checks above passing ones", () => {
    const text = page(
      pr({
        checks: [
          { name: "lint", state: "pass", bucket: "pass" },
          { name: "test", state: "fail", bucket: "fail" },
          { name: "build", state: "pending", bucket: "pending" },
        ],
      }),
    );
    const rows = text
      .split("\n")
      .filter((l) => l.startsWith("| ") && !/Check \||^\| ---/.test(l));
    expect(rows.map((r) => r.split(" | ")[0])).toEqual([
      "| test",
      "| build",
      "| lint",
    ]);
  });

  it("says GitHub's verdicts in words", () => {
    const text = page(
      pr({
        reviews: [
          { author: "sam", state: "CHANGES_REQUESTED" },
          { author: "alex", state: "APPROVED" },
        ],
        reviewDecision: "CHANGES_REQUESTED",
      }),
    );
    expect(text).toContain("- **sam** asked for changes");
    expect(text).toContain("- **alex** approved");
    expect(text).toContain("Decision: asked for changes.");
  });

  it("says what is blocked and why", () => {
    const text = page(pr({ mergeBlocked: "a check is failing" }));
    expect(text).toContain("Merging is blocked: a check is failing.");
  });

  it("leaves out the sections it has nothing for", () => {
    const text = page(pr({ reviews: null, checks: null }));
    expect(text).not.toContain("## Reviews");
    expect(text).not.toContain("## Checks");
  });
});
