// What the frame hides and what it draws. Kept out of the page so the rule
// can be tested without a DOM: given a node and where the cursor is, does the
// syntax show?

// Node names lezer-markdown gives the markers ade hides. The text they wrap
// stays; only the marker goes.
export const MARKERS = new Set([
  "HeaderMark",
  "EmphasisMark",
  "StrikethroughMark",
  "CodeMark",
  "LinkMark",
  "QuoteMark",
  "ListMark",
  // A link's target is syntax: the text is what the document reads as.
  "URL",
]);

// Nodes whose whole range is replaced by something drawn.
export const BLOCKS = new Set(["FencedCode", "Table", "HorizontalRule"]);

// A span of the document, as lezer reports a node.
export interface Span {
  from: number;
  to: number;
}

// touched is whether the cursor is inside a span, counting both edges: a
// cursor resting just after `**` is still editing that emphasis.
export function touched(span: Span, head: number): boolean {
  return head >= span.from && head <= span.to;
}

// reveals is whether a node shows its source. Scope decides what counts as
// near: a marker reveals when the cursor is on it, a block when the cursor is
// anywhere in the block. A list marker never hides — removing it would move
// the text left and lose the shape of the list.
export function reveals(
  name: string,
  marker: Span,
  parent: Span,
  head: number | undefined,
): boolean {
  if (head === undefined) return false;
  return touched(BLOCKS.has(name) ? marker : parent, head);
}

// hides is whether this node is one the frame takes out of view at all.
export function hides(name: string): boolean {
  return MARKERS.has(name);
}

// A heading's level, from the run of # it opens with. Zero when the mark is
// not a heading's, which is how a setext underline reads.
export function level(markerText: string): number {
  const run = /^#{1,6}/.exec(markerText.trim());
  return run ? run[0].length : 0;
}

// The # run is the whole of the node: the space after it is heading text, and
// hiding only the run leaves every heading indented by it.
export function gap(after: string): number {
  const run = /^[ \t]+/.exec(after);
  return run ? run[0].length : 0;
}

// lang is the language of a fenced block, for the label a drawn block keeps
// and for spotting the mermaid ones.
export function lang(fenceLine: string): string {
  return fenceLine
    .trim()
    .replace(/^[`~]{3,}/, "")
    .trim()
    .split(/\s+/)[0]
    .toLowerCase();
}

export function isMermaid(fenceLine: string): boolean {
  return lang(fenceLine) === "mermaid";
}

// bullet is what a list marker is drawn as. An ordered list keeps its number,
// which is what it reads as either way; a bullet's "-" is syntax.
export function bullet(markerText: string): string | undefined {
  return /^[-*+]$/.test(markerText.trim()) ? "•" : undefined;
}

// cells splits a GFM table into rows, dropping the alignment row: it is the
// one line of a table that says nothing once the table is drawn.
export function cells(src: string): string[][] {
  return src
    .split("\n")
    .map((line) => line.trim())
    .filter((line) => line.startsWith("|"))
    .filter((line) => !/^\|[\s:|-]+\|?$/.test(line))
    .map((line) =>
      line
        .replace(/^\|/, "")
        .replace(/\|$/, "")
        .split("|")
        .map((cell) => cell.trim()),
    );
}

// body is a fenced block's contents, without its fences. What mermaid draws.
export function body(block: string): string {
  const lines = block.split("\n");
  if (lines.length > 0 && /^\s*[`~]{3,}/.test(lines[0])) lines.shift();
  if (lines.length > 0 && /^\s*[`~]{3,}\s*$/.test(lines[lines.length - 1]))
    lines.pop();
  return lines.join("\n");
}

// picture reads `![alt](url "title")`. Only a relative path resolves: the
// frame may load files beside the document and nothing else.
export function picture(src: string, base: string): { alt: string; url: string } | undefined {
  const m = /^!\[([^\]]*)\]\(\s*<?([^\s>)]+)>?(?:\s+["'(].*)?\)$/.exec(src.trim());
  if (!m || !base) return undefined;
  const [, alt, path] = m;
  if (/^[a-z][a-z0-9+.-]*:/i.test(path) || path.startsWith("/")) return undefined;
  return { alt, url: new URL(path, base.endsWith("/") ? base : `${base}/`).toString() };
}
