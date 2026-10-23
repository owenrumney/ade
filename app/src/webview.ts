import { protocol, net } from "electron";
import { readFile } from "node:fs/promises";
import { join, normalize } from "node:path";
import { pathToFileURL } from "node:url";

// ade's webviews live on a scheme of their own, as VS Code's do: the
// server allows CORS to a webview scheme and nothing else.

// `internal/web/cors.go` names this scheme beside vscode-webview://.
export const SCHEME = "ade-webview";

// privileged has to be declared before the app is ready, which is why
// this is called from the top of main rather than inside whenReady.
export function declare(): void {
  // Plural, and a list: the singular spelling does not exist and fails
  // at load with "not a function", before any window is drawn.
  protocol.registerSchemesAsPrivileged([
    {
      scheme: SCHEME,
      privileges: {
        standard: true,
        secure: true,
        supportFetchAPI: true,
        corsEnabled: true,
      },
    },
  ]);
}

// Pages are held rather than written to disk: a panel's skeleton is
// built per open, and giving it a url means it can be reloaded.
const pages = new Map<string, string>();
const keys = new Map<string, string>();

// A panel's id is not a path: a design in another space carries the
// checkout's, slashes and all. The key is opaque; the id stays here.
export function hold(id: string, html: string): string {
  let key = keys.get(id);
  if (!key) {
    key = String(keys.size + 1);
    keys.set(id, key);
  }
  pages.set(key, html);
  return `${SCHEME}://panel/${key}`;
}

// serve answers the scheme: a held page, or a file under vscode/ for
// the bundles and stylesheets the panels load.
export function serve(root: string): void {
  protocol.handle(SCHEME, async (request) => {
    const at = new URL(request.url);
    if (at.host === "panel") {
      const html = pages.get(at.pathname.replace(/^\//, ""));
      if (!html) return new Response("no such panel", { status: 404 });
      return new Response(html, {
        headers: { "content-type": "text/html; charset=utf-8" },
      });
    }
    if (at.host === "asset") {
      // Under vscode/ and nowhere else: the path comes from a page, and
      // a page is not a thing to trust with a path.
      const want = normalize(at.pathname).replace(/^(\.\.[/\\])+/, "");
      const file = join(root, "vscode", want);
      if (!file.startsWith(join(root, "vscode"))) {
        return new Response("no", { status: 403 });
      }
      try {
        return new Response(await readFile(file), {
          headers: { "content-type": kind(file) },
        });
      } catch {
        return new Response("not found", { status: 404 });
      }
    }
    return net.fetch(pathToFileURL(root).toString());
  });
}

function kind(file: string): string {
  if (file.endsWith(".js")) return "text/javascript; charset=utf-8";
  if (file.endsWith(".css")) return "text/css; charset=utf-8";
  if (file.endsWith(".svg")) return "image/svg+xml";
  return "application/octet-stream";
}
