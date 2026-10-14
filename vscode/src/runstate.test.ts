import { describe, expect, it } from "vitest";
import { badge, elapsed, going, offersCommit, quiet, since } from "./runstate";
import type { RunState } from "./client";

const at = Date.parse("2026-09-30T18:00:00Z");

describe("badge", () => {
  it("shows nothing when the agent is idle and nothing is queued", () => {
    expect(badge({ running: false }, 0)).toBeUndefined();
    expect(badge(undefined, 0)).toBeUndefined();
  });

  it("spins while a run is in flight", () => {
    expect(badge({ running: true, kind: "chat" }, 0)?.text).toContain(
      "sync~spin",
    );
  });

  it("turns the server's kind into words where it reads badly", () => {
    expect(badge({ running: true, kind: "commit-message" }, 0)?.text).toContain(
      "writing a commit message",
    );
  });

  // A kind nobody has named yet is shown as it came: an odd word beats none.
  it("passes an unknown kind straight through", () => {
    expect(badge({ running: true, kind: "distil" }, 0)?.text).toContain(
      "distil",
    );
  });

  it("carries the age once there is one", () => {
    expect(badge({ running: true, kind: "chat" }, 90_000)?.text).toContain(
      "1m",
    );
  });

  it("stops spinning and warns when the turn is held on an ask", () => {
    const got = badge({ running: true, kind: "chat", asking: [{}] }, 480_000);
    expect(got?.text).toContain("waiting on you");
    expect(got?.text).not.toContain("sync~spin");
    expect(got?.warn).toBe(true);
  });

  it("names a silence past a minute and warns past five", () => {
    const state = { running: true, kind: "chat" };
    expect(badge(state, 90_000, 30_000)?.text).not.toContain("quiet");
    expect(badge(state, 480_000, 180_000)?.text).toContain("quiet 3m");
    expect(badge(state, 480_000, 180_000)?.warn).toBe(false);
    expect(badge(state, 480_000, 300_000)?.warn).toBe(true);
  });

  it("measures the silence from the last event of a running turn", () => {
    const last = new Date(at).toISOString();
    expect(quiet({ running: true, lastEventAt: last }, at + 7000)).toBe(7000);
    expect(quiet({ running: false, lastEventAt: last }, at + 7000)).toBe(0);
    expect(quiet({ running: true }, at)).toBe(0);
  });

  it("says what is waiting when the agent is idle with a queue", () => {
    const got = badge({ running: false, queued: 2 }, 0);
    expect(got?.text).toContain("2 queued");
    expect(got?.tooltip).toContain("Run the queue");
  });

  it("reports a failed run rather than falling silent", () => {
    const got = badge({ running: false, error: "the model refused" }, 0);
    expect(got?.warn).toBe(true);
    expect(got?.tooltip).toContain("the model refused");
  });

  it("names the model and the sandbox in the tooltip, not the bar", () => {
    const got = badge(
      { running: true, kind: "chat", model: "opus", sandbox: true },
      0,
    );
    expect(got?.tooltip).toContain("opus");
    expect(got?.tooltip).toContain("sandboxed");
    expect(got?.text).not.toContain("opus");
  });
});

describe("since", () => {
  const started = new Date(at).toISOString();

  it("is the age of a running turn", () => {
    expect(since({ running: true, startedAt: started }, at + 5000)).toBe(5000);
  });

  it("is nothing for an idle agent", () => {
    expect(since({ running: false, startedAt: started }, at + 5000)).toBe(0);
  });

  // A clock behind the server's reads as a negative age, which is no age.
  it("never goes backwards", () => {
    expect(since({ running: true, startedAt: started }, at - 5000)).toBe(0);
  });

  it("is nothing when the server named no start", () => {
    expect(since({ running: true }, at)).toBe(0);
    expect(since({ running: true, startedAt: "not a date" }, at)).toBe(0);
  });
});

describe("elapsed", () => {
  it("counts seconds below a minute", () => {
    expect(elapsed(0)).toBe("0s");
    expect(elapsed(59_000)).toBe("59s");
  });

  it("counts minutes after that, because seconds stop mattering", () => {
    expect(elapsed(60_000)).toBe("1m");
    expect(elapsed(59 * 60_000)).toBe("59m");
  });

  it("counts hours for a run that has been going all afternoon", () => {
    expect(elapsed(90 * 60_000)).toBe("1h30m");
  });
});

describe("what the agent is doing", () => {
  it("says the current tool call after the age", () => {
    const got = badge({ running: true, kind: "chat", doing: "Bash: git diff" }, 0);
    expect(got?.text).toBe("$(sync~spin) ade · working · Bash: git diff");
  });

  it("lists the files touched so far in the tooltip, not the text", () => {
    const got = badge(
      { running: true, kind: "chat", touched: ["a.go", "b.ts"] },
      0,
    );
    expect(got?.tooltip).toContain("2 files touched: a.go, b.ts");
    expect(got?.text).not.toContain("a.go");
  });
});

describe("a turn the agent started itself", () => {
  it("is named, not shown as its kind", () => {
    expect(badge({ running: true, kind: "wake" }, 0)?.text).toBe(
      "$(sync~spin) ade · carrying on by itself",
    );
  });
});

