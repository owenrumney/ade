import { describe, expect, it } from "vitest";
import { type Store, STALE_MS, take, want, WANTED } from "./wanted";

function store(): Store & { held: Record<string, unknown> } {
  const held: Record<string, unknown> = {};
  return {
    held,
    get: <T>(key: string) => held[key] as T | undefined,
    update: (key, value) => {
      held[key] = value;
      return Promise.resolve();
    },
  };
}

const now = 1_000_000;

describe("what a new window was opened for", () => {
  it("survives the window change and is taken once", async () => {
    const s = store();
    await want(s, { path: "/code/api", number: 12 }, now);
    expect(await take(s, "/code/api", now)).toMatchObject({
      path: "/code/api",
      number: 12,
    });
    expect(await take(s, "/code/api", now)).toBeUndefined();
  });

  // The folder switch closes every tab the old workspace had. A session opened
  // elsewhere has to say so, or the new window comes up on nothing.
  it("carries the conversation to resume", async () => {
    const s = store();
    await want(
      s,
      {
        path: "/code/api",
        chat: true,
        session: "s1",
        agent: "claude",
        model: "opus",
      },
      now,
    );
    expect(await take(s, "/code/api", now)).toMatchObject({
      chat: true,
      session: "s1",
      agent: "claude",
      model: "opus",
    });
  });

  it("carries a chat with no session named", async () => {
    const s = store();
    await want(s, { path: "/code/api", chat: true }, now);
    const got = await take(s, "/code/api", now);
    expect(got?.chat).toBe(true);
    expect(got?.session).toBeUndefined();
  });

  it("is not shown in a window it was not meant for", async () => {
    const s = store();
    await want(s, { path: "/code/api", number: 12 }, now);
    expect(await take(s, "/code/web", now)).toBeUndefined();
  });

  // A session now opens a new window, so the window that wrote the note is
  // still running and still polling. Clearing on a mismatch let it eat its own
  // note before the new window had started.
  it("is left alone by the window that wrote it", async () => {
    const s = store();
    await want(s, { path: "/code/api", chat: true }, now);
    await take(s, "/code/web", now);
    expect(await take(s, "/code/api", now)).toMatchObject({ chat: true });
  });

  // And is not waiting a week later for whatever opens that checkout next.
  it("retires once it is old", async () => {
    const s = store();
    await want(s, { path: "/code/api", number: 12 }, now);
    expect(await take(s, "/code/api", now + STALE_MS + 1)).toBeUndefined();
  });

  it("is swept by a window it was not for, once it is old", async () => {
    const s = store();
    await want(s, { path: "/code/api", number: 12 }, now);
    await take(s, "/code/web", now + STALE_MS + 1);
    expect(s.held[WANTED]).toBeUndefined();
  });

  it("does nothing without a store", async () => {
    await want(undefined, { path: "/x", number: 1 });
    expect(await take(undefined, "/x")).toBeUndefined();
  });
});
