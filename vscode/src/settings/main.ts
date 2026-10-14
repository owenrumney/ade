declare function acquireVsCodeApi(): { postMessage(message: unknown): void };

interface View {
  path: string;
  scanDirs: string[];
  ignoredDirs: string[];
  instructions: string;
  instructionsMaxLength: number;
  dashboardUrl: string;
  dashboardHost: string;
  dashboardClaude: string;
  dashboardCodex: string;
  dashboardTokenSet: boolean;
  dashboardTokenEnd?: string;
  linearKeySet: boolean;
  linearKeyEnd?: string;
  homeAssistantUrl: string;
  homeAssistantDesk: string;
  homeAssistantNotify: string;
  homeAssistantTokenSet: boolean;
  homeAssistantTokenEnd?: string;
  gitHubPackagesTokenSet: boolean;
  gitHubPackagesTokenEnd?: string;
  previewUrl: string;
  previewOff: boolean;
  sharpenOff: boolean;
  mergeQueuedOff: boolean;
}

const vscode = acquireVsCodeApi();
const byID = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;
const form = byID<HTMLFormElement>("settings");
const status = byID<HTMLElement>("status");

const value = (id: string) => byID<HTMLInputElement | HTMLTextAreaElement>(id).value;
const lines = (id: string) => value(id).split("\n").map((line) => line.trim()).filter(Boolean);
const set = (id: string, next: string) => (byID<HTMLInputElement | HTMLTextAreaElement>(id).value = next);
const say = (message: string) => (status.textContent = message);

function secret(route: string, isSet: boolean, end?: string): void {
  const node = byID<HTMLElement>(`${route}-state`);
  node.textContent = isSet ? `set${end ? ` · ends ${end}` : ""}` : "not set";
}

function show(next: View): void {
  byID<HTMLElement>("path").textContent = next.path;
  set("scan-dirs", next.scanDirs.join("\n"));
  set("ignored-dirs", next.ignoredDirs.join("\n"));
  set("instructions", next.instructions ?? "");
  set("dashboard-url", next.dashboardUrl ?? "");
  set("dashboard-host", next.dashboardHost ?? "");
  set("dashboard-claude", next.dashboardClaude ?? "");
  set("dashboard-codex", next.dashboardCodex ?? "");
  set("home-assistant-url", next.homeAssistantUrl ?? "");
  set("home-assistant-desk", next.homeAssistantDesk ?? "");
  set("home-assistant-notify", next.homeAssistantNotify ?? "");
  set("preview-url", next.previewUrl ?? "");
  byID<HTMLInputElement>("preview-off").checked = Boolean(next.previewOff);
  byID<HTMLInputElement>("sharpen-off").checked = Boolean(next.sharpenOff);
  byID<HTMLInputElement>("merge-queued-off").checked = Boolean(next.mergeQueuedOff);
  byID<HTMLElement>("instruction-limit").textContent = `Maximum ${next.instructionsMaxLength} characters.`;
  secret("dashboard-token", next.dashboardTokenSet, next.dashboardTokenEnd);
  secret("linear-key", next.linearKeySet, next.linearKeyEnd);
  secret("home-assistant-token", next.homeAssistantTokenSet, next.homeAssistantTokenEnd);
  secret("github-packages-token", next.gitHubPackagesTokenSet, next.gitHubPackagesTokenEnd);
}

// The editor makes the request, not this page: a webview renders on the
// machine you are sitting at, which may be nowhere near the ade it is about.
interface Answer { id?: string; view?: View; error?: string }
const waiting = new Map<string, (answer: Answer) => void>();
let asked = 0;

function request(path: string, init: { method?: string; body?: unknown } = {}): Promise<View> {
  const id = String(++asked);
  return new Promise<View>((ok, no) => {
    waiting.set(id, (answer) => {
      if (answer.error || !answer.view) return no(new Error(answer.error || "ade said nothing"));
      ok(answer.view);
    });
    vscode.postMessage({ kind: "api", id, path, method: init.method, body: init.body });
  });
}

async function load(): Promise<void> {
  try { show(await request("/api/settings")); } catch (error) { say(String(error)); }
}

form.addEventListener("submit", (event) => {
  event.preventDefault();
  void (async () => {
    const save = byID<HTMLButtonElement>("save");
    save.disabled = true;
    say("Saving…");
    try {
      show(await request("/api/settings", { method: "PATCH", body: {
        scanDirs: lines("scan-dirs"), ignoredDirs: lines("ignored-dirs"), instructions: value("instructions"),
        dashboardUrl: value("dashboard-url"), dashboardHost: value("dashboard-host"), dashboardClaude: value("dashboard-claude"), dashboardCodex: value("dashboard-codex"),
        homeAssistantUrl: value("home-assistant-url"), homeAssistantDesk: value("home-assistant-desk"), homeAssistantNotify: value("home-assistant-notify"),
        previewUrl: value("preview-url"), previewOff: byID<HTMLInputElement>("preview-off").checked,
        sharpenOff: byID<HTMLInputElement>("sharpen-off").checked,
        mergeQueuedOff: byID<HTMLInputElement>("merge-queued-off").checked,
      } }));
      say("Settings saved.");
    } catch (error) { say(String(error)); } finally { save.disabled = false; }
  })();
});

document.addEventListener("click", (event) => {
  const button = (event.target as HTMLElement).closest<HTMLButtonElement>("button[data-secret], button[data-forget]");
  if (!button) return;
  const route = button.dataset.secret ?? button.dataset.forget;
  if (!route) return;
  void (async () => {
    try {
      if (button.dataset.secret) {
        const next = value(route).trim();
        if (!next) throw new Error("Enter a value first.");
        show(await request(`/api/settings/${route}`, { method: "PUT", body: { value: next } }));
        set(route, "");
        say("Secret saved.");
      } else {
        show(await request(`/api/settings/${route}`, { method: "DELETE" }));
        say("Secret forgotten.");
      }
    } catch (error) { say(String(error)); }
  })();
});

window.addEventListener("message", (event: MessageEvent<Answer & { kind?: string }>) => {
  const said = event.data;
  if (said.kind === "serving") {
    void load();
    return;
  }
  if (said.kind !== "api.result" || !said.id) return;
  const settle = waiting.get(said.id);
  waiting.delete(said.id);
  settle?.(said);
});

vscode.postMessage({ kind: "ready" });
