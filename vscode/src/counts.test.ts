import { describe, expect, it } from "vitest";
import type { Repo } from "./launcher";
import { label, sessionsIn } from "./counts";

describe("label", () => {
  it("is the number on its own", () => {
    expect(label(3)).toBe("3");
  });

  it("names what it counted when the rows are something else", () => {
    expect(label(3, "open")).toBe("3 open");
  });

  // A column of "0" on every view you are not using is noise.
  it("is nothing at zero", () => {
    expect(label(0)).toBe("");
    expect(label(0, "open")).toBe("");
    expect(label(-1)).toBe("");
  });
});

describe("sessionsIn", () => {
  it("counts sessions, not repositories or checkouts", () => {
    const repos = [
      {
        checkouts: [{ sessions: [{}, {}] }, { sessions: [{}] }],
      },
      { checkouts: [{ sessions: [{}] }] },
    ] as unknown as Repo[];
    expect(sessionsIn(repos)).toBe(4);
  });

  // The launcher sends null for a checkout it has not read yet.
  it("treats an unread checkout as none rather than throwing", () => {
    const repos = [
      { checkouts: null },
      { checkouts: [{ sessions: null }] },
    ] as unknown as Repo[];
    expect(sessionsIn(repos)).toBe(0);
  });
});
