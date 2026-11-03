import { describe, expect, it } from "vitest";
import {
  at,
  clamp,
  counter,
  header,
  lit,
  mermaidOf,
  moved,
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

describe("mermaidOf", () => {
  it("numbers the nodes rather than trusting the ids it was given", () => {
    expect(mermaidOf(flow)).toBe(
      ['flowchart LR', '  n0["Client config"]', '  n1["GitHub API"]', '  n0 -->|"one request"| n1'].join(
        "\n",
      ),
    );
  });

  it("draws an edge with no words on it", () => {
    const bare = { ...flow, edges: [{ from: "config", to: "api" }] };
    expect(mermaidOf(bare)).toContain("  n0 --> n1");
  });

  it("takes mermaid's own punctuation out of a label", () => {
    const risky = {
      nodes: [
        { id: "a", label: 'the "quoted" one | and more' },
        { id: "b", label: "B" },
      ],
      edges: [],
    };
    expect(mermaidOf(risky)).toContain('n0["the quoted one and more"]');
  });

  it("is nothing when there is no flow worth drawing", () => {
    expect(mermaidOf(undefined)).toBe("");
    expect(mermaidOf({ nodes: [{ id: "a", label: "A" }], edges: [] })).toBe("");
  });
});
