import { describe, expect, it } from "vitest";
import { build, flatten, opened, squashed, toggled } from "./tree";

const PATHS = [
  { path: "internal/desk/tree.go" },
  { path: "internal/desk/sections.go" },
  { path: "internal/launcher/designsstore.go", status: "added" as const },
  { path: "README.md" },
];

describe("building a tree from paths", () => {
  it("nests each path under its directories", () => {
    const tree = build(PATHS);
    expect(tree.map((n) => n.name)).toEqual(["internal", "README.md"]);
    const inner = tree[0].children!;
    expect(inner.map((n) => n.name)).toEqual(["desk", "launcher"]);
    expect(inner[0].children!.map((n) => n.name)).toEqual(["sections.go", "tree.go"]);
  });

  it("puts directories above files and sorts each level", () => {
    const tree = build([{ path: "z.go" }, { path: "a/b.go" }, { path: "a.go" }]);
    expect(tree.map((n) => n.name)).toEqual(["a", "a.go", "z.go"]);
  });

  it("keeps what the diff said about a file", () => {
    const tree = build(PATHS);
    const file = tree[0].children![1].children![0];
    expect(file.name).toBe("designsstore.go");
    expect(file.status).toBe("added");
  });
});

describe("what is drawn", () => {
  it("shows the top and no deeper until a directory is opened", () => {
    const tree = build(PATHS);
    expect(flatten(tree, new Set()).map((s) => s.node.name)).toEqual([
      "internal",
      "README.md",
    ]);
  });

  it("carries each row's own depth, so drawing never walks back up", () => {
    const tree = build(PATHS);
    const rows = flatten(tree, new Set(["internal", "internal/desk"]));
    expect(rows.map((s) => [s.node.name, s.depth])).toEqual([
      ["internal", 0],
      ["desk", 1],
      ["sections.go", 2],
      ["tree.go", 2],
      ["launcher", 1],
      ["README.md", 0],
    ]);
  });

  it("marks the last child at each depth", () => {
    const rows = flatten(build(PATHS), new Set(["internal"]));
    const last = rows.filter((s) => s.last).map((s) => s.node.name);
    expect(last).toEqual(["launcher", "README.md"]);
  });
});

describe("opening and shutting", () => {
  it("flips one directory and leaves the rest", () => {
    const open = toggled(new Set(["a"]), "b");
    expect([...open].sort()).toEqual(["a", "b"]);
    expect([...toggled(open, "a")]).toEqual(["b"]);
  });

  it("opens every directory on the way to a file", () => {
    const open = opened(new Set(), "internal/desk/tree.go");
    expect([...open].sort()).toEqual(["internal", "internal/desk"]);
  });

  it("opens nothing for a file at the top", () => {
    expect([...opened(new Set(), "README.md")]).toEqual([]);
  });
});

// A chain of single directories is one row: three clicks to reach a file
// nobody keeps anything else beside is what makes a tree tiresome.
describe("squashing a chain of single directories", () => {
  it("joins them into one row", () => {
    const tree = squashed(build([{ path: "internal/web/assets/x.css" }]));
    expect(tree.map((n) => n.name)).toEqual(["internal/web/assets"]);
    expect(tree[0].children!.map((n) => n.name)).toEqual(["x.css"]);
  });

  it("stops where the directory holds more than one thing", () => {
    const tree = squashed(build(PATHS));
    expect(tree.map((n) => n.name)).toEqual(["internal", "README.md"]);
    expect(tree[0].children!.map((n) => n.name)).toEqual(["desk", "launcher"]);
  });

  it("does not squash a directory holding one file", () => {
    const tree = squashed(build([{ path: "a/only.go" }]));
    expect(tree.map((n) => n.name)).toEqual(["a"]);
  });
});
