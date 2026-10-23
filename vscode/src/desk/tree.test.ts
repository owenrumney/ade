import { describe, expect, it } from "vitest";
import { nodesOf } from "./tree";
import type { Section } from "./sections";

const line = (label: string) => ({
  icon: "circle-outline",
  label,
  meta: "",
  do: { what: "row" },
});

describe("what a section draws at its root", () => {
  it("heads the groups when it has them", () => {
    const section: Section = {
      id: "prs",
      title: "Pull requests",
      empty: "",
      lines: [line("a"), line("b")],
      groups: [
        { name: "To review", lines: [line("a")] },
        { name: "Yours", lines: [line("b")] },
      ],
    };
    expect(nodesOf(section)).toEqual([
      { kind: "group", name: "To review", lines: [line("a")] },
      { kind: "group", name: "Yours", lines: [line("b")] },
    ]);
  });

  it("drops a heading with nothing under it", () => {
    const section: Section = {
      id: "prs",
      title: "Pull requests",
      empty: "",
      lines: [line("a")],
      groups: [
        { name: "To review", lines: [line("a")] },
        { name: "Yours", lines: [] },
      ],
    };
    expect(nodesOf(section).map((n) => n.kind === "group" && n.name)).toEqual([
      "To review",
    ]);
  });

  it("draws rows flat when there are no groups", () => {
    const section: Section = {
      id: "waiting",
      title: "Waiting on you",
      empty: "",
      lines: [line("a"), line("b")],
    };
    expect(nodesOf(section).map((n) => n.kind)).toEqual(["line", "line"]);
  });

  it("is empty for a section the board did not carry", () => {
    expect(nodesOf(undefined)).toEqual([]);
  });
});
