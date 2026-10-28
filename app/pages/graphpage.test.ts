import { describe, expect, it } from "vitest";
import {
  busy,
  type GraphCheckout,
  type GraphRepo,
  page,
  running,
  state,
} from "./graphpage";

const checkout = (
  name: string,
  more: Partial<GraphCheckout> = {},
): GraphCheckout => ({
  path: `/code/${name}`,
  name,
  sessions: 0,
  ...more,
});

const repo = (name: string, checkouts: GraphCheckout[]): GraphRepo => ({
  path: `/code/${name}`,
  name,
  checkouts,
});

describe("state", () => {
  it("says what a checkout is doing", () => {
    expect(state(checkout("a"))).toBe("not running");
    expect(state(checkout("a", { server: { pid: 1 } }))).toBe("idle");
    expect(
      state(
        checkout("a", {
          server: { pid: 1, agent: { running: true, kind: "review" } },
        }),
      ),
    ).toBe("working: review");
  });
});

describe("busy", () => {
  it("drops a checkout nobody has opened, and a repository left with none", () => {
    const got = busy([
      repo("ade", [checkout("main", { sessions: 2 }), checkout("spare")]),
      repo("quiet", [checkout("main")]),
    ]);
    expect(got.map((r) => r.name)).toEqual(["ade"]);
    expect(got[0].checkouts?.map((c) => c.name)).toEqual(["main"]);
  });
});

describe("the graph page", () => {
  it("says so when there is nothing to draw", () => {
    const text = page([repo("quiet", [checkout("main")])]);
    expect(text).toContain("Nothing is running");
    expect(text).not.toContain("mermaid");
  });

  it("draws each checkout under its repository and lists it too", () => {
    const text = page([
      repo("ade", [
        checkout("main", {
          branch: "main",
          sessions: 2,
          server: { pid: 1, agent: { running: true } },
        }),
        checkout("pr-7", { branch: "fix/thing", sessions: 1 }),
      ]),
    ]);
    expect(text).toContain('r0(["ade"])');
    expect(text).toContain('r0 --> r0c0["main<br/>working"]');
    expect(text).toContain("class r0c0 working");
    expect(text).toContain("| ade | pr-7 | fix/thing | not running | 1 |");
  });

  it("keeps a branch name from breaking the node it is in", () => {
    const text = page([
      repo("x", [checkout("c", { branch: 'a"b[c]', sessions: 1 })]),
    ]);
    expect(text).toContain('r0c0["a b c<br/>not running"]');
  });
});

describe("running", () => {
  it("is only what has a server, working ones first", () => {
    const got = running([
      repo("ade", [
        checkout("idle", { server: { pid: 1 } }),
        checkout("quiet", { sessions: 3 }),
        checkout("busy", { server: { pid: 2, agent: { running: true } } }),
      ]),
    ]);
    expect(got.map((r) => r.checkout.name)).toEqual(["busy", "idle"]);
    expect(got[0].repo.name).toBe("ade");
  });

  it("is empty when nothing is up", () => {
    expect(running([repo("ade", [checkout("a", { sessions: 2 })])])).toEqual(
      [],
    );
  });
});
