import type { Thread } from "./client";
import { claimState, foldable, folded, whyNotSendable } from "./commentrow";
import { deletable, wasSent } from "./here";

// Plan is what to do to the threads on screen so they match the last fetch.
// Rebuilding the whole set instead would close the box somebody is typing in.
export interface Plan<T> {
  create: Thread[];
  update: { held: T; thread: Thread }[];
  dispose: T[];
}

// Relevant decides whether a thread can be drawn at all. A repo-scoped thread
// has no line, and a left-side one points at code this change deleted.
export function relevant(t: Thread): boolean {
  return (
    t.scope !== "repo" && t.side !== "left" && t.line > 0 && Boolean(t.file)
  );
}

// plan diffs the incoming threads against what is held, by id.
export function plan<T>(held: Map<string, T>, incoming: Thread[]): Plan<T> {
  const out: Plan<T> = { create: [], update: [], dispose: [] };
  const seen = new Set<string>();

  for (const thread of incoming) {
    if (!relevant(thread)) continue;
    seen.add(thread.id);
    const existing = held.get(thread.id);
    if (existing === undefined) out.create.push(thread);
    else out.update.push({ held: existing, thread });
  }
  for (const [id, existing] of held) {
    if (!seen.has(id)) out.dispose.push(existing);
  }
  return out;
}

// What a drawn thread sets as its contextValue, and what the menus match on.
// Ours alone: a bare "open" would match another extension's threads too.
export const OPEN = "adeOpen";
export const RESOLVED = "adeResolved";

export const stateOf = (resolved?: boolean) => (resolved ? RESOLVED : OPEN);

// threadState adds the claim to the thread's contextValue: only a menu on the
// thread is handed one, and a verb has nothing else to look up.
export function threadState(t: Thread, hasPR: boolean, open = false): string {
  const parts = [stateOf(t.resolved)];
  if (foldable(t)) parts.push(open ? "unfolded" : "folded");
  const claim = claimState(t, hasPR).replace(/^ade\.claim\.?/, "");
  if (claim) parts.push(claim);
  return parts.join(".");
}

// verdict is what was decided about a claim, or "" while nothing has been.
// A resolved flag cannot tell a fix from a refusal, which is the whole point.
export function verdict(t: Thread): string {
  if (t.withdrawn) return "withdrawn";
  if (t.duplicateOf) return "duplicate";
  for (let i = t.comments.length - 1; i >= 0; i--) {
    const outcome = t.comments[i].outcome;
    if (outcome) return outcome;
  }
  return t.disputed ? "disputed" : "";
}

// refused are the claims the resolver ruled against, for removing them in one
// go. A declined thread stays open on purpose, so nothing else clears it.
export function refused(threads: Thread[]): Thread[] {
  return deletable(threads.filter((t) => verdict(t) === "declined"));
}

// badge is the one word beside a comment's author, the only slot a comment
// has. On the claim it says where the rewritten words have got to.
export function badge(
  t: Thread,
  c: Thread["comments"][number],
  at: number,
): string | undefined {
  if (at > 0) return c.outcome || undefined;
  if (wasSent(t.sentAt)) return "sent";
  if (t.outgoing) return "draft";
  return c.outcome || undefined;
}

// label is what the thread says above its comments: the verdict first, or
// resolved when nothing was decided, which is why its button says Reopen.
export function label(t: Thread, hasPR = true): string {
  const parts: string[] = [];
  if (wasSent(t.sentAt)) parts.push("sent to GitHub");
  const decided = verdict(t);
  if (decided) parts.push(decided);
  else if (t.resolved) parts.push("resolved");
  if (t.severity) parts.push(t.severity);
  if (t.category) parts.push(t.category);
  if (t.orphaned) parts.push("the code this was pinned to has changed");
  const was = folded(t);
  if (was > 1) parts.push(`what ${was} findings came to`);
  const refused = t.sentAt ? "" : whyNotSendable(t, hasPR);
  if (refused) parts.push(`cannot go to GitHub: ${refused}`);
  return parts.join(" · ");
}
