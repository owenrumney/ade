// Whether a session is the agent's own terminal rather than ade's chat. On
// unless it was switched off: the chat is what is on its way out.
const KEY = "ade.terminal";

export function terminalMode(): boolean {
  try {
    return localStorage.getItem(KEY) !== "0";
  } catch {
    return true;
  }
}

export function setTerminalMode(on: boolean): void {
  localStorage.setItem(KEY, on ? "1" : "0");
}

const AGENT = "ade.agent";

// lastAgent is the CLI the last terminal session was started on.
export function lastAgent(): string {
  try {
    return localStorage.getItem(AGENT) === "codex" ? "codex" : "claude";
  } catch {
    return "claude";
  }
}

export function setLastAgent(agent: string): void {
  try {
    if (agent) localStorage.setItem(AGENT, agent);
  } catch {
    // A window that cannot remember asks claude next time.
  }
}

// newSeed is what a New Session form starts from.
export function newSeed(): { terminal: boolean; agent?: string } {
  return terminalMode() ? { terminal: true, agent: lastAgent() } : { terminal: false };
}
