// Which files you have ticked off in a review, and the version you ticked.
// A file the agent changes again loses its tick: a stale one is worse than none.

export type Marks = Record<string, string>;

export interface Counted {
  path: string;
  status: string;
  additions: number;
  deletions: number;
}

// The counts, not a hash of the hunks: the window holds these already.
export function stamp(file: Counted): string {
  return `${file.status}:${file.additions}:${file.deletions}`;
}

// pruned drops a tick whose file has gone from the diff or changed since.
export function pruned(marks: Marks, files: Counted[]): Marks {
  const out: Marks = {};
  for (const file of files) {
    if (marks[file.path] === stamp(file)) out[file.path] = marks[file.path];
  }
  return out;
}

export function flip(marks: Marks, file: Counted): Marks {
  const next = { ...marks };
  if (next[file.path]) delete next[file.path];
  else next[file.path] = stamp(file);
  return next;
}

// Only the session in front of you: every session's would grow without bound.
interface Held {
  session: string;
  marks: Marks;
}

export function read(raw: string | null, session: string): Marks {
  if (!raw || !session) return {};
  try {
    const held = JSON.parse(raw) as Partial<Held>;
    if (held?.session !== session || !held.marks) return {};
    return { ...held.marks };
  } catch {
    return {};
  }
}

export function write(marks: Marks, session: string): string {
  return JSON.stringify({ session, marks } satisfies Held);
}
