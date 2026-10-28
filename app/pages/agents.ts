// Every agent at work, anywhere: /api/serving shaped into a tree. One
// answer for both windows, or the two disagree by the month's end.

// Health is what a session's server says its agent is up to.
export interface Health {
  running?: boolean;
  model?: string;
  error?: string;
  queued?: number;
  asking?: number;
  sideRuns?: string[] | null;
  children?: { kind?: string; name: string; doing?: string }[] | null;
  stacked?: { id: string; kind: string; what?: string }[] | null;
  lastEventAt?: string;
}

// Active is one announced session, as /api/serving lists it.
export interface Active {
  id: string;
  dir: string;
  name: string;
  repo?: string;
  branch?: string;
  machine?: string;
  agent?: Health | null;
}

export type State = "failed" | "wedged" | "asking" | "working" | "idle";

// The same five states, in the same order, as internal/machine/state.go:
// first match wins.
export function stateOf(agent: Health | null | undefined): State {
  if (!agent) return "idle";
  if (agent.error) return "failed";
  if (!agent.running && (agent.queued ?? 0) > 0) return "wedged";
  if ((agent.asking ?? 0) > 0) return "asking";
  return agent.running ? "working" : "idle";
}

// attention is whether a state is your turn, which is what turns the
// icon red. Silence is not in it: see internal/machine/state.go.
export function attention(state: State): boolean {
  return state === "failed" || state === "wedged" || state === "asking";
}

// Node is one row of the tree. A session's own agent carries the state; what
// it called carries what it is doing.
export interface Node {
  id: string;
  // session is the conversation this row belongs to, a child's included, so
  // clicking one opens the chat it came from; dir is where that checkout is.
  session: string;
  dir: string;
  // A side run is an agent you named, beside the main one; a sub-agent is
  // one the main agent called itself. Not the same thing to read.
  kind: "session" | "side" | "agent" | "task";
  name: string;
  state: State;
  model?: string;
  branch?: string;
  machine?: string;
  doing?: string;
}

// Branch is one session and what it called.
export interface Branch {
  row: Node;
  children: Node[];
}

export function tree(all: Active[]): Branch[] {
  // By name alone. Sorting by state first moved a row under the pointer
  // every time an agent went quiet, which is every few seconds.
  return [...all]
    .sort((a, b) => named(a).localeCompare(named(b)))
    .map((each) => ({
      row: {
        id: each.id,
        session: each.id,
        dir: each.dir,
        kind: "session" as const,
        name: named(each),
        state: stateOf(each.agent),
        model: each.agent?.model,
        branch: each.branch,
        machine: each.machine,
      },
      children: [
        ...(each.agent?.sideRuns ?? []).map((side) => ({
          id: `${each.id}:side:${side}`,
          session: each.id,
          dir: each.dir,
          kind: "side" as const,
          name: `@${side}`,
          state: "working" as State,
        })),
        ...(each.agent?.children ?? []).map((child) => ({
          id: `${each.id}:child:${child.name}`,
          session: each.id,
          dir: each.dir,
          kind: child.kind === "task" ? ("task" as const) : ("agent" as const),
          name: child.name,
          state: "working" as State,
          doing: child.doing,
        })),
      ],
    }));
}

// Stacked is one turn waiting behind a run, and which session it is
// waiting on: a queue drawn without that says when but not where.
export interface Stacked {
  id: string;
  session: string;
  where: string;
  kind: string;
  what: string;
  // position is 1 for the one that runs next in its own session.
  position: number;
}

// queues is every session's queue, in the order the sessions read in, each
// turn carrying the session it is stacked behind.
export function queues(all: Active[]): Stacked[] {
  return tree(all).flatMap((branch) =>
    ((all.find((a) => a.id === branch.row.id)?.agent?.stacked ?? []) as {
      id: string;
      kind: string;
      what?: string;
    }[])
      .filter((each) => each.id)
      .map((each, i) => ({
        id: each.id,
        session: branch.row.session,
        where: branch.row.name,
        kind: each.kind,
        what: (each.what ?? "").trim(),
        position: i + 1,
      })),
  );
}

// waiting is how many agents want an answer, which is the badge and the
// number inside a red icon.
export function waiting(all: Active[]): number {
  return all.filter((each) => attention(stateOf(each.agent))).length;
}

// The icon a row wears, by its state. Codicon names: the editor draws these
// and ade's own window maps them to its own set.
export function icon(node: Node): string {
  if (node.kind === "task") return "terminal";
  switch (node.state) {
    case "failed":
      return "error";
    case "wedged":
      return "warning";
    case "asking":
      return "question";
    case "working":
      return node.kind === "session" ? "sync~spin" : "circle-filled";
    default:
      return "circle-outline";
  }
}

// What a row says after its name: the state first, because that is what the
// list is read for.
export function detail(node: Node): string {
  if (node.kind === "session") {
    return [node.state, node.model, node.branch].filter(Boolean).join(" · ");
  }
  const kind =
    node.kind === "task"
      ? "command"
      : node.kind === "side"
        ? "running beside"
        : "sub-agent";
  return [kind, node.doing].filter(Boolean).join(" · ");
}

// named is what a session is called in the list: the repository, since one
// window per checkout makes the repository the thing you recognise.
function named(each: Active): string {
  return each.repo || each.name || each.id;
}

// same stops a redraw the poll would otherwise fire every few seconds, which
// collapses nothing here but moves what a click was aimed at.
export function same(a: Branch[], b: Branch[]): boolean {
  return (
    a.length === b.length &&
    a.every(
      (branch, i) =>
        branch.row.id === b[i].row.id &&
        branch.row.state === b[i].row.state &&
        branch.children.length === b[i].children.length &&
        branch.children.every(
          (child, j) =>
            child.id === b[i].children[j].id &&
            child.doing === b[i].children[j].doing,
        ),
    )
  );
}
