import type { Estimate } from "./client";
import type { Host, Model } from "./launcher";

// Where the work happens. A machine is a third answer rather than a second
// question: it leaves this session alone either way.
export interface Where {
  id: "here" | "worktree" | "machine";
  machine: string;
  label: string;
  detail: string;
}

export const AGENTS = [
  { id: "claude", label: "Claude Code" },
  { id: "codex", label: "Codex" },
] as const;

// wheres is what this session can actually offer: no local checkout on a
// snapshot, no worktree without the server's proposal, no disconnected machine.
export function wheres(
  branch: string | undefined,
  hosts: Host[],
  snapshot: boolean,
  tree?: { branch: string } | null,
): Where[] {
  const out: Where[] = [];
  if (!snapshot) {
    out.push({
      id: "here",
      machine: "",
      label: "Here",
      detail: `in this checkout, on ${branch ?? "this branch"} — the conversation is cleared`,
    });
    if (tree) {
      out.push({
        id: "worktree",
        machine: "",
        label: "In a new worktree",
        detail: `beside this checkout, on ${tree.branch}, with its own agent`,
      });
    }
  }
  for (const host of hosts.filter((h) => !h.local && h.connected)) {
    out.push({
      id: "machine",
      machine: host.name,
      label: host.label || host.name,
      detail:
        "clones the repository there and builds; this session stays as it is",
    });
  }
  return out;
}

// Which models the picked agent runs. The empty one is the agent's own
// default, which is what every other door here offers.
export function models(
  all: Model[],
  agent: string,
): { id: string; label: string }[] {
  const mine = all
    .filter((m) => m.agent === agent)
    .map((m) => ({ id: m.id, label: m.label || m.id }));
  return [{ id: "", label: `default — whatever ${agent} uses` }, ...mine];
}

// The sentence under the confirmation. Built from past runs, so it says how
// many it is built from: "usually 20–40 min" from one run is not a usually.
export function says(got: Estimate | undefined): string {
  if (!got || got.runs === 0) return "";
  const bits = [`usually ${span(got.lowSeconds, got.highSeconds)}`];
  const window = got.window;
  if (window) {
    bits.push(
      `about ${Math.max(1, Math.round(window.about))}% of your ${window.name} window (${Math.round(window.used)}% used)`,
    );
  }
  bits.push(
    `from ${got.runs} ${got.kind}${got.runs === 1 ? "" : "s"} ${got.group}`,
  );
  return bits.join(" · ");
}

export function span(low: number, high: number): string {
  const from = Math.max(1, Math.floor(low / 60));
  const to = Math.max(from, Math.ceil(high / 60));
  return to < 60 ? `${from}–${to} min` : `${clock(from)}–${clock(to)}`;
}

function clock(minutes: number): string {
  if (minutes < 60) return `${minutes} min`;
  const rest = minutes % 60;
  return rest ? `${Math.floor(minutes / 60)}h ${rest}m` : `${minutes / 60}h`;
}
