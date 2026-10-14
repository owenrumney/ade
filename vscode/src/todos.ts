import type { Server } from "./discover";
import { tell, drop, patch } from "./launcher";

// Todo mirrors todo.Item. The store a todo lives in is what scopes it: repo is
// filled by the launcher on the way out, and names the checkout that answered.
export interface Todo {
  id: string;
  text: string;
  note?: string;
  due?: string;
  from?: string;
  session?: string;
  started?: string;
  createdAt?: string;
  doneAt?: string;
  repo?: string;
}

export async function list(server: Server, dir?: string): Promise<Todo[]> {
  const where = dir ? `&dir=${encodeURIComponent(dir)}` : "";
  const res = await fetch(
    `${server.base}/api/todos?t=${encodeURIComponent(server.token)}${where}`,
    { headers: { "X-Ade-Token": server.token } },
  );
  if (!res.ok) throw new Error(`todos: ${res.status}`);
  const body = (await res.json()) as { items: Todo[] | null };
  return body.items ?? [];
}

export async function add(
  server: Server,
  item: { text: string; dir?: string; note?: string; due?: string },
): Promise<Todo> {
  return tell<Todo>(server, "/api/todos", item);
}

export async function edit(
  server: Server,
  id: string,
  change: {
    dir?: string;
    text?: string;
    note?: string;
    due?: string;
    done?: boolean;
    started?: string;
  },
): Promise<void> {
  await patch(server, `/api/todos/${encodeURIComponent(id)}`, change);
}

export async function remove(
  server: Server,
  id: string,
  dir?: string,
): Promise<void> {
  const where = dir ? `?dir=${encodeURIComponent(dir)}` : "";
  await drop(server, `/api/todos/${encodeURIComponent(id)}${where}`);
}

export function done(item: Todo): boolean {
  return !!item.doneAt;
}

export function overdue(item: Todo, now: Date): boolean {
  if (done(item) || !item.due) return false;
  return new Date(item.due).getTime() < now.getTime();
}

// waiting is what the badge counts: open items, overdue ones included.
export function waiting(items: Todo[]): number {
  return items.filter((item) => !done(item)).length;
}

// describe is the dimmed half of a row: when it is due, where it came from,
// and whether it became work. One line, read at a glance or not at all.
export function describe(item: Todo, now: Date): string {
  const parts: string[] = [];
  if (item.due) parts.push(when(item.due, now));
  if (item.repo) parts.push(item.repo);
  if (item.started) parts.push("in progress");
  else if (item.from && item.from !== "you") parts.push(`from the ${item.from}`);
  return parts.join(" · ");
}

export function icon(item: Todo, now: Date): string {
  if (done(item)) return "check";
  if (item.started) return "sync";
  if (overdue(item, now)) return "warning";
  return "circle-large-outline";
}

// when says a date the way you would: today, tomorrow, overdue, or the day.
export function when(due: string, now: Date): string {
  const days = dayGap(new Date(due), now);
  if (days < 0) return days === -1 ? "yesterday" : `${-days} days late`;
  if (days === 0) return "today";
  if (days === 1) return "tomorrow";
  if (days < 7) return `in ${days} days`;
  return new Date(due).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
  });
}

function dayGap(due: Date, now: Date): number {
  const start = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate());
  const ms = start(due).getTime() - start(now).getTime();
  return Math.round(ms / 86_400_000);
}

// dueFrom reads what you typed into a date. Typing a date is the friction a
// capture cannot afford, so the words you would actually use are accepted.
export function dueFrom(said: string, now: Date): string | undefined {
  const text = said.trim().toLowerCase();
  if (!text || text === "none" || text === "never") return "";
  const days = relative(text);
  if (days !== undefined) return atNine(now, days);
  const parsed = Date.parse(said.trim());
  if (Number.isNaN(parsed)) return undefined;
  return new Date(parsed).toISOString();
}

function relative(text: string): number | undefined {
  if (text === "today" || text === "now") return 0;
  if (text === "tomorrow") return 1;
  if (text === "next week") return 7;
  const counted = /^in (\d+) (day|days|week|weeks)$/.exec(text);
  if (!counted) return undefined;
  const n = Number(counted[1]);
  return counted[2].startsWith("week") ? n * 7 : n;
}

function atNine(now: Date, days: number): string {
  const at = new Date(now.getFullYear(), now.getMonth(), now.getDate() + days);
  at.setHours(9, 0, 0, 0);
  return at.toISOString();
}

// todoFrom is a review thread as a capture: its claim on one line under
// #review, with the place it was made as the note.
export function todoFrom(thread: {
  file: string;
  line: number;
  comments: { body: string }[];
}): { text: string; note: string } {
  const said =
    (thread.comments[0]?.body ?? "")
      .split("\n")
      .map((s) => s.trim().replace(/^[#>*-]+\s*/, ""))
      .find(Boolean) ?? "";
  const short = said.length > 90 ? `${said.slice(0, 89).trimEnd()}…` : said;
  return { text: `${short} #review`.trim(), note: `${thread.file}:${thread.line}` };
}
