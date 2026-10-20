// A session on another machine is looked at in a window on that machine: the
// checkout is a real directory there, so the editor gets its files for free.
// ade resolves that window's authority itself; see resolver.ts.
import { AUTHORITY } from "./resolver";

// authority is what VS Code calls the far side of an ssh connection. The host
// is an ssh destination, which is what ade bookmarks and what ssh takes.
export function authority(host: string): string {
  return `${AUTHORITY}+${host}`;
}

// folderURI addresses a directory on that machine. The path is absolute and
// the machine's own, which is what the launcher reports for a remote checkout.
export function folderURI(host: string, dir: string): string {
  return `vscode-remote://${authority(host)}${dir}`;
}

// said is what to tell someone who has just started work elsewhere.
export function said(machine: string, verb = "started"): string {
  return `ade: ${verb} on ${machine}.`;
}

// machineWide is whether this window can talk to a launcher: a window on
// another machine is about one session, not the machine you are sitting at.
export function machineWide(remoteName: string | undefined): boolean {
  return remoteName === undefined;
}

// whyNotHere is what to say instead of offering a machine-wide surface. The
// launcher is here and owns the host, so "there is none" is not the reason.
export function whyNotHere(what: "session" | "surface"): string {
  const where =
    what === "session"
      ? "Start one from the window on your own machine, with ade: Start work on another machine."
      : "Machines, jobs and disk are in the window on your own machine.";
  return `ade: this window is one session on another machine. ${where}`;
}

// launcherAt is the launcher a session was reached through: the proxied
// address with the /s/<id> taken off, which is that launcher's own origin.
export function launcherAt(
  base: string,
  token: string,
):
  | {
      base: string;
      token: string;
    }
  | undefined {
  let parsed: URL;
  try {
    parsed = new URL(base);
  } catch {
    return undefined;
  }
  if (!/^\/s\/[^/]+$/.test(parsed.pathname)) return undefined;
  return { base: parsed.origin, token };
}

// Serving is one session a launcher reports as running on its machine.
export interface Serving {
  id: string;
  dir: string;
}

// currentProxy is the address to reach a checkout's session at now. The one in
// the record carries a session id, and a new conversation changes it.
export function currentProxy(
  base: string,
  token: string,
  root: string,
  serving: Serving[],
): string | undefined {
  const here = serving.find((s) => s.dir === root);
  if (!here) return undefined;
  return `${base}/s/${encodeURIComponent(here.id)}/?t=${encodeURIComponent(token)}`;
}
