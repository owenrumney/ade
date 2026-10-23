import { describe, expect, it } from "vitest";
import { bookmarkedHosts, isLoopback, parseSshConfig } from "./localnetwork";

describe("the Local Network trigger", () => {
  it("reads the bookmarked hosts", () => {
    expect(bookmarkedHosts('{"hosts":[{"name":"pi@pi5.local"},{"label":"x"},{"name":"bigboy"}]}')).toEqual([
      "pi@pi5.local",
      "bigboy",
    ]);
    expect(bookmarkedHosts("{}")).toEqual([]);
    expect(bookmarkedHosts("not json")).toEqual([]);
  });

  it("dials an ssh alias at its real host and port", () => {
    const output = "user pi\nhostname 172.16.0.107\nport 2222\n";
    expect(parseSshConfig(output, { host: "pi5", port: 22 })).toEqual({ host: "172.16.0.107", port: 2222 });
    expect(parseSshConfig("", { host: "pi5.local", port: 22 })).toEqual({ host: "pi5.local", port: 22 });
  });

  it("skips loopback, which never makes macOS ask", () => {
    for (const h of ["localhost", "127.0.0.1", "::1"]) expect(isLoopback(h)).toBe(true);
    for (const h of ["pi5.local", "172.16.0.107"]) expect(isLoopback(h)).toBe(false);
  });
});
