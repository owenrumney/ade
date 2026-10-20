import { describe, expect, it } from "vitest";
import {
  authority,
  currentProxy,
  folderURI,
  launcherAt,
  machineWide,
  said,
  whyNotHere,
} from "./remote";

describe("addressing another machine", () => {
  it("is ade-ssh and the destination ade bookmarked", () => {
    expect(authority("pi@pi5.local")).toBe("ade-ssh+pi@pi5.local");
    expect(folderURI("pi@pi5.local", "/home/pi/code/ade")).toBe(
      "vscode-remote://ade-ssh+pi@pi5.local/home/pi/code/ade",
    );
  });

  it("names no extension to install, because there is none", () => {
    expect(said("pi5")).toBe("ade: started on pi5.");
    expect(said("pi5")).not.toContain("Remote-SSH");
  });
});

describe("a window on another machine", () => {
  it("is one session, not the machine you are sitting at", () => {
    expect(machineWide(undefined)).toBe(true);
    expect(machineWide("ade-ssh")).toBe(false);
    expect(machineWide("wsl")).toBe(false);
  });

  it("says where the thing you wanted actually is", () => {
    expect(whyNotHere("session")).toContain("Start work on another machine");
    expect(whyNotHere("surface")).toContain("Machines, jobs and disk");
    for (const what of ["session", "surface"] as const) {
      expect(whyNotHere(what)).toContain("one session on another machine");
      expect(whyNotHere(what)).not.toContain("no launcher");
    }
  });
});

describe("the launcher a session was reached through", () => {
  it("is the proxied address with the session part taken off", () => {
    expect(launcherAt("http://127.0.0.1:36448/s/abc", "worker")).toEqual({
      base: "http://127.0.0.1:36448",
      token: "worker",
    });
  });

  it("is nothing when the session was reached directly", () => {
    expect(launcherAt("http://127.0.0.1:24590", "own")).toBeUndefined();
    expect(launcherAt("not a url", "own")).toBeUndefined();
    expect(
      launcherAt("http://127.0.0.1:36448/s/abc/more", "x"),
    ).toBeUndefined();
  });
});

describe("a record whose proxied address has gone stale", () => {
  const serving = [
    { id: "live", dir: "/home/pi/code/owenrumney/comet" },
    { id: "other", dir: "/home/pi/code/elsewhere" },
  ];

  it("is replaced by the session the launcher says is serving that folder", () => {
    expect(
      currentProxy(
        "http://127.0.0.1:36448",
        "worker",
        "/home/pi/code/owenrumney/comet",
        serving,
      ),
    ).toBe("http://127.0.0.1:36448/s/live/?t=worker");
  });

  it("is nothing when that folder has no session at all", () => {
    expect(
      currentProxy(
        "http://127.0.0.1:36448",
        "worker",
        "/home/pi/code/gone",
        serving,
      ),
    ).toBeUndefined();
    expect(
      currentProxy("http://127.0.0.1:36448", "worker", "/x", []),
    ).toBeUndefined();
  });
});

// A conversation that already exists was not just started, and the sentence
// that opens a window on it should not say it was.
describe("what to say about work on another machine", () => {
  it("takes the verb from the caller", () => {
    expect(said("pi5", "that conversation is")).toBe(
      "ade: that conversation is on pi5.",
    );
  });
});
