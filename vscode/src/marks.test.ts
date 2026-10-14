import { describe, expect, it } from "vitest";
import * as vscode from "vscode";
import type { Thread } from "./client";
import { markFor, Marks, marksFor } from "./marks";

const thread = (file: string, extra: Partial<Thread> = {}): Thread =>
  ({ id: file + Math.random(), file, side: "right", line: 1, comments: [], ...extra }) as Thread;

describe("explorer marks", () => {
  it("badges a touched file and counts open threads", () => {
    expect(markFor(true, 0, true)).toMatchObject({ badge: "✎", colour: "charts.blue", propagate: true, tooltip: "ade is editing this" });
    expect(markFor(true, 0, false)?.tooltip).toBe("ade edited this turn");
    expect(markFor(false, 2, false)).toMatchObject({ badge: "2", colour: "charts.orange", propagate: false, tooltip: "2 open review comments" });
    expect(markFor(true, 1, false)).toMatchObject({ badge: "✎1", colour: "charts.orange", tooltip: "ade edited this turn · 1 open review comment" });
    expect(markFor(false, 12, false)?.badge).toBe("9");
    expect(markFor(false, 0, true)).toBeUndefined();
  });

  it("ignores threads that are settled, withdrawn or orphaned", () => {
    const marks = marksFor(["a.go"], [
      thread("a.go"),
      thread("b.go", { resolved: true }),
      thread("c.go", { withdrawn: true }),
      thread("d.go", { orphaned: true }),
      thread("e.go"),
      thread("e.go"),
    ], true);
    expect([...marks.keys()].sort()).toEqual(["a.go", "e.go"]);
    expect(marks.get("a.go")?.badge).toBe("✎1");
    expect(marks.get("e.go")?.badge).toBe("2");
  });

  it("decorates files under the root and tells the explorer what moved", () => {
    const marks = new Marks();
    const fired: string[][] = [];
    marks.onDidChangeFileDecorations((uris) => fired.push(uris.map((u) => u.fsPath)));
    marks.apply("/repo", ["src/a.ts"], [thread("src/b.ts")], true);
    expect(fired).toEqual([["/repo/src/a.ts", "/repo/src/b.ts"]]);
    expect(marks.provideFileDecoration(vscode.Uri.file("/repo/src/a.ts"))).toMatchObject({ badge: "✎", propagate: true });
    expect(marks.provideFileDecoration(vscode.Uri.file("/elsewhere/src/a.ts"))).toBeUndefined();
    expect(marks.provideFileDecoration(vscode.Uri.file("/repo"))).toBeUndefined();

    marks.apply("/repo", ["src/a.ts"], [thread("src/b.ts")], true);
    expect(fired).toHaveLength(1);

    marks.apply("/repo", [], [thread("src/b.ts")], false);
    expect(fired[1]).toEqual(["/repo/src/a.ts"]);
    expect(marks.provideFileDecoration(vscode.Uri.file("/repo/src/a.ts"))).toBeUndefined();

    marks.apply(undefined, [], [], false);
    expect(fired[2]).toEqual(["/repo/src/b.ts"]);
  });
});
