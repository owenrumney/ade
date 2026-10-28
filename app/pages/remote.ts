// The launcher a session was reached through: the proxied address with
// the /s/<id> taken off.
export function launcherAt(
  base: string,
  token: string,
):
  | {
      base: string;
      token: string;
    }
  | undefined {
  let parsed: URL;
  try {
    parsed = new URL(base);
  } catch {
    return undefined;
  }
  if (!/^\/s\/[^/]+$/.test(parsed.pathname)) return undefined;
  return { base: parsed.origin, token };
}

export interface Serving {
  id: string;
  dir: string;
}

// The address to reach a checkout's session at now: a new conversation
// changes the session id the record carries.
export function currentProxy(
  base: string,
  token: string,
  root: string,
  serving: Serving[],
): string | undefined {
  const here = serving.find((s) => s.dir === root);
  if (!here) return undefined;
  return `${base}/s/${encodeURIComponent(here.id)}/?t=${encodeURIComponent(token)}`;
}
