import { describe, expect, it } from "vitest";
import { REPORTS } from "./reports";
import type { ProjectView } from "../../vscode/src/projectrow";

const view: ProjectView = {
  name: "ade",
  remembered: true,
  instructions: "keep it short",
  maxLength: 2000,
  unread: 2,
  proposed: [{ id: "p1", heading: "The launcher owns the proxy", body: "One origin." }],
  entries: [
    { id: "e1", heading: "One server per checkout", body: "Sessions share it.", added: "2026-10-01T09:00:00Z" },
    { id: "e2", heading: "Gone", body: "Old.", state: "superseded" },
  ],
};

describe("what ade remembers, as a document", () => {
  const page = () => REPORTS.memory(view);

  it("names the repository and what is unread", () => {
    expect(page()).toContain("# What ade remembers about ade");
    expect(page()).toContain("2 transcripts have not been read yet.");
  });

  it("keeps the fork's own split between proposed and remembered", () => {
    const text = page();
    expect(text).toContain("## Proposed");
    expect(text).toContain("## Remembered");
    expect(text.indexOf("## Proposed")).toBeLessThan(text.indexOf("## Remembered"));
  });

  it("draws an entry's body, which the fork's pick cannot", () => {
    expect(page()).toContain("### One server per checkout · 2026-10-01");
    expect(page()).toContain("Sessions share it.");
  });

  it("leaves out one that was superseded, as roots() does", () => {
    expect(page()).not.toContain("Gone");
  });

  it("says so when the repository is not remembered at all", () => {
    const off = REPORTS.memory({ ...view, remembered: false });
    expect(off).toContain("Nothing is kept between sessions");
    expect(off).not.toContain("## Remembered");
  });
});
