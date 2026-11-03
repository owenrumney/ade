// Notes are comments pinned to quoted text. The store says which occurrence
// of the quote a note sits on; this finds that occurrence in what is on screen.

export interface NoteComment {
  id: string;
  author: string;
  body: string;
  at: string;
}

export interface Note {
  id: string;
  anchor: { quote: string; nth: number };
  comments: NoteComment[];
  resolved: boolean;
  orphaned?: boolean;
}

export interface Span {
  from: number;
  to: number;
}

// occurrences lists where quote starts, not overlapping. The same walk the
// store makes: the count has to agree, or nth means two different places.
export function occurrences(text: string, quote: string): number[] {
  const out: number[] = [];
  if (!quote) return out;
  for (let from = 0; from + quote.length <= text.length; ) {
    const at = text.indexOf(quote, from);
    if (at < 0) break;
    out.push(at);
    from = at + quote.length;
  }
  return out;
}

// spanOf is where a note sits. The page's text can be a beat behind the store,
// so a quote that is there but not that many times falls back to the first.
export function spanOf(text: string, note: Note): Span | undefined {
  const quote = note.anchor?.quote ?? "";
  const found = occurrences(text, quote);
  if (found.length === 0) return undefined;
  const at = found[note.anchor.nth] ?? found[0];
  return { from: at, to: at + quote.length };
}

// nthOf is which occurrence of quote the selection at from is, which is how
// the page tells the store which span it means.
export function nthOf(text: string, from: number, quote: string): number {
  const n = occurrences(text, quote).indexOf(from);
  return n < 0 ? 0 : n;
}

export interface Mark extends Span {
  id: string;
  cls: string;
}

// marksFor is every note that can be drawn on this text, in document order.
// An orphan has nothing to draw on, so it is not here: its quote has gone.
export function marksFor(text: string, notes: Note[]): Mark[] {
  const out: Mark[] = [];
  for (const note of notes) {
    const span = spanOf(text, note);
    if (!span) continue;
    out.push({ ...span, id: note.id, cls: note.resolved ? "cm-note cm-note-done" : "cm-note" });
  }
  return out.sort((a, b) => a.from - b.from || a.to - b.to);
}

// quoted is what a note filed from the gutter is pinned to: the selection when
// it reaches into that line, and the line's own words when it does not. Blank
// for a line with nothing on it, which has nothing to quote.
export function quoted(text: string, line: Span, selection: Span): Span | undefined {
  const picked = selection.to > selection.from && selection.to > line.from && selection.from < line.to;
  const span = picked
    ? { from: Math.max(selection.from, 0), to: Math.min(selection.to, text.length) }
    : line;
  const before = text.slice(span.from, span.to);
  const from = span.from + (before.length - before.trimStart().length);
  const to = span.to - (before.length - before.trimEnd().length);
  return to > from ? { from, to } : undefined;
}
