// spans folds a sorted list of line numbers into runs. A turn usually writes a
// block, and one decoration per run beats one per line for the same picture.
export function spans(lines: number[]): { from: number; to: number }[] {
  const sorted = [...new Set(lines)].filter((n) => n > 0).sort((a, b) => a - b);
  const out: { from: number; to: number }[] = [];
  for (const line of sorted) {
    const last = out[out.length - 1];
    if (last && line === last.to + 1) last.to = line;
    else out.push({ from: line, to: line });
  }
  return out;
}

// summarise is what the hover says about the turns behind a line. No commit is a
// different answer from a commit, not a gap.
export function summarise(
  turns: { at: string; summary: string; commit?: string }[],
): string {
  if (turns.length === 0) return "";
  const lines = turns.map((t) => {
    const when = t.at ? new Date(t.at).toLocaleString() : "";
    const where = t.commit
      ? ` · ${t.commit.slice(0, 8)}`
      : " · not committed yet";
    return `- ${t.summary}\n  \n  _${when}${where}_`;
  });
  return `**Why is this here?**\n\n${lines.join("\n")}`;
}
