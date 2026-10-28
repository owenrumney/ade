import { describe, expect, it } from "vitest";
import type { Comment, Thread } from "./client";
import { answerable, pending, replyable } from "./replies";

const said = (author: string, more: Partial<Comment> = {}): Comment => ({
  id: author + Math.random(),
  author,
  body: "words",
  at: "2026-10-01T10:00:00Z",
  ...more,
});

const thread = (comments: Comment[], more: Partial<Thread> = {}): Thread => ({
  id: "t1",
  file: "a.go",
  side: "right",
  line: 4,
  externalId: "9001",
  comments,
  ...more,
});

describe("pending", () => {
  it("is your words after the reviewer's last", () => {
    const t = thread([
      said("alice", { externalId: "1" }),
      said("you"),
      said("draft"),
    ]);
    expect(pending(t).map((c) => c.author)).toEqual(["you", "draft"]);
  });

  it("leaves out what a later imported reply already answered", () => {
    const t = thread([
      said("alice", { externalId: "1" }),
      said("you"),
      said("alice", { externalId: "2" }),
    ]);
    expect(pending(t)).toEqual([]);
  });

  it("leaves out what was sent, and what an agent said", () => {
    const t = thread([
      said("alice", { externalId: "1" }),
      said("you", { sentAt: "2026-10-01T11:00:00Z" }),
      said("security"),
    ]);
    expect(pending(t)).toEqual([]);
  });

  it("does not take a zero time for sent", () => {
    const t = thread([
      said("alice", { externalId: "1" }),
      said("you", { sentAt: "0001-01-01T00:00:00Z" }),
    ]);
    expect(pending(t)).toHaveLength(1);
  });
});

describe("answerable", () => {
  it("is an open thread from the pull request", () => {
    expect(answerable(thread([]))).toBe(true);
    expect(answerable(thread([], { externalId: undefined }))).toBe(false);
    expect(answerable(thread([], { resolved: true }))).toBe(false);
    expect(answerable(thread([], { orphaned: true }))).toBe(false);
  });
});

describe("replyable", () => {
  it("keeps only the threads with something to send", () => {
    const ready = thread([said("alice", { externalId: "1" }), said("you")], {
      id: "ready",
    });
    const quiet = thread([said("alice", { externalId: "1" })], { id: "quiet" });
    const ours = thread([said("you")], { id: "ours", externalId: undefined });
    expect(replyable([ready, quiet, ours]).map((t) => t.id)).toEqual(["ready"]);
  });
});
