import { describe, expect, it } from "vitest";
import { between } from "./shellpath";

describe("the login shell's PATH", () => {
  it("is read from between the marks, whatever the rc files print", () => {
    expect(between("welcome!\n__ADE_PATH__/opt/homebrew/bin:/usr/bin__ADE_PATH__")).toBe(
      "/opt/homebrew/bin:/usr/bin",
    );
  });

  it("is nothing when the shell said nothing usable", () => {
    expect(between("")).toBeUndefined();
    expect(between("__ADE_PATH____ADE_PATH__")).toBeUndefined();
    expect(between("__ADE_PATH__/usr/bin")).toBeUndefined();
  });
});
