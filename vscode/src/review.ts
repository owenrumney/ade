import { basename } from "node:path";
import type { Meta } from "./client";

// The scopes the server offers, in the order its own switcher uses.
export const SCOPES = [
  {
    id: "branch",
    label: "This branch",
    detail: "Everything on the branch, committed or not",
  },
  {
    id: "committed",
    label: "Committed only",
    detail: "What has been committed",
  },
  {
    id: "uncommitted",
    label: "Uncommitted only",
    detail: "What is still pending commit",
  },
] as const;

// The levels `internal/review` accepts. Anything else is refused there, so a
// pick is the right shape and a free-text box would not be.
export const LEVELS = [
  { id: "medium", detail: "fewer, high-confidence findings" },
  { id: "high", detail: "broader coverage" },
  { id: "low", detail: "the obvious only" },
  { id: "xhigh", detail: "wider still, including the uncertain" },
  { id: "max", detail: "everything" },
] as const;

// what names what a review is about to read, so the level pick is never a
// surprise. The scope decides it and the scope is not otherwise on screen.
export function what(meta: Meta | undefined): string {
  const scope = meta?.scope ?? "branch";
  const where =
    SCOPES.find((s) => s.id === scope)?.label.toLowerCase() ?? scope;
  const files = meta?.files ?? 0;
  const count = files === 1 ? "1 file" : `${files} files`;
  return meta?.branch
    ? `${count} on ${meta.branch}, ${where}`
    : `${count}, ${where}`;
}

// empty is a diff with nothing in it. The server refuses the review, and
// saying so first beats a round trip that returns an error.
export function empty(meta: Meta | undefined): boolean {
  return (meta?.files ?? 0) === 0;
}

// nothing names the checkout and the view it found nothing in: a window can see
// several ade servers, and it attaches to the first folder with one.
export function nothing(meta: Meta | undefined): string {
  const repo = meta?.repo ? basename(meta.repo) : "";
  const where = repo ? ` in ${repo}` : "";
  return `ade: nothing to review${where} — ${what(meta)}.`;
}

// A fetched snapshot has only the one view, so the scope cannot be changed
// and offering to is a command that always fails.
export function scopeable(meta: Meta | undefined): boolean {
  return meta?.mode !== "pr snapshot";
}

// Which agents the server will accept: `reviews: true`, and user scope only —
// a branch could otherwise ship the reviewer that reviews it.
export function reviewers<T extends { reviews: boolean; scope: string }>(
  agents: T[],
): T[] {
  return agents.filter((a) => a.reviews && a.scope === "user");
}

// The level is read only when no agent is named, so asking otherwise is
// asking a question whose answer is thrown away.
export function levelled(picked: string[]): boolean {
  return picked.length === 0;
}

// Which offered reviewers arrive ticked. A wanted name this machine cannot
// offer is skipped: the setting is a preference, not an assertion.
export function preselect<T extends { name: string }>(
  offered: T[],
  wanted: string[],
): Set<string> {
  const asked = new Set(
    wanted.map((n) => n.trim().replace(/^@/, "")).filter(Boolean),
  );
  return new Set(offered.filter((a) => asked.has(a.name)).map((a) => a.name));
}
