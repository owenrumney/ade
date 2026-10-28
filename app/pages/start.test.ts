import { describe, expect, it } from "vitest";
import {
  candidates,
  findAde,
  lastLine,
  launcherArgs,
  missing,
  serverArgs,
  startLog,
} from "./start";

const env = { GOBIN: "/gobin", GOPATH: "/gopath" } as NodeJS.ProcessEnv;

describe("finding ade", () => {
  it("looks where a Go binary lands, because Finder gives a minimal PATH", () => {
    const where = candidates(env, "/home/owen");
    expect(where).toContain("/gobin/ade");
    expect(where).toContain("/gopath/bin/ade");
    expect(where).toContain("/opt/homebrew/bin/ade");
  });

  it("prefers ADE_BIN over anything it would guess", () => {
    expect(
      candidates({ ...env, ADE_BIN: "/custom/ade" }, "/home/owen")[0],
    ).toBe("/custom/ade");
  });

  it("takes the first one that is actually executable", () => {
    expect(
      findAde(env, "/home/owen", (p) => p === "/opt/homebrew/bin/ade"),
    ).toBe("/opt/homebrew/bin/ade");
  });

  it("falls back to the bare name, which works from a terminal", () => {
    expect(findAde(env, "/home/owen", () => false)).toBe("ade");
  });
});

describe("what it is started with", () => {
  it("keeps the launcher alive while only the editor is watching", () => {
    expect(launcherArgs).toContain("--idle");
    expect(launcherArgs[launcherArgs.indexOf("--idle") + 1]).toBe("0");
  });

  it("opens no browser, because this window is the browser", () => {
    expect(launcherArgs).toContain("--no-open");
    expect(serverArgs).toContain("--no-open");
  });
});

describe("a spawn that failed", () => {
  it("is a machine with no ade only when the binary was not there", () => {
    expect(missing({ name: "", message: "", code: "ENOENT" })).toBe(true);
    expect(missing({ name: "", message: "", code: "EACCES" })).toBe(false);
    expect(missing({ name: "", message: "" })).toBe(false);
  });
});

describe("why a server would not start", () => {
  it("is the last thing it said", () => {
    expect(lastLine("starting\nade: open chat store: timeout\n")).toBe(
      "ade: open chat store: timeout",
    );
    expect(lastLine("")).toBe("");
  });

  it("is logged per checkout, so two windows do not share one file", () => {
    expect(startLog("/a", "/tmp")).not.toBe(startLog("/b", "/tmp"));
    expect(startLog("/a", "/tmp")).toBe(startLog("/a", "/tmp"));
  });
});
