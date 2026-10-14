import { describe, expect, it } from "vitest";
import type { Machine } from "./launcher";
import { age, prunable } from "./pruneremote";

const now = Date.parse("2026-10-05T12:00:00Z");

const machine = {
  machine: "ade@ade-owen",
  repos: [
    {
      path: "/home/ade/code/a/ade",
      name: "ade",
      owner: "owenrumney",
      checkouts: [
        {
          path: "/home/ade/code/a/ade",
          name: "ade",
          branch: "main",
          sessions: [
            { id: "old", agent: "claude", dir: "", name: "", branch: "main", lastUsed: "2026-09-25T16:27:02Z" },
            { id: "live", agent: "claude", dir: "", name: "", branch: "main", live: true, lastUsed: "2026-10-05T11:00:00Z" },
            { id: "recent", agent: "claude", dir: "", name: "", branch: "x", lastUsed: "2026-10-04T10:00:00Z" },
          ],
        },
      ],
    },
  ],
} as unknown as Machine;

describe("what a machine is holding", () => {
  it("lists the finished conversations, oldest first", () => {
    const rows = prunable(machine, now);
    expect(rows.map((r) => r.session.id)).toEqual(["old", "recent"]);
  });

  // A running conversation is not a pile-up, and deleting one loses work.
  it("leaves out the one that is running", () => {
    expect(prunable(machine, now).some((r) => r.session.live)).toBe(false);
  });

  it("says how long ago each was used", () => {
    expect(age("2026-09-25T16:27:02Z", now)).toBe("9 days ago");
    expect(age("2026-10-05T09:00:00Z", now)).toBe("3 hours ago");
    expect(age(undefined, now)).toBe("never used");
  });
});
