// The mounts control: what a sandbox lets in, as a list. Used by the sandbox
// panel and the New session screen. No vscode and no node, so it bundles.
import {
  MODES,
  added,
  edit,
  envKeys,
  keyRefused,
  summary,
  type Draft,
} from "./view";

export interface Hooks {
  // change is the whole draft after one edit: the only write the server has
  // is the whole file, so a screen saving per edit sends all of it anyway.
  change(draft: Draft): void;
  // browse asks the host for a folder dialog, which a webview cannot open.
  // Without it the add row is the path box alone.
  browse?(): void;
}

function el(tag: string, cls?: string, text?: string): HTMLElement {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

function button(label: string, cls: string, onClick: () => void): HTMLElement {
  const node = el("button", cls, label);
  node.addEventListener("click", onClick);
  return node;
}

// how is the read-only/read-write box, which is the one choice a mount has.
function how(rw: boolean): HTMLSelectElement {
  const sel = document.createElement("select");
  sel.className = "mount-how";
  for (const mode of MODES) {
    const option = document.createElement("option");
    option.value = mode.id;
    option.textContent = mode.label;
    option.selected = mode.id === (rw ? "rw" : "ro");
    sel.append(option);
  }
  return sel;
}

export function control(draft: Draft, hooks: Hooks): HTMLElement {
  const box = el("section", "mounts");
  box.append(el("p", "mounts-summary", summary(draft)));
  box.append(folders(draft, hooks));
  box.append(variables(draft, hooks));
  return box;
}

function folders(draft: Draft, hooks: Hooks): HTMLElement {
  const box = el("div", "mounts-group");
  box.append(el("h3", "", "Folders"));
  for (const mount of draft.paths) {
    const row = el("div", "mount-row");
    row.append(el("span", "mount-path", mount.path));
    const mode = how(Boolean(mount.rw));
    mode.addEventListener("change", () =>
      hooks.change(
        edit(draft, { kind: "mount", path: mount.path, rw: mode.value === "rw" }),
      ),
    );
    row.append(mode);
    row.append(
      button("Remove", "mount-drop", () =>
        hooks.change(edit(draft, { kind: "unmount", path: mount.path })),
      ),
    );
    box.append(row);
  }
  box.append(addFolder(draft, hooks));
  return box;
}

function addFolder(draft: Draft, hooks: Hooks): HTMLElement {
  const row = el("div", "mount-add");
  const path = document.createElement("input");
  path.className = "mount-input";
  path.placeholder = "/another/folder";
  const mode = how(false);
  const why = el("p", "mount-why");
  const add = (): void => {
    const next = added(draft, path.value, mode.value === "rw");
    if ("refused" in next) {
      why.textContent = next.refused;
      return;
    }
    why.textContent = "";
    path.value = "";
    hooks.change(next.draft);
  };
  path.addEventListener("keydown", (event) => {
    if ((event as KeyboardEvent).key === "Enter") add();
  });
  row.append(path, mode, button("Mount", "mount-do", add));
  if (hooks.browse)
    row.append(button("Browse…", "mount-browse", () => hooks.browse?.()));
  const box = el("div", "mount-adding");
  box.append(row, why);
  return box;
}

function variables(draft: Draft, hooks: Hooks): HTMLElement {
  const box = el("div", "mounts-group");
  box.append(el("h3", "", "Variables"));
  for (const key of envKeys(draft)) {
    const row = el("div", "mount-row");
    row.append(el("span", "mount-path", key));
    row.append(el("span", "mount-value", "set in the container"));
    // Replacing is a write of the key alone: the stored value is behind a
    // marker, so there is nothing to show and nothing to edit in place.
    row.append(
      button("Replace…", "mount-drop", () => {
        const value = document.createElement("input");
        value.className = "mount-input";
        value.type = "password";
        value.placeholder = "new value";
        const done = (): void => {
          if (value.value)
            hooks.change(edit(draft, { kind: "set", key, value: value.value }));
        };
        value.addEventListener("keydown", (event) => {
          if ((event as KeyboardEvent).key === "Enter") done();
        });
        row.replaceChildren(
          el("span", "mount-path", key),
          value,
          button("Set", "mount-do", done),
        );
        value.focus();
      }),
    );
    row.append(
      button("Remove", "mount-drop", () =>
        hooks.change(edit(draft, { kind: "unset", key })),
      ),
    );
    box.append(row);
  }
  box.append(addVariable(draft, hooks));
  return box;
}

function addVariable(draft: Draft, hooks: Hooks): HTMLElement {
  const row = el("div", "mount-add");
  const key = document.createElement("input");
  key.className = "mount-input";
  key.placeholder = "NPM_TOKEN";
  const value = document.createElement("input");
  value.className = "mount-input";
  value.type = "password";
  value.placeholder = "value";
  const why = el("p", "mount-why");
  const set = (): void => {
    const refused = keyRefused(draft, key.value);
    if (refused) {
      why.textContent = refused;
      return;
    }
    if (!value.value) {
      why.textContent = "Give it a value.";
      return;
    }
    why.textContent = "";
    const name = key.value.trim();
    const secret = value.value;
    key.value = "";
    value.value = "";
    hooks.change(edit(draft, { kind: "set", key: name, value: secret }));
  };
  value.addEventListener("keydown", (event) => {
    if ((event as KeyboardEvent).key === "Enter") set();
  });
  row.append(key, value, button("Set", "mount-do", set));
  const box = el("div", "mount-adding");
  box.append(row, why);
  box.append(
    el("p", "mount-why", "Kept in a file under .git that only you can read."),
  );
  return box;
}
