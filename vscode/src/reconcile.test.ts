import { describe, expect, it } from "vitest";
import type { Thread } from "./client";
import { badge, label, plan, refused, relevant, verdict } from "./reconcile";

function thread(over: Partial<Thread> = {}): Thread {
  return {
    id: "t1",
    file: "internal/web/web.go",
    side: "right",
    line: 12,
    comments: [{ id: "c1", author: "agent", body: "wrong", at: "" }],
    ...over,
  };
}

describe("what can be drawn", () => {
  it("is a thread pinned to a line of a file", () => {
    expect(relevant(thread())).toBe(true);
  });

  it("is not a thread about the change as a whole", () => {
    expect(relevant(thread({ scope: "repo", file: "", line: 0 }))).toBe(false);
  });

  it("is not a thread on a line this change deleted", () => {
    // There is no line in the editor to hang it on.
    expect(relevant(thread({ side: "left" }))).toBe(false);
  });
});

describe("the plan", () => {
  it("draws a thread that is new", () => {
    const got = plan(new Map<string, string>(), [thread()]);
    expect(got.create.map((t) => t.id)).toEqual(["t1"]);
    expect(got.update).toHaveLength(0);
    expect(got.dispose).toHaveLength(0);
  });

  it("keeps a thread that is still there rather than redrawing it", () => {
    // Redrawing closes the box somebody is typing a reply into.
    const held = new Map([["t1", "held"]]);
    const got = plan(held, [thread({ comments: [] })]);
    expect(got.create).toHaveLength(0);
    expect(got.update.map((u) => u.held)).toEqual(["held"]);
  });

  it("takes away a thread the fetch no longer has", () => {
    const held = new Map([["t1", "held"]]);
    expect(plan(held, []).dispose).toEqual(["held"]);
  });

  it("takes away a thread that has stopped being drawable", () => {
    const held = new Map([["t1", "held"]]);
    expect(plan(held, [thread({ scope: "repo", line: 0 })]).dispose).toEqual([
      "held",
    ]);
  });
});

describe("the label", () => {
  it("says how badly it wants attention and what kind of thing it is", () => {
    expect(label(thread({ severity: "high", category: "correctness" }))).toBe(
      "high · correctness",
    );
  });

  it("says when the code moved out from under it", () => {
    expect(label(thread({ orphaned: true }))).toContain("has changed");
  });

  // Without it the thread reads as open and the one button on it says Reopen,
  // which is how "there is no resolve button" happens.
  it("says when it is already resolved", () => {
    expect(label(thread({ resolved: true, severity: "medium" }))).toBe(
      "resolved · medium",
    );
  });

  it("says nothing when there is nothing to say", () => {
    expect(label(thread())).toBe("");
  });

  // The Electron app badges this; the editor had nothing, so a refused claim
  // read as an agreed one.
  it("leads with the verdict when there is one", () => {
    const declined = thread({
      severity: "low",
      category: "style",
      comments: [
        { id: "c1", author: "staff-reviewer", body: "wrong", at: "" },
        { id: "c2", author: "agent", body: "no", at: "", outcome: "declined" },
      ],
    });
    expect(label(declined)).toBe("declined · low · style");
  });
});

describe("what was decided about a claim", () => {
  it("is nothing while nobody has ruled", () => {
    expect(verdict(thread())).toBe("");
    expect(verdict(thread({ resolved: true }))).toBe("");
  });

  it("is the last outcome anyone recorded", () => {
    expect(
      verdict(
        thread({
          comments: [
            {
              id: "c1",
              author: "agent",
              body: "",
              at: "",
              outcome: "declined",
            },
            { id: "c2", author: "agent", body: "", at: "", outcome: "fixed" },
          ],
        }),
      ),
    ).toBe("fixed");
  });

  // The rebuttal round outranks the resolver: a withdrawn claim was never
  // owed a fix. Same order as comments.Thread.Fate in Go.
  it("puts the rebuttal round above the resolver", () => {
    const outcome = [
      { id: "c1", author: "agent", body: "", at: "", outcome: "fixed" },
    ];
    expect(verdict(thread({ withdrawn: true, comments: outcome }))).toBe(
      "withdrawn",
    );
    expect(verdict(thread({ duplicateOf: "t9", comments: outcome }))).toBe(
      "duplicate",
    );
  });

  it("falls back to disputed, which is an objection and not a ruling", () => {
    expect(verdict(thread({ disputed: true }))).toBe("disputed");
  });
});

