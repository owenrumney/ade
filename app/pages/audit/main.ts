import { clock, listed, text, type Noted, type Shown } from "./view";

declare function acquireVsCodeApi(): { postMessage(message: unknown): void };
const host = acquireVsCodeApi();
const root = document.getElementById("audit") as HTMLElement;

let all: Noted[] = [];
let days: string[] = [];
let day = "";
let filter = "";
let open = new Set<string>();

function el(tag: string, cls?: string, text?: string): HTMLElement {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

function head(): HTMLElement {
  const bar = el("div", "bar");
  const pick = document.createElement("select");
  for (const each of days.length ? days : [day]) {
    const one = document.createElement("option");
    one.value = one.textContent = each;
    one.selected = each === day;
    pick.append(one);
  }
  pick.addEventListener("change", () => {
    day = pick.value;
    host.postMessage({ kind: "do", what: "day", day });
  });
  const box = document.createElement("input");
  box.className = "filter";
  box.placeholder = "filter";
  box.value = filter;
  box.addEventListener("input", () => {
    filter = box.value;
    draw(true);
  });
  const copy = el("button", undefined, "Copy");
  copy.addEventListener("click", () => {
    void navigator.clipboard.writeText(text(listed(all, filter)));
  });
  bar.append(pick, box, copy);
  return bar;
}

function line(row: Shown): HTMLElement {
  const node = el("div", `line ${row.outcome}`);
  node.append(el("span", "clock", clock(row.at)));
  node.append(el("span", `kind ${row.kind}`, row.kind));
  node.append(el("span", "what", row.what));
  if (row.where) node.append(el("span", "where", row.where));
  if (row.why) node.append(el("span", "why", row.why));
  if (row.under.length) {
    const more = el("span", "under", `${row.under.length} call${row.under.length === 1 ? "" : "s"}`);
    node.append(more);
    node.addEventListener("click", () => {
      const key = row.at + row.what;
      if (open.has(key)) open.delete(key);
      else open.add(key);
      draw(true);
    });
    if (open.has(row.at + row.what)) {
      const box = el("div", "folded");
      for (const one of row.under) {
        const said = [one.what, one.ok ? "ok" : (one.why ?? "failed"), `${one.ms ?? 0}ms`];
        // The field names a write carried, never their values.
        if (one.fields?.length) said.push(one.fields.join(", "));
        box.append(el("div", one.ok ? "step" : "step failed", said.join(" · ")));
      }
      node.append(box);
    }
  }
  return node;
}

function draw(keepFocus = false): void {
  const at = document.activeElement as HTMLElement | null;
  const typing = at?.classList.contains("filter");
  root.replaceChildren();
  root.append(head());
  const rows = listed(all, filter);
  if (rows.length === 0) {
    root.append(el("p", "none", "Nothing yet. Press something."));
    return;
  }
  const list = el("div", "lines");
  for (const row of rows) list.append(line(row));
  root.append(list);
  if (keepFocus && typing) {
    const box = root.querySelector<HTMLInputElement>(".filter");
    box?.focus();
    box?.setSelectionRange(box.value.length, box.value.length);
  }
}

window.addEventListener("message", (event: MessageEvent<{ kind?: string; entries?: Noted[]; days?: string[]; day?: string }>) => {
  const said = event.data;
  if (said?.kind !== "state") return;
  all = said.entries ?? [];
  days = said.days ?? [];
  day = said.day ?? days[0] ?? "";
  draw(true);
});

host.postMessage({ kind: "ready" });
// A tail, not a stream: two seconds is faster than anyone reads.
setInterval(() => host.postMessage({ kind: "do", what: "refresh", day }), 2000);
