// A file tree. Monaco has none — it is the editor widget, and the explorer
// is workbench code — so this is ade's, and it is not much.

export interface Node {
  path: string;
  name: string;
  dir?: boolean;
  children?: Node[];
  // status is what the diff says about it, which is why the tree is here
  // rather than in an explorer pane of its own.
  status?: "added" | "changed" | "removed";
}

// Shown is one visible row: the node, how deep it sits, and whether it is
// open. Depth is the row's own, so drawing never walks back up.
export interface Shown {
  node: Node;
  depth: number;
  open: boolean;
  // last marks the final child at its depth, for the guide that stops there.
  last: boolean;
}

// flatten is the tree as rows, folded where it is folded. A directory not
// in `open` is shut, so a fresh tree opens at the top and no deeper.
export function flatten(
  nodes: Node[],
  open: Set<string>,
  depth = 0,
): Shown[] {
  const out: Shown[] = [];
  nodes.forEach((node, i) => {
    const isOpen = node.dir === true && open.has(node.path);
    out.push({ node, depth, open: isOpen, last: i === nodes.length - 1 });
    if (isOpen && node.children) {
      out.push(...flatten(node.children, open, depth + 1));
    }
  });
  return out;
}

// toggled is `open` with one directory flipped. A new set, so a draw can
// compare what it has against what it had.
export function toggled(open: Set<string>, path: string): Set<string> {
  const next = new Set(open);
  if (!next.delete(path)) next.add(path);
  return next;
}

// opened is every directory on the way to a path, so revealing a file shows
// it rather than scrolling to a folded parent.
export function opened(open: Set<string>, path: string): Set<string> {
  const next = new Set(open);
  const parts = path.split("/");
  for (let i = 1; i < parts.length; i++) {
    next.add(parts.slice(0, i).join("/"));
  }
  return next;
}

// build turns a flat list of paths into a tree. Directories come first and
// each level is sorted, which is the order a file list is read in.
export function build(paths: { path: string; status?: Node["status"] }[]): Node[] {
  const root: Node = { path: "", name: "", dir: true, children: [] };
  for (const { path, status } of paths) {
    const parts = path.split("/").filter(Boolean);
    let at = root;
    parts.forEach((name, i) => {
      const here = parts.slice(0, i + 1).join("/");
      const leaf = i === parts.length - 1;
      let found = at.children?.find((c) => c.path === here);
      if (!found) {
        found = { path: here, name, dir: !leaf, children: leaf ? undefined : [] };
        at.children?.push(found);
      }
      if (leaf) found.status = status;
      at = found;
    });
  }
  sort(root);
  return root.children ?? [];
}

function sort(node: Node): void {
  if (!node.children) return;
  node.children.sort((a, b) => {
    if (Boolean(a.dir) !== Boolean(b.dir)) return a.dir ? -1 : 1;
    return a.name.localeCompare(b.name);
  });
  for (const child of node.children) sort(child);
}

// squashed collapses a directory that holds nothing but one directory, so
// `internal/web/assets` is one row rather than three you must open in turn.
export function squashed(nodes: Node[]): Node[] {
  return nodes.map((node) => {
    if (!node.dir || !node.children) return node;
    let at = node;
    let name = node.name;
    while (at.children?.length === 1 && at.children[0].dir) {
      at = at.children[0];
      name = `${name}/${at.name}`;
    }
    return { ...at, name, children: squashed(at.children ?? []) };
  });
}
