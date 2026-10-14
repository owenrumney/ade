// nameProblem is why a name cannot be a document and its branch, or nothing.
// Empty is allowed: it means no workspace, not a bad one.
export function nameProblem(value: string): string | undefined {
  const name = value.trim();
  if (!name || /[A-Za-z0-9]/.test(name.replace(/\.md$/, ""))) return undefined;
  return "a name needs a letter or a digit to make a branch from";
}

// title is a document's file name as the heading it gives itself:
// `commit-memory.md` reads as "Commit memory".
export function title(name: string): string {
  const stem = name.replace(/\.md$/, "").split("-").join(" ");
  return stem.charAt(0).toUpperCase() + stem.slice(1);
}
