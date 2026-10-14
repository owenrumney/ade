// What a comment menu hands a command. The editor passes the thread; the
// Comments view and the per-comment menus pass something holding it.
export interface Held<T> {
  thread?: T;
  parent?: T;
  comments?: unknown;
}

// threadOf unwraps the argument to the thread it is about. Without it a button
// contributed to one menu does nothing when it is on another.
export function threadOf<T extends object>(arg: unknown): T | undefined {
  if (!arg || typeof arg !== "object") return undefined;
  const held = arg as Held<T>;
  if (Array.isArray(held.comments)) return arg as T;
  return held.thread ?? held.parent;
}

// idFrom is the thread id a row in ade's own comments tree carries. A VS Code
// CommentThread has no id, so a string one here is never ambiguous.
export function idFrom(arg: unknown): string | undefined {
  if (!arg || typeof arg !== "object") return undefined;
  const id = (arg as { id?: unknown }).id;
  return typeof id === "string" && id ? id : undefined;
}

// openCount is how many comments a Resolve All would send to the agent: the
// unresolved ones, which is what the diff's own button counts.
export function openCount(threads: { resolved?: boolean }[]): number {
  return threads.filter((t) => !t.resolved).length;
}
