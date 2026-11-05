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

// The server pings every 20s, so this is three missed pings.
const stale = 60_000;

// events names each event in an SSE body and throws when the stream goes
// quiet: a half-open socket delivers nothing and never errors.
export async function events(
  body: ReadableStream<Uint8Array>,
  named: (event: string) => void,
): Promise<void> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let rest = "";
  for (;;) {
    let tick: ReturnType<typeof setTimeout> | undefined;
    const quiet = new Promise<never>((_, fail) => {
      tick = setTimeout(() => fail(new Error("ade stopped sending events")), stale);
    });
    let got: Awaited<ReturnType<typeof reader.read>>;
    try {
      got = await Promise.race([reader.read(), quiet]);
    } catch (err) {
      void reader.cancel().catch(() => undefined);
      throw err;
    } finally {
      clearTimeout(tick);
    }
    if (got.done) return;
    rest += decoder.decode(got.value, { stream: true });
    const frames = rest.split("\n\n");
    rest = frames.pop() ?? "";
    for (const frame of frames) {
      const name = /^event: (\w+)$/m.exec(frame);
      if (name) named(name[1]);
    }
  }
}
