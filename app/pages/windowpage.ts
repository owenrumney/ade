import type { Limits, Usage } from "./usage";

// Share is one checkout's part of the current short window.
export interface Share {
  dir: string;
  tokens: number;
  percent: number;
  sessions: number;
}

export interface WindowSpend {
  since?: string;
  shares?: Share[] | null;
}

// page is the plan windows and who is spending the current one. The shares
// are what the bar's percentage cannot say: which checkout it went on.
export function page(usage: Usage, spend: WindowSpend, now: number): string {
  const out = ["# Plan usage", ""];
  for (const limits of [usage.claude, usage.codex]) {
    if (!limits?.windows?.length) continue;
    out.push(`## ${limits.provider}`, "");
    for (const w of limits.windows)
      out.push(
        `- **${w.name}** ${Math.round(w.percent)}%${resets(w.resetsAt, now)}`,
      );
    if (limits.checkedAt) out.push("", `Read ${when(limits.checkedAt)}.`);
    out.push("");
  }
  if (out.length === 2) out.push("No reading yet.", "");
  if (usage.error) out.push(`The last read failed: ${usage.error}`, "");

  const shares = [...(spend.shares ?? [])].sort((a, b) => b.tokens - a.tokens);
  if (shares.length === 0) return out.join("\n");

  out.push(
    `## What spent this window${spend.since ? `, since ${when(spend.since)}` : ""}`,
    "",
    "| Checkout | Share | Tokens | Sessions |",
    "| --- | ---: | ---: | ---: |",
  );
  for (const share of shares)
    out.push(
      `| ${share.dir} | ${share.percent.toFixed(1)}% | ${tokens(share.tokens)} | ${share.sessions} |`,
    );
  return out.join("\n") + "\n";
}

function resets(at: string | undefined, now: number): string {
  if (!at) return "";
  const left = Date.parse(at) - now;
  if (!Number.isFinite(left) || left <= 0) return "";
  const minutes = Math.round(left / 60000);
  if (minutes < 60) return `, resets in ${minutes}m`;
  const hours = Math.floor(minutes / 60);
  // Past a couple of days the minutes are noise, and the hours nearly are.
  if (hours >= 48) return `, resets in ${Math.round(hours / 24)} days`;
  return `, resets in ${hours}h ${minutes % 60}m`;
}

function when(at: string): string {
  return at.slice(0, 16).replace("T", " ");
}

function tokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${Math.round(n / 1_000)}k`;
  return String(n);
}

// unread keeps the import honest: Limits is the shape page walks.
export type { Limits };
