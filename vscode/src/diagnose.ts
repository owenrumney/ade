import type { Server } from "./discover";

export interface Probe {
  name: string;
  status?: string;
  reachable: boolean;
  refused?: boolean;
  took: string;
  reached?: string;
  err?: string;
}

export interface HostProbe {
  name: string;
  address: string;
  reachable: boolean;
  took: string;
  err?: string;
  hint?: string;
}

export interface Diagnosis {
  results: Probe[] | null;
  hosts: HostProbe[] | null;
  files?: { open: number; limit: number };
}

// diagnose asks the launcher what it can reach. From that process, not this
// one: the editor's own fetch is judged under a different network policy.
export async function diagnose(server: Server): Promise<Diagnosis> {
  const res = await fetch(
    `${server.base}/api/diagnose/net?t=${encodeURIComponent(server.token)}`,
    { method: "POST", headers: { "X-Ade-Token": server.token } },
  );
  if (!res.ok) throw new Error(`diagnose: ${res.status}`);
  return (await res.json()) as Diagnosis;
}

// report is the diagnosis as a markdown page, machines first: a LAN host that
// will not answer is the question this is usually asked for.
export function netReport(found: Diagnosis): string {
  const out = ["# What ade can reach", "", "## Machines", ""];
  const hosts = found.hosts ?? [];
  if (!hosts.length) out.push("No machines bookmarked.");
  for (const host of hosts) {
    out.push(
      host.reachable
        ? `- **${host.name}** — ssh port open at \`${host.address}\` in ${host.took}`
        : `- **${host.name}** — UNREACHABLE at \`${host.address}\` after ${host.took}: ${host.err ?? "no answer"}`,
    );
    if (host.hint) out.push(`  - ${host.hint}`);
  }
  out.push("", "## Services", "");
  for (const probe of found.results ?? []) out.push(`- ${service(probe)}`);
  if (found.files)
    out.push("", `Open files: ${found.files.open} of ${found.files.limit}.`);
  return out.join("\n") + "\n";
}

function service(probe: Probe): string {
  if (probe.err)
    return `**${probe.name}** — UNREACHABLE after ${probe.took} (got as far as: ${probe.reached ?? "nothing"}): ${probe.err}`;
  if (probe.refused)
    return `**${probe.name}** — reachable, but refused the credential (${probe.status})`;
  return `**${probe.name}** — ok, ${probe.status} in ${probe.took}`;
}
