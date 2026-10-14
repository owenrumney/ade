import type { Model } from "./launcher";

// A model belongs to an agent and ade runs one agent per conversation, so
// picking a model is two different requests.
export type Move =
  | { kind: "none" }
  | { kind: "model"; model: string }
  | { kind: "agent"; provider: string; model: string };

export function agentOf(list: Model[], id: string): string {
  return list.find((m) => m.id === id)?.agent ?? "";
}

export function move(
  list: Model[],
  now: { provider?: string; model?: string },
  id: string,
): Move {
  const provider = agentOf(list, id) || now.provider || "";
  if (provider && now.provider && provider !== now.provider)
    return { kind: "agent", provider, model: id };
  if (id === (now.model ?? "")) return { kind: "none" };
  return { kind: "model", model: id };
}

// A provider switch is not "only the model changes", so the confirmation
// says what it actually does.
export function warning(provider: string): string {
  const name = provider === "codex" ? "Codex" : "Claude Code";
  return `That model runs on ${name}. Continuing there starts a new ${name} session and carries this conversation across as context.`;
}
