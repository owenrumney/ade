// SandboxFile is the repository's sandbox as the server shows it. A secret's
// value comes back as a marker, and sending the marker back keeps the value.
export interface SandboxFile {
  on?: boolean;
  paths: Mount[] | null;
  env: { [key: string]: string } | null;
  path?: string;
  runtime?: string;
  fixed?: string;
  // What the container gets whether or not the file names it, and why.
  always?: Always[] | null;
}

// Always is a mount ade makes itself. Not editable: taking one away
// would be a container the agent cannot work in.
export interface Always {
  path: string;
  why: string;
  rw?: boolean;
}

export interface Mount {
  path: string;
  rw?: boolean;
}

// Edit is one change a pick can make. The whole file goes back each time,
// because that is the only write the server has.
export type Edit =
  | { kind: "mount"; path: string; rw: boolean }
  | { kind: "unmount"; path: string }
  | { kind: "set"; key: string; value: string }
  | { kind: "unset"; key: string };

export interface Body {
  paths: Mount[];
  env: { [key: string]: string };
}

// apply is the file after one edit. Everything untouched goes back as it
// came, markers included, so a secret nobody retyped is not lost.
export function apply(file: SandboxFile, edit: Edit): Body {
  let paths = [...(file.paths ?? [])];
  const env = { ...(file.env ?? {}) };
  switch (edit.kind) {
    case "mount":
      paths = paths.filter((mount) => mount.path !== edit.path);
      paths.push(edit.rw ? { path: edit.path, rw: true } : { path: edit.path });
      break;
    case "unmount":
      paths = paths.filter((mount) => mount.path !== edit.path);
      break;
    case "set":
      env[edit.key] = edit.value;
      break;
    case "unset":
      delete env[edit.key];
      break;
  }
  return { paths, env };
}

// Row is one line of the pick: something there already, or a way to add one.
export type Row =
  | { kind: "mount"; label: string; description: string; mount: Mount }
  | { kind: "env"; label: string; description: string; key: string }
  | { kind: "add-mount" | "add-env"; label: string; description: string };

// rows is the file as a list, mounts first. A value is never shown: a secret
// is a marker already, and a plain one is still nobody's business on screen.
export function rows(file: SandboxFile): Row[] {
  const mounts: Row[] = (file.paths ?? []).map((mount) => ({
    kind: "mount",
    label: mount.path,
    description: mount.rw ? "mounted read-write" : "mounted read-only",
    mount,
  }));
  const env: Row[] = Object.keys(file.env ?? {})
    .sort()
    .map((key) => ({
      kind: "env",
      label: key,
      description: "set in the container",
      key,
    }));
  return [
    ...mounts,
    ...env,
    {
      kind: "add-mount",
      label: "Mount a folder…",
      description: "at its own path, inside the container",
    },
    {
      kind: "add-env",
      label: "Set a variable…",
      description: "inside the container only",
    },
  ];
}

// validKey mirrors envKeyRE in internal/sandbox, so a name the server would
// refuse is refused in the box instead.
export function validKey(key: string): boolean {
  return /^[A-Za-z_][A-Za-z0-9_]*$/.test(key);
}
