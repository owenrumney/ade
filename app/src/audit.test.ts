import { afterEach, describe, expect, it } from "vitest";
import { mkdtemp, readFile, writeFile, mkdir, readdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { dayOf, days, named, note, prune, read, settled } from "./audit";

// homedir() is $HOME on this platform, which is how the trail is pointed
// somewhere a test may write.
const was = process.env.HOME;
async function home(): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), "ade-audit-"));
  process.env.HOME = dir;
  return join(dir, ".ade", "audit");
}

afterEach(() => {
  process.env.HOME = was;
});

describe("what the trail keeps", () => {
  it("writes one line per action, and reads it back", async () => {
    const dir = await home();
    note({ at: "2026-10-27T21:00:00.000Z", kind: "command", what: "ade.commitPanel", where: "todos" });
    note({ at: "2026-10-27T21:00:01.000Z", kind: "http", what: "POST /api/commit", ok: true, ms: 40 });
    await settled();

    const raw = await readFile(join(dir, `${dayOf()}.jsonl`), "utf8");
    expect(raw.trim().split("\n")).toHaveLength(2);
    const back = await read();
    expect(back.map((e) => e.what)).toEqual(["ade.commitPanel", "POST /api/commit"]);
  });

  // A settings write carries tokens. The trail says which fields moved and
  // never what they moved to.
  it("names a body's fields and keeps none of their values", () => {
    expect(named({ dashboardToken: "sk-live-123", previewOff: true })).toEqual([
      "dashboardToken",
      "previewOff",
    ]);
    expect(JSON.stringify(named({ token: "secret" }))).not.toContain("secret");
    expect(named("a string")).toBeUndefined();
    expect(named({})).toBeUndefined();
  });

  it("skips a torn line rather than losing the day", async () => {
    const dir = await home();
    await mkdir(dir, { recursive: true });
    await writeFile(join(dir, `${dayOf()}.jsonl`), '{"what":"one","kind":"command","at":"x"}\n{bad\n');
    expect((await read()).map((e) => e.what)).toEqual(["one"]);
  });

  it("drops the days past the window it keeps", async () => {
    const dir = await home();
    await mkdir(dir, { recursive: true });
    for (const day of ["2026-09-01", "2026-10-26", "2026-10-27"]) {
      await writeFile(join(dir, `${day}.jsonl`), "");
    }
    await prune(30, new Date("2026-10-27T12:00:00Z"));
    expect(await days()).toEqual(["2026-10-27", "2026-10-26"]);
    expect(await readdir(dir)).not.toContain("2026-09-01.jsonl");
  });

  it("has nothing to read where nothing was written", async () => {
    await home();
    expect(await read()).toEqual([]);
    expect(await days()).toEqual([]);
  });
});
