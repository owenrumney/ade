import type { Comment, Thread } from "./client";
import { wasSent } from "./here";
import { answerable, pending } from "./replies";

// foldAbove is how many comments a thread may have before it is folded. A
// review that found four things on one line is a wall to read.
export const foldAbove = 3;

export function foldable(t: Thread): boolean {
  return !t.outgoing && t.comments.length > foldAbove;
}

// summary is the one comment that stands in for the rest: who found what,
// a line each, so the thread says what is in it without being it.
export function summary(t: Thread): Comment {
  const rest = t.comments.slice(1);
  const lines = rest.map((c) => `- **${c.author}** ${firstLine(c.body)}`);
  return {
    id: `${t.id}:folded`,
    author: "review",
    body: `${rest.length} more findings\n\n${lines.join("\n")}`,
    at: rest[rest.length - 1]?.at ?? "",
  };
}

function firstLine(body: string): string {
  const line = body.split("\n").find((one) => one.trim()) ?? "";
  return line.trim();
}

// shownComments is what the widget draws: once a thread is rewritten, the
// words that would go are the thread. The findings are the working out.
export function shownComments(t: Thread, open = false): Comment[] {
  if (!t.outgoing) {
    if (!open && foldable(t)) return [t.comments[0], summary(t)];
    return t.comments;
  }
  const last = t.comments[t.comments.length - 1];
  return [
    {
      id: `${t.id}:outgoing`,
      author: "you",
      body: t.outgoing,
      at: t.sentAt || last?.at || "",
    },
  ];
}

// unsent is an outgoing comment GitHub has not had yet: the pane's Draft.
export function unsent(t: Thread): boolean {
  return Boolean(t.outgoing) && !wasSent(t.sentAt);
}

// folded is how many comments the outgoing one stands for.
export function folded(t: Thread): number {
  return t.outgoing ? t.comments.length : 0;
}

// whyNotSendable is why Convert is not offered, in outgoing.go's own words:
// the verb is hidden, so its absence is otherwise all that is said.
export function whyNotSendable(t: Thread, hasPR: boolean): string {
  if (!hasPR) return "there is no pull request for this branch";
  if (t.externalId) return "came from the pull request; answer with a reply";
  if (t.resolved || t.orphaned) return "";
  if (t.scope === "repo")
    return "a comment on the change as a whole has no line to attach to";
  if (!t.file || t.line <= 0) return "not anchored to a line";
  return "";
}

// claimState is the contextValue the thread's first comment carries, which is
// where Convert and Send sit. Only the first: the verbs act on the thread.
export function claimState(t: Thread, hasPR: boolean): string {
  if (wasSent(t.sentAt)) return "ade.claim.sent";
  // A review comment from the pull request is answered in its own thread:
  // drafted first, then sent once there are words waiting.
  if (answerable(t)) {
    if (pending(t).length === 0) return "ade.claim.answer";
    return hasPR ? "ade.claim.reply" : "ade.claim";
  }
  // Mirrors notSendable in internal/web/outgoing.go. A thread from the pull
  // request is answered with a reply, not a new comment on the line.
  const mine =
    hasPR &&
    !t.externalId &&
    !t.resolved &&
    !t.orphaned &&
    t.scope !== "repo" &&
    Boolean(t.file) &&
    t.line > 0;
  if (!mine) return "ade.claim";
  return t.outgoing ? "ade.claim.send" : "ade.claim.convert";
}

// A reply carries no verbs of its own.
export const REPLY = "ade.said";
