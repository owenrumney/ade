// Where a checkout's design documents live: a path through it is a document.
const DOCS = "/.git/ade/docs/";

export interface DocRef {
  panel: string;
  title: string;
}

// One in `dir` reads through its own session, one elsewhere through the launcher.
export function docRef(path: string, dir: string): DocRef | undefined {
  const full = path.startsWith(".git/") ? `${dir}/${path}` : path;
  const cut = full.indexOf(DOCS);
  if (cut < 0) return undefined;
  const file = full.slice(cut + DOCS.length);
  if (!file.endsWith(".md") || file.split("/").includes("..")) return undefined;
  const where = full.slice(0, cut);
  const title = file.replace(/\.md$/, "");
  if (!where || where === dir) return { panel: `design:${file}`, title };
  return { panel: `spacedesign:${where}|${file}`, title };
}
