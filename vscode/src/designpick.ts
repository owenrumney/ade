import { base } from "./design";
import { title } from "./docs";

// Row is one design document as the picker draws it: the title it reads as,
// and the path, which is what tells two documents of the same name apart.
export interface Row {
  file: string;
  label: string;
  description: string;
  detail: string;
}

export function rows(files: readonly string[]): Row[] {
  return [...files]
    .sort((a, b) => a.localeCompare(b))
    .map((file) => ({
      file,
      label: title(base(file)),
      description: file,
      detail: "",
    }));
}

export interface Act {
  id:
    | "open"
    | "rendered"
    | "preview"
    | "discuss"
    | "build"
    | "seed"
    | "grill"
    | "condense"
    | "archive"
    | "delete";
  label: string;
  detail: string;
}

// Open leads because it is what the tree row did on a click. Build is the one
// people come here for, so it is above the two that change the document.
export const ACTS: Act[] = [
  {
    id: "rendered",
    label: "Open it",
    detail: "as it reads, diagrams drawn",
  },
  { id: "open", label: "Open the Markdown", detail: "the source, in the editor" },
  { id: "build", label: "Build this design", detail: "start an agent on it" },
  {
    id: "preview",
    label: "Preview it",
    detail: "the rendered Markdown, beside the editor",
  },
  {
    id: "discuss",
    label: "Discuss it in chat",
    detail: "open it and the chat",
  },
  {
    id: "seed",
    label: "Ask what the draft has missed",
    detail: "the agent reacts to what is written so far",
  },
  {
    id: "grill",
    label: "Grill me on it",
    detail: "the agent interviews you about it until nothing is left assumed",
  },
  {
    id: "condense",
    detail: "the condenser rewrites it to be scannable, losing nothing",
    label: "Condense it",
  },
  {
    id: "archive",
    label: "Archive it",
    detail: "the work is finished: publish it to the wiki and delete it here",
  },
  {
    id: "delete",
    label: "Delete it",
    detail: "it is not in any commit, so this cannot be undone",
  },
];
