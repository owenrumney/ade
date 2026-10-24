import type { Switched } from "./client";

// What to say after a branch switch. Work moved out of the tree, or put back,
// has to be said or the files look like they vanished.
export function switchSaid(out: Switched): string {
  const parts = [`on ${out.branch}`];
  if (out.stashed) {
    parts.push(`${files(out.stashed)} set aside, and kept for when you come back`);
  }
  if (out.restored) {
    parts.push(`${files(out.restored)} put back`);
  }
  if (out.stuck?.length) {
    parts.push(`${out.stuck.join(", ")} conflicted and are still in the stash`);
  } else if (out.note) {
    parts.push(out.note);
  }
  return parts.join("; ");
}

function files(n: number): string {
  return `${n} file${n === 1 ? "" : "s"}`;
}
