// What the explain panel draws, kept out of the page so it can be tested.
export interface Line {
  n: number;
  text: string;
  in?: boolean;
}

export interface Step {
  ref: string;
  title: string;
  file: string;
  line: number;
  endLine?: number;
  body: string;
  node?: string;
  excerpt?: Line[];
}

export interface Node {
  id: string;
  label: string;
  file?: string;
  line?: number;
}

export interface Edge {
  from: string;
  to: string;
  label?: string;
}

export interface Flow {
  nodes: Node[];
  edges: Edge[];
}

export interface Walkthrough {
  steps: Step[];
  flow?: Flow;
  files: number;
  base?: string;
  scope?: string;
  stale?: boolean;
}

// header names the scope the walkthrough was written for, so a walk through a
// third of the change is visible rather than silent.
export function header(walk: Walkthrough | undefined): string {
  if (!walk) return "";
  const files = `${walk.files} file${walk.files === 1 ? "" : "s"}`;
  const where =
    walk.scope === "uncommitted"
      ? "what is not committed"
      : walk.scope === "committed"
        ? "what is committed"
        : `the branch against ${walk.base || "its base"}`;
  const steps = `${walk.steps.length} step${walk.steps.length === 1 ? "" : "s"}`;
  return `${steps} through ${where}, ${files}.`;
}

// where is the link under a step: the range it is about.
export function where(step: Step): string {
  const end = step.endLine ?? step.line;
  return end > step.line ? `${step.file}:${step.line}-${end}` : `${step.file}:${step.line}`;
}

// counter is the "01/07" under the step, zero-padded so it does not reflow.
export function counter(at: number, total: number): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(at + 1)}/${pad(total)}`;
}

export function clamp(at: number, total: number): number {
  if (total < 1) return 0;
  return Math.min(Math.max(at, 0), total - 1);
}

export function at(walk: Walkthrough | undefined, i: number): Step | undefined {
  return walk?.steps[clamp(i, walk.steps.length)];
}

// moved is whether the step on screen belongs to a different walkthrough. A
// regenerate replaces the steps, so the place in them goes too.
export function moved(was: Walkthrough | undefined, now: Walkthrough | undefined): boolean {
  const refs = (w: Walkthrough | undefined) =>
    (w?.steps ?? []).map((one) => `${one.ref}\u0000${one.title}`).join("\n");
  return refs(was) !== refs(now);
}

// lit is which node a step is at, or -1. The index is the identity.
export function lit(flow: Flow | undefined, step: Step | undefined): number {
  if (!flow || !step?.node) return -1;
  return flow.nodes.findIndex((node) => node.id === step.node);
}

// Lane is one node, drawn as a column with a line down it.
export interface Lane {
  label: string;
  file?: string;
  line?: number;
  lit: boolean;
}

// Wire is one edge, a row across lanes. from === to is work at one lane.
export interface Wire {
  label: string;
  from: number;
  to: number;
  lit: boolean;
}

export interface Sequence {
  lanes: Lane[];
  wires: Wire[];
}

// sequence is the lanes and their messages, in the order the edges come.
export function sequence(flow: Flow | undefined, step: Step | undefined): Sequence {
  if (!flow || flow.nodes.length < 2) return { lanes: [], wires: [] };
  const which = new Map<string, number>();
  for (const [i, node] of flow.nodes.entries()) if (!which.has(node.id)) which.set(node.id, i);
  const on = lit(flow, step);
  const lanes = flow.nodes.map((node, i) => ({
    label: node.label,
    file: node.file,
    line: node.line,
    lit: i === on,
  }));
  const wires: Wire[] = [];
  for (const edge of flow.edges) {
    const from = which.get(edge.from);
    const to = which.get(edge.to);
    if (from === undefined || to === undefined) continue;
    wires.push({ label: edge.label ?? "", from, to, lit: from === on });
  }
  return { lanes, wires };
}
