import type { Machine, Session } from "./launcher";

export interface Prunable {
  session: Session;
  label: string;
  description: string;
  detail: string;
}

// prunable is every conversation a machine is holding, oldest first: the ones
// worth clearing are the ones nothing has touched for longest.
export function prunable(machine: Machine, now = Date.now()): Prunable[] {
  const out: Prunable[] = [];
  for (const repo of machine.repos ?? []) {
    for (const checkout of repo.checkouts ?? []) {
      for (const session of checkout.sessions ?? []) {
        if (session.live) continue;
        out.push({
          session,
          label: session.title || session.id.slice(0, 8),
          description: `${repo.name} · ${session.branch || "no branch"}`,
          detail: `${age(session.lastUsed, now)} · ${checkout.path}`,
        });
      }
    }
  }
  return out.sort((a, b) => when(a.session) - when(b.session));
}

function when(session: Session): number {
  const at = session.lastUsed ? Date.parse(session.lastUsed) : NaN;
  return Number.isNaN(at) ? 0 : at;
}

// age is how long ago, in the words the rest of the tree uses.
export function age(at: string | undefined, now: number): string {
  const was = at ? Date.parse(at) : NaN;
  if (Number.isNaN(was)) return "never used";
  const days = Math.floor((now - was) / 86400000);
  if (days >= 1) return `${days} day${days === 1 ? "" : "s"} ago`;
  const hours = Math.floor((now - was) / 3600000);
  if (hours >= 1) return `${hours} hour${hours === 1 ? "" : "s"} ago`;
  return "today";
}
