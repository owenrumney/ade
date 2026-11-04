import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const read = (...at: string[]) => readFileSync(join(__dirname, "..", ...at), "utf8");

// Monaco is seven eighths of what the window used to parse on every
// start. It stays out of the main bundle only while nothing in the
// window imports it directly, which one careless import would undo.
describe("the editor is loaded when a file is opened and not before", () => {
  it("is reached through the façade, never imported into the window", () => {
    const sources = ["app.ts", "panels.ts", "sheets.ts", "panel.ts", "omnibox.ts"];
    for (const file of sources) {
      const src = read("renderer", file);
      expect(src, `${file} imports the editor directly`).not.toMatch(/from "\.\/editor"/);
    }
    expect(read("renderer", "app.ts")).toContain('from "./code"');
  });

  it("is the only thing that reaches monaco, and it is its own bundle", () => {
    expect(read("renderer", "editor.ts")).toContain("monaco-editor");
    expect(read("renderer", "editorentry.ts")).toContain('from "./editor"');
    expect(read("..", "Makefile")).toContain("renderer/editorentry.ts");
  });

  it("draws a review's edit over the diff's left side, not as a plain file", () => {
    expect(read("renderer", "editorentry.ts")).toContain("mountDiff");
    expect(read("renderer", "app.ts")).toMatch(
      /view\.editing\) \{\s+return mountDiff\(groupOf\(tab\), view\.base/,
    );
  });

  it("brings monaco's stylesheet, which went with its bundle", () => {
    expect(read("renderer", "code.ts")).toContain("../dist/editor.css");
    expect(read("renderer", "index.html")).not.toContain("editor.css");
  });
});

// The window is user-select: none, which is right for a list of rows and
// wrong for a message about a failure: one you cannot select is one you
// retype to whoever is going to fix it.
describe("what the window says can be taken away", () => {
  const css = readFileSync(join(__dirname, "theme.css"), "utf8");

  it("lets the text be selected where it is read rather than clicked", () => {
    for (const at of [".sidebar .empty", ".statusbar .saying", ".nothing"]) {
      expect(css, at).toContain(at);
    }
  });

  it("copies both of them on a click", () => {
    const src = readFileSync(join(__dirname, "app.ts"), "utf8");
    expect(src).toContain('saying.title = "Click to copy"');
    expect(src).toContain("private note(text: string)");
    expect(src).toContain("navigator.clipboard.writeText");
  });
});

// `/api/design` answers with the document twice: `body` rendered to
// HTML for a reader, and `raw` as it was written. The editor wants the
// second, and handed the first it shows the markup.
describe("the design editor is given the document, not its rendering", () => {
  const src = readFileSync(join(__dirname, "app.ts"), "utf8");
  const reader = src.slice(src.indexOf('which.startsWith("design:")'));

  it("reads raw from the session's design route", () => {
    expect(reader.slice(0, 600)).toContain(".raw ?? \"\"");
    expect(reader.slice(0, 600)).not.toContain(".body ?? \"\"");
  });

  it("still writes under body, which is what the save route takes", () => {
    expect(reader.slice(0, 900)).toContain('"/api/design", { file, body: text }');
  });
});

// Opening a design from a space page went straight to show(), so the window
// stayed on whatever session it was already on.
describe("every way into a design goes through the one that moves", () => {
  const src = readFileSync(join(__dirname, "app.ts"), "utf8");

  it("has no design tab opened without it", () => {
    const direct = src.match(/this\.show\(\{ panel: [^}]*\}, [^)]*"design"\)/g) ?? [];
    expect(direct).toEqual([]);
  });

  it("moves to the session holding the document", () => {
    const at = src.indexOf("private showDesign(");
    expect(at).toBeGreaterThan(0);
    expect(src.slice(at, at + 300)).toContain("this.moveTo(this.docFor(panel)?.session");
  });
});
