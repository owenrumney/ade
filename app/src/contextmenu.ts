import { clipboard, Menu, shell, type BrowserWindow, type ContextMenuParams } from "electron";

// The menu at the pointer, for what a page does not own: editing, a
// link, and the panel the click landed in.

export interface Item {
  label?: string;
  role?: string;
  type?: "separator";
  // what names the item for a test. Electron ignores it.
  what?: string;
}

export interface Where {
  // The panel the frame belongs to, when the click was inside one.
  panel?: string;
  dev?: boolean;
}

// fit is what the menu holds, worked out from the click. Pure, so the
// shape of a menu is a test rather than a thing to right-click and see.
type Clicked = Omit<Partial<ContextMenuParams>, "editFlags"> & {
  editFlags?: Partial<ContextMenuParams["editFlags"]>;
};

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
  // Nothing to edit and nothing to follow: offer the panel itself rather
  // than no menu at all, which reads as a dead right-click.
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

// wire puts it on a window. `panelOf` says which panel a frame's url is,
// so the menu can name it.
export function wire(
  win: BrowserWindow,
  panelOf: (url: string) => string | undefined,
  dev = !process.env.ADE_PACKAGED,
): void {
  win.webContents.on("context-menu", (_event, params) => {
    const panel = panelOf(params.frameURL ?? "");
    const items = fit(params, { panel, dev });
    if (items.length === 0) return;
    const menu = Menu.buildFromTemplate(
      items.map((item) => {
        if (item.type === "separator") return { type: "separator" as const };
        if (item.role) return { role: item.role as "cut" };
        return { label: item.label ?? "", click: () => run(win, item.what ?? "", params) };
      }),
    );
    menu.popup({ window: win });
  });
}

function run(win: BrowserWindow, what: string, params: ContextMenuParams): void {
  switch (what) {
    case "open-link":
      if (params.linkURL) void shell.openExternal(params.linkURL);
      return;
    case "copy-link":
      if (params.linkURL) clipboard.writeText(params.linkURL);
      return;
    case "reload-panel":
      win.webContents.reloadIgnoringCache();
      return;
    case "inspect":
      win.webContents.inspectElement(params.x, params.y);
      return;
    default:
      return;
  }
}
