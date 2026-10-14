import { describe as group, expect, it } from "vitest";
import {
  describe,
  dueFrom,
  icon,
  overdue,
  todoFrom,
  waiting,
  when,
  type Todo,
} from "./todos";

const now = new Date("2026-10-08T12:00:00Z");

function todo(over: Partial<Todo> = {}): Todo {
  return { id: "t1", text: "ring the bell", ...over };
}

group("a date reads the way you would say it", () => {
  it("names today and tomorrow", () => {
    expect(when("2026-10-08T17:00:00Z", now)).toBe("today");
    expect(when("2026-10-09T09:00:00Z", now)).toBe("tomorrow");
  });

  it("says how late, not when", () => {
    expect(when("2026-10-07T09:00:00Z", now)).toBe("yesterday");
    expect(when("2026-10-05T09:00:00Z", now)).toBe("3 days late");
  });

  it("counts the near days and dates the far ones", () => {
    expect(when("2026-10-11T09:00:00Z", now)).toBe("in 3 days");
    expect(when("2026-11-20T09:00:00Z", now)).toContain("20");
  });
});

group("typing a date costs one word", () => {
  it("takes the words you would use", () => {
    expect(dueFrom("tomorrow", now)?.slice(0, 10)).toBe("2026-10-09");
    expect(dueFrom("in 2 days", now)?.slice(0, 10)).toBe("2026-10-10");
    expect(dueFrom("next week", now)?.slice(0, 10)).toBe("2026-10-15");
  });

  it("takes a written date too", () => {
    expect(dueFrom("2026-12-01", now)?.slice(0, 10)).toBe("2026-12-01");
  });

  it("clears the date on none, and refuses what it cannot read", () => {
    expect(dueFrom("none", now)).toBe("");
    expect(dueFrom("   ", now)).toBe("");
    expect(dueFrom("sometime soonish", now)).toBeUndefined();
  });
});

group("a row says where it came from and what became of it", () => {
  it("shows the date, the checkout and the work", () => {
    const line = describe(
      todo({ due: "2026-10-09T09:00:00Z", repo: "ade", started: "s4" }),
      now,
    );
    expect(line).toBe("tomorrow · ade · in progress");
  });

  it("names an agent that filed one, so a chatty one is visible", () => {
    expect(describe(todo({ from: "agent" }), now)).toBe("from the agent");
    expect(describe(todo({ from: "you" }), now)).toBe("");
  });

  it("marks late, started and done apart", () => {
    expect(icon(todo({ due: "2026-10-01T09:00:00Z" }), now)).toBe("warning");
    expect(icon(todo({ started: "s1" }), now)).toBe("sync");
    expect(icon(todo({ doneAt: "2026-10-08T09:00:00Z" }), now)).toBe("check");
  });
});

group("the badge counts what is still open", () => {
  it("leaves out what is finished", () => {
    const items = [todo(), todo({ id: "t2", doneAt: "2026-10-08T09:00:00Z" })];
    expect(waiting(items)).toBe(1);
  });

  it("never calls a finished item late", () => {
    const late = { due: "2026-10-01T09:00:00Z", doneAt: "2026-10-02T09:00:00Z" };
    expect(overdue(todo(late), now)).toBe(false);
  });
});

group("a todo from a review thread", () => {
  it("takes the claim's first line, under #review, and notes the place", () => {
    const made = todoFrom({
      file: "internal/web/web.go",
      line: 42,
      comments: [{ body: "\n- the handler ignores the error\nmore words" }, { body: "a reply" }],
    });
    expect(made).toEqual({ text: "the handler ignores the error #review", note: "internal/web/web.go:42" });
  });

  it("keeps a long claim to one short line", () => {
    const made = todoFrom({ file: "a.go", line: 1, comments: [{ body: "x".repeat(200) }] });
    expect(made.text).toBe(`${"x".repeat(89)}… #review`);
    expect(todoFrom({ file: "a.go", line: 1, comments: [] }).text).toBe("#review");
  });
});
