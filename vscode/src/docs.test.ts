import { describe, expect, it } from "vitest";
import { nameProblem, title } from "./docs";

describe("a name for new work", () => {
  it("may be empty, which is a conversation on this branch", () => {
    expect(nameProblem("")).toBeUndefined();
    expect(nameProblem("   ")).toBeUndefined();
  });

  it("takes anything a branch can be made from", () => {
    expect(nameProblem("cancel-running-process")).toBeUndefined();
    expect(nameProblem("Cancel the running process")).toBeUndefined();
    expect(nameProblem("fix-1234.md")).toBeUndefined();
  });

  it("refuses a name that would slug to nothing", () => {
    expect(nameProblem("--")).toBeDefined();
    expect(nameProblem("?!")).toBeDefined();
  });
});

describe("a document's name", () => {
  it("reads as the heading the document gives itself", () => {
    expect(title("commit-memory.md")).toBe("Commit memory");
    expect(title("vscode-extension.md")).toBe("Vscode extension");
  });

  it("survives a name with no dashes", () => {
    expect(title("files.md")).toBe("Files");
  });
});
