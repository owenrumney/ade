import { basename } from "node:path";

// A session across repositories opens as one window over every worktree. Home
// leads: the extension serves the first folder it finds a checkout in.
export function multiRoot(home: string, others: string[]): string {
  const folders = [home, ...others.filter((path) => path !== home)].map(
    (path) => ({ path, name: basename(path) }),
  );
  return JSON.stringify({ folders, settings: {} }, null, 2) + "\n";
}
