import { readFile } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";

// Where ade is: the launcher writes its address and token to
// ~/.ade/server.json, the file discover.ts reads.
export interface At {
  base: string;
  token: string;
}

export function recordPath(): string {
  return join(homedir(), ".ade", "server.json");
}

// found reads the record. Nothing means no launcher is running, which
// is ordinary: the window says so rather than failing.
export async function found(): Promise<At | undefined> {
  try {
    const raw = await readFile(recordPath(), "utf8");
    const rec = JSON.parse(raw) as { url?: string; pid?: number };
    return at(rec.url);
  } catch {
    return undefined;
  }
}

// at splits the recorded url into an origin and the token it carries in
// its query, which is the only place the token is ever written.
export function at(url: string | undefined): At | undefined {
  if (!url) return undefined;
  try {
    const parsed = new URL(url);
    const token = parsed.searchParams.get("t");
    if (!token) return undefined;
    return { base: parsed.origin, token };
  } catch {
    return undefined;
  }
}

// ask is one GET against the launcher. The token goes in the header
// rather than the query, so it stays out of any log of the url.
export async function ask<T>(where: At, path: string, wait?: number): Promise<T> {
  const res = await fetch(where.base + path, {
    headers: { "X-Ade-Token": where.token },
    signal: wait ? AbortSignal.timeout(wait) : undefined,
  });
  if (!res.ok) throw new Error(await said(res, path));
  return (await res.json()) as T;
}

// said is what went wrong, in ade's words where it gave any: a status
// code alone says where it broke and nothing about why.
async function said(res: Response, path: string): Promise<string> {
  const text = await res.text().catch(() => "");
  try {
    const body = JSON.parse(text) as { error?: string };
    if (body.error) return `${path}: ${body.error}`;
  } catch {
    // Not JSON: whatever it is, the status is all there is.
  }
  return `${path}: ${res.status} ${res.statusText}`.trim();
}

export async function tell<T>(
  where: At,
  path: string,
  body: unknown,
  method: "POST" | "PUT" | "PATCH" | "DELETE" = "POST",
): Promise<T> {
  const res = await fetch(where.base + path, {
    method,
    headers: {
      "X-Ade-Token": where.token,
      "Content-Type": "application/json",
    },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(await said(res, path));
  return (res.status === 204 ? undefined : await res.json()) as T;
}
