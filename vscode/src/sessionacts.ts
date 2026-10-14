import type { Session } from "./launcher";

// Act is what Find a session offers once you have picked one: the Sessions
// tree's row verbs. Opening it is what the list is for, so it leads.
export interface Act {
  id: "open" | "rename" | "archive" | "unarchive" | "delete";
  label: string;
  detail: string;
}

export function acts(session: Session): Act[] {
  return [
    {
      id: "open",
      label: "Open it",
      detail: session.here ? "in this window" : session.dir,
    },
    {
      id: "rename",
      label: "Rename it…",
      detail: "your name sticks; ade stops renaming it",
    },
    session.archived
      ? {
          id: "unarchive",
          label: "Bring it back",
          detail: "list it with the rest again",
        }
      : {
          id: "archive",
          label: "Archive it",
          detail: "put it away; this can be undone",
        },
    {
      id: "delete",
      label: "Delete it",
      detail: "out of every list for good",
    },
  ];
}
