import { describe, expect, it } from "vitest";
import { keeper } from "./held";

describe("a part of the gather that failed this time", () => {
  it("keeps the rows it had, and still names the trouble", async () => {
    const kept = keeper();
    const trouble: Record<string, string> = {};
    expect(await kept("todos", "/code/ade", Promise.resolve([{ id: "a" }]), trouble)).toHaveLength(1);

    const again = await kept<{ id: string }>(
      "todos",
      "/code/ade",
      Promise.reject(new Error("The operation was aborted due to timeout")),
      trouble,
    );
    expect(again).toEqual([{ id: "a" }]);
    expect(trouble.todos).toContain("timeout");
  });

  it("does not hand one checkout's list to another", async () => {
    const kept = keeper();
    const trouble: Record<string, string> = {};
    await kept("todos", "/code/ade", Promise.resolve([{ id: "a" }]), trouble);
    expect(await kept("todos", "/code/other", Promise.reject(new Error("no")), trouble)).toEqual([]);
  });

  it("says why in the caller's words", async () => {
    const kept = keeper((e) => `${e.message} — older ade`);
    const trouble: Record<string, string> = {};
    await kept("jobs", "", Promise.reject(new Error("404 Not Found")), trouble);
    expect(trouble.jobs).toBe("404 Not Found — older ade");
  });
});
