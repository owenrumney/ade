import { describe, expect, it, vi } from "vitest";

let release: (() => void) | undefined;
const waited = new Promise<void>((ok) => (release = ok));

vi.mock("./launcher", async () => {
  const real = await vi.importActual<typeof import("./launcher")>("./launcher");
  return {
    ...real,
    find: async () => ({ base: "http://127.0.0.1:1", token: "t", root: "/r" }),
    hosts: async () => {
      await waited;
      return [{ name: "linux" }];
    },
    ask: async () => ({ hosts: [] }),
  };
});

import { Machines } from "./machines";

const settled = () => new Promise((ok) => setTimeout(ok, 0));

// The picker waits on this read, so a slow launcher must not resolve the list
// early: an empty one would draw as "no machines" and be wrong.
describe("the machine list while it loads", () => {
  it("holds until the launcher answers, then has the rows", async () => {
    const state = new Machines();
    let done = false;
    const list = state.list().then((hosts) => {
      done = true;
      return hosts;
    });

    await settled();
    expect(done).toBe(false);

    release?.();
    expect(await list).toHaveLength(1);
  });
});
