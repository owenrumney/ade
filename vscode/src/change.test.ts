import { describe, expect, it } from "vitest";
import {
  atOf,
  BLOB,
  colour,
  nest,
  folder,
  letter,
  rank,
  revUri,
  scopeLabel,
  sides,
  staleSides,
  summary,
  tally,
  title,
} from "./change";
import type { Diff, DiffFile } from "./client";

const file = (over: Partial<DiffFile>): DiffFile => ({
  path: "internal/web/web.go",
  status: "M",
  binary: false,
  additions: 3,
  deletions: 1,
  ...over,
});

const diff = (over: Partial<Diff>): Diff => ({
  base: "origin/main",
  head: "",
  mode: "worktree",
  commits: 0,
  files: [file({})],
  ...over,
});

describe("the view's subtitle", () => {
  it("counts the files and the lines both ways", () => {
    expect(
      summary(
        diff({
          files: [file({}), file({ path: "a", additions: 1, deletions: 0 })],
        }),
      ),
    ).toBe("2 files +4 −1");
  });

  it("says why there is nothing when the server explained it", () => {
    expect(summary(diff({ files: [], note: "no base branch found" }))).toBe(
      "no base branch found",
    );
    expect(summary(diff({ files: [] }))).toBe("no changes");
  });
});

describe("a file's row", () => {
  it("counts lines, except where there are none to count", () => {
    expect(tally(file({}))).toBe("+3 −1");
    expect(tally(file({ binary: true }))).toBe("binary");
  });

  it("reads an untracked file as an addition", () => {
    expect(letter(file({ untracked: true, status: "?" }))).toBe("U");
    expect(colour(file({ untracked: true }))).toContain("untracked");
  });

  it("falls back to modified for a status git spells differently", () => {
    expect(letter(file({ status: "T" }))).toBe("M");
  });

  it("puts the directory beside the name", () => {
    expect(folder("internal/web/web.go")).toBe("internal/web");
    expect(folder("Makefile")).toBe("");
  });

  it("sorts by path, not by churn", () => {
    const rows = [file({ path: "z", additions: 90 }), file({ path: "a" })].sort(
      rank,
    );
    expect(rows.map((f) => f.path)).toEqual(["a", "z"]);
  });
});

describe("which sides a file has", () => {
  it("gives an added file no left and a deleted one no right", () => {
    expect(sides(file({ status: "A" }))).toEqual({ left: false, right: true });
    expect(sides(file({ status: "D" }))).toEqual({ left: true, right: false });
    expect(sides(file({ status: "M" }))).toEqual({ left: true, right: true });
  });
});

describe("the diff tab's name", () => {
  it("names both ends, because the left one is what the scope changes", () => {
    expect(title(file({}), diff({}))).toBe(
      "web.go (origin/main ↔ working tree)",
    );
    expect(title(file({}), diff({ mode: "uncommitted" }))).toBe(
      "web.go (HEAD ↔ working tree)",
    );
  });

  it("shows a rename as a move", () => {
    expect(
      title(file({ oldPath: "old/web.go", status: "R" }), diff({})),
    ).toContain("web.go → web.go");
  });
});

describe("the scope", () => {
  it("has a short name for the view's title", () => {
    expect(scopeLabel("uncommitted")).toBe("Uncommitted");
    expect(scopeLabel(undefined)).toBe("Branch");
  });
});

describe("the file list as a tree", () => {
  const paths = (nodes: ReturnType<typeof nest>): string[] =>
    nodes.map((n) => (n.kind === "dir" ? `${n.label}/` : n.file.path));

  it("folds a chain of single-child directories into one row", () => {
    const got = nest([
      file({ path: "internal/web/web.go" }),
      file({ path: "internal/web/blob.go" }),
    ]);
    expect(paths(got)).toEqual(["internal/web/"]);
    expect(paths((got[0] as { kids: ReturnType<typeof nest> }).kids)).toEqual([
      "internal/web/blob.go",
      "internal/web/web.go",
    ]);
  });

  // A nested row's label is relative: the parent already says `internal`.
  it("stops folding where the tree actually branches", () => {
    const got = nest([
      file({ path: "internal/web/a.go" }),
      file({ path: "internal/gitdiff/b.go" }),
    ]);
    expect(paths(got)).toEqual(["internal/"]);
    const kids = (got[0] as { kids: ReturnType<typeof nest> }).kids;
    expect(paths(kids)).toEqual(["gitdiff/", "web/"]);
  });

  it("keeps a directory that also holds files of its own", () => {
    const got = nest([
      file({ path: "web/a.go" }),
      file({ path: "web/src/b.ts" }),
    ]);
    expect(paths(got)).toEqual(["web/"]);
    expect(paths((got[0] as { kids: ReturnType<typeof nest> }).kids)).toEqual([
      "src/",
      "web/a.go",
    ]);
  });

  it("puts directories above the files beside them", () => {
    const got = nest([
      file({ path: "Makefile" }),
      file({ path: "cmd/ade/main.go" }),
    ]);
    expect(paths(got)).toEqual(["cmd/ade/", "Makefile"]);
  });
});

describe("staleSides", () => {
  // The file on disk is the editor's own document and must not be reloaded
  // under whoever is typing in it.
  it("is the sides read from ade, not the files on disk", () => {
    const open = [
      { scheme: "file", path: "/r/a.go" },
      { scheme: BLOB, path: "/a.go" },
      { scheme: "ade-wiki", path: "/page.md" },
    ];
    expect(staleSides(open)).toEqual([{ scheme: BLOB, path: "/a.go" }]);
  });
});

describe("a file at a revision", () => {
  // Two tabs of one file are otherwise both "blame.go", and the language
  // comes off the extension, so both have to survive.
  it("names the tab for the commit and still ends in the extension", () => {
    expect(revUri("11f7987", "internal/web/blame.go").path).toBe(
      "/internal/web/blame@11f7987.go",
    );
  });

  it("reads back what it wrote", () => {
    for (const [sha, path] of [
      ["11f7987", "a.go"],
      ["11f7987", "internal/web/blame.go"],
      ["11f7987", "a file with spaces.md"],
      ["11f7987", "Makefile"],
      ["11f7987", "tools/Dockerfile"],
      ["11f7987", "mail@home.txt"],
      ["11f7987", "src/v1.2/thing.go"],
      ["11f7987", ".gitignore"],
    ]) {
      expect(atOf(revUri(sha, path))).toEqual({ sha, path });
    }
  });

  it("answers nothing for a uri that is not one of ours", () => {
    expect(atOf({ path: "/a.go" })).toEqual({ sha: "", path: "" });
    expect(atOf({ path: "" })).toEqual({ sha: "", path: "" });
    expect(atOf({ path: "/internal/web/blame.go" })).toEqual({
      sha: "",
      path: "",
    });
  });
});
