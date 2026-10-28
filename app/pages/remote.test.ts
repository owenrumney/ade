import { describe, expect, it } from "vitest";
import { currentProxy, launcherAt } from "./remote";

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
