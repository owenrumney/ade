import { describe, expect, it } from "vitest";
import {
  children,
  flat,
  label,
  line,
  context,
  type Entry,
  page,
  parse,
  type ProjectView,
  roots,
} from "./projectrow";

const entry = (id: string, more: Partial<Entry> = {}): Entry => ({
  id,
  heading: `Heading ${id}`,
  body: `Body ${id}`,
  ...more,
});

const view = (more: Partial<ProjectView> = {}): ProjectView => ({
  name: "ade",
  remembered: true,
  instructions: "",
  maxLength: 4000,
  entries: [],
  proposed: [],
  unread: 0,
  ...more,
});

describe("roots", () => {
  it("is empty before anything has been read", () => {
    expect(roots(undefined)).toEqual([]);
  });

  it("offers only the instructions and the switch when memory is off", () => {
    const rows = roots(view({ remembered: false, proposed: [entry("p")] }));
    expect(rows.map((row) => row.kind)).toEqual(["instructions", "off"]);
  });

  it("puts what is waiting on you above what is settled", () => {
    const rows = roots(
      view({ proposed: [entry("p")], entries: [entry("e")], unread: 3 }),
    );
    expect(rows.map((row) => ("title" in row ? row.title : row.kind))).toEqual([
      "instructions",
      "distil",
      "Proposed",
      "Remembered",
    ]);
  });

  it("leaves out an empty group and a superseded entry", () => {
    const rows = roots(
      view({
        entries: [entry("old", { state: "superseded" })],
        proposed: null,
      }),
    );
    expect(rows.map((row) => row.kind)).toEqual(["instructions"]);
  });
});

describe("children and context", () => {
  it("gives a proposed entry and a kept one different menus", () => {
    const [, proposed, kept] = roots(
      view({ proposed: [entry("p")], entries: [entry("e")] }),
    );
    expect(children(proposed).map(context)).toEqual(["ade.memory.proposed"]);
    expect(children(kept).map(context)).toEqual(["ade.memory.kept"]);
    expect(context(proposed)).toBe("ade.memory.group");
  });
});

describe("an entry as a page", () => {
  it("survives being opened and saved untouched", () => {
    const e = entry("e", { body: "First.\n\nSecond." });
    expect(parse(page(e))).toEqual({ heading: e.heading, body: e.body });
  });

  it("is refused without a heading line", () => {
    expect(parse("just words\n")).toBeUndefined();
    expect(parse("# \n\nbody")).toBeUndefined();
  });
});

describe("the memory pick", () => {
  it("is one flat list, with the groups gone", () => {
    const rows = flat(
      view({ proposed: [entry("p")], entries: [entry("e")], unread: 2 }),
    );
    expect(rows.map((r) => r.kind)).toEqual([
      "instructions",
      "distil",
      "entry",
      "entry",
    ]);
  });

  it("says what each row is", () => {
    const rows = flat(view({ proposed: [entry("p")], entries: [entry("e")] }));
    expect(rows.map(label)).toEqual(["Instructions", "Heading p", "Heading e"]);
    expect(rows.map(line)).toEqual(["none yet", "proposed", "remembered"]);
  });

  it("offers to start remembering when it is off", () => {
    const rows = flat(view({ remembered: false }));
    expect(rows.map(label)).toContain("Start remembering this project");
  });
});
