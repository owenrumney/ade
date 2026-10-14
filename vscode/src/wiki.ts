import type { Hit, Model } from "./launcher";

// A wiki page is read, not edited: it lives in a clone ade syncs, so a
// document you could save over is one whose edits the next fetch discards.
export const WIKI = "ade-wiki";

export function wikiPath(uri: { path: string }): string {
  return uri.path.replace(/^\//, "");
}

// pick is one search result as a row: the page, then the line that matched.
export function pick(hit: Hit): {
  label: string;
  description: string;
  detail: string;
} {
  return {
    label: hit.path.split("/").pop() ?? hit.path,
    description: `${hit.scope} · ${hit.path}`,
    detail: hit.snippet.trim(),
  };
}

// modelPick is one model as a row. The price is what distinguishes two that
// otherwise read alike, so it goes in the description rather than the detail.
export function modelPick(m: Model): { label: string; description: string } {
  const bits = [m.agent];
  if (m.latest) bits.push("latest");
  if (m.in && m.out) bits.push(`$${m.in}/$${m.out} per Mtok`);
  return { label: m.label || m.id, description: bits.join(" · ") };
}

// port reads what was typed. Only a real loopback port: the number is handed
// to the launcher, which forwards it.
export function port(said: string): number | undefined {
  const n = Number(said.trim());
  if (!Number.isInteger(n) || n < 1 || n > 65535) return undefined;
  return n;
}
