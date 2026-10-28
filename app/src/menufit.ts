// What the menu at the pointer holds. Only electron's types, erased at
// build: a test may not load electron, which CI installs without a binary.
import type { ContextMenuParams } from "electron";

export interface Item {
  label?: string;
  role?: string;
  type?: "separator";
  what?: string;
}

export interface Where {
  panel?: string;
  dev?: boolean;
}

export type Clicked = Omit<Partial<ContextMenuParams>, "editFlags"> & {
  editFlags?: Partial<ContextMenuParams["editFlags"]>;
};

// Pure, so a menu's shape is a test rather than a thing to right-click.
export function fit(params: Clicked, where: Where = {}): Item[] {
  const out: Item[] = [];
  const flags = params.editFlags ?? {};
  const selected = (params.selectionText ?? "").trim().length > 0;
  if (params.isEditable) {
    out.push({ role: "cut", what: "cut" });
    out.push({ role: "copy", what: "copy" });
    out.push({ role: "paste", what: "paste" });
    out.push({ type: "separator" });
    out.push({ role: "selectAll", what: "selectAll" });
  } else if (selected) {
    out.push({ role: "copy", what: "copy" });
    if (flags.canSelectAll) out.push({ role: "selectAll", what: "selectAll" });
  }
  if (params.linkURL) {
    if (out.length) out.push({ type: "separator" });
    out.push({ label: "Open the link", what: "open-link" });
    out.push({ label: "Copy the link", what: "copy-link" });
  }
  // A right-click that draws nothing reads as broken, so offer the panel.
  if (where.panel) {
    if (out.length) out.push({ type: "separator" });
    out.push({ label: "Reload this panel", what: "reload-panel" });
  }
  if (where.dev) {
    if (out.length) out.push({ type: "separator" });
    out.push({ label: "Inspect", what: "inspect" });
  }
  return out;
}
