import { describe, expect, it } from "vitest";
import { rootsFor } from "./current";

const folders = ["/w/ade", "/w/go-sarif", "/w/lazytrivy"];

describe("which checkout the window attaches to", () => {
  it("puts the picked one first", () => {
    expect(rootsFor(folders, "/w/go-sarif", "/w/ade/main.go")).toEqual([
      "/w/go-sarif",
      "/w/ade",
      "/w/lazytrivy",
    ]);
  });

  it("falls back to the folder nearest the active editor", () => {
    expect(rootsFor(folders, undefined, "/w/lazytrivy/cmd/x.go")[0]).toBe(
      "/w/lazytrivy",
    );
  });

  it("ignores a pick the window does not hold", () => {
    expect(rootsFor(folders, "/elsewhere/repo", "/w/ade/main.go")[0]).toBe(
      "/w/ade",
    );
  });
});
