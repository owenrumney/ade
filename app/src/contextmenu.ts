import { clipboard, Menu, shell, type BrowserWindow, type ContextMenuParams } from "electron";
import { fit } from "./menufit";

// The menu at the pointer, for what a page does not own: editing, a
// link, and the panel the click landed in. What it holds is menufit.ts.

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
