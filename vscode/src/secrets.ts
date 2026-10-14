import type { Server } from "./discover";

// SecretsView is the part of ade's settings that says whether each secret is
// set. The value itself never comes back: only its last four characters.
export interface SecretsView {
  linearKeySet?: boolean;
  linearKeyEnd?: string;
  homeAssistantTokenSet?: boolean;
  homeAssistantTokenEnd?: string;
  gitHubPackagesTokenSet?: boolean;
  gitHubPackagesTokenEnd?: string;
}

export interface Secret {
  // route is the last segment of /api/settings/<route>.
  route: string;
  label: string;
  detail: string;
  set: boolean;
  end: string;
}

// secrets is the three ade keeps, in the order the settings page had them.
export function secrets(view: SecretsView): Secret[] {
  return [
    {
      route: "linear-key",
      label: "Linear API key",
      detail: "Reads your tickets for the Issues view",
      set: Boolean(view.linearKeySet),
      end: view.linearKeyEnd ?? "",
    },
    {
      route: "home-assistant-token",
      label: "Home Assistant token",
      detail: "Lets ade call your Home Assistant",
      set: Boolean(view.homeAssistantTokenSet),
      end: view.homeAssistantTokenEnd ?? "",
    },
    {
      route: "github-packages-token",
      label: "GitHub packages token",
      detail: "Authenticates private npm packages in sandboxes",
      set: Boolean(view.gitHubPackagesTokenSet),
      end: view.gitHubPackagesTokenEnd ?? "",
    },
  ];
}

// stateOf is what the row says: never the secret, only that there is one.
export function stateOf(secret: Secret): string {
  if (!secret.set) return "not set";
  return secret.end ? `set · ends ${secret.end}` : "set";
}

async function call(
  server: Server,
  path: string,
  init: RequestInit = {},
): Promise<SecretsView> {
  const res = await fetch(
    `${server.base}${path}?t=${encodeURIComponent(server.token)}`,
    {
      ...init,
      headers: {
        "X-Ade-Token": server.token,
        "Content-Type": "application/json",
      },
    },
  );
  const body = (await res.json().catch(() => ({}))) as SecretsView & {
    error?: string;
  };
  if (!res.ok) throw new Error(body.error || `settings: ${res.status}`);
  return body;
}

export const readSecrets = (server: Server) => call(server, "/api/settings");

// setSecret replaces one secret and nothing else. The settings file is the
// only store: the agents read it, so the editor keeps no second copy.
export const setSecret = (server: Server, route: string, value: string) =>
  call(server, `/api/settings/${route}`, {
    method: "PUT",
    body: JSON.stringify({ value }),
  });

export const forgetSecret = (server: Server, route: string) =>
  call(server, `/api/settings/${route}`, { method: "DELETE" });
