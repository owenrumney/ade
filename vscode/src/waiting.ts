import type { Waiting } from "./launcher";

// What a row says in a notification: the branch if there is one, then what it
// asked. A session's own name is a uuid and says nothing.
export function line(row: Waiting): string {
  const where = row.branch || row.name;
  return row.title ? `${where}: ${row.title}` : where;
}

// fresh is the rows that have appeared since the last reading. A notification
// per poll for the same session is how an editor teaches you to ignore it.
export function fresh(rows: Waiting[], told: Set<string>): Waiting[] {
  return rows.filter((row) => !told.has(row.id));
}

// forget drops the ids no longer waiting, so a session that asks again after
// being answered is announced again.
export function forget(rows: Waiting[], told: Set<string>): Set<string> {
  const live = new Set(rows.map((row) => row.id));
  return new Set([...told].filter((id) => live.has(id)));
}

// badge is the number on the activity bar and what hovering it says.
export function badge(
  rows: Waiting[],
): { value: number; tooltip: string } | undefined {
  if (rows.length === 0) return undefined;
  const one = rows.length === 1;
  return {
    value: rows.length,
    tooltip: one
      ? "1 session is waiting on you"
      : `${rows.length} sessions are waiting on you`,
  };
}

// here is what is waiting in this window's own checkout, which is the only
// thing the editor can act on without changing folder.
export function here(rows: Waiting[], root: string | undefined): Waiting[] {
  if (!root) return [];
  return rows.filter((row) => row.dir === root);
}
