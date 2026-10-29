// What the Machines panel draws, worked out from what /api/hosts answers.
// The DOM is main.ts.

export interface Host {
  name: string;
  label?: string;
  connected?: boolean;
  url?: string;
  local?: boolean;
  state?: string;
  sessions?: number;
  error?: string;
  warning?: string;
  working?: string;
  failed?: string;
}

// Shown is one row as the list draws it. `act` is the only verb the row
// offers, because a machine is either dialled or it is not.
export interface Shown {
  name: string;
  label: string;
  state: "connected" | "quiet" | "off" | "here";
  note: string;
  sessions: number;
  warning: string;
  error: string;
  act: "connect" | "disconnect" | "";
}

// Quiet is up but its last answer failed. Drawing that as connected is how
// an afternoon goes: the machine answers nothing and the row looks fine.
export function stateOf(host: Host): Shown["state"] {
  if (host.local) return "here";
  if (!host.connected) return "off";
  return host.state === "quiet" ? "quiet" : "connected";
}

export function shown(host: Host): Shown {
  const state = stateOf(host);
  return {
    name: host.name,
    label: host.label || host.name,
    state,
    // The ssh name, under a label that hides it. This machine has no ssh
    // name worth printing: "local" is a placeholder, not an address.
    note: host.label && !host.local ? host.name : "",
    sessions: host.sessions ?? 0,
    warning: host.warning ?? "",
    // The failure that mattered, in full: the first thing anyone does with
    // one is paste it to someone else.
    error: host.error || host.failed || "",
    act: state === "here" ? "" : state === "off" ? "connect" : "disconnect",
  };
}

export function listed(hosts: Host[] | undefined): Shown[] {
  return (hosts ?? []).map(shown);
}

// said is what a row says about itself under the name, left to right.
export function said(row: Shown): string {
  const parts: string[] = [];
  if (row.state === "here") parts.push("this machine");
  else if (row.state === "off") parts.push("not connected");
  else if (row.state === "quiet") parts.push("connected, but its last answer failed");
  else parts.push("connected");
  if (row.sessions > 0) parts.push(`${row.sessions} session${row.sessions === 1 ? "" : "s"}`);
  if (row.note) parts.push(row.note);
  return parts.join(" · ");
}
