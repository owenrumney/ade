// Which CLI the last session was started on, so the next one offers it first.
const AGENT = "ade.agent";

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
export function newSeed(): { agent: string } {
  return { agent: lastAgent() };
}
