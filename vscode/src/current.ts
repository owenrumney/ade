import { nearest } from "./discover";

export interface Current {
  root: string;
  session?: string;
}

// rootsFor orders the window's folders for attaching: the pick first when the
// window holds it, then the folder nearest the active editor, as before.
export function rootsFor(
  folders: string[],
  picked: string | undefined,
  active: string | undefined,
): string[] {
  const rest = nearest(folders, active);
  if (!picked || !folders.includes(picked)) return rest;
  return [picked, ...rest.filter((root) => root !== picked)];
}
