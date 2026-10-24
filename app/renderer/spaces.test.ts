import { describe, expect, it } from "vitest";
import type { Repo } from "./real";
import { designs } from "./real";
import { BAR, inBarOrder } from "./model";
import { spaces, within, type Space } from "./spaces";

const repos = [
  { name: "ade", owner: "owenrumney", path: "/c/ade", checkouts: [] },
  { name: "coast", owner: "infracost", path: "/c/coast", checkouts: [] },
  { name: "scratch-1", path: "/c/scratch-1", checkouts: [] },
] as unknown as Repo[];

const space = (repo: string, extra: Partial<Space> = {}): Space => ({
  repo,
  sessions: 0,
  here: true,
  ...extra,
});

describe("what the open spaces cover", () => {
  it("is every repository when none is open: opening none is not asking to see less", () => {
    expect(within(repos, [])).toEqual(repos);
  });

  it("is the open ones, in the order they were opened", () => {
    const got = within(repos, [space("infracost/coast"), space("owenrumney/ade")]);
    expect(got.map((r) => r.name)).toEqual(["coast", "ade"]);
  });

  it("leaves out an open space this machine has no clone of", () => {
    expect(within(repos, [space("nobody/nowhere")])).toEqual([]);
  });
});

describe("the Spaces list", () => {
  it("offers Close on one that is here and Clone on one that is not", () => {
    const place = spaces([
      space("owenrumney/ade", { sessions: 3, designs: ["a.md"] }),
      space("nobody/nowhere", { here: false }),
    ]);
    expect(place.rows[0].acts?.[0]).toMatchObject({ label: "Close", what: "space-close" });
    expect(place.rows[0].meta).toBe("3 sessions · 1 designs");
    expect(place.rows[1].acts?.[0]).toMatchObject({ label: "Clone", what: "space-clone" });
    expect(place.rows[1].tone).toBe("bad");
  });

  it("says how to open one rather than drawing an empty list", () => {
    expect(spaces([]).note).toContain("Open a space");
  });
});

describe("the Design tab", () => {
  it("groups every open space's documents by space, and keeps the store", () => {
    const place = designs(
      [{ name: "stored", meta: {}, modified: "" }] as never,
      ["ignored.md"],
      "/c/ade",
      [
        space("owenrumney/ade", { path: "/c/ade", designs: ["spaces.md"] }),
        space("infracost/coast", { path: "/c/coast", designs: ["pricing.md"] }),
      ],
    );
    expect(place.rows.map((r) => r.group)).toEqual([
      "owenrumney/ade",
      "infracost/coast",
      "Shared store",
    ]);
    // The checkout is in the id: a design read against the wrong one is a
    // 404 at best and another repository's document at worst.
    expect(place.rows[1].id).toBe("space:/c/coast|pricing.md");
  });

  it("falls back to the picked checkout when no space is open", () => {
    const place = designs([], ["here.md"], "/c/ade", []);
    expect(place.rows[0].id).toBe("here:here.md");
    expect(place.rows[0].group).toBe("In ade");
  });
});

// A chat in ade is ade open, whether or not anybody opened it. Closing is
// not offered: it would come back the moment you looked at the session.
describe("the space you are in", () => {
  it("is marked, and has nothing to close", () => {
    const place = spaces([space("owenrumney/ade", { current: true, sessions: 2 })]);
    expect(place.rows[0].meta).toContain("you are here");
    expect(place.rows[0].acts).toEqual([]);
  });

  it("still offers Close on one that was opened by hand", () => {
    const place = spaces([space("infracost/coast")]);
    expect(place.rows[0].acts?.[0]).toMatchObject({ what: "space-close" });
  });
});

// The bar's order is also what the digits mean, so it is one list and the
// places are sorted into it rather than built in it.
describe("the activity bar", () => {
  it("opens on notifications, then spaces", () => {
    expect(BAR.slice(0, 5)).toEqual(["waiting", "spaces", "sessions", "designs", "prs"]);
  });

  it("puts the places in that order whatever order they were built in", () => {
    const made = ["team", "spaces", "waiting", "prs"].map((id) => ({ id }) as never);
    expect(inBarOrder(made).map((p) => p.id)).toEqual(["waiting", "spaces", "prs", "team"]);
  });

  it("keeps one the bar does not name, at the end", () => {
    const made = [{ id: "mystery" }, { id: "waiting" }] as never[];
    expect(inBarOrder(made).map((p) => p.id)).toEqual(["waiting", "mystery"]);
  });
});
