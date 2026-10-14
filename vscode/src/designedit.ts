import * as vscode from "vscode";

// DesignEditor opens a design document as it reads rather than as it is
// written. It is the second way to open one; the text editor stays the first.
//
// Backed by the TextDocument, so a write from the agent — seed, fold,
// condense — reaches the frame the way it reaches any open editor.
export class DesignEditor implements vscode.CustomTextEditorProvider {
  static readonly viewType = "ade.designEditor";

  constructor(private readonly extensionUri: vscode.Uri) {}

  resolveCustomTextEditor(
    document: vscode.TextDocument,
    panel: vscode.WebviewPanel,
  ): void {
    // The document's folder, so a picture beside it can be drawn.
    const folder = vscode.Uri.joinPath(document.uri, "..");
    panel.webview.options = {
      enableScripts: true,
      localResourceRoots: [vscode.Uri.joinPath(this.extensionUri, "dist"), folder],
    };
    // own marks the edit this frame made, so the change it causes is not sent
    // back to it as news.
    let own = false;
    const send = () =>
      void panel.webview.postMessage({
        kind: "doc",
        text: document.getText(),
        base: panel.webview.asWebviewUri(folder).toString(),
      });

    const changed = vscode.workspace.onDidChangeTextDocument((e) => {
      if (e.document.uri.toString() !== document.uri.toString()) return;
      if (own) return;
      send();
    });
    // A drawn diagram keeps the size it was drawn at, so a new one reloads the page.
    const restyled = vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration("ade.design")) panel.webview.html = this.page(panel.webview);
    });
    panel.onDidDispose(() => {
      changed.dispose();
      restyled.dispose();
    });

    panel.webview.onDidReceiveMessage((msg: { kind?: string; text?: string }) => {
      if (msg.kind === "ready") {
        void panel.webview.postMessage({ kind: "style", ...style() });
        send();
        return;
      }
      if (msg.kind !== "edit" || typeof msg.text !== "string") return;
      if (msg.text === document.getText()) return;
      const edit = new vscode.WorkspaceEdit();
      edit.replace(document.uri, whole(document), msg.text);
      own = true;
      void vscode.workspace.applyEdit(edit).then(() => {
        own = false;
      });
    });

    // Last: the page asks for the document as it loads, and a listener
    // attached after that is one the first ask has already gone past.
    panel.webview.html = this.page(panel.webview);
  }

  private page(webview: vscode.Webview): string {
    const script = webview.asWebviewUri(
      vscode.Uri.joinPath(this.extensionUri, "dist", "designedit.js"),
    );
    const nonce = Math.random().toString(36).slice(2);
    // Shipped with the extension rather than fetched from the session server:
    // a design document is worth reading when no session is running.
    const mermaidSrc = webview.asWebviewUri(
      vscode.Uri.joinPath(this.extensionUri, "dist", "mermaid.min.js"),
    );
    // Loaded, not initialised: the frame does that, where the theme's own
    // variables resolve and can be handed to it.
    const mermaid = `<script nonce="${nonce}" src="${mermaidSrc}"></script>`;
    // CodeMirror writes its theme into a style element as it starts, so the
    // policy has to allow one; a webview with no policy at all draws nothing.
    const csp = [
      "default-src 'none'",
      `style-src ${webview.cspSource} 'unsafe-inline'`,
      `script-src 'nonce-${nonce}' ${webview.cspSource}`,
      `font-src ${webview.cspSource}`,
      `img-src ${webview.cspSource} data:`,
    ].join("; ");
    return `<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta http-equiv="Content-Security-Policy" content="${csp}" />
    <style>html,body,#doc{height:100%;margin:0}</style>
  </head>
  <body>
    <div id="doc"></div>
    ${mermaid}
    <script nonce="${nonce}" src="${script}"></script>
  </body>
</html>`;
  }
}

export function style(): { fontFamily: string; fontSize: number; diagramFontSize: number } {
  const c = vscode.workspace.getConfiguration("ade.design");
  return {
    fontFamily: c.get<string>("fontFamily") ?? "",
    fontSize: c.get<number>("fontSize") ?? 0,
    diagramFontSize: c.get<number>("diagramFontSize") ?? 16,
  };
}

// whole is the document's full range, which is what an edit from the frame
// replaces: the frame sends the text it has, never a diff.
export function whole(document: vscode.TextDocument): vscode.Range {
  return new vscode.Range(
    0,
    0,
    document.lineCount,
    document.lineAt(Math.max(document.lineCount - 1, 0)).text.length,
  );
}
