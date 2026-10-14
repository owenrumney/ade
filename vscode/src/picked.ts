// What a source control gesture hands a command: a row, a group heading, or a
// multi-select. All three arrive as arguments, so all three flatten here.
export interface Pickable {
  resourceUri?: { fsPath: string };
  resourceStates?: { resourceUri: { fsPath: string } }[];
}

export function picked(args: unknown[]): { fsPath: string }[] {
  const out: { fsPath: string }[] = [];
  for (const arg of args.flat()) {
    const one = arg as Pickable | null;
    if (one?.resourceStates)
      out.push(...one.resourceStates.map((r) => r.resourceUri));
    else if (one?.resourceUri) out.push(one.resourceUri);
  }
  return out;
}

// rel is a path as the server names it: relative to the root it diffs, which
// is not always a prefix of what the editor is showing.
export function rel(root: string, fsPath: string): string | undefined {
  const prefix = root.endsWith("/") ? root : root + "/";
  return fsPath.startsWith(prefix) ? fsPath.slice(prefix.length) : undefined;
}
