// What `ade create <name>` would make of a name, with no node modules.

// The server's rule: docs.Filename, then web.Slug in lower case.
export function branchFor(name: string): string {
  const file = filename(name);
  if (!file) return "";
  return (file.split("/").pop() ?? "")
    .replace(/\.[^.]*$/, "")
    .replace(/[^a-zA-Z0-9._-]+/g, "-")
    .replace(/^[-.]+|[-.]+$/g, "")
    .toLowerCase();
}

export function worktreeFor(root: string, branch: string): string {
  const at = root.replace(/\/+$/, "");
  const cut = at.lastIndexOf("/");
  const dir = cut <= 0 ? at.slice(0, cut + 1) : at.slice(0, cut);
  return `${dir}/${at.slice(cut + 1)}-${branch}`;
}

// home is passed in: the window has none of its own to read.
export function tilde(path: string, home: string): string {
  if (!home) return path;
  return path === home || path.startsWith(home + "/") ? "~" + path.slice(home.length) : path;
}

function filename(name: string): string {
  const said = name.replace(/\\/g, "/").trim();
  if (!said) return "";
  const clean = normalized(said);
  if (!clean || clean === "." || clean === ".." || clean.startsWith("../")) return "";
  if (clean.startsWith("/")) return "";
  return /\.(md|markdown)$/i.test(clean) ? clean : clean + ".md";
}

// The part of posix.normalize this needs.
function normalized(said: string): string {
  const out: string[] = [];
  for (const bit of said.split("/")) {
    if (bit === "" || bit === ".") continue;
    if (bit === ".." && out.length && out[out.length - 1] !== "..") out.pop();
    else out.push(bit);
  }
  const path = out.join("/");
  return said.startsWith("/") ? "/" + path : path;
}
