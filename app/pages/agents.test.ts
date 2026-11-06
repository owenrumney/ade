import { describe, expect, it } from "vitest";
import { type Active, attention, stateOf } from "./agents";

describe("what a server's agent is doing", () => {
  it("ranks the four states, first match first", () => {
    expect(stateOf(undefined)).toBe("idle");
    expect(stateOf(null)).toBe("idle");
    expect(stateOf({ error: "boom", running: true, asking: 1 })).toBe("failed");
    expect(stateOf({ running: true, asking: 1 })).toBe("asking");
    expect(stateOf({ running: true })).toBe("working");
    expect(stateOf({ running: false })).toBe("idle");
  });

  it("reads an agent in its own terminal from its badge", () => {
    expect(stateOf({ terminal: "working" })).toBe("working");
    expect(stateOf({ terminal: "needs" })).toBe("asking");
    expect(stateOf({ terminal: "idle" })).toBe("idle");
    expect(stateOf({ terminal: "ended" })).toBe("idle");
  });

  // A failure outranks the badge: the terminal may still say it is working.
  it("puts an error before whatever the terminal says", () => {
    expect(stateOf({ error: "boom", terminal: "working" })).toBe("failed");
  });

  it("calls a turn yours only when it is, so a long run stays quiet", () => {
    expect(attention("failed")).toBe(true);
    expect(attention("asking")).toBe(true);
    expect(attention("working")).toBe(false);
    expect(attention("idle")).toBe(false);
  });

  it("reads the state off an announced session as the launcher lists it", () => {
    const up: Active = { id: "s", dir: "/r/a", name: "a", agent: { terminal: "needs" } };
    expect(attention(stateOf(up.agent))).toBe(true);
  });
});
