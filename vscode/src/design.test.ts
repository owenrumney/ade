import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { same } from "./design";
import { ACTS, rows } from "./designpick";

// A worktree's git dir holds its session record and nothing else: the design
// documents live under the git dir the whole repository shares, and in
// `.scratch` or --docs when either is set. Working the path out here listed an
// empty Design view for every worktree, while ade itself showed the document.
const design = readFileSync(join(__dirname, "design.ts"), "utf8");
const extension = readFileSync(join(__dirname, "extension.ts"), "utf8");
const manifest = JSON.parse(
  readFileSync(join(__dirname, "..", "package.json"), "utf8"),
) as {
  contributes: {
    commands: { command: string; icon?: string }[];
    menus: Record<string, { command: string; when?: string }[]>;
  };
};

describe("where the design list looks", () => {
  it("does not build the path out of a git dir", () => {
    expect(design).not.toContain("gitDir");
    expect(design).not.toMatch(/"ade",\s*"docs"/);
  });

  it("takes the directory the server reports", () => {
    expect(extension).toContain("design.serving(state.design?.dir)");
  });
});

// The documents live under .git, where VS Code's file watcher does not reach,
// so a document written while you watched the list never appeared.
describe("how the design list notices a new document", () => {
  it("sweeps on a state event rather than trusting the watcher", () => {
    const serving = design.slice(design.indexOf("serving(dir:"));
    expect(serving.slice(0, serving.indexOf("}\n\n"))).toContain(
      "this.sweep()",
    );
  });

  it("re-reads only when the listing changed", () => {
    const sweep = design.slice(design.indexOf("private async sweep"));
    expect(sweep.slice(0, sweep.indexOf("}\n\n"))).toContain(
      "same(found, this.fetch.now)",
    );
  });

  it("does not let two sweeps overlap", () => {
    expect(design).toContain("this.sweeping");
  });

  it("compares two listings without redrawing an unchanged one", () => {
    expect(same([], [])).toBe(true);
    expect(same(["a.md"], ["a.md"])).toBe(true);
    expect(same(["a.md"], ["a.md", "b.md"])).toBe(false);
    expect(same(["a.md", "b.md"], ["a.md", "c.md"])).toBe(false);
  });
});

// The server walks the design directory and nests it; the view read one level
// and filtered, so a document in a subfolder was invisible in the editor.
// The server walks the design directory and nests it; the list read one level
// and filtered, so a document in a subfolder was invisible in the editor.
describe("a design document in a subfolder", () => {
  const files = [
    "enhancements/01-one.md",
    "enhancements/02-two.md",
    "deep/a/b.md",
    "flat.md",
  ];

  // Flat, and sorted by path. A picker filters on what it shows, so the folder
  // has to be in the row rather than be a row of its own.
  it("lists every document, path and all", () => {
    expect(rows(files).map((r) => r.description)).toEqual([
      "deep/a/b.md",
      "enhancements/01-one.md",
      "enhancements/02-two.md",
      "flat.md",
    ]);
  });

  it("labels each with its title, not its path", () => {
    expect(rows(["enhancements/01-one.md"])[0].label).toBe("01 one");
  });

  it("walks the directory rather than reading one level", () => {
    expect(design).toContain("withFileTypes: true");
    expect(design).toContain('"**/*.md"');
  });

  it("offers every document to the picker, not just the top level", () => {
    expect(extension).toContain("const found = [...design.files]");
  });
});

// The documents are written while you are not looking, so the picker re-reads
// on the way open rather than trusting whatever the last sweep left.
describe("the design picker", () => {
  it("is a command, and no longer a view", () => {
    const ids = manifest.contributes.commands.map((one) => one.command);
    expect(ids).toContain("ade.design");
    expect(ids).not.toContain("ade.refreshDesign");
  });

  it("refreshes before it draws", () => {
    const pick = extension.slice(
      extension.indexOf("async function pickDesign"),
    );
    expect(pick.slice(0, pick.indexOf("await pick("))).toContain(
      "design.refresh()",
    );
  });
});

// The Electron app had a Condense button. The picker is where a design
// document's actions live now, so losing it there loses it everywhere.
describe("condense", () => {
  const act = ACTS.find((a) => a.id === "condense");

  it("is one of the document's actions", () => {
    expect(act, "no condense act in designpick").toBeDefined();
    expect(act?.label).toBe("Condense it");
  });

  it("sits above delete", () => {
    const ids = ACTS.map((a) => a.id);
    expect(ids.indexOf("condense")).toBeLessThan(ids.indexOf("delete"));
  });

  it("asks first, because the document is rewritten in place", () => {
    const fn = extension.slice(
      extension.indexOf("async function condenseDesign"),
    );
    const body = fn.slice(0, fn.indexOf("c.condenseDesign()"));
    expect(body).toContain("showWarningMessage");
    expect(body).toContain("modal: true");
  });

  it("tells the server which document before starting", () => {
    const fn = extension.slice(
      extension.indexOf("async function condenseDesign"),
    );
    const body = fn.slice(0, fn.indexOf("c.condenseDesign()"));
    expect(body, "the condenser asks which one when nobody said").toContain(
      "c.openDoc(name)",
    );
  });
});

// Two buttons on the open document, the interview and the condenser: the acts
// a draft needs most, one click from the words themselves.
describe("the document's title buttons", () => {
  const title = manifest.contributes.menus["editor/title"] ?? [];

  it("are grill and condense, on a design document only", () => {
    expect(title.map((m) => m.command)).toEqual([
      "ade.grillDesign",
      "ade.condenseDesign",
    ]);
    for (const m of title) expect(m.when).toBe("resourcePath in ade.designDocs");
  });

  it("read the key the design list publishes", () => {
    expect(design).toContain('"ade.designDocs"');
  });

  it("know the open document from its uri", () => {
    const fn = extension.slice(extension.indexOf("function docName"));
    expect(fn.slice(0, fn.indexOf("\n}\n"))).toContain("instanceof vscode.Uri");
  });
});

describe("grill", () => {
  it("is one of the document's actions, above condense", () => {
    const ids = ACTS.map((a) => a.id);
    expect(ids.indexOf("grill")).toBeGreaterThan(-1);
    expect(ids.indexOf("grill")).toBeLessThan(ids.indexOf("condense"));
  });

  it("sends the skill as a slash command, after naming the document", () => {
    const fn = extension.slice(extension.indexOf("async function grillDesign"));
    const body = fn.slice(0, fn.indexOf("chat.reveal"));
    expect(body, "the skill needs the document named first").toContain(
      "c.openDoc(name)",
    );
    expect(body).toContain("`/grill-me ");
  });
});
