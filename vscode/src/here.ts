// Which threads a command acting on "here" means. Kept apart from the editor so
// the rule is testable: the menus are not, and that is what hid the buttons.
export interface Pinned {
  id: string;
  file: string;
  line: number;
  resolved?: boolean;
  orphaned?: boolean;
  externalId?: string;
  sentAt?: string;
}

// wasSent is the only way to ask: a zero time serialises as year 1, which is
// truthy and would read as sent.
export const wasSent = (sentAt?: string) =>
  Boolean(sentAt) && new Date(sentAt as string).getUTCFullYear() > 1970;

// deletable is what this store owns. An imported comment comes back on the next
// fetch and a sent one lives on the pull request, so neither is ours to drop.
export function deletable<T extends Pinned>(threads: T[]): T[] {
  return threads.filter((t) => !t.externalId && !wasSent(t.sentAt));
}

// at is the open threads on a line, by the path ade names the file by.
export function at(threads: Pinned[], file: string, line: number): Pinned[] {
  return threads.filter(
    (t) => !t.resolved && t.file === file && t.line === line,
  );
}

// inFile is the open threads anywhere in a file, for when the cursor is not on
// one: resolving the file's comments beats telling you to find them.
export function inFile(threads: Pinned[], file: string): Pinned[] {
  return threads.filter((t) => !t.resolved && t.file === file);
}

// orphans are the threads whose code has gone. The store's sweep clears these
// an hour after it does; this is the same set, now.
export function orphans(threads: Pinned[]): Pinned[] {
  return deletable(threads.filter((t) => t.orphaned));
}

// done are the threads that have been dealt with. Nothing clears these by
// itself: a resolved comment is kept until somebody says otherwise.
export function done(threads: Pinned[]): Pinned[] {
  return deletable(threads.filter((t) => t.resolved));
}
