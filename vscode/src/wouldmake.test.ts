import { describe, expect, it } from "vitest";
import { homedir } from "node:os";
import { join } from "node:path";
import { branchFor, tilde, worktreeFor, wouldMake } from "./wouldmake";

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

  it("is said as you type, and not for a name that makes nothing", () => {
    expect(wouldMake("Cart Redesign", "/srv/code/ade")?.message).toBe(
      "Branch cart-redesign · worktree /srv/code/ade-cart-redesign",
    );
    expect(wouldMake("---", "/srv/code/ade")).toBeUndefined();
  });

  it("writes home as a tilde", () => {
    expect(tilde(join(homedir(), "code", "ade-cart"))).toBe("~/code/ade-cart");
    expect(tilde("/srv/code/ade-cart")).toBe("/srv/code/ade-cart");
  });
});
