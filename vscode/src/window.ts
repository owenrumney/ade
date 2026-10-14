// Where a checkout opens. An empty window has no tabs to lose, so taking it
// over costs nothing and a second empty window is a window to close.
export function inNewWindow(
  setting: string | undefined,
  open: number,
): boolean {
  return setting === "newWindow" && open > 0;
}

// What the window does once the session has gone. Something the user has not
// read yet keeps it open: closing would take the notification with it.
export function closeAfter(
  setting: string | undefined,
  saidSomething: boolean,
): boolean {
  return setting !== "stay" && !saidSomething;
}
