// ade resolves its own remote authority: ADE.app is a VSCodium build, and
// Microsoft's Remote-SSH is licensed only for Microsoft's. See the doc.
import type { Server } from "./discover";

// Not Microsoft's ssh-remote: every other resolver claims that one.
export const AUTHORITY = "ade-ssh";

export interface Resolved {
  host: string;
  port: number;
  token: string;
  commit: string;
}

// Failed with retry is for what a retry could fix — a laptop that slept, a
// dropped tailnet, a server past its idle shutdown.
export class Failed extends Error {
  constructor(
    message: string,
    readonly retry: boolean,
  ) {
    super(message);
  }
}

// hostOf is the machine an authority names. VS Code hands over the whole
// authority, and ade bookmarks only the destination part.
export function hostOf(authority: string): string {
  const [kind, ...rest] = authority.split("+");
  return rest.length ? rest.join("+") : kind;
}

// Starting a launcher from inside a resolve would put a second ade on the
// machine, for the one case where a local window is right there.
export const NO_LAUNCHER =
  "ade's launcher is not running. Open ADE.app and try again.";

// Deps are what a resolve needs, injected so the path can be tested without
// a machine. unvalidated is the one-shot the omnibox pressed.
export interface Deps {
  launcher(): Promise<Server | undefined>;
  editorServer(
    server: Server,
    host: string,
    unvalidated: boolean,
  ): Promise<{ port: number; token: string; commit: string }>;
  unvalidated(host: string): Promise<boolean>;
  log(line: string): void;
}

// resolve is stateless: the workbench reconnects its own websocket, so this
// is the reconnect path too and it asks the launcher every time.
export async function resolve(
  authority: string,
  deps: Deps,
): Promise<Resolved> {
  const host = hostOf(authority);
  const launcher = await deps.launcher();
  if (!launcher) throw new Failed(NO_LAUNCHER, false);

  const unvalidated = await deps.unvalidated(host);
  deps.log(
    unvalidated
      ? `resolving ${host} against an unvalidated server`
      : `resolving ${host}`,
  );
  try {
    const got = await deps.editorServer(launcher, host, unvalidated);
    deps.log(`${host}: server ${got.commit} on 127.0.0.1:${got.port}`);
    return { host, ...got };
  } catch (err) {
    throw new Failed(`ade could not reach ${host}: ${String(err)}`, true);
  }
}

// label is what the remote indicator reads. The raw authority would show an
// implementation detail where anyone looks to check which machine they are on.
export function label(host: string): string {
  return `ade: ${host}`;
}

// One workflow run publishes linux arm64 and x64 together, so either arch
// answers the preflight for both.
export const PREFLIGHT_ARCH = "x64";

// serverURL fills in product.json's serverDownloadUrlTemplate. The launcher
// does it again for the host's own arch; this is for the HEAD.
export function serverURL(template: string, arch = PREFLIGHT_ARCH): string {
  return template.replace("${os}", "linux").replace("${arch}", arch);
}

// releaseVersion is the tag the server is published under, which is the only
// thing the publish run needs.
export function releaseVersion(template: string): string {
  return /\/download\/([^/]+)\//.exec(template)?.[1] ?? "";
}

export function noServer(version: string): string {
  return `ade has published no editor server for this build (${version}).`;
}

export const PUBLISH = "Publish the server";
export const UNVALIDATED = "Connect unvalidated";

// published is the preflight: opening the window anyway fails on the host
// minutes later, on the far side.
export async function published(
  url: string,
  head: (url: string) => Promise<number>,
): Promise<boolean> {
  try {
    return (await head(url)) < 400;
  } catch {
    // A network that cannot answer is not a missing server, and refusing the
    // window here would be the wrong diagnosis.
    return true;
  }
}

export function dispatch(version: string): string[] {
  return [
    "workflow",
    "run",
    "editor-reh.yml",
    "-f",
    `release_version=${version}`,
  ];
}

// noGh names the one command to run where gh is absent or not logged in.
export function noGh(version: string): string {
  return `ade could not run gh. Publish the server with: gh workflow run editor-reh.yml -f release_version=${version}`;
}
