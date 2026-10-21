import { control } from "../mounts/control";
import { added, type Draft } from "../mounts/view";
import type { SandboxFile } from "../sandboxfile";

declare function acquireVsCodeApi(): { postMessage(message: unknown): void };
const host = acquireVsCodeApi();
const root = document.getElementById("sandbox") as HTMLElement;

interface Shown {
  file?: SandboxFile;
  busy?: boolean;
  note?: string;
}

let state: Shown = {};
let refused = "";

function el(tag: string, cls?: string, text?: string): HTMLElement {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

function draft(): Draft {
  return {
    paths: state.file?.paths ?? [],
    env: state.file?.env ?? {},
  };
}

// save sends the whole file, which is the only write the server has. The
// panel keeps no draft: there is nothing on screen worth losing.
function save(next: Draft): void {
  host.postMessage({ kind: "save", body: next });
}

function where(file: SandboxFile): string {
  const on = file.on ? "The agent runs in a container" : "The agent runs on this host";
  const runtime = file.runtime ? ` (${file.runtime})` : "";
  return `${on}${runtime}. ${file.path ?? ""}`;
}

function draw(): void {
  root.replaceChildren();
  root.append(el("h1", "", "Sandbox"));
  if (state.note) root.append(el("p", "note", state.note));
  if (refused) root.append(el("p", "note", refused));
  if (!state.file) {
    root.append(el("p", "sub", "Reading the sandbox…"));
    return;
  }
  root.append(el("p", "sub", where(state.file)));
  if (!state.file.on)
    root.append(
      el(
        "p",
        "sub",
        "Not sandboxed now, so none of this is in force; it is what the next container gets.",
      ),
    );
  root.append(control(draft(), { change: save, browse: () => host.postMessage({ kind: "browse" }) }));
  root.append(
    el(
      "p",
      "sub",
      "A change drops the running container; the conversation resumes in a new one on the next turn.",
    ),
  );
}

window.addEventListener(
  "message",
  (event: MessageEvent<{ kind?: string; path?: string } & Shown>) => {
    const said = event.data;
    if (said?.kind === "state") {
      state = { file: said.file, busy: said.busy, note: said.note };
      refused = "";
      draw();
      return;
    }
    // A folder the host's dialog answered with: the same add as the row.
    if (said?.kind === "mounted" && said.path) {
      const next = added(draft(), said.path, false);
      if ("refused" in next) {
        refused = next.refused;
        draw();
        return;
      }
      save(next.draft);
    }
  },
);

host.postMessage({ kind: "ready" });
