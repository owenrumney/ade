// RESERVED is what macOS takes before an application sees it. A binding here
// is one that silently never fires.
export const RESERVED = [
  "cmd+alt+d", // show or hide the Dock
  "cmd+alt+esc", // force quit
  "cmd+space", // spotlight
  "cmd+alt+space", // finder search
  "ctrl+up", // mission control
  "ctrl+down", // application windows
  "ctrl+left", // move a space left
  "ctrl+right", // move a space right
  "cmd+h", // hide the application
  "cmd+alt+h", // hide the others
  "cmd+q", // quit
  "cmd+tab", // switch application
];

// taken reports which of a manifest's mac bindings macOS would swallow.
export function taken(
  bindings: { command: string; mac?: string; key?: string }[],
): string[] {
  const held = new Set(RESERVED);
  return bindings
    .filter((b) => held.has((b.mac ?? b.key ?? "").toLowerCase()))
    .map((b) => b.command);
}
