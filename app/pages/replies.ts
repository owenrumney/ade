import type { Comment, Thread } from "./client";
import { wasSent } from "./here";

// pending is what a thread has to say back to GitHub: your own unsent comments
// after the last imported one. Mirrors PendingReply in internal/comments.
export function pending(t: Thread): Comment[] {
  let last = -1;
  t.comments.forEach((c, at) => {
    if (c.externalId) last = at;
  });
  return t.comments
    .slice(last + 1)
    .filter(
      (c) =>
        !c.externalId &&
        (c.author === "you" || c.author === "draft") &&
        !wasSent(c.sentAt),
    );
}

// answerable is a review comment from the pull request that is still open, so
// there is somebody to reply to. Mirrors notReplyable in internal/web.
export function answerable(t: Thread): boolean {
  return Boolean(t.externalId) && !t.resolved && !t.orphaned;
}

// replyable is the threads a send would post on: answerable, with words waiting.
export function replyable(threads: Thread[]): Thread[] {
  return threads.filter((t) => answerable(t) && pending(t).length > 0);
}
