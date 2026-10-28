import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { dropped, MAX_FILES, mediaOf, pasted, refuse } from "./files";

const png = { size: 10, name: "a.png", type: "image/png" } as File;

describe("what a paste offers", () => {
  it("takes the images and nothing else", () => {
    const items = [
      { kind: "file", type: "image/png", getAsFile: () => png },
      { kind: "string", type: "text/plain", getAsFile: () => null },
      { kind: "file", type: "application/pdf", getAsFile: () => png },
    ] as unknown as DataTransferItemList;
    expect(pasted(items)).toEqual([png]);
  });

  it("is nothing at all when the clipboard has no items", () => {
    expect(pasted(undefined)).toEqual([]);
  });

  it("skips an item that will not hand over a file", () => {
    const items = [{ kind: "file", type: "image/png", getAsFile: () => null }] as unknown as DataTransferItemList;
    expect(pasted(items)).toEqual([]);
  });
});

describe("the caps", () => {
  it("refuse a file over the server's limit before the round trip", () => {
    expect(refuse({ ...png, size: (20 << 20) + 1 } as File, 0)).toContain("20MB");
  });

  it("refuse more files than one message carries", () => {
    expect(refuse(png, MAX_FILES)).toContain(String(MAX_FILES));
  });

  it("allow an ordinary image", () => {
    expect(refuse(png, 0)).toBe("");
  });
});

// A drag out of the editor's tree sets text/uri-list and no files at all.
describe("what a drag offers", () => {
  it("takes the file paths and leaves the rest of the list", () => {
    const list = "# comment\r\nfile:///code/a.png\r\nhttps://example.com\r\n\r\nfile:///code/b.pdf";
    expect(dropped(list)).toEqual(["file:///code/a.png", "file:///code/b.pdf"]);
  });

  it("is nothing when the drag carried none", () => {
    expect(dropped(undefined)).toEqual([]);
    expect(dropped("")).toEqual([]);
  });

  it("names the type an image is viewed by, and nothing else", () => {
    expect(mediaOf("A.PNG")).toBe("image/png");
    expect(mediaOf("a.jpeg")).toBe("image/jpeg");
    expect(mediaOf("spec.pdf")).toBe("application/octet-stream");
    expect(mediaOf("Makefile")).toBe("application/octet-stream");
  });
});

// The webview cannot read the disk, so a path-only drag went nowhere.
describe("dropping a file the editor named by path", () => {
  const page = readFileSync(join(__dirname, "main.ts"), "utf8");
  const markup = readFileSync(join(__dirname, "body.ts"), "utf8");

  it("falls back to the paths when the drag carried no files", () => {
    expect(page).toContain('dropped(moved?.getData("text/uri-list"))');
    expect(page).toContain('host.postMessage({ kind: "drop", id, paths });');
  });

  it("offers a file and not only an image", () => {
    expect(markup).toContain("paste an image or drop a file");
  });
});
