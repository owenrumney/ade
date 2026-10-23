import { execFile } from "node:child_process";

const MARK = "__ADE_PATH__";

// An app opened from the Dock gets launchd's PATH, not the shell's, so
// claude and gh under /opt/homebrew are not found by what it starts.
export function shellPath(shell = process.env.SHELL || "/bin/zsh"): Promise<string | undefined> {
  return new Promise((done) => {
    execFile(
      shell,
      ["-ilc", `printf '${MARK}%s${MARK}' "$PATH"`],
      { timeout: 5000 },
      (_err, out) => done(between(String(out ?? ""))),
    );
  });
}

export function between(out: string): string | undefined {
  const at = out.indexOf(MARK);
  const end = out.indexOf(MARK, at + MARK.length);
  if (at < 0 || end < 0) return undefined;
  return out.slice(at + MARK.length, end) || undefined;
}
