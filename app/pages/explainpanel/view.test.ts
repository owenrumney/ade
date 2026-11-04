import { describe, expect, it } from "vitest";
import {
  at,
  clamp,
  counter,
  header,
  lit,
  moved,
  sequence,
  where,
  type Flow,
  type Step,
  type Walkthrough,
} from "./view";

function step(over: Partial<Step> = {}): Step {
  return {
    ref: "S1",
    title: "The value is read once",
    file: "a.go",
    line: 12,
    endLine: 20,
    body: "Every caller loses its error path.",
    ...over,
  };
}

function walkthrough(over: Partial<Walkthrough> = {}): Walkthrough {
  return { steps: [step()], files: 3, base: "main", ...over };
}

const flow: Flow = {
  nodes: [
    { id: "config", label: "Client config" },
    { id: "api", label: "GitHub API" },
  ],
  edges: [{ from: "config", to: "api", label: "one request" }],
};

describe("header", () => {
  it("names the scope the walk was written for", () => {
    expect(header(walkthrough())).toBe("1 step through the branch against main, 3 files.");
  });

  it("counts the steps as well as the files", () => {
    expect(header(walkthrough({ steps: [step(), step()] }))).toContain("2 steps through");
  });

  it("says so when the walk is only of what is not committed", () => {
    expect(header(walkthrough({ scope: "uncommitted", files: 1 }))).toContain(
      "what is not committed, 1 file.",
    );
  });
});

describe("where", () => {
  it("is a range when the step covers one", () => {
    expect(where(step())).toBe("a.go:12-20");
  });

  it("is one line when it does not", () => {
    expect(where(step({ endLine: 12 }))).toBe("a.go:12");
  });
});

describe("counter", () => {
  it("pads so the number does not reflow as you walk", () => {
    expect(counter(0, 7)).toBe("01/07");
    expect(counter(6, 7)).toBe("07/07");
  });
});

describe("clamp", () => {
  it("keeps the place inside the steps", () => {
    expect(clamp(-1, 5)).toBe(0);
    expect(clamp(9, 5)).toBe(4);
    expect(clamp(2, 0)).toBe(0);
  });
});

describe("at", () => {
  it("is the step on screen", () => {
    expect(at(walkthrough(), 0)?.ref).toBe("S1");
  });

  it("is nothing when there is no walkthrough", () => {
    expect(at(undefined, 0)).toBeUndefined();
  });
});

describe("moved", () => {
  it("is true when the steps were written again", () => {
    expect(moved(walkthrough(), walkthrough({ steps: [step({ ref: "S2" })] }))).toBe(true);
  });

  it("is false for the same steps arriving twice", () => {
    expect(moved(walkthrough(), walkthrough())).toBe(false);
  });
});

describe("lit", () => {
  it("is the node the step is at", () => {
    expect(lit(flow, step({ node: "api" }))).toBe(1);
  });

  it("is nothing for a step that names no node", () => {
    expect(lit(flow, step())).toBe(-1);
  });

  it("is nothing when there is no flow", () => {
    expect(lit(undefined, step({ node: "api" }))).toBe(-1);
  });
});

describe("sequence", () => {
  const chain: Flow = {
    nodes: [
      { id: "a", label: "A", file: "a.go", line: 4 },
      { id: "b", label: "B" },
      { id: "c", label: "C" },
    ],
    edges: [
      { from: "a", to: "b", label: "one request" },
      { from: "b", to: "c" },
    ],
  };

  it("is a lane a node, in the order the flow declares them", () => {
    expect(sequence(flow, step()).lanes.map((l) => l.label)).toEqual([
      "Client config",
      "GitHub API",
    ]);
  });

  it("carries the node's file so the lane head can open it", () => {
    const { lanes } = sequence(chain, step());
    expect(lanes[0]).toMatchObject({ file: "a.go", line: 4 });
    expect(lanes[1].file).toBeUndefined();
  });

  it("is a row an edge, pointing between the lanes it joins", () => {
    expect(sequence(chain, step()).wires).toEqual([
      { label: "one request", from: 0, to: 1, lit: false },
      { label: "", from: 1, to: 2, lit: false },
    ]);
  });

  it("keeps an edge that skips ahead, and one that goes back", () => {
    const far = { ...chain, edges: [{ from: "a", to: "c" }, { from: "c", to: "a" }] };
    expect(sequence(far, step()).wires).toEqual([
      { label: "", from: 0, to: 2, lit: false },
      { label: "", from: 2, to: 0, lit: false },
    ]);
  });

  it("keeps an edge from a lane to itself, as work at that lane", () => {
    const self = { ...chain, edges: [{ from: "b", to: "b", label: "render" }] };
    expect(sequence(self, step()).wires).toEqual([
      { label: "render", from: 1, to: 1, lit: false },
    ]);
  });

  it("lights the step's lane and everything leaving it", () => {
    const { lanes, wires } = sequence(chain, step({ node: "a" }));
    expect(lanes.map((l) => l.lit)).toEqual([true, false, false]);
    expect(wires.map((w) => w.lit)).toEqual([true, false]);
  });

  it("takes the first of a repeated id, and drops an edge to a node nobody declared", () => {
    const twice: Flow = {
      nodes: [
        { id: "a", label: "A" },
        { id: "b", label: "B" },
        { id: "a", label: "A again" },
      ],
      edges: [
        { from: "a", to: "b", label: "once" },
        { from: "b", to: "nope" },
      ],
    };
    const { lanes, wires } = sequence(twice, step());
    expect(lanes.map((l) => l.label)).toEqual(["A", "B", "A again"]);
    expect(wires).toEqual([{ label: "once", from: 0, to: 1, lit: false }]);
  });

  it("is nothing when there is no flow worth drawing", () => {
    expect(sequence(undefined, step())).toEqual({ lanes: [], wires: [] });
    expect(sequence({ nodes: [{ id: "a", label: "A" }], edges: [] }, step())).toEqual({
      lanes: [],
      wires: [],
    });
  });
});
