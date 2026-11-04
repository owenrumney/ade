// What of the diff was on screen, and for how long. The server does the
// counting; this only reports what it saw.

export interface Span {
  file: string;
  start: number;
  lines: number;
}

export interface Dwelt extends Span {
  ms: number;
}

// A row of the drawn diff, as the DOM has it.
export interface Box {
  line: number;
  top: number;
  bottom: number;
}

// How much of a row has to be inside the viewport to count as on screen.
const enough = 0.5;

// onScreen is the runs of line numbers a viewport holds, as spans. A row half
// off the bottom is not read, so it waits for the scroll that brings it in.
export function onScreen(file: string, rows: Box[], view: { top: number; bottom: number }): Span[] {
  const seen = rows
    .filter((row) => {
      const height = row.bottom - row.top;
      if (height <= 0) return false;
      const shown = Math.min(row.bottom, view.bottom) - Math.max(row.top, view.top);
      return shown / height >= enough;
    })
    .map((row) => row.line)
    .sort((a, b) => a - b);

  const out: Span[] = [];
  for (const [i, line] of seen.entries()) {
    const last = out[out.length - 1];
    if (i > 0 && last && line === last.start + last.lines) {
      last.lines++;
      continue;
    }
    out.push({ file, start: line, lines: 1 });
  }
  return out;
}

// plus adds the time since the last look to every span still on screen. A span
// that has gone keeps what it earned: the server decides what that is worth.
export function plus(pending: Dwelt[], spans: Span[], ms: number): Dwelt[] {
  if (ms <= 0) return pending;
  const out = pending.map((one) => ({ ...one }));
  for (const span of spans) {
    const have = out.find(
      (one) => one.file === span.file && one.start === span.start && one.lines === span.lines,
    );
    if (have) have.ms += ms;
    else out.push({ ...span, ms });
  }
  return out;
}

// merge is two pending reports as one, which is how a report that did not
// land is put back in front of what has been seen since.
export function merge(a: Dwelt[], b: Dwelt[]): Dwelt[] {
  let out = a.map((one) => ({ ...one }));
  for (const one of b) out = plus(out, [one], one.ms);
  return out;
}

// The longest gap between looks that counts as dwell. Anything longer is a
// machine that was asleep or a window nobody was looking at.
export const idleAfter = 4_000;

// rowsOf is every drawn diff: its file, and where each right-hand line sits.
// A hidden tab keeps its pane, so only a pane on screen is asked. The left
// side is the revision being replaced, which the ledger does not count.
export function rowsOf(root: Element | null): { file: string; rows: Box[] }[] {
  const diffs = root
    ? [...root.querySelectorAll<HTMLElement>(".pane:not([hidden]) .diff[data-file]")]
    : [];
  const out: { file: string; rows: Box[] }[] = [];
  for (const diff of diffs) {
    const file = diff.dataset.file ?? "";
    if (!file) continue;
    const rows: Box[] = [];
    for (const node of diff.querySelectorAll<HTMLElement>("[data-right]")) {
      const line = Number(node.dataset.right);
      if (!line) continue;
      const at = node.getBoundingClientRect();
      rows.push({ line, top: at.top, bottom: at.bottom });
    }
    out.push({ file, rows });
  }
  return out;
}
