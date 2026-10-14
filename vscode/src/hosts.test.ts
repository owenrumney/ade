import { describe, expect, it } from "vitest";
import type { Host } from "./launcher";
import {
  busy,
  hostIcon as icon,
  hostLine as say,
  hostLine,
  loadLine,
  reason,
  said,
  shown,
  finished,
  withLoad,
} from "./launcher";

const off: Host = { name: "pi@pi5.local", state: "off", sessions: 0 };
const on: Host = {
  name: "owen@bigboy",
  connected: true,
  state: "connected",
  sessions: 2,
};

describe("what a machine's row says", () => {
  it("is its state, and what is running on it", () => {
    expect(say(on)).toBe("connected · 2 sessions");
    expect(say(off)).toBe("off");
  });

  it("counts one session as one", () => {
    expect(say({ ...on, sessions: 1 })).toContain("1 session");
  });

  it("falls back to reachable or not when the launcher names no state", () => {
    expect(say({ name: "x", connected: true })).toBe("connected");
    expect(say({ name: "x" })).toBe("off");
  });
});

describe("what a machine looks like", () => {
  it("shows this Mac apart from the rest", () => {
    expect(icon({ name: "local", local: true, connected: true })).toBe(
      "vm-active",
    );
  });

  it("shows reachable apart from not", () => {
    expect(icon(on)).toBe("vm-running");
    expect(icon(off)).toBe("vm-outline");
  });

  it("shows a machine that failed as failed, whatever its state says", () => {
    expect(icon({ ...on, error: "no route to host" })).toBe("error");
  });
});

describe("a machine mid-flight", () => {
  it("spins rather than showing where it was", () => {
    expect(icon({ ...off, state: "connecting" })).toBe("sync~spin");
    expect(icon({ ...on, state: "disconnecting" })).toBe("sync~spin");
  });

  it("says what it is doing", () => {
    expect(say({ ...off, state: "connecting" })).toBe("connecting");
  });

  // A second click on a ten-minute update is the thing to prevent.
  it("is busy while its worker is being told something", () => {
    for (const state of ["syncing", "updating", "stopping"])
      expect(busy({ ...on, state })).toBe(true);
  });

  it("is not busy when it has settled", () => {
    expect(busy(on)).toBe(false);
    expect(busy(off)).toBe(false);
  });
});

describe("said", () => {
  it("unwraps the server's sentence from its error envelope", () => {
    expect(said('{"error":"pi5.local is already a machine ade knows"}')).toBe(
      "pi5.local is already a machine ade knows",
    );
  });

  it("passes plain text through", () => {
    expect(said("no route to host")).toBe("no route to host");
  });

  it("does not throw on an empty or broken body", () => {
    expect(said("")).toBe("");
    expect(said("{oops")).toBe("{oops");
  });
});

// The sandbox toggle is refused with a sentence worth reading — "the agent is
// running; wait for it or cancel it first" — wrapped in two layers of noise.
describe("reason", () => {
  it("digs the server's sentence out of a Client error", () => {
    const err = new Error(
      '/api/sandbox/on: 409 {"error":"the agent is running; wait for it or cancel it first"}',
    );
    expect(reason(err)).toBe(
      "the agent is running; wait for it or cancel it first",
    );
  });

  it("keeps a message that is not of that shape", () => {
    expect(reason(new Error("fetch failed"))).toBe("Error: fetch failed");
    expect(reason("plain")).toBe("plain");
  });

  // A body that is json but not an error envelope has no sentence to find.
  it("falls back to the body when there is no error field", () => {
    expect(reason(new Error('/api/x: 500 {"ok":false}'))).toBe('{"ok":false}');
  });
});

describe("machine load", () => {
  const load = {
    name: "pi",
    cores: 4,
    load1: 1.25,
    memTotal: 8 * 1024 ** 3,
    memUsed: 3.5 * 1024 ** 3,
    agents: 2,
  };

  it("reads as load against cores, memory, and agents", () => {
    expect(loadLine(load)).toBe("load 1.3/4 · 3.5/8.0 GB · 2 agents");
    expect(loadLine({ ...load, memTotal: 0, agents: 0 })).toBe("load 1.3/4");
    expect(loadLine({ ...load, agents: 1 })).toContain("1 agent");
  });

  it("lands on the machine it is of, and on no other", () => {
    const got = withLoad(
      [{ name: "pi", connected: true }, { name: "nas" }],
      [load],
    );
    expect(hostLine(got[0])).toBe(
      "connected · load 1.3/4 · 3.5/8.0 GB · 2 agents",
    );
    expect(got[1].load).toBeUndefined();
  });
});

describe("a machine's row while its worker is told something", () => {
  it("draws the launcher's own work, which outlasts this window", () => {
    const row = shown({ ...on, working: "updating" });
    expect(row.state).toBe("updating");
    expect(busy(row)).toBe(true);
  });

  it("prefers the launcher's work to this window's guess at it", () => {
    expect(shown({ ...on, working: "updating" }, "syncing").state).toBe(
      "updating",
    );
  });

  it("still draws connect and disconnect, which no launcher reports", () => {
    expect(shown(off, "connecting").state).toBe("connecting");
  });

  it("shows what the last action said once nothing is running", () => {
    const row = shown({ ...on, failed: "registry refused the pull" });
    expect(row.error).toBe("registry refused the pull");
    expect(busy(row)).toBe(false);
  });

  it("hides the last failure while a new attempt runs", () => {
    expect(
      shown({ ...on, working: "updating", failed: "old news" }).error,
    ).toBe("");
  });
});

describe("telling the window how an action it did not wait for went", () => {
  const was = new Map([["owen@bigboy", "updating"]]);

  it("reports a machine that has stopped working", () => {
    const { settled } = finished(was, [{ ...on, label: "bigboy" }]);
    expect(settled).toEqual([
      { name: "owen@bigboy", label: "bigboy", doing: "updating" },
    ]);
  });

  it("carries the failure through so the window can say it", () => {
    const { settled } = finished(was, [{ ...on, failed: "no space left" }]);
    expect(settled[0].failed).toBe("no space left");
  });

  it("says nothing about a machine that is still working", () => {
    const { settled, working } = finished(was, [
      { ...on, working: "updating" },
    ]);
    expect(settled).toEqual([]);
    expect(working.get("owen@bigboy")).toBe("updating");
  });

  it("says nothing about a machine that never started", () => {
    expect(finished(new Map(), [on, off]).settled).toEqual([]);
  });

  it("reports an outcome once, because the next call starts from now", () => {
    const first = finished(was, [on]);
    expect(first.settled).toHaveLength(1);
    expect(finished(first.working, [on]).settled).toEqual([]);
  });
});
