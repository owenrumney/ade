import { describe, expect, it } from "vitest";
import type { Session } from "./launcher";
import type { Row } from "./switcher";
import { wellCount, wellRows } from "./well";

const row = (s: Partial<Session>, repo: string, waiting: boolean): Row => ({
  session: { id: "x", agent: "claude", name: "", branch: "", dir: "", ...s } as Session,
  repo,
  waiting,
  here: false,
});

describe("the well", () => {
  const sessions = [
    row({ id: "a", title: "the editor fork" }, "ade", true),
    row({ id: "b", title: "quiet one" }, "ade", false),
    row({ id: "c", branch: "feat/x" }, "tilt", true),
  ];

  it("lists what is waiting on you, nearest first", () => {
    const rows = wellRows({ sessions, offers: [{ id: "o", kind: "ask", from: "sam", question: "which port?" }], waiting: [{ id: "w", kind: "prompt", message: "then tests" }, { id: "w2", kind: "prompt", message: "and docs" }], asking: 1, hereId: "c" });
    expect(rows.map((r) => r.label)).toEqual([
      "The agent is waiting on you",
      "the editor fork asked you",
      "Answer sam · which port?",
      "Queued · then tests",
    ]);
    expect(rows[3].description).toBe("2 waiting for the agent");
  });

  it("counts the same rows, and is empty when nothing waits", () => {
    expect(wellCount({ sessions: [sessions[1]], offers: [], waiting: [], asking: 0 })).toBe(0);
    expect(wellCount({ sessions, offers: [], waiting: [], asking: 0 })).toBe(2);
  });
});

describe("a session that asked you", () => {
  const asking = (over: Partial<Row>): Row => ({
    session: { id: "a", agent: "claude", name: "n", branch: "feat/x", dir: "/r", live: true } as Session,
    repo: "ade",
    waiting: true,
    here: false,
    ...over,
  });

  it("puts the question, and takes the answer here", () => {
    const [r] = wellRows({ sessions: [asking({ asked: "Postgres or SQLite?" })], offers: [], waiting: [], asking: 0 });
    expect(r.label).toBe("feat/x: Postgres or SQLite?");
    expect(r.command).toBe("ade.answerSession");
  });

  it("still answers here when the question could not be read", () => {
    const [r] = wellRows({ sessions: [asking({})], offers: [], waiting: [], asking: 0 });
    expect(r.label).toBe("feat/x asked you");
    expect(r.description).toBe("answer it without going there");
    expect(r.command).toBe("ade.answerSession");
  });

  // A session whose server has gone cannot be spoken to, so the row offers
  // the only thing that works.
  it("opens one that is no longer running", () => {
    const row = asking({ session: { id: "a", agent: "claude", name: "n", branch: "feat/x", dir: "/r" } as Session, asked: "Which?" });
    const [r] = wellRows({ sessions: [row], offers: [], waiting: [], asking: 0 });
    expect(r.command).toBe("ade.omnibox.open");
    expect(r.description).toBe("Which?");
  });
});

// The bell's title counts what is waiting. An action listed among those rows
// is counted by the eye and not by the title, which is what read as a bug.
describe("the bell's own count", () => {
  it("is exactly the rows it lists, so the badge and the title agree", () => {
    const from = {
      sessions: [],
      offers: [{ id: "o", kind: "ask" as const, from: "sam", question: "which port?" }],
      waiting: [],
      asking: 1,
    };
    expect(wellCount(from)).toBe(wellRows(from).length);
    expect(wellCount(from)).toBe(2);
  });

  it("counts nothing when nothing waits", () => {
    expect(wellCount({ sessions: [], offers: [], waiting: [], asking: 0 })).toBe(0);
  });
});
