import { execFile } from "node:child_process";
import { promises as fs } from "node:fs";
import { connect } from "node:net";
import { homedir } from "node:os";
import { join } from "node:path";

export interface Target {
  host: string;
  port: number;
}

// macOS asks about Local Network only when the app's own process dials the
// LAN, and everything the app starts inherits that verdict.
export async function askForLocalNetwork(): Promise<void> {
  let raw: string;
  try {
    raw = await fs.readFile(join(homedir(), ".ade", "settings.json"), "utf8");
  } catch {
    return;
  }
  const targets = await Promise.all(bookmarkedHosts(raw).map(resolveTarget));
  await Promise.all(targets.filter((t) => !isLoopback(t.host)).map(dial));
}

export function bookmarkedHosts(settings: string): string[] {
  try {
    const parsed = JSON.parse(settings) as { hosts?: { name?: unknown }[] };
    return (parsed.hosts ?? [])
      .map((h) => h.name)
      .filter((n): n is string => typeof n === "string" && n !== "");
  } catch {
    return [];
  }
}

export function parseSshConfig(output: string, fallback: Target): Target {
  let { host, port } = fallback;
  for (const line of output.split("\n")) {
    const [key, value] = line.trim().split(/\s+/, 2);
    if (key === "hostname" && value) host = value;
    else if (key === "port" && Number(value) > 0) port = Number(value);
  }
  return { host, port };
}

export function isLoopback(host: string): boolean {
  return host === "localhost" || host === "::1" || host.startsWith("127.");
}

function resolveTarget(name: string): Promise<Target> {
  const fallback = { host: name.slice(name.lastIndexOf("@") + 1), port: 22 };
  return new Promise((resolve) => {
    execFile("ssh", ["-G", name], { timeout: 2000 }, (err, stdout) => {
      resolve(err ? fallback : parseSshConfig(stdout, fallback));
    });
  });
}

function dial(target: Target): Promise<void> {
  return new Promise((resolve) => {
    const socket = connect({ host: target.host, port: target.port, timeout: 3000 });
    const done = () => {
      socket.destroy();
      resolve();
    };
    socket.once("connect", done);
    socket.once("timeout", done);
    socket.once("error", done);
  });
}
