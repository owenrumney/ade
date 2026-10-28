// The renderer closes every block with a newline; paste gets a blank line.
export function copyable(text: string): string {
  return text.replace(/\n+$/, "");
}

// A mermaid block is drawn, not read, and an empty one has nothing to take.
export function copyWorthy(className: string, text: string): boolean {
  return (
    !className.split(/\s+/).includes("mermaid") && copyable(text).trim() !== ""
  );
}
