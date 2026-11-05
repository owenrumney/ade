// What a session's server says its agent is up to, and what that makes it.

export interface Health {
  running?: boolean;
  error?: string;
  asking?: number;
  sideRuns?: string[] | null;
  lastEventAt?: string;
  // terminal is what the agent's own terminal is doing: working, needs, idle, ended.
  terminal?: string;
}

// Active is one announced session, as /api/active lists it.
export interface Active {
  id: string;
  dir: string;
  name: string;
  repo?: string;
  branch?: string;
  agent?: Health | null;
}

export type State = "failed" | "asking" | "working" | "idle";

// First match wins.
export function stateOf(agent: Health | null | undefined): State {
  if (!agent) return "idle";
  if (agent.error) return "failed";
  if ((agent.asking ?? 0) > 0 || agent.terminal === "needs") return "asking";
  return agent.running || agent.terminal === "working" ? "working" : "idle";
}

// attention is whether a state is your turn, which is what turns the icon red.
export function attention(state: State): boolean {
  return state === "failed" || state === "asking";
}
