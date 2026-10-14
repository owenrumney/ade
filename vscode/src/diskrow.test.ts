import { describe, expect, it } from "vitest";
import { context, inside, page, roots, size, warning } from "./diskrow";

describe("size", () => {
  it("reads as a person would say it", () => {
    expect(size(0)).toBe("0 B");
    expect(size(900)).toBe("900 B");
    expect(size(1536)).toBe("1.5 KB");
    expect(size(495 * 1024)).toBe("495 KB");
    expect(size(3.2 * 1024 ** 3)).toBe("3.2 GB");
    expect(size(-5)).toBe("0 B");
  });
});

describe("roots", () => {
  it("puts the place costing most first", () => {
    const rows = roots({
      total: 30,
      stores: [
        { label: "small", path: "/s", bytes: 10 },
        { label: "big", path: "/b", bytes: 20 },
      ],
    });
    expect(
      rows.map((row) => (row.kind === "store" ? row.store.label : "")),
    ).toEqual(["big", "small"]);
  });

  it("is empty before a reading", () => {
    expect(roots(undefined)).toEqual([]);
    expect(roots({ total: 0, stores: null })).toEqual([]);
  });
});

describe("inside", () => {
  it("lists what can be emptied above what is merely there", () => {
    const rows = inside({
      label: "ade",
      path: "/a",
      bytes: 30,
      parts: [
        { name: "small", bytes: 1 },
        { name: "big", bytes: 9, dir: true },
      ],
      purgeable: [
        {
          label: "Build cache",
          path: "/a/cache",
          bytes: 5,
          files: 3,
          recoverable: true,
        },
      ],
    });
    expect(rows.map(context)).toEqual([
      "ade.disk.purge",
      "ade.disk.part",
      "ade.disk.part",
    ]);
    expect(rows[1].kind === "part" && rows[1].part.name).toBe("big");
  });
});

describe("warning", () => {
  const tree = { label: "x", path: "/x", bytes: 2048, files: 1 };

  it("says a cache comes back", () => {
    expect(warning({ ...tree, recoverable: true })).toBe(
      "2.0 KB in 1 file. The next build makes it again.",
    );
  });

  it("says attachments do not", () => {
    expect(warning({ ...tree, files: 4, recoverable: false })).toBe(
      "2.0 KB in 4 files. This cannot be undone.",
    );
  });
});

describe("the disk page", () => {
  const disk = {
    total: 3072,
    stores: [
      { label: "ade", path: "/a", bytes: 2048 },
      { label: "ade itself", path: "/b", bytes: 1024 },
    ],
  };

  it("leads with the total and lists each place, biggest first", () => {
    const text = page(disk, new Map());
    expect(text).toContain("**3.0 KB** across 2 places");
    expect(text.indexOf("## ade · 2.0 KB")).toBeLessThan(
      text.indexOf("## ade itself"),
    );
  });

  it("says what can be emptied, and whether it comes back", () => {
    const text = page(
      disk,
      new Map([
        [
          "/a",
          {
            label: "ade",
            path: "/a",
            bytes: 2048,
            parts: [{ name: "chat.db", bytes: 512 }],
            purgeable: [
              {
                label: "Build cache",
                path: "/a/c",
                bytes: 1024,
                files: 2,
                recoverable: true,
              },
              {
                label: "Attachments",
                path: "/a/x",
                bytes: 512,
                files: 1,
                recoverable: false,
              },
            ],
          },
        ],
      ]),
    );
    expect(text).toContain(
      "| Build cache | 1.0 KB | can be emptied, and comes back |",
    );
    expect(text).toContain("| Attachments | 512 B | can be emptied |");
    expect(text).toContain("| chat.db | 512 B | |");
  });

  it("says so for a place it could not look inside", () => {
    expect(page(disk, new Map())).toContain("_Nothing inside worth listing._");
  });
});
