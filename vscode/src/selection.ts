import { relative } from "node:path";

// where names a selection the way a person would: the file, and the line or the
// span it covers. It is what the composer shows above the code.
export function where(
  root: string,
  file: string,
  from: number,
  to: number,
): string {
  const path = relative(root, file) || file;
  const rel = path.split("\\").join("/");
  return from === to ? `${rel}:${from}` : `${rel}:${from}-${to}`;
}

// trimIndent drops the indentation every line shares, so a selection from deep
// inside a function does not arrive as a column of whitespace.
export function trimIndent(text: string): string {
  const lines = text.replace(/\s+$/, "").split("\n");
  const indents = lines
    .filter((line) => line.trim() !== "")
    .map((line) => line.length - line.trimStart().length);
  const common = indents.length ? Math.min(...indents) : 0;
  return lines.map((line) => line.slice(common)).join("\n");
}
