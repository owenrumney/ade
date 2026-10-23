// Usage is the launcher's reading of the plan windows, per provider.
export interface Window {
  name: string;
  percent: number;
  resetsAt?: string;
}

export interface Limits {
  provider: string;
  windows: Window[] | null;
  checkedAt?: string;
}

export interface Usage {
  claude?: Limits;
  codex?: Limits;
  error?: string;
}

export interface Badge {
  text: string;
  tooltip: string;
  warn: boolean;
}

// Past this much of a window the bar says so in colour, while there is still
// some of it left to be careful with.
export const NEAR = 80;

// STALE is how old a reading may be before its age is worth saying. Codex
// reports on a run, so a quiet day leaves the last one standing.
export const STALE = 60 * 60 * 1000;

// Plan is what a status bar says about the windows, without saying how: both
// front ends decide it the same way and draw their own icon.
export interface Plan {
  windows: Window[];
  bars: string;
  tooltip: string;
  warn: boolean;
  stale: boolean;
}

// plan is the first provider with a reading, every window it has. No reading
// is no item, not a zero.
export function plan(usage: Usage | undefined, now: number): Plan | undefined {
  const read = [usage?.claude, usage?.codex].filter(
    (l): l is Limits => (l?.windows?.length ?? 0) > 0,
  );
  if (read.length === 0) return undefined;
  const shown = read[0].windows ?? [];
  const lines = read.flatMap((l) => [
    ...(l.windows ?? []).map((w) => `${l.provider} ${w.name}: ${say(w, now)}`),
    ...(stale(l, now)
      ? [`${l.provider}: last read ${span(age(l, now))} ago`]
      : []),
  ]);
  // Why the numbers stopped moving, beside how old they are.
  const why = usage?.error ? [`The last read failed: ${usage.error}`] : [];
  return {
    windows: shown,
    bars: shown.map((w) => `${w.name} ${pct(w)}`).join(" · "),
    tooltip: ["Plan usage", ...lines, ...why, "Click for what spent it."].join("\n"),
    warn: shown.some((w) => w.percent >= NEAR),
    stale: stale(read[0], now),
  };
}

// badge is plan, spelled the way the editor's status bar wants it.
export function badge(
  usage: Usage | undefined,
  now: number,
): Badge | undefined {
  const it = plan(usage, now);
  if (!it) return undefined;
  return {
    text: `$(dashboard) ${it.stale ? "~" : ""}${it.bars}`,
    tooltip: it.tooltip,
    warn: it.warn,
  };
}

// A reading with no checkedAt is taken at its word: the age is the server's to
// report, and absent is not old.
function age(l: Limits, now: number): number {
  return l.checkedAt ? now - Date.parse(l.checkedAt) : NaN;
}

function stale(l: Limits, now: number): boolean {
  return age(l, now) > STALE;
}

function pct(w: Window): string {
  return `${Math.round(w.percent)}%`;
}

function say(w: Window, now: number): string {
  const left = w.resetsAt ? Date.parse(w.resetsAt) - now : NaN;
  // Go sends a zero time as year 1, which is a reset long past, not soon.
  if (!(left > 0)) return pct(w);
  return `${pct(w)}, resets in ${span(left)}`;
}

export function span(ms: number): string {
  const mins = Math.round(ms / 60000);
  if (mins < 60) return `${mins}m`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ${mins % 60}m`;
  return `${Math.floor(hours / 24)}d ${hours % 24}h`;
}