describe("the label on a thread that cannot be sent", () => {
  const t = (over: Partial<Thread> = {}) =>
    ({ id: "1", file: "a.go", side: "right", line: 4, comments: [], ...over }) as Thread;

  it("carries the reason, so the missing verb is explained", () => {
    expect(label(t({ scope: "repo" }), true)).toContain("cannot go to GitHub");
    expect(label(t(), false)).toContain("no pull request");
  });

  it("is unchanged when it can go, or once it has gone", () => {
    expect(label(t(), true)).toBe("");
    expect(label(t({ scope: "repo", sentAt: "2026-10-05T10:00:00Z" }), true)).not.toContain(
      "cannot go",
    );
  });
});

describe("the label on a thread that was rewritten", () => {
  const t = (over: Partial<Thread> = {}) =>
    ({
      id: "1",
      file: "a.go",
      side: "right",
      line: 4,
      comments: [
        { id: "1", author: "a", body: "x", at: "" },
        { id: "2", author: "b", body: "y", at: "" },
      ],
      ...over,
    }) as Thread;

  it("says what the one comment stands for", () => {
    expect(label(t({ outgoing: "please fix" }), true)).toContain(
      "what 2 findings came to",
    );
  });

  it("says nothing when it stands for one", () => {
    const one = t({ outgoing: "please fix", comments: [{ id: "1", author: "a", body: "x", at: "" }] });
    expect(label(one, true)).not.toContain("came to");
  });
});

describe("where the rewritten words have got to", () => {
  const sent = "2026-10-06T10:00:00Z";

  it("badges the claim draft once it has been converted", () => {
    const t = thread({ outgoing: "the check runs after the deref" });
    expect(badge(t, t.comments[0], 0)).toBe("draft");
  });

  it("badges it sent once it has gone", () => {
    const t = thread({ outgoing: "words", sentAt: sent });
    expect(badge(t, t.comments[0], 0)).toBe("sent");
  });

  it("says nothing on a claim nobody has converted", () => {
    const t = thread();
    expect(badge(t, t.comments[0], 0)).toBeUndefined();
  });

  it("leaves a reply's own outcome alone", () => {
    const t = thread({ outgoing: "words" });
    const reply = { id: "c2", author: "you", body: "no", at: "", outcome: "declined" };
    expect(badge(t, reply, 1)).toBe("declined");
  });

  it("says so above the thread too", () => {
    expect(label(thread({ severity: "high", category: "security", sentAt: sent })))
      .toBe("sent to GitHub · high · security");
  });

  it("and a zero time is not sent", () => {
    const t = thread({ outgoing: "words", sentAt: "0001-01-01T00:00:00Z" });
    expect(badge(t, t.comments[0], 0)).toBe("draft");
    expect(label(t)).toBe("");
  });
});

describe("the declined comments a removal would take", () => {
  const no = (over: Partial<Thread> = {}) =>
    thread({
      comments: [
        { id: "c1", author: "agent", body: "wrong", at: "" },
        { id: "c2", author: "agent", body: "no", at: "", outcome: "declined" },
      ],
      ...over,
    });

  it("is every thread the resolver ruled against", () => {
    const all = [no({ id: "a" }), thread({ id: "b" })];
    expect(refused(all).map((t) => t.id)).toEqual(["a"]);
  });

  it("leaves a fixed one alone", () => {
    const fixed = thread({
      comments: [{ id: "c1", author: "agent", body: "done", at: "", outcome: "fixed" }],
    });
    expect(refused([fixed])).toEqual([]);
  });

  it("is not an imported comment or one already sent", () => {
    const theirs = [
      no({ id: "p", externalId: "gh-9" }),
      no({ id: "s", sentAt: "2026-02-03T04:05:06Z" }),
    ];
    expect(refused(theirs)).toEqual([]);
  });

  it("counts a later reply of yours as nothing decided again", () => {
    const argued = no({
      id: "q",
      comments: [
        { id: "c1", author: "agent", body: "wrong", at: "" },
        { id: "c2", author: "agent", body: "no", at: "", outcome: "declined" },
        { id: "c3", author: "you", body: "do it anyway", at: "" },
      ],
    });
    expect(refused([argued]).map((t) => t.id)).toEqual(["q"]);
  });
});
