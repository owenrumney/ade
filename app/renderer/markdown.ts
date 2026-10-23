// Markdown as ade reads it: headings, lists, fences, tables and the
// three inline marks. Not a complete implementation.

export interface Block {
  kind: "h1" | "h2" | "h3" | "p" | "ul" | "ol" | "code" | "quote" | "rule" | "table";
  text?: string;
  items?: string[];
  rows?: string[][];
  lang?: string;
}

// blocks splits a document into what it is made of. Line-based, because
// every block markdown has is decided by how a line starts.
export function blocks(src: string): Block[] {
  const out: Block[] = [];
  const lines = src.replace(/\r\n/g, "\n").split("\n");
  let at = 0;

  // A wrapped line belongs to the item above it, not to a paragraph of
  // its own: markdown ends an item at a blank line or the next marker.
  const list = (marker: RegExp, kind: "ul" | "ol") => {
    const items: string[] = [];
    while (at < lines.length && marker.test(lines[at])) {
      let item = lines[at].replace(marker, "");
      at++;
      while (at < lines.length && lines[at].trim() && !starts(lines[at])) {
        item += " " + lines[at].trim();
        at++;
      }
      items.push(item);
    }
    out.push({ kind, items });
  };

  while (at < lines.length) {
    const line = lines[at];

    if (!line.trim()) {
      at++;
      continue;
    }
    if (/^```/.test(line)) {
      const lang = line.slice(3).trim();
      const held: string[] = [];
      at++;
      while (at < lines.length && !/^```/.test(lines[at])) held.push(lines[at++]);
      at++;
      out.push({ kind: "code", text: held.join("\n"), lang });
      continue;
    }
    if (/^---+\s*$/.test(line)) {
      out.push({ kind: "rule" });
      at++;
      continue;
    }
    const heading = /^(#{1,3})\s+(.*)$/.exec(line);
    if (heading) {
      out.push({ kind: `h${heading[1].length}` as Block["kind"], text: heading[2] });
      at++;
      continue;
    }
    if (/^>\s?/.test(line)) {
      const held: string[] = [];
      while (at < lines.length && /^>\s?/.test(lines[at])) {
        held.push(lines[at].replace(/^>\s?/, ""));
        at++;
      }
      out.push({ kind: "quote", text: held.join(" ") });
      continue;
    }
    if (/^\s*[-*]\s+/.test(line)) {
      list(/^\s*[-*]\s+/, "ul");
      continue;
    }
    if (/^\s*\d+\.\s+/.test(line)) {
      list(/^\s*\d+\.\s+/, "ol");
      continue;
    }
    if (/^\|.*\|\s*$/.test(line)) {
      const rows: string[][] = [];
      while (at < lines.length && /^\|.*\|\s*$/.test(lines[at])) {
        const cells = lines[at].trim().slice(1, -1).split("|").map((c) => c.trim());
        // The ---|--- row under a header is layout, not content.
        if (!cells.every((c) => /^:?-{2,}:?$/.test(c))) rows.push(cells);
        at++;
      }
      out.push({ kind: "table", rows });
      continue;
    }
    // A paragraph runs until a blank line or the start of another block.
    const held: string[] = [];
    while (at < lines.length && lines[at].trim() && !starts(lines[at])) {
      held.push(lines[at++].trim());
    }
    // A line that only opens a block leaves nothing to say; step past it
    // rather than looping on it for ever.
    if (held.length === 0) {
      at++;
      continue;
    }
    out.push({ kind: "p", text: held.join(" ") });
  }
  return out;
}

// starts is whether a line opens a block of its own, which is what ends
// the paragraph or list item above it.
function starts(line: string): boolean {
  return /^(#{1,3}\s|```|---+\s*$|>|\s*[-*]\s|\s*\d+\.\s|\|)/.test(line);
}

// inline marks up one run of text: code, bold, italic, links. Returns
// html, so every piece of the source is escaped on the way through.
export function inline(text: string): string {
  const safe = text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
  return safe
    .replace(/`([^`]+)`/g, "<code>$1</code>")
    .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>")
    .replace(/(^|[\s(])\*([^*\s][^*]*)\*/g, "$1<em>$2</em>")
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2">$1</a>');
}
