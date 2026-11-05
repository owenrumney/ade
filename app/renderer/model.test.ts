import { describe, expect, it } from "vitest";
import { counted, grouped, matches, type Place, type Row } from "./model";

const row = (over: Partial<Row>): Row => ({ id: "r", name: "", icon: "session", ...over });

describe("a list's filter", () => {
  const session = row({
    name: "Lean terminal host",
    meta: "ade · codex",
    tags: [
      { text: "DEV-412", kind: "ticket" },
      { text: "brief", kind: "brief" },
      { text: "urgent", kind: "own" },
    ],
  });

  it("matches the name and what is said beside it, whatever the case", () => {
    expect(matches(session, "terminal")).toBe(true);
    expect(matches(session, "TERMINAL")).toBe(true);
    expect(matches(session, "codex")).toBe(true);
  });

  // Finding a session again is what the tags are for.
  it("matches a tag: the ticket, the brief, or one typed by hand", () => {
    expect(matches(session, "dev-412")).toBe(true);
    expect(matches(session, "brief")).toBe(true);
    expect(matches(session, "urgent")).toBe(true);
  });

  it("refuses what is in none of them", () => {
    expect(matches(session, "squealer")).toBe(false);
    expect(matches(row({ name: "plain" }), "brief")).toBe(false);
  });

  it("matches everything while nothing is typed", () => {
    expect(matches(row({ name: "plain" }), "")).toBe(true);
  });
});

describe("a list's headings", () => {
  it("keep the lines in the order they came, under the heading each names", () => {
    const rows = [
      row({ id: "a", group: "Offered to you" }),
      row({ id: "b", group: "Yours to move" }),
      row({ id: "c", group: "Offered to you" }),
    ];
    expect(grouped(rows).map((g) => [g.name, g.rows.map((r) => r.id)])).toEqual([
      ["Offered to you", ["a", "c"]],
      ["Yours to move", ["b"]],
    ]);
  });

  // The sessions are one flat list: no heading is one unnamed group.
  it("are a single unnamed group for lines that name none", () => {
    const flat = grouped([row({ id: "a" }), row({ id: "b" })]);
    expect(flat).toHaveLength(1);
    expect(flat[0].name).toBe("");
  });
});

describe("the number on a bar button", () => {
  const place = (over: Partial<Place>): Place => ({ id: "p", title: "P", icon: "bell", rows: [], ...over });

  it("is the lines that want something, when the list says no number of its own", () => {
    const rows = [row({ tone: "warn" }), row({ tone: "bad" }), row({ tone: "good" }), row({})];
    expect(counted(place({ rows }))).toBe(2);
  });

  it("is the list's own number when it gives one, even nought", () => {
    expect(counted(place({ rows: [row({ tone: "bad" })], count: 0 }))).toBe(0);
    expect(counted(place({ count: 5 }))).toBe(5);
  });
});
