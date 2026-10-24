// The chat is a dock on the right, not a tab. What that costs the window.

export const CHAT_MIN = 280;
export const CHAT_MAX = 900;
export const SIDEBAR_MIN = 180;
export const SIDEBAR_MAX = 720;

// A dock that hides itself as the window narrows reads as a bug.
export function sideWidth(which: "sidebar" | "chat", want: number): number {
  const low = which === "chat" ? CHAT_MIN : SIDEBAR_MIN;
  const high = which === "chat" ? CHAT_MAX : SIDEBAR_MAX;
  return Math.min(high, Math.max(low, Math.round(want)));
}

// The panel ids that used to open a chat tab.
const CHAT_PANELS = new Set(["ade.openChat", "ade.focusChat", "ade.concierge"]);

export function isChat(panel: string): boolean {
  return CHAT_PANELS.has(panel);
}
