import type { VerifyCheck } from "./client";

// The check list as the one-line edit box shows it, and back. A grid would be
// a webview; a line you can read in the box is the whole of the editing.

// parse reads `name=command | name=command`. A part with no `=` is a command
// named after its first word, which is what you meant.
export function parse(typed: string): VerifyCheck[] {
  const out: VerifyCheck[] = [];
  for (const part of typed.split("|")) {
    const text = part.trim();
    if (!text) continue;
    const at = text.indexOf("=");
    if (at < 0) {
      out.push({ name: text.split(/\s+/)[0], run: text });
      continue;
    }
    const name = text.slice(0, at).trim();
    const run = text.slice(at + 1).trim();
    if (name && run) out.push({ name, run });
  }
  return out;
}

// typed is what the box is opened with, which parse reads back.
export function typed(checks: VerifyCheck[]): string {
  return checks.map((check) => `${check.name}=${check.run}`).join(" | ");
}

// kept gives an edited list back the fields the box cannot show. A check whose
// name and command are unchanged is the same check, so its narrow form stays.
export function kept(edited: VerifyCheck[], had: VerifyCheck[]): VerifyCheck[] {
  return edited.map((check) => {
    const was = had.find((h) => h.name === check.name && h.run === check.run);
    if (!was) return check;
    return { ...check, dir: was.dir, narrow: was.narrow };
  });
}
