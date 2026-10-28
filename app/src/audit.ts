import { appendFile, mkdir, readdir, readFile, unlink } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";

// What the person did, one JSON line each. The agent's own steps are the
// conversation's; this is the other half.

export interface Entry {
  at: string;
  trace?: string;
  kind: "command" | "verb" | "panel" | "http";
  what: string;
  where?: string;
  session?: string;
  dir?: string;
  ok?: boolean;
  why?: string;
  ms?: number;
  // Which fields a write carried. Names only — a settings PUT holds tokens.
  fields?: string[];
}

export function auditDir(): string {
  return join(homedir(), ".ade", "audit");
}

export function dayOf(at = new Date()): string {
  return at.toISOString().slice(0, 10);
}

// named is the keys a body carried, so a write says what it touched without
// saying what it said. Nothing else from a body is ever written.
export function named(body: unknown): string[] | undefined {
  if (!body || typeof body !== "object" || Array.isArray(body)) return undefined;
  const keys = Object.keys(body as Record<string, unknown>).sort();
  return keys.length > 0 ? keys.slice(0, 20) : undefined;
}

// One line at a time: two windows interleave lines rather than tearing one.
let queue: Promise<void> = Promise.resolve();

export function note(entry: Entry): void {
  queue = queue.then(() => write(entry)).catch(() => {});
}

// settled is what note() is waiting on, for a test and for a clean quit.
export function settled(): Promise<void> {
  return queue;
}

async function write(entry: Entry): Promise<void> {
  const dir = auditDir();
  try {
    await mkdir(dir, { recursive: true });
    await appendFile(join(dir, `${dayOf()}.jsonl`), JSON.stringify(entry) + "\n");
  } catch (err) {
    // Never the reason an action fails.
    console.error("audit:", err instanceof Error ? err.message : err);
  }
}

// read is one day's entries, oldest first, for the panel. A line that will
// not parse is skipped: a torn tail must not take the page with it.
export async function read(day = dayOf(), cap = 2000): Promise<Entry[]> {
  let raw = "";
  try {
    raw = await readFile(join(auditDir(), `${day}.jsonl`), "utf8");
  } catch {
    return [];
  }
  const out: Entry[] = [];
  for (const line of raw.split("\n")) {
    if (!line.trim()) continue;
    try {
      out.push(JSON.parse(line) as Entry);
    } catch {
      continue;
    }
  }
  return out.slice(-cap);
}

// days is what there is to read, newest first.
export async function days(): Promise<string[]> {
  try {
    const found = await readdir(auditDir());
    return found
      .filter((f) => f.endsWith(".jsonl"))
      .map((f) => f.replace(/\.jsonl$/, ""))
      .sort()
      .reverse();
  } catch {
    return [];
  }
}

// prune drops what is older than keep days, on start.
export async function prune(keep = 30, now = new Date()): Promise<void> {
  const oldest = new Date(now.getTime() - keep * 86_400_000).toISOString().slice(0, 10);
  for (const day of await days()) {
    if (day >= oldest) continue;
    await unlink(join(auditDir(), `${day}.jsonl`)).catch(() => {});
  }
}
