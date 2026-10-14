// RolesView mirrors roles.View: the Markdown prompts in ~/.ade/roles, and
// which one new main chats start with.
export interface RolesView {
  roles: Role[] | null;
  default: string;
}

export interface Role {
  name: string;
  prompt: string;
  model?: string;
}

export type Row =
  | { kind: "role"; role: Role; selected: boolean }
  | { kind: "none"; selected: boolean }
  | { kind: "new" }
  | { kind: "install" };

// rows is the pick: the roles, then no role at all, then the two ways to get
// one. Installing the defaults is offered only when there is nothing yet.
export function rows(view: RolesView): Row[] {
  const roles = view.roles ?? [];
  const out: Row[] = roles.map((role) => ({
    kind: "role",
    role,
    selected: role.name === view.default,
  }));
  out.push({
    kind: "none",
    selected: !roles.some((r) => r.name === view.default),
  });
  out.push({ kind: "new" });
  if (roles.length === 0) out.push({ kind: "install" });
  return out;
}

export function label(row: Row): string {
  switch (row.kind) {
    case "role":
      return `${row.selected ? "$(check) " : "$(blank) "}${row.role.name}`;
    case "none":
      return `${row.selected ? "$(check) " : "$(blank) "}No role`;
    case "new":
      return "$(add) New role…";
    case "install":
      return "$(cloud-download) Install the roles ade ships with";
  }
}

export function detail(row: Row): string {
  switch (row.kind) {
    case "role":
      return first(row.role.prompt);
    case "none":
      return "A chat starts with ade's own rules and your standing instruction";
    case "new":
      return "A Markdown file in ~/.ade/roles, opened to write";
    case "install":
      return "Two examples to start from";
  }
}

// first is a role's opening line, which is what says what it is for.
function first(prompt: string): string {
  const line = prompt.split("\n").find((l) => l.trim()) ?? "";
  return line.length > 120 ? line.slice(0, 117) + "…" : line;
}

// validName mirrors nameRE in internal/roles: it becomes a file name.
export function validName(name: string): boolean {
  return /^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(name);
}

// skeleton is what a new role opens as. A role is the whole file, so there is
// no frontmatter to get wrong.
export function skeleton(name: string): string {
  return [
    `You are working with Owen on ${name}.`,
    "",
    "Replace this with how you want the work done: what to prefer, what to",
    "avoid, and anything that should override the defaults.",
    "",
  ].join("\n");
}
