// What the status bar says about the container. Standing, not per-turn.
export interface Boxed {
  text: string;
  tooltip: string;
}

// badge is the indicator, or nothing: unboxed is ordinary and needs no word.
export function badge(
  sandbox: boolean | undefined,
  running: boolean | undefined,
): Boxed | undefined {
  if (!sandbox) return undefined;
  return {
    text: "$(shield) sandboxed",
    tooltip: running
      ? "This turn is running in a container. Click to take the agent out — it ends the turn."
      : "The agent runs in a container, with only the paths you allowed. Click to take it out.",
  };
}
