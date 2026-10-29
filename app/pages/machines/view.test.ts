import { describe, expect, it } from "vitest";
import { listed, said, shown, stateOf } from "./view";

describe("a machine row", () => {
  it("offers Connect when it is off and Disconnect when it is not", () => {
    expect(shown({ name: "pi@pi5.local" }).act).toBe("connect");
    expect(shown({ name: "pi@pi5.local", connected: true, state: "connected" }).act).toBe(
      "disconnect",
    );
  });

  it("says quiet rather than connected when the last answer failed", () => {
    const quiet = shown({ name: "a", connected: true, state: "quiet" });
    expect(stateOf({ name: "a", connected: true, state: "quiet" })).toBe("quiet");
    // Still dialled, so letting it go is still the verb.
    expect(quiet.act).toBe("disconnect");
    expect(said(quiet)).toContain("last answer failed");
  });

  it("gives this machine no button", () => {
    const here = shown({ name: "This Mac", local: true, connected: true });
    expect(here.state).toBe("here");
    expect(here.act).toBe("");
    expect(said(here)).toContain("this machine");
  });

  it("keeps the whole of what went wrong", () => {
    const long = "ssh: connect to host pi5.local port 22: No route to host";
    expect(shown({ name: "pi", error: long }).error).toBe(long);
    // A failure recorded against work it was doing counts too.
    expect(shown({ name: "pi", failed: "worker stopped" }).error).toBe("worker stopped");
  });

  it("counts sessions and keeps the ssh name under a label", () => {
    const row = shown({
      name: "owen@bigboy.local",
      label: "bigboy",
      connected: true,
      state: "connected",
      sessions: 2,
    });
    expect(row.label).toBe("bigboy");
    expect(said(row)).toBe("connected · 2 sessions · owen@bigboy.local");
  });

  it("draws nothing for no machines", () => {
    expect(listed(undefined)).toEqual([]);
  });
});
