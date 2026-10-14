import { describe, expect, it } from "vitest";
import type { Host } from "./launcher";
import { acts, rows } from "./machinepick";

const host = (over: Partial<Host> = {}): Host => ({ name: "linux", ...over });

describe("the machine rows", () => {
  it("calls it by its label, and by its host name without one", () => {
    expect(rows([host({ label: "the pi" })])[0].label).toBe("the pi");
    expect(rows([host()])[0].label).toBe("linux");
  });

  it("says whether it is connected", () => {
    expect(rows([host({ connected: true })])[0].description).toContain(
      "connected",
    );
    expect(rows([host()])[0].description).toContain("off");
  });

  // What this window started is held here, not on the server, so the row has
  // to say it from the click: the launcher only says it once ssh has begun.
  it("shows what this window started before the launcher reports it", () => {
    const working = new Map([["linux", "connecting"]]);
    expect(rows([host()], working)[0].description).toContain("connecting");
  });

  it("carries the error the launcher reported", () => {
    expect(rows([host({ error: "no route to host" })])[0].detail).toBe(
      "no route to host",
    );
  });
});

describe("what a machine offers", () => {
  const ids = (h: Host) => acts(h).map((a) => a.id);

  it("offers connect on one that is off and disconnect on one that is on", () => {
    expect(ids(host())).toContain("connect");
    expect(ids(host({ connected: true }))).toContain("disconnect");
    expect(ids(host())).not.toContain("disconnect");
    expect(ids(host({ connected: true }))).not.toContain("connect");
  });

  // The server refuses these three on a machine that is not connected.
  it("offers the worker verbs only on a connected machine", () => {
    for (const id of ["credentials", "update", "stop"]) {
      expect(ids(host({ connected: true }))).toContain(id);
      expect(ids(host())).not.toContain(id);
    }
  });

  // A label is settings, not a connection, so it needs neither state.
  it("offers renaming and forgetting whether or not it is connected", () => {
    for (const h of [host(), host({ connected: true })]) {
      expect(ids(h)).toContain("rename");
      expect(ids(h)).toContain("forget");
    }
  });

  // This Mac is not a bookmark and has no ssh connection to drop.
  it("offers nothing on this Mac", () => {
    expect(ids(host({ local: true, connected: true }))).toEqual([]);
  });

  // Mid-connect every verb is wrong: connect again, disconnect a half-open
  // ssh, or forget a machine the launcher is still talking to.
  it("offers nothing while a machine is connecting", () => {
    expect(ids(host({ state: "connecting" }))).toEqual([]);
    expect(
      acts(rows([host()], new Map([["linux", "connecting"]]))[0].host),
    ).toEqual([]);
  });
});
