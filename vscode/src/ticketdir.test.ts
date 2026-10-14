import { describe, expect, it } from "vitest";
import { choices, detail } from "./ticketdir";
import type { Repo } from "./launcher";

function repo(path: string, name: string, owner = "owenrumney"): Repo {
  return { path, name, owner, checkouts: null };
}

describe("which repository a ticket starts in", () => {
  const list = [
    repo("/w/mdview", "mdview"),
    repo("/w/ade", "ade"),
    repo("/w/squealer", "squealer"),
  ];

  it("puts this window's repository first", () => {
    const got = choices(list, "/w/ade");
    expect(got[0].path).toBe("/w/ade");
    expect(got[0].description).toBe("this window");
    expect(got[0].here).toBe(true);
  });

  it("keeps the rest in the order the server gave them", () => {
    expect(choices(list, "/w/ade").map((c) => c.path)).toEqual([
      "/w/ade",
      "/w/mdview",
      "/w/squealer",
    ]);
  });

  it("offers every repository when the window is on none of them", () => {
    const got = choices(list, "/w/elsewhere");
    expect(got).toHaveLength(3);
    expect(got.some((c) => c.here)).toBe(false);
  });

  it("names a repository by owner and name, the path as the detail", () => {
    const got = choices([repo("/w/ade", "ade")], undefined);
    expect(got[0].label).toBe("owenrumney/ade");
    expect(got[0].detail).toBe("/w/ade");
  });

  it("falls back to the path when the server named neither", () => {
    expect(choices([repo("/w/x", "", "")], undefined)[0].label).toBe("/w/x");
  });
});

describe("what the confirmation says", () => {
  it("says the window stays put when the ticket starts elsewhere", () => {
    expect(detail("Fix the thing", "/w/mdview", false)).toContain(
      "This window stays where it is",
    );
  });

  it("does not say that when it starts here", () => {
    const said = detail("Fix the thing", "/w/ade", true);
    expect(said).toContain("/w/ade");
    expect(said).not.toContain("This window stays");
  });
});
