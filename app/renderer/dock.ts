// The three columns beside the terminal, each dragged by its own grip and kept.

export type Edge = "sidebar" | "files" | "side";

// css is the variable the column's width is read from; `back` is a grip on
// the column's left, where a pointer moving left widens it.
export const WIDTHS: Record<Edge, { key: string; css: string; min: number; max: number; back?: true }> = {
  // Its own key: the width the old window kept was for a sidebar of lists, not tabs.
  sidebar: { key: "ade.sessions.w", css: "--sidebar-w", min: 160, max: 560 },
  files: { key: "ade.files", css: "--files-w", min: 200, max: 640 },
  side: { key: "ade.side.w", css: "--side-w", min: 240, max: 720, back: true },
};

// SIDE_SHUT is whether the sessions are folded away, kept between runs.
export const SIDE_SHUT = "ade.sessions.shut";

// A column that hides itself as the window narrows reads as a bug.
export function sideWidth(which: Edge, want: number): number {
  return Math.min(WIDTHS[which].max, Math.max(WIDTHS[which].min, Math.round(want)));
}

// A sidebar section whose height is dragged: how tall it may grow, kept
// between runs like the column widths. Unset is the stylesheet's own cap.
export type Band = "run" | "comments";
export const BANDS: Record<Band, { key: string; css: string; min: number; max: number }> = {
  run: { key: "ade.review.run.h", css: "--run-h", min: 48, max: 720 },
  comments: { key: "ade.review.comments.h", css: "--comments-h", min: 48, max: 720 },
};

export function bandHeight(which: Band, want: number): number {
  return Math.min(BANDS[which].max, Math.max(BANDS[which].min, Math.round(want)));
}
