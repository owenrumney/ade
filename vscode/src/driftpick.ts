import { type DriftItem, type DriftState, roots, said } from "./driftrow";

// Row is one requirement as the picker draws it: worst first, then what was
// waved off. The order is roots', so the picker and the squiggles agree.
export interface Row {
  item: DriftItem;
  label: string;
  description: string;
  detail: string;
}

export function rows(state: DriftState | undefined): Row[] {
  return roots(state)
    .flatMap((row) => (row.kind === "item" ? [row.item] : row.items))
    .map((item) => ({
      item,
      label: item.text,
      description: said(item),
      detail: item.dismissed ? "dismissed" : "",
    }));
}

export interface Act {
  id: "open" | "dismiss" | "restore";
  label: string;
  detail: string;
}

// Opening the document is where the requirement is written; the code a
// contradiction names has its own squiggle and needs no verb here.
export function acts(item: DriftItem): Act[] {
  return [
    {
      id: "open",
      label: "Open it in the document",
      detail: "where the requirement is written",
    },
    item.dismissed
      ? {
          id: "restore",
          label: "Bring it back",
          detail: "check the change against it again",
        }
      : {
          id: "dismiss",
          label: "Dismiss it",
          detail: "stop checking the change against it",
        },
  ];
}
