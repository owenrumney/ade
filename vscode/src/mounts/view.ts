// The shaping behind the mounts control: the draft both screens hold, the
// edits over it, and why a path or a key is refused. No DOM, so it is tested.
import { apply, validKey, type Edit, type Mount } from "../sandboxfile";

// Draft is a sandbox as a screen holds it. A value is a marker where the
// server redacted one, so an untouched variable survives the write.
export interface Draft {
  paths: Mount[];
  env: { [key: string]: string };
}

export const empty: Draft = { paths: [], env: {} };

export const MODES = [
  { id: "ro", label: "Read-only" },
  { id: "rw", label: "Read-write" },
];

export function edit(draft: Draft, change: Edit): Draft {
  return apply(draft, change);
}

// envKeys is the variables as rows. Keys only: a value on screen is either a
// marker or a secret, and neither is worth drawing.
export function envKeys(draft: Draft): string[] {
  return Object.keys(draft.env).sort();
}

// pathRefused is why a folder cannot be added, or nothing. Relative is
// refused here because the mount is made at the host path, not resolved.
export function pathRefused(draft: Draft, path: string): string | undefined {
  const want = path.trim();
  if (!want) return "Name a folder.";
  if (!want.startsWith("/") && !want.startsWith("~"))
    return "An absolute path, as this machine sees it.";
  if (draft.paths.some((mount) => mount.path === want)) return "Already mounted.";
  return undefined;
}

// added is the draft with one more folder, or why it was refused. Both the
// add row and a folder the host's dialog answered with come through here.
export function added(
  draft: Draft,
  path: string,
  rw: boolean,
): { draft: Draft } | { refused: string } {
  const refused = pathRefused(draft, path);
  if (refused) return { refused };
  return { draft: edit(draft, { kind: "mount", path: path.trim(), rw }) };
}

export function keyRefused(draft: Draft, key: string): string | undefined {
  const want = key.trim();
  if (!want) return "Name the variable.";
  if (!validKey(want)) return "Letters, digits and _, not starting with a digit.";
  if (want in draft.env) return "Already set.";
  return undefined;
}

// summary is the line under the heading: what the container gets beyond the
// checkout itself, which it always has.
export function summary(draft: Draft): string {
  const folders = draft.paths.length;
  const vars = Object.keys(draft.env).length;
  if (folders === 0 && vars === 0)
    return "Only the checkout, which the agent always gets.";
  const bits = [];
  if (folders > 0) bits.push(folders === 1 ? "1 folder" : `${folders} folders`);
  if (vars > 0) bits.push(vars === 1 ? "1 variable" : `${vars} variables`);
  return `${bits.join(" and ")}, on top of the checkout.`;
}
