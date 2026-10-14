import { busy, type Host, hostLine, shown } from "./launcher";

// Row is one machine as the picker draws it. The host is the drawn one: what
// this window started is held here, and the verbs offered have to follow it.
export interface Row {
  host: Host;
  label: string;
  description: string;
  detail: string;
}

export function rows(
  hosts: Host[],
  working = new Map<string, string>(),
): Row[] {
  return hosts.map((host) => {
    const drawn = shown(host, working.get(host.name));
    return {
      host: drawn,
      label: host.label || host.name,
      description: hostLine(drawn),
      detail: drawn.error ?? "",
    };
  });
}

// Act is what the second pick offers. The ids are the verbs the extension
// already had on the tree's rows.
export interface Act {
  id:
    | "connect"
    | "disconnect"
    | "credentials"
    | "update"
    | "stop"
    | "rename"
    | "forget";
  label: string;
  detail: string;
}

// Only a remote machine can be connected or let go, and one mid-flight offers
// neither verb rather than both. The worker verbs need a connection.
export function acts(host: Host): Act[] {
  if (host.local || busy(host)) return [];
  const out: Act[] = host.connected
    ? [
        {
          id: "disconnect",
          label: "Disconnect",
          detail: "drop the ssh connection; the worker keeps running",
        },
        {
          id: "credentials",
          label: "Send this Mac's agent logins",
          detail: "so the worker can run an agent as you",
        },
        {
          id: "update",
          label: "Update the worker",
          detail: "pull the images; takes minutes",
        },
        {
          id: "stop",
          label: "Stop the worker",
          detail: "remove the container, keep the machine",
        },
      ]
    : [{ id: "connect", label: "Connect", detail: "open the ssh connection" }];
  out.push(
    { id: "rename", label: "Rename the row…", detail: host.name },
    { id: "forget", label: "Forget", detail: "stop listing it here" },
  );
  return out;
}
