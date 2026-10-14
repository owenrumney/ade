import * as vscode from "vscode";
import { input, pick } from "./pick";
import type { BusOffer } from "./client";
import { shortEnough } from "./bus";

// Answer is what the person said: one of the offered options, their own
// words, or nothing at all when they walked away.
export interface Answer {
  option?: string;
  text?: string;
}

export const OWN_WORDS = "$(edit) Something else…";

// pickItems is the quick pick: the options as offered, then a way out of them.
// Each carries who is waiting, since a quick pick has no room for a byline.
export function pickItems(offer: BusOffer): vscode.QuickPickItem[] {
  const items: vscode.QuickPickItem[] = (offer.options ?? []).map((option) => ({
    label: option,
  }));
  items.push({ label: "", kind: vscode.QuickPickItemKind.Separator });
  items.push({ label: OWN_WORDS, description: "answer in your own words" });
  return items;
}

// escapeHtml keeps a question with angle brackets in it a question rather
// than markup.
export function escapeHtml(text: string): string {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

// askPage is the panel: the whole question, the options as buttons, and a box
// for words of your own. A long question is read here, not elided into a row.
export function askPage(offer: BusOffer, nonce: string): string {
  const options = (offer.options ?? [])
    .map(
      (option, at) =>
        `<button class="option" data-option="${escapeHtml(option)}" id="o${at}">${escapeHtml(option)}</button>`,
    )
    .join("\n");
  const asked = [offer.from, offer.agent, offer.model]
    .filter((bit): bit is string => Boolean(bit))
    .map(escapeHtml)
    .join(" · ");
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
      .who { color: var(--vscode-descriptionForeground); margin-bottom: 12px; }
      .question { font-size: 1.15em; line-height: 1.5; white-space: pre-wrap; margin-bottom: 20px; }
      .options { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 16px; }
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
      button.on, button.send {
        background: var(--vscode-button-background);
        color: var(--vscode-button-foreground);
      }
      button[disabled] { opacity: 0.5; cursor: default; }
      textarea {
        width: 100%;
        min-height: 5em;
        font-family: inherit;
        font-size: inherit;
        padding: 8px;
        color: var(--vscode-input-foreground);
        background: var(--vscode-input-background);
        border: 1px solid var(--vscode-input-border, transparent);
        border-radius: 4px;
        box-sizing: border-box;
      }
      .send { margin-top: 12px; }
    </style>
  </head>
  <body>
    <div class="who">${asked} asks</div>
    <div class="question">${escapeHtml(offer.question ?? "")}</div>
    <div class="options">${options}</div>
    <textarea id="words" placeholder="${options ? "or say something else" : "your answer"}"></textarea>
    <div><button class="send" id="send" disabled>Answer</button></div>
    <script nonce="${nonce}">
      const api = acquireVsCodeApi();
      const words = document.getElementById("words");
      const send = document.getElementById("send");
      let picked = "";
      const ready = () => (send.disabled = !picked && !words.value.trim());
      for (const button of document.querySelectorAll(".option")) {
        button.onclick = () => {
          const option = button.dataset.option;
          picked = picked === option ? "" : option;
          for (const other of document.querySelectorAll(".option"))
            other.classList.toggle("on", other.dataset.option === picked);
          ready();
        };
      }
      words.oninput = ready;
      send.onclick = () => {
        send.disabled = true;
        api.postMessage({ option: picked, text: words.value.trim() });
      };
    </script>
  </body>
</html>`;
}

// ask puts the question to the person the way its own shape deserves: a quick
// pick for a short one with options, a panel for anything longer.
export async function ask(offer: BusOffer): Promise<Answer | undefined> {
  if (shortEnough(offer)) return quickAsk(offer);
  return panelAsk(offer);
}

async function quickAsk(offer: BusOffer): Promise<Answer | undefined> {
  const picked = await pick(pickItems(offer), {
    title: `${offer.from} asks`,
    placeHolder: offer.question,
    // The question is somebody's agent stopped mid-turn: a click elsewhere
    // should not throw the answer away.
    ignoreFocusOut: true,
  });
  if (!picked) return undefined;
  if (picked.label !== OWN_WORDS) return { option: picked.label };

  const text = await input({
    title: `${offer.from} asks`,
    prompt: offer.question,
    ignoreFocusOut: true,
  });
  return text?.trim() ? { text: text.trim() } : undefined;
}

function panelAsk(offer: BusOffer): Promise<Answer | undefined> {
  return new Promise((resolve) => {
    const panel = vscode.window.createWebviewPanel(
      "ade.busAsk",
      `${offer.from} asks`,
      vscode.ViewColumn.Beside,
      { enableScripts: true, retainContextWhenHidden: true },
    );
    let answered = false;
    panel.webview.html = askPage(offer, nonce());
    panel.webview.onDidReceiveMessage((message: Answer) => {
      answered = true;
      resolve(message);
      panel.dispose();
    });
    // A panel closed without an answer is the question left alone, not an
    // empty answer sent on somebody's behalf.
    panel.onDidDispose(() => {
      if (!answered) resolve(undefined);
    });
  });
}

function nonce(): string {
  const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  let out = "";
  for (let i = 0; i < 32; i++) out += chars[Math.floor(Math.random() * chars.length)];
  return out;
}
