import { describe, expect, it } from "vitest";
import { modelPick, pick, port, wikiPath } from "./wiki";

describe("wikiPath", () => {
  it("drops the leading slash a uri path carries", () => {
    expect(wikiPath({ path: "/general/local-network.md" })).toBe(
      "general/local-network.md",
    );
  });

  it("leaves a bare path alone", () => {
    expect(wikiPath({ path: "page.md" })).toBe("page.md");
  });
});

describe("pick", () => {
  const hit = {
    path: "repos/owenrumney/ade/local-network.md",
    line: 12,
    snippet: "  the grant is keyed to the exact string  ",
    scope: "repo",
  };

  it("leads with the file, because the path is too long to read first", () => {
    expect(pick(hit).label).toBe("local-network.md");
  });

  it("keeps the whole path where it can still be seen", () => {
    expect(pick(hit).description).toContain(
      "repos/owenrumney/ade/local-network.md",
    );
  });

  it("trims the snippet, which comes with the line's indentation", () => {
    expect(pick(hit).detail).toBe("the grant is keyed to the exact string");
  });
});

describe("modelPick", () => {
  it("prefers the label, falling back to the id", () => {
    expect(
      modelPick({ id: "claude-x", label: "Claude X", agent: "claude" }).label,
    ).toBe("Claude X");
    expect(
      modelPick({ id: "claude-x", label: "", agent: "claude" }).label,
    ).toBe("claude-x");
  });

  it("puts the price where two alike models can be told apart", () => {
    const got = modelPick({
      id: "a",
      label: "A",
      agent: "claude",
      in: "3",
      out: "15",
    });
    expect(got.description).toContain("$3/$15 per Mtok");
  });

  it("says nothing about price when the catalogue has none", () => {
    expect(modelPick({ id: "a", label: "A", agent: "codex" }).description).toBe(
      "codex",
    );
  });
});

describe("port", () => {
  it("takes a loopback port number", () => {
    expect(port("5173")).toBe(5173);
    expect(port(" 8080 ")).toBe(8080);
  });

  it("refuses anything that is not one", () => {
    for (const bad of ["", "0", "65536", "-1", "80.5", "http://x:80", "abc"]) {
      expect(port(bad), bad).toBeUndefined();
    }
  });
});
