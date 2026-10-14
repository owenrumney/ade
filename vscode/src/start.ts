import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import {
  accessSync,
  closeSync,
  constants,
  openSync,
  readFileSync,
} from "node:fs";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";

// Where a Go binary actually lands. The editor is often started from Finder
// with a minimal PATH, so the places have to be looked in by name.
export function candidates(
  env: NodeJS.ProcessEnv = process.env,
  home = homedir(),
): string[] {
  const out: string[] = [];
  if (env.ADE_BIN) out.push(env.ADE_BIN);
  if (env.GOBIN) out.push(join(env.GOBIN, "ade"));
  out.push(join(env.GOPATH || join(home, "go"), "bin", "ade"));
  out.push(
    "/opt/homebrew/bin/ade",
    "/usr/local/bin/ade",
    join(home, "bin", "ade"),
  );
  return out;
}

// findAde falls back to the bare name, which works when the editor was started
// from a terminal and is the only thing left to try when it was not.
export function findAde(
  env: NodeJS.ProcessEnv = process.env,
  home = homedir(),
  can: (path: string) => boolean = executable,
): string {
  return candidates(env, home).find(can) || "ade";
}

function executable(path: string): boolean {
  try {
    accessSync(path, constants.X_OK);
    return true;
  } catch {
    return false;
  }
}

// The launcher, as the shell starts one. --idle 0 so it does not shut itself
// down while the editor is the only thing watching.
export const launcherArgs = [
  "launch",
  "--foreground",
  "--no-open",
  "--idle",
  "0",
];

// A checkout's own server. No browser: this window is the browser.
export const serverArgs = ["--foreground", "--no-open"];

// start runs ade and leaves it running. Detached, because a server that dies
// with the editor is not the one every other window is also using.
// A machine with no ade is ordinary: a window on a worker has the sessions it
// serves and no cli of its own.
export function start(
  args: string[],
  cwd?: string,
  onMissing?: () => void,
  onFailed?: (why: string) => void,
): void {
  // A file, not a pipe: a pipe closes with the editor and SIGPIPEs the server.
  const log = startLog(cwd);
  const stderr = openLog(log);
  const child = spawn(findAde(), args, {
    cwd,
    detached: true,
    stdio: ["ignore", "ignore", stderr ?? "ignore"],
    env: process.env,
  });
  if (stderr !== undefined) closeSync(stderr);
  child.on("error", (err: NodeJS.ErrnoException) => {
    if (missing(err)) onMissing?.();
  });
  child.on("exit", (code) => {
    if (code) onFailed?.(lastLine(readLog(log)) || `ade exited with ${code}`);
  });
  child.unref();
}

export function startLog(cwd = "", dir = tmpdir()): string {
  const id = createHash("sha256").update(cwd).digest("hex").slice(0, 12);
  return join(dir, `ade-start-${id}.log`);
}

export function lastLine(text: string): string {
  return text.trim().split("\n").pop()?.trim() ?? "";
}

function openLog(path: string): number | undefined {
  try {
    return openSync(path, "w", 0o600);
  } catch {
    return undefined;
  }
}

function readLog(path: string): string {
  try {
    return readFileSync(path, "utf8");
  } catch {
    return "";
  }
}

// missing is a spawn that failed because there is no ade here, as against one
// that failed for a reason worth an error: a permission, a broken binary.
export function missing(err: NodeJS.ErrnoException): boolean {
  return err.code === "ENOENT";
}
