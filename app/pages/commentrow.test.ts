import { describe, expect, it } from "vitest";
import type { Thread } from "./client";
import {
  claimState,
  foldAbove,
  foldable,
  folded,
  shownComments,
  unsent,
  whyNotSendable,
} from "./commentrow";

const thread = (over: Partial<Thread> = {}): Thread => ({
  id: "t",
  file: "web.go",
  side: "right",
  line: 10,
  comments: [{ id: "c", author: "security", body: "the claim", at: "" }],
  ...over,
});

describe("what a review comment from the pull request offers", () => {
  const theirs = {
    id: "c1",
    author: "alice",
    body: "why?",
    at: "",
    externalId: "1",
  };
  const mine = { id: "c2", author: "draft", body: "because", at: "" };

  it("offers a draft while there is nothing to send", () => {
    const t = thread({ externalId: "gh-9", comments: [theirs] });
    expect(claimState(t, true)).toBe("ade.claim.answer");
  });

  it("offers the send once a reply is waiting", () => {
    const t = thread({ externalId: "gh-9", comments: [theirs, mine] });
    expect(claimState(t, true)).toBe("ade.claim.reply");
  });

  it("does not offer a send with no pull request to send to", () => {
    const t = thread({ externalId: "gh-9", comments: [theirs, mine] });
    expect(claimState(t, false)).toBe("ade.claim");
  });

  it("offers nothing on one that is resolved", () => {
    const t = thread({
      externalId: "gh-9",
      resolved: true,
      comments: [theirs],
    });
    expect(claimState(t, true)).toBe("ade.claim");
  });
});

describe("which of Convert and Send a claim offers", () => {
  it("offers neither without a pull request to comment on", () => {
    expect(claimState(thread(), false)).toBe("ade.claim");
  });

  it("offers Convert first, then Send once there are words", () => {
    expect(claimState(thread(), true)).toBe("ade.claim.convert");
    expect(claimState(thread({ outgoing: "please fix" }), true)).toBe(
      "ade.claim.send",
    );
  });

  it("offers neither once it has gone", () => {
    const sent = thread({ outgoing: "x", sentAt: "2026-02-03T04:05:06Z" });
    expect(claimState(sent, true)).toBe("ade.claim.sent");
  });

  // A zero time marshals as year 1 and would read as already sent.
  it("does not mistake a zero time for having been sent", () => {
    expect(claimState(thread({ sentAt: "0001-01-01T00:00:00Z" }), true)).toBe(
      "ade.claim.convert",
    );
  });

  // The same refusals internal/web/outgoing.go gives, so a button is never
  // offered for a send the server would answer 400 to.
  it("offers neither for what the server would refuse", () => {
    for (const over of [
      { resolved: true },
      { orphaned: true },
      { scope: "repo" },
      { file: "" },
      { line: 0 },
    ]) {
      expect(claimState(thread(over), true), JSON.stringify(over)).toBe(
        "ade.claim",
      );
    }
  });
});

// The verb is hidden rather than offered and refused, so its absence used to
// be the only thing said about why a comment could not go.
describe("why a comment cannot go to GitHub", () => {
  it("names the reason the server would give", () => {
    expect(whyNotSendable(thread(), false)).toContain("no pull request");
    expect(whyNotSendable(thread({ scope: "repo" }), true)).toContain(
      "no line to attach to",
    );
    expect(whyNotSendable(thread({ file: "" }), true)).toContain(
      "not anchored to a line",
    );
    expect(whyNotSendable(thread({ line: 0 }), true)).toContain(
      "not anchored to a line",
    );
  });

  it("says nothing when it can go, or when the row already says it", () => {
    expect(whyNotSendable(thread(), true)).toBe("");
    // Resolved and orphaned are on the row already, in their own words.
    expect(whyNotSendable(thread({ resolved: true }), true)).toBe("");
    expect(whyNotSendable(thread({ orphaned: true }), true)).toBe("");
  });
});

describe("a thread that has been rewritten for the author", () => {
  const found = (over: Partial<Thread> = {}) =>
    thread({
      comments: [
        { id: "1", author: "staff-reviewer", body: "this leaks", at: "t1" },
        { id: "2", author: "security", body: "agreed", at: "t2" },
        { id: "3", author: "architect", body: "and here", at: "t3" },
      ],
      ...over,
    });

  it("shows the words that would go, not the findings behind them", () => {
    const t = found({ outgoing: "This leaks the token; please close it." });
    const shown = shownComments(t);
    expect(shown).toHaveLength(1);
    expect(shown[0].author).toBe("you");
    expect(shown[0].body).toBe("This leaks the token; please close it.");
  });

  it("leaves a thread with nothing to send as it is", () => {
    expect(shownComments(found())).toHaveLength(3);
    expect(folded(found())).toBe(0);
  });

  it("says how many it stands for, so none look lost", () => {
    expect(folded(found({ outgoing: "x" }))).toBe(3);
  });

  it("keeps standing in once it has been sent", () => {
    const t = found({ outgoing: "x", sentAt: "2026-10-05T10:00:00Z" });
    expect(shownComments(t)).toHaveLength(1);
    expect(shownComments(t)[0].at).toBe("2026-10-05T10:00:00Z");
  });
});

describe("a thread with more findings than fit", () => {
  const many = (n: number) =>
    thread({
      comments: Array.from({ length: n }, (_, i) => ({
        id: String(i),
        author: i % 2 ? "security" : "staff-reviewer",
        body: `finding ${i}\n\nthe long part nobody reads twice`,
        at: `t${i}`,
      })),
    });

  it("is folded past the threshold and left alone below it", () => {
    expect(foldable(many(foldAbove))).toBe(false);
    expect(foldable(many(foldAbove + 1))).toBe(true);
  });

  it("keeps the first in full and says what the rest are", () => {
    const shown = shownComments(many(6));
    expect(shown).toHaveLength(2);
    expect(shown[0].body).toContain("finding 0");
    expect(shown[1].body).toContain("5 more findings");
    expect(shown[1].body).toContain("**security** finding 1");
    // The first line of each, not the whole of it.
    expect(shown[1].body).not.toContain("nobody reads twice");
  });

  it("shows every one when asked", () => {
    expect(shownComments(many(6), true)).toHaveLength(6);
  });

  // A thread already rewritten is one comment: there is nothing to fold.
  it("is not folded once it has been converted", () => {
    expect(foldable(many(6))).toBe(true);
    expect(foldable(thread({ ...many(6), outgoing: "please fix" }))).toBe(false);
  });
});

describe("whether a comment is a draft", () => {
  it("is once a comment has been converted", () => {
    expect(unsent(thread({ outgoing: "words" }))).toBe(true);
  });

  it("is not once it has gone", () => {
    expect(
      unsent(thread({ outgoing: "words", sentAt: "2026-10-06T10:00:00Z" })),
    ).toBe(false);
  });

  it("is not before Convert has run", () => {
    expect(unsent(thread())).toBe(false);
  });
});
