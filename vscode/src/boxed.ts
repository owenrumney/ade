// What the status bar says about the container. Standing, not per-turn.
export interface Boxed {
  text: string;
  tooltip: string;
  // background is a theme colour id for the whole item. VS Code honours only
  // statusBarItem.errorBackground and statusBarItem.warningBackground here.
  background: string;
}

// The filled background the api allows: a container is not an error.
export const BOXED_BACKGROUND = "statusBarItem.warningBackground";

// badge is the indicator, or nothing: unboxed is ordinary and needs no word.
export function badge(
  sandbox: boolean | undefined,
  running: boolean | undefined,
): Boxed | undefined {
  if (!sandbox) return undefined;
  return {
    text: "$(shield) sandboxed",
    background: BOXED_BACKGROUND,
    tooltip: running
      ? "This turn is running in a container. Click to take the agent out — it ends the turn."
      : "The agent runs in a container, with only the paths you allowed. Click to take it out.",
  };
}
