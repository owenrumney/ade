import { describe, expect, it } from "vitest";
import { plan } from "./newsession/plan";
import {
  ROWS,
  fresh,
  held,
  home,
  missing,
  onto,
  rowFor,
  summary,
  tick,
  type Chosen,
} from "./newsession/view";

const chose = (part: Partial<Chosen> = {}): Chosen => ({ ...fresh, ...part });

describe("what the screen will make", () => {
  it("lists a worktree per ticked repository, home first", () => {
    const made = plan(
      chose({ name: "cancel button", repos: ["/code/ade", "/code/worker"] }),
    );

    expect(made.branch).toBe("cancel-button");
    expect(made.doc).toBe("cancel-button.md");
    expect(made.makes).toEqual([
      { repo: "ade", path: "/code/ade-cancel-button", home: true },
      { repo: "worker", path: "/code/worker-cancel-button", home: false },
    ]);
  });

  it("is one branch across all of them, which is what spans them", () => {
    const made = plan(chose({ name: "FIX-623", repos: ["/code/a", "/code/b"] }));
    expect(made.makes.map((one) => one.path)).toEqual([
      "/code/a-fix-623",
      "/code/b-fix-623",
    ]);
  });

  it("makes no worktree for a conversation on the branch already checked out", () => {
    const made = plan(chose({ mode: "direct", repos: ["/code/ade"] }));
    expect(made.branch).toBe("");
    expect(made.makes).toEqual([{ repo: "ade", path: "/code/ade", home: true }]);
  });

  it("takes the branch from the document a design session carries on", () => {
    const made = plan(
      chose({
        mode: "design",
        design: "FIX-623-cancel.md",
        repos: ["/code/ade"],
      }),
    );

    expect(made.branch).toBe("fix-623-cancel");
    expect(made.doc).toBe("FIX-623-cancel.md");
    expect(made.written).toBe(true);
    expect(made.makes).toEqual([
      { repo: "ade", path: "/code/ade-fix-623-cancel", home: true },
    ]);
  });

  it("carries a written document across every repository ticked", () => {
    const made = plan(
      chose({
        mode: "design",
        design: "cart-redesign.md",
        repos: ["/code/coast", "/code/worker"],
      }),
    );
    expect(made.makes.map((one) => one.path)).toEqual([
      "/code/coast-cart-redesign",
      "/code/worker-cart-redesign",
    ]);
  });

  it("has nothing to show before the work is named", () => {
    expect(plan(chose({ repos: ["/code/ade"] })).makes).toEqual([
      { repo: "ade", path: "/code/ade", home: true },
    ]);
    expect(plan(chose({ repos: ["/code/ade"] })).branch).toBe("");
  });
});

describe("a document carried to another machine", () => {
  it("is marked carried only when it is this machine's", () => {
    const there = chose({ mode: "design", design: "cart.md", repos: ["/a"] });
    expect(plan(there).carried).toBe(false);
    expect(plan(there).written).toBe(true);

    const here = chose({ ...there, ship: true });
    expect(plan(here).carried).toBe(true);
  });

  it("is not carried once the row no longer names a document", () => {
    const left = chose({
      mode: "work",
      name: "cancel",
      design: "cart.md",
      ship: true,
      repos: ["/a"],
    });
    expect(
      plan(left).carried,
      "a switch to Work must not ship the document left behind",
    ).toBe(false);
  });

  it("tells the two machines' copies of one name apart", () => {
    const docs = [
      { file: "cart.md", ship: true },
      { file: "cart.md", ship: false },
    ];
    const chosen = chose({ mode: "design", design: "cart.md", ship: true });

    expect(held(docs, chosen)).toEqual({ file: "cart.md", ship: true });
    expect(held(docs, chose({ ...chosen, ship: false }))).toEqual({
      file: "cart.md",
      ship: false,
    });
    expect(held(docs, chose({ ...chosen, design: "gone.md" }))).toBeUndefined();
  });
});

describe("ticking repositories", () => {
  it("keeps the order they were ticked in, so home does not move", () => {
    let chosen = chose({ repos: ["/a", "/b", "/c"] });
    chosen = chose({ repos: tick(chosen, "/b") });
    expect(chosen.repos).toEqual(["/a", "/c"]);
    expect(home(chosen)).toBe("/a");
  });

  it("replaces rather than adds where only one repository makes sense", () => {
    const chosen = chose({ mode: "direct", repos: ["/a"] });
    expect(tick(chosen, "/b")).toEqual(["/b"]);
  });

  it("adds where the row spans repositories", () => {
    expect(tick(chose({ repos: ["/a"] }), "/b")).toEqual(["/a", "/b"]);
  });

  it("drops all but home when the row changes to one that works in one", () => {
    const chosen = chose({ repos: ["/a", "/b"] });
    expect(onto(chosen, "direct")).toEqual(["/a"]);
    expect(onto(chosen, "design")).toEqual(["/a", "/b"]);
    expect(onto(chosen, "scratch")).toEqual([]);
  });
});

describe("what the screen still needs", () => {
  it("wants a name for work and nothing for a direct session", () => {
    expect(missing(chose({ repos: ["/a"] }))).toContain("Name");
    expect(missing(chose({ mode: "direct", repos: ["/a"] }))).toBeUndefined();
  });

  it("wants a repository for anything that runs in one", () => {
    expect(missing(chose({ name: "x" }))).toContain("repository");
    expect(missing(chose({ mode: "scratch", name: "spike" }))).toBeUndefined();
  });

  it("wants a document before a session can carry one on", () => {
    expect(missing(chose({ mode: "design", repos: ["/a"] }))).toContain(
      "design",
    );
    expect(
      missing(chose({ mode: "design", repos: ["/a"], design: "cart.md" })),
    ).toBeUndefined();
  });

  it("asks for the repository first, since the document list is its own", () => {
    expect(missing(chose({ mode: "design" }))).toContain("repository");
  });

  it("names every row ade already had a door for", () => {
    expect(ROWS.map((row) => row.mode)).toEqual([
      "work",
      "plan",
      "design",
      "direct",
      "scratch",
    ]);
    expect(rowFor("scratch").picks).toBe(false);
  });
});

describe("the line under the button", () => {
  it("says the branch and where it goes", () => {
    const chosen = chose({ name: "cancel", repos: ["/code/ade", "/code/worker"] });
    expect(summary(chosen, plan(chosen))).toBe("cancel in ade and 1 more.");
  });

  it("says a conversation when nothing is branched", () => {
    const chosen = chose({ mode: "direct", repos: ["/code/ade"] });
    expect(summary(chosen, plan(chosen))).toBe("A conversation in ade.");
  });
});