describe("a sub-agent at work", () => {
  it("keeps the bar alive after the run that called it ended", () => {
    const got = badge(
      {
        running: false,
        children: [{ kind: "agent", name: "Inventory ade features", doing: "Read: agent.go", since: "2026-10-11T19:00:00Z" }],
      },
      0,
    );
    expect(got?.text).toBe(
      "$(sync~spin) ade · 1 agent working · Inventory ade features › Read: agent.go",
    );
    expect(got?.warn).toBe(false);
  });

  it("is what a running turn is on, ahead of its own last call", () => {
    const got = badge(
      {
        running: true,
        kind: "chat",
        doing: "Agent: Inventory ade features",
        children: [
          { kind: "agent", name: "Inventory ade features", doing: "Grep internal/web", since: "2026-10-11T19:00:00Z" },
          { kind: "agent", name: "Second", since: "2026-10-11T19:00:00Z" },
        ],
      },
      0,
    );
    expect(got?.text).toBe(
      "$(sync~spin) ade · working · Inventory ade features › Grep internal/web +1",
    );
  });
});

describe("a backgrounded command", () => {
  const task = { kind: "task", name: "go test ./...", doing: "ok internal/web", since: "2026-10-11T19:00:00Z" };

  it("is a task running, not an agent working", () => {
    expect(badge({ running: false, children: [task] }, 0)?.text).toBe(
      "$(sync~spin) ade · 1 task running · go test ./... › ok internal/web",
    );
  });

  it("counted with an agent, says neither word", () => {
    const got = badge(
      { running: false, children: [task, { kind: "agent", name: "Explore", since: "2026-10-11T19:00:00Z" }] },
      0,
    );
    expect(got?.text).toContain("2 working");
  });
});

describe("a turn that has finished", () => {
  it("says what it changed, where the bar was blank", () => {
    const got = badge({ running: false, finished: { files: 3, at: "2026-10-11T19:00:00Z" } }, 0);
    expect(got?.text).toBe("$(check) ade · 3 files changed");
    expect(got?.warn).toBe(false);
  });

  it("says so when it changed nothing", () => {
    expect(badge({ running: false, finished: { files: 0, at: "2026-10-11T19:00:00Z" } }, 0)?.text).toBe(
      "$(check) ade · nothing changed",
    );
  });

  it("is waiting on you when it ended by asking", () => {
    const got = badge({ running: false, finished: { files: 2, asking: true, at: "2026-10-11T19:00:00Z" } }, 0);
    expect(got?.text).toBe("$(question) ade · waiting on you");
    expect(got?.warn).toBe(true);
  });

  it("gives way to work still going", () => {
    const got = badge(
      {
        running: false,
        finished: { files: 3, at: "2026-10-11T19:00:00Z" },
        children: [{ kind: "task", name: "go test ./...", since: "2026-10-11T19:00:00Z" }],
      },
      0,
    );
    expect(got?.text).toContain("1 task running");
  });
});

describe("a run going round", () => {
  const round = (over: Partial<RunState>) =>
    going({ running: true, kind: "chat", doing: "Bash: go test ./...", ...over }, at);

  it("is not working, however busy it looks", () => {
    const got = round({ repeats: 4, repeatedAt: new Date(at - 660_000).toISOString() });
    expect(got?.text).toBe("$(issue-reopened) ade \u00b7 looks stuck \u00b7 4\u00d7 Bash: go test ./... \u00b7 11m");
    expect(got?.warn).toBe(true);
  });

  it("gives a retry the benefit of the doubt", () => {
    expect(round({ repeats: 2 })).toBeUndefined();
  });

  it("needs something to name", () => {
    expect(round({ repeats: 5, doing: undefined })).toBeUndefined();
  });

  // A held tool call is waiting on you, which is the truer thing to say.
  it("gives way to a question", () => {
    const state = { running: true, kind: "chat", doing: "Bash: go test ./...", repeats: 5, asking: [{}] };
    expect(badge(state, 0)?.text).toContain("waiting on you");
  });
});

describe("a finished turn with something to commit", () => {
  const done = (over: Partial<RunState>) =>
    ({ running: false, finished: { files: 3, at: "2026-10-11T19:00:00Z" }, ...over }) as RunState;

  it("offers the commit, and says so", () => {
    expect(offersCommit(done({}))).toBe(true);
    expect(badge(done({}), 0)?.tooltip).toContain("commit message");
  });

  it("offers nothing when the turn changed nothing", () => {
    expect(offersCommit(done({ finished: { files: 0, at: "x" } }))).toBe(false);
  });

  // Both of these have something truer to say about the same moment.
  it("gives way to a question and to work still going", () => {
    expect(offersCommit(done({ finished: { files: 3, asking: true, at: "x" } }))).toBe(false);
    expect(offersCommit(done({ children: [{ kind: "task", name: "go test", since: "x" }] }))).toBe(false);
  });

  it("offers nothing while a turn runs", () => {
    expect(offersCommit(done({ running: true }))).toBe(false);
  });
});
