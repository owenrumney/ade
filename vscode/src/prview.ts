import * as vscode from "vscode";
import type { Review } from "./launcher";
import { acts, type Act } from "./pending";
import { escapeHtml } from "./busask";
import { reviewLine } from "./reviews";

// A pending pull request as a panel. Nothing is fetched to draw it, so it
// says only what the list already knew.
export function reviewPage(
  review: Review,
  nonce: string,
  now = Date.now(),
): string {
  const buttons = acts(review)
    .map(
      (act) =>
        `<button data-id="${act.id}" class="${act.id === "ade" ? "go" : ""}" title="${escapeHtml(act.detail)}">${escapeHtml(act.label)}</button>`,
    )
    .join("\n      ");
  const decision = review.reviewDecision
    ? `<div class="line">${escapeHtml(review.reviewDecision.toLowerCase().replace(/_/g, " "))}</div>`
    : "";
  return `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta http-equiv="Content-Security-Policy"
          content="default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-${nonce}';" />
    <style>
      body {
        font-family: var(--vscode-font-family);
        font-size: var(--vscode-font-size);
        color: var(--vscode-editor-foreground);
        padding: 16px 20px;
        max-width: 60em;
      }
      h1 { font-size: 1.3em; font-weight: 600; margin: 0 0 8px; line-height: 1.4; }
      .line { color: var(--vscode-descriptionForeground); margin-bottom: 6px; }
      .url { margin: 12px 0 20px; word-break: break-all; }
      .acts { display: flex; flex-wrap: wrap; gap: 8px; }
      button {
        font-family: inherit;
        font-size: inherit;
        padding: 6px 12px;
        border: 1px solid var(--vscode-button-border, transparent);
        border-radius: 4px;
        background: var(--vscode-button-secondaryBackground);
        color: var(--vscode-button-secondaryForeground);
        cursor: pointer;
      }
      button.go {
        background: var(--vscode-button-background);
        color: var(--vscode-button-foreground);
      }
    </style>
  </head>
  <body>
    <h1>#${review.number} ${escapeHtml(review.title)}</h1>
    <div class="line">${escapeHtml(reviewLine(review, now))}</div>
    ${decision}
    <div class="url">${escapeHtml(review.url)}</div>
    <div class="acts">
      ${buttons}
    </div>
    <script nonce="${nonce}">
      const api = acquireVsCodeApi();
      for (const button of document.querySelectorAll("button"))
        button.onclick = () => {
          for (const other of document.querySelectorAll("button"))
            other.disabled = true;
          api.postMessage({ id: button.dataset.id });
        };
    </script>
  </body>
</html>`;
}

// showReview opens that panel and waits for the button. Closing it without
// pressing one is the request left alone, not an action taken on your behalf.
export function showReview(review: Review): Promise<Act["id"] | undefined> {
  return new Promise((resolve) => {
    const panel = vscode.window.createWebviewPanel(
      "ade.pendingPR",
      `${review.repo}#${review.number}`,
      vscode.ViewColumn.Active,
      { enableScripts: true, retainContextWhenHidden: true },
    );
    let pressed = false;
    panel.webview.html = reviewPage(review, nonce());
    panel.webview.onDidReceiveMessage((said: { id?: Act["id"] }) => {
      pressed = true;
      resolve(said.id);
      panel.dispose();
    });
    panel.onDidDispose(() => {
      if (!pressed) resolve(undefined);
    });
  });
}

function nonce(): string {
  const chars =
    "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let out = "";
  for (let i = 0; i < 32; i++)
    out += chars[Math.floor(Math.random() * chars.length)];
  return out;
}
