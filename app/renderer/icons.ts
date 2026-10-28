// The icon set, drawn rather than fetched: one font fewer, and each is the
// same stroke as the next.
export const PATHS: Record<string, string> = {
  // Two branches and the arrow asking for one to go into the other.
  pr: "M6 7a2 2 0 1 0 0-4 2 2 0 0 0 0 4M6 7v10M6 21a2 2 0 1 0 0-4 2 2 0 0 0 0 4M18 21a2 2 0 1 0 0-4 2 2 0 0 0 0 4M18 17V10a3 3 0 0 0-3-3h-4M13 5l-2 2 2 2",
  design: "M5 4h9l5 5v11H5zM14 4v5h5M8 13h8M8 16h5",
  session: "M4 5h16v10H9l-5 4z",
  todo: "M4 7h10M4 12h10M4 17h7M18 6l2 2 3-3",
  // A ring with a centre, as Linear and GitHub draw a ticket. The
  // exclamation this replaced read as something being wrong with it.
  issue: "M12 4a8 8 0 1 0 0 16 8 8 0 0 0 0-16M12 9.5a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5",
  inbox: "M3 13h5l1 3h6l1-3h5M3 13l3-8h12l3 8v6H3z",
  // A robot head: the Agents place, which is not the Team place.
  robot: "M12 3v2M6 5h12a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2M9 10v2M15 10v2M9.5 15h5",
  // Two people: the Team place, and a row that is somebody.
  people: "M9 11a3.2 3.2 0 1 0 0-6.4 3.2 3.2 0 0 0 0 6.4M3 19c0-3 2.7-4.6 6-4.6s6 1.6 6 4.6M16.5 11.2a2.6 2.6 0 1 0 0-5.2M18 14.6c2 .5 3 1.9 3 4.4",
  diff: "M6 3v12M6 18a2 2 0 1 0 0 .01M18 21V9M18 6a2 2 0 1 0 0-.01M6 9h6a3 3 0 0 1 3 3v6",
  code: "M9 6 3 12l6 6M15 6l6 6-6 6",
  bell: "M12 3a5 5 0 0 0-5 5v4l-2 3h14l-2-3V8a5 5 0 0 0-5-5M10 18a2 2 0 0 0 4 0",
  branch: "M6 4v10M6 17a2 2 0 1 0 0 .01M6 4a2 2 0 1 0 0-.01M18 7a2 2 0 1 0 0-.01M18 9c0 4-6 2-6 6",
  shield: "M12 3l7 3v5c0 5-3 7-7 9-4-2-7-4-7-9V6z",
  check: "M4 12l5 5L20 6",
  dot: "M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8",
  ring: "M12 5a7 7 0 1 0 0 14 7 7 0 0 0 0-14",
  play: "M7 4l12 8-12 8z",
  warn: "M12 4l9 16H3zM12 10v4M12 17v.01",
  eye: "M2 12s4-6 10-6 10 6 10 6-4 6-10 6-10-6-10-6M12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6",
  edit: "M4 20h4L19 9l-4-4L4 16zM14 6l4 4",
  folder: "M3 6h6l2 2h10v11H3z",
  "folder-open": "M3 7h6l2 2h8v2H6l-3 8zM3 19h14l3-8",
  file: "M6 3h8l5 5v13H6zM14 3v5h5",
  chevron: "M9 6l6 6-6 6",
  fold: "M13 6l-6 6 6 6M19 6l-6 6 6 6",
  unfold: "M5 6l6 6-6 6M11 6l6 6-6 6",
  plus: "M12 5v14M5 12h14",
  refresh: "M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7",
  wider: "M4 5v14M20 5v14M10 8l-4 4 4 4M14 8l4 4-4 4",
  narrower: "M4 5v14M20 5v14M6 8l4 4-4 4M18 8l-4 4 4 4",
  // A dial: how much of the plan window has gone.
  gauge: "M4 18a8 8 0 1 1 16 0M12 14l4-4",
  // Lines being looked over: the Review place, and a review asked for on the bus.
  review: "M4 5h8M4 9h5M4 13h6M4 17h4M16.5 10.5a4 4 0 1 0 0 8 4 4 0 0 0 0-8M19.4 17.4 22 20",
  // Sheets stacked: a space is one repository and all its worktrees.
  space: "M12 3 3 7.5l9 4.5 9-4.5zM3 12l9 4.5L21 12M3 16.5 12 21l9-4.5",
  history: "M12 3a9 9 0 1 0 8.5 6M21 3v6h-6M12 7v5l3 2",
  ask: "M4 5h16v11H9l-5 4V5M12 13v.01M10 9a2 2 0 1 1 2 2",
  answer: "M9 7 4 12l5 5M4 12h9a7 7 0 0 1 7 7",
  // A prompt and a caret: a shell, not the chevrons that mean source.
  terminal: "M3 5h18v14H3zM7 10l3 2.5-3 2.5M12.5 15H17",
};

// icon is one glyph at the size asked for, or nothing drawn at all when the
// name is unknown: a missing icon must never be a missing row.
export function icon(name: string, size = 16): SVGSVGElement {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("width", String(size));
  svg.setAttribute("height", String(size));
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("fill", "none");
  svg.setAttribute("stroke", "currentColor");
  svg.setAttribute("stroke-width", "1.6");
  svg.setAttribute("stroke-linecap", "round");
  svg.setAttribute("stroke-linejoin", "round");
  const d = PATHS[name];
  if (d) {
    const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
    path.setAttribute("d", d);
    svg.append(path);
  }
  return svg;
}
