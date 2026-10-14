// Disk mirrors the launcher's reading of everywhere ade writes.
export interface Disk {
  total: number;
  stores: Store[] | null;
  at?: string;
}

export interface Store {
  label: string;
  path: string;
  bytes: number;
  parts?: Part[] | null;
}

export interface Part {
  name: string;
  bytes: number;
  dir?: boolean;
}

// Detail is one place walked fresh, and what can be emptied inside it.
export interface Detail extends Store {
  purgeable?: Purgeable[] | null;
}

export interface Purgeable {
  label: string;
  path: string;
  bytes: number;
  files: number;
  // Recoverable is something a build makes again; the rest is gone for good.
  recoverable: boolean;
}

export type Row =
  | { kind: "store"; store: Store }
  | { kind: "purge"; tree: Purgeable }
  | { kind: "part"; part: Part };

// roots is the places, largest first: the question is what is costing space.
export function roots(disk: Disk | undefined): Row[] {
  return [...(disk?.stores ?? [])]
    .sort((a, b) => b.bytes - a.bytes)
    .map((store) => ({ kind: "store", store }));
}

// inside is one place opened: what can be emptied first, since that is the
// row with a verb, then what it holds, largest first.
export function inside(detail: Detail | undefined): Row[] {
  if (!detail) return [];
  const purge: Row[] = (detail.purgeable ?? []).map((tree) => ({
    kind: "purge",
    tree,
  }));
  const parts: Row[] = [...(detail.parts ?? [])]
    .sort((a, b) => b.bytes - a.bytes)
    .map((part) => ({ kind: "part", part }));
  return [...purge, ...parts];
}

export function context(row: Row): string {
  return `ade.disk.${row.kind}`;
}

const UNITS = ["B", "KB", "MB", "GB", "TB"];

// size is bytes as a person reads them: one decimal under ten, none above.
export function size(bytes: number): string {
  let n = Math.max(0, bytes);
  let unit = 0;
  while (n >= 1024 && unit < UNITS.length - 1) {
    n /= 1024;
    unit++;
  }
  const digits = unit > 0 && n < 10 ? 1 : 0;
  return `${n.toFixed(digits)} ${UNITS[unit]}`;
}

// warning is what the purge confirmation says, which differs on the one
// thing that matters: whether it comes back.
export function warning(tree: Purgeable): string {
  const what = `${size(tree.bytes)} in ${tree.files} file${tree.files === 1 ? "" : "s"}`;
  return tree.recoverable
    ? `${what}. The next build makes it again.`
    : `${what}. This cannot be undone.`;
}

// page is the whole reading as a document: every place, and inside each what
// it holds. The verb is not here — emptying something is a pick off this.
export function page(disk: Disk, details: Map<string, Detail>): string {
  const out = [
    "# What ade is costing in space",
    "",
    `**${size(disk.total)}** across ${disk.stores?.length ?? 0} place${
      (disk.stores?.length ?? 0) === 1 ? "" : "s"
    }.`,
    "",
  ];
  for (const row of roots(disk)) {
    if (row.kind !== "store") continue;
    out.push(
      `## ${row.store.label} · ${size(row.store.bytes)}`,
      "",
      `\`${row.store.path}\``,
      "",
    );
    const detail = details.get(row.store.path);
    const held = detail ? inside(detail) : [];
    if (held.length === 0) {
      out.push("_Nothing inside worth listing._", "");
      continue;
    }
    out.push("| What | Size | |", "| --- | ---: | --- |");
    for (const held1 of held) {
      if (held1.kind === "purge")
        out.push(
          `| ${held1.tree.label} | ${size(held1.tree.bytes)} | can be emptied${held1.tree.recoverable ? ", and comes back" : ""} |`,
        );
      else if (held1.kind === "part")
        out.push(`| ${held1.part.name} | ${size(held1.part.bytes)} | |`);
    }
    out.push("");
  }
  return out.join("\n");
}
