import { describe, expect, it } from "vitest";
import { branchFor, tilde, worktreeFor } from "./wouldmake";

// The cases are web.Slug's own (internal/web/worktree_test.go), lower-cased,
// plus what docs.Filename does to the name on its way there.
describe("the branch a session name makes", () => {
  it.each([
    ["cart-redesign", "cart-redesign"],
    ["cart-redesign.md", "cart-redesign"],
    ["Cart Redesign (v2)", "cart-redesign-v2"],
    ["--cart--", "cart"],
    ["designs/cart", "cart"],
    ["v1.2", "v1.2"],
    ["---", ""],
    ["   ", ""],
    ["../escape", ""],
  ])("%j makes %j", (name, branch) => {
    expect(branchFor(name)).toBe(branch);
  });
});

describe("the worktree it gets", () => {
  it("sits beside the checkout, named after it", () => {
    expect(worktreeFor("/srv/code/ade/", "cart")).toBe("/srv/code/ade-cart");
  });

  it("writes home as a tilde, and leaves a path outside it alone", () => {
    expect(tilde("/home/owen/code/ade-cart", "/home/owen")).toBe("~/code/ade-cart");
    expect(tilde("/srv/code/ade-cart", "/home/owen")).toBe("/srv/code/ade-cart");
    expect(tilde("/srv/code/ade-cart", "")).toBe("/srv/code/ade-cart");
  });
});

// A branch keeps its slashes; the directory beside the repository cannot.
it("flattens a branch name into one directory name", () => {
  expect(worktreeFor("/srv/code/ade", "feat/ADE-211")).toBe("/srv/code/ade-feat-ADE-211");
});
