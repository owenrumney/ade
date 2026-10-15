import * as vscode from "vscode";
import type { BusOnline } from "./client";
import { urlOf } from "./pr";

// reviewTarget is the pull request to ask about: the row handed in, or this branch's.
export async function reviewTarget(
  arg: unknown,
  branch: () => Promise<{ url: string } | undefined>,
): Promise<string> {
  return urlOf(arg) || (await branch())?.url || "";
}

export const ANYONE = "$(person) Anyone who can take it";
export const EVERYONE = "$(organization) Everyone, each replies";
export const OTHER = "$(edit) Someone else, or a group…";

// Target is a quick pick row that knows what to send as "to".
export interface Target extends vscode.QuickPickItem {
  to?: string;
}

// reviewers is who to offer, out of a who result: one row per person who takes
// reviews, yourself left off. The bus refuses your own instances anyway.
export function reviewers(rows: BusOnline[] | null | undefined, self: string): BusOnline[] {
  return people(rows, self, (row) => Boolean(row.accepts?.review));
}

// askable is who takes a question, which is a different switch from reviews.
export function askable(rows: BusOnline[] | null | undefined, self: string): BusOnline[] {
  return people(rows, self, (row) => row.accepts?.ask !== false);
}

function people(
  rows: BusOnline[] | null | undefined,
  self: string,
  takes: (row: BusOnline) => boolean,
): BusOnline[] {
  const best = new Map<string, BusOnline>();
  for (const row of rows ?? []) {
    if (row.user === self || !takes(row)) continue;
    const held = best.get(row.user);
    if (!held || rank(row) < rank(held)) best.set(row.user, row);
  }
  return [...best.values()].sort((a, b) => a.user.localeCompare(b.user));
}

// Active before idle before dnd: who is most likely to pick it up.
function rank(row: BusOnline): number {
  return ["active", "idle", "dnd"].indexOf(row.presence);
}

export function targetItems(rows: BusOnline[]): Target[] {
  const items: Target[] = [
    { label: ANYONE, description: "the first to claim it wins", to: "anyone" },
  ];
  if (rows.length) {
    items.push({ label: "", kind: vscode.QuickPickItemKind.Separator });
    for (const row of rows) {
      items.push({ label: row.user, description: describe(row), to: row.user });
    }
  }
  items.push({ label: "", kind: vscode.QuickPickItemKind.Separator });
  items.push({ label: EVERYONE, description: "every reply comes back", to: "*" });
  items.push({ label: OTHER, description: "a group name, or an email" });
  return items;
}

// describe is the line beside a person: where they are and what they said they
// are doing, so you can tell a busy reviewer from an idle one.
export function describe(row: BusOnline): string {
  const bits = [row.presence];
  if (row.instance.harness) bits.push(row.instance.harness);
  if (row.status) bits.push(row.status);
  else if (row.instance.label) bits.push(row.instance.label);
  return bits.filter(Boolean).join(" · ");
}

// Asked is what the launcher answers a review request with: who was asked on
// the bus, and what it managed to do about the pull request in GitHub.
export interface Asked {
  to: string;
  reviewerSet?: boolean;
  reviewer?: string;
  reviewerNote?: string;
}

// askedLine is the line after a review request. The ask leads; whether GitHub
// names a reviewer follows, said either way.
export function askedLine(sent: Asked): string {
  const who = `asked ${sent.to} to review`;
  if (sent.reviewerSet && sent.reviewer) {
    return `${who} · ${sent.reviewer} is on the pull request`;
  }
  return sent.reviewerNote ? `${who} · ${sent.reviewerNote}` : who;
}
