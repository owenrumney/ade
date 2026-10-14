import { afterEach, describe, expect, it, vi } from "vitest";
import { stale, watch } from "./launcher";
import type { Server } from "./discover";

const server: Server = { base: "http://127.0.0.1:1", token: "t", root: "/r" };

// reading stands in for the event stream: each chunk is delivered in turn and
// the stream then goes quiet rather than closing, which is the half-open case.
function reading(chunks: string[]) {
  const encoder = new TextEncoder();
  let at = 0;
  const reader = {
    cancelled: false,
    read: () =>
      at < chunks.length
        ? Promise.resolve({ done: false, value: encoder.encode(chunks[at++]) })
        : new Promise<never>(() => undefined),
    cancel: async () => {
      reader.cancelled = true;
    },
  };
  vi.stubGlobal("fetch", async () => ({ body: { getReader: () => reader } }));
  return reader;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("the event stream", () => {
  it("names each event it is sent", async () => {
    vi.useFakeTimers();
    reading(["event: agent\ndata: {}\n\n", "event: state\ndata: {}\n\n"]);
    const seen: string[] = [];

    const done = watch(server, (name) => seen.push(name)).catch(() => undefined);
    await vi.advanceTimersByTimeAsync(1);
    expect(seen).toEqual(["agent", "state"]);
    await vi.advanceTimersByTimeAsync(stale + 10);
    await done;
  });

  // The bug this guards: a socket that half-opens delivers nothing and never
  // errors, so the reader blocked for ever and the page silently froze.
  it("gives up on a stream that goes quiet, so the caller can reconnect", async () => {
    vi.useFakeTimers();
    const reader = reading([]);

    const failed = watch(server, () => undefined).then(
      () => "no error",
      (err: unknown) => String(err),
    );
    await vi.advanceTimersByTimeAsync(stale + 10);

    expect(await failed).toContain("ade stopped sending events");
    expect(reader.cancelled).toBe(true);
  });

  // The server pings every 20s, so a quiet session must not be mistaken for
  // a dead one: each ping has to put the deadline back.
  it("keeps waiting while the pings keep coming", async () => {
    vi.useFakeTimers();
    const encoder = new TextEncoder();
    let pings = 0;
    const reader = {
      read: () =>
        pings++ < 4
          ? new Promise<{ done: boolean; value: Uint8Array }>((settle) =>
              setTimeout(
                () => settle({ done: false, value: encoder.encode("event: ping\ndata: {}\n\n") }),
                stale - 1000,
              ),
            )
          : new Promise<never>(() => undefined),
      cancel: async () => undefined,
    };
    vi.stubGlobal("fetch", async () => ({ body: { getReader: () => reader } }));

    const seen: string[] = [];
    const settled = watch(server, (name) => seen.push(name)).then(
      () => "ended",
      () => "gave up",
    );
    await vi.advanceTimersByTimeAsync((stale - 1000) * 4 + 10);

    expect(seen).toEqual(["ping", "ping", "ping", "ping"]);
    await vi.advanceTimersByTimeAsync(stale + 10);
    expect(await settled).toBe("gave up");
  });
});
