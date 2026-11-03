// The palette a panel is given. A panel is an iframe, so it cannot reach
// the window's theme.css: the hexes are here again under the same names.

// Anything not here falls back to the browser: add it rather than
// restyling the panel.
export const THEME = `
  --ui: -apple-system, "Segoe UI", Ubuntu, "Droid Sans", sans-serif;
  --ui-size: 13px;
  --mono: "Berkeley Mono", ui-monospace, "SF Mono", Menlo, Consolas, monospace;
  --mono-size: 12px;

  --ink: #bfbdb6;
  --ink-dim: #5a6378;
  --ink-bright: #ffffff;

  --bg: #0d1017;
  --bg-chrome: #0d1017;
  --bg-raised: #141821;
  --line: #1b1f29;
  --line-strong: #2d333f;
  --line-no: #5a6378a6;
  --selection: #409fff40;

  --accent: #e6b450;
  --accent-ink: #765b24;
  --accent-hover: #f0c368;
  --accent-soft: #47526640;
  --hover: #47526640;
  --selected-quiet: #47526633;
  /* A gold button on a dark ground needs no edge of its own. */
  --button-edge: transparent;

  --input-bg: #10141c;

  --good: #aad94c;
  --warn: #e6b450;
  --bad: #f26d78;
  --info: #73b8ff;
  --info-bright: #8fc8ff;

  --tint-good: #70bf5633;
  --tint-warn: #e6b45033;
  --tint-bad: #f26d7833;
  --tint-info: #73b8ff33;
  --tint-dim: #47526633;

  --git-add: #70bf56;
  --git-del: #f26d78;

  /* The chat's two voices. Read by chat.css and set nowhere until now. */
  --said: #aad94c;
  --heard: #ffb454;

  /* A series, for a bar that is neither good nor bad: Ayu's own hues. */
  --chart-orange: #ff8f40;
  --chart-purple: #d2a6ff;
`;

// What a stylesheet declares, and what it reads: the test that holds the
// panels and this file in step needs both sides by name.
export function declared(css: string): string[] {
  return sorted(css.matchAll(/^\s*(--[a-zA-Z0-9-]+)\s*:/gm));
}

export function read(css: string): string[] {
  return sorted(css.matchAll(/var\(\s*(--[a-zA-Z0-9-]+)/g));
}

// taken is what a page reads from the palette in TypeScript, where no var()
// says so: a quoted name, less the ones the page sets for itself.
export function taken(ts: string): string[] {
  const own = new Set(sorted(ts.matchAll(/setProperty\(\s*["'`](--[a-zA-Z0-9-]+)/g)));
  const named = sorted(ts.matchAll(/["'`](--[a-zA-Z0-9-]+)["'`]/g));
  return [...new Set([...read(ts), ...named])].filter((v) => !own.has(v)).sort();
}

function sorted(found: Iterable<RegExpMatchArray>): string[] {
  return [...new Set([...found].map((m) => m[1]))].sort();
}
