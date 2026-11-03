import { shell, type BrowserWindow } from "electron";

// ade's own pages are files and its panels a scheme of their own, so
// anything http belongs to the browser rather than to this window.
export function outward(url: string): boolean {
  return /^https?:\/\//i.test(url);
}

export function away(win: BrowserWindow): void {
  win.webContents.on("will-frame-navigate", (details) => {
    if (!outward(details.url)) return;
    details.preventDefault();
    void shell.openExternal(details.url);
  });
  win.webContents.setWindowOpenHandler(({ url }) => {
    if (outward(url)) void shell.openExternal(url);
    return { action: "deny" };
  });
}
