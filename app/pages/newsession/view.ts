// The new-session screen's shaping: the rows, what each still needs, and the
// summary of what Start would do. No vscode and no node, so it bundles.
import type { Mount } from "../sandboxfile";

export type Mode = "work" | "ticket" | "plan" | "design" | "branch" | "direct" | "scratch";

// Row is one of ade's existing ways in. The screen routes to them; it invents
// no behaviour of its own.
export interface Row {
  mode: Mode;
  label: string;
  detail: string;
  // names is where the branch comes from. "none" is a conversation where you
  // already are.
  names: "box" | "ticket" | "design" | "branch" | "none";
  // spans is whether more than one repository may be ticked.
  spans: boolean;
  // picks is whether a repository is ticked at all.
  picks: boolean;
}

export const ROWS: Row[] = [
  {
    mode: "work",
    label: "Work",
    detail: "a branch, a worktree and a design document",
    names: "box",
    spans: true,
    picks: true,
  },
  {
    mode: "ticket",
    label: "Ticket",
    detail: "the same, from a Linear ticket or a GitHub issue",
    names: "ticket",
    spans: true,
    picks: true,
  },
  {
    mode: "plan",
    label: "Plan",
    detail: "the document first; build it when you are happy with it",
    names: "box",
    spans: true,
    picks: true,
  },
  {
    mode: "design",
    label: "Design",
    detail: "carry on with a document already written",
    names: "design",
    spans: true,
    picks: true,
  },
  {
    mode: "branch",
    label: "Open branch",
    detail: "a branch or worktree you already have",
    names: "branch",
    spans: false,
    picks: true,
  },
  {
    mode: "direct",
    label: "Direct",
    detail: "talk on the branch this checkout is already on",
    names: "none",
    spans: false,
    picks: true,
  },
  {
    mode: "scratch",
    label: "Scratch",
    detail: "a checkout for work with nowhere to live yet",
    names: "box",
    spans: false,
    picks: false,
  },
];

export function rowFor(mode: Mode): Row {
  return ROWS.find((r) => r.mode === mode) ?? ROWS[0];
}

// Chosen is everything the screen holds. Every field but the name has a
// default, which is the point: they are on screen before they are accepted.
export interface Chosen {
  mode: Mode;
  name: string;
  // ticket is the Ticket row's box: FIX-623, or #12 for an issue of home.
  ticket: string;
  // design is a document by the file name the list gave, and ship is whether
  // it is this machine's and has to be carried to the one the session runs on.
  design: string;
  ship: boolean;
  message: string;
  machine: string;
  agent: string;
  model: string;
  role: string;
  sandbox: boolean;
  // mounts and env are the repository's sandbox, written before the session
  // starts. Every worktree of it gets them: the file is the repository's.
  mounts: Mount[];
  env: { [key: string]: string };
  // repos are ticked in the order they were ticked. The first is home.
  repos: string[];
  // The Open branch row's pick. `at` is the checkout it already has, empty
  // for a branch that has none, which is made when it is opened.
  branch: string;
  at: string;
}

// Doc is one document the Design row offers. ship marks one that is here and
// not on the machine the session will run on, so Start carries it over.
export interface Doc {
  file: string;
  ship: boolean;
}

// held is the entry a chosen document names, which is the pair and not the
// file: the same name can be on both machines, and they are not the same doc.
export function held(docs: Doc[], chosen: Chosen): Doc | undefined {
  return docs.find(
    (one) => one.file === chosen.design && one.ship === chosen.ship,
  );
}

// Make is one repository this would check out, as the summary lists them.
export interface Make {
  repo: string;
  path: string;
  home: boolean;
}

export interface Plan {
  branch: string;
  doc: string;
  // written marks a document that is already there, which is not made again.
  written: boolean;
  // carried marks one that is here and goes with the session.
  carried: boolean;
  makes: Make[];
}

export const fresh: Chosen = {
  mode: "work",
  name: "",
  ticket: "",
  design: "",
  ship: false,
  message: "",
  machine: "",
  agent: "",
  model: "",
  role: "",
  sandbox: true,
  mounts: [],
  env: {},
  repos: [],
  branch: "",
  at: "",
};

// home is where the agent runs, the session's root and where the document
// goes: the first repository ticked.
export function home(chosen: Chosen): string | undefined {
  return chosen.repos[0];
}

export type Ref = { linear: string } | { issue: number };

// The shapes the server accepts, so one it would refuse is refused here
// rather than after the worktree is made.
const linearRE = /^[A-Za-z][A-Za-z0-9]{0,15}-[0-9]{1,9}$/;
const issueRE = /^#?([0-9]{1,9})$/;

export function ref(text: string): Ref | undefined {
  const want = text.trim();
  if (linearRE.test(want)) return { linear: want.toUpperCase() };
  const issue = issueRE.exec(want);
  if (issue && issue[1] !== "0") return { issue: Number(issue[1]) };
  return undefined;
}

// names is what the branch is made from: the name typed, the ticket, or the
// document picked, whose own name ade reuses rather than scaffolding a second.
export function names(chosen: Chosen): string {
  const row = rowFor(chosen.mode);
  if (row.names === "box") return chosen.name.trim();
  // An issue is named after its title, which only the server can read.
  if (row.names === "ticket") {
    const one = ref(chosen.ticket);
    return one && "linear" in one ? one.linear : "";
  }
  if (row.names === "design") return chosen.design;
  if (row.names === "branch") return chosen.branch;
  return "";
}

// tick adds or removes a repository, keeping the order they were ticked in so
// home does not move when a later one is turned off.
export function tick(chosen: Chosen, path: string): string[] {
  if (chosen.repos.includes(path))
    return chosen.repos.filter((p) => p !== path);
  if (!rowFor(chosen.mode).spans) return [path];
  return [...chosen.repos, path];
}

// onto is the repositories that survive a change of row: one, where the new
// row works in one. Home stays home.
export function onto(chosen: Chosen, mode: Mode): string[] {
  const row = rowFor(mode);
  if (!row.picks) return [];
  return row.spans ? chosen.repos : chosen.repos.slice(0, 1);
}

// missing is what the screen still needs before it can start, or nothing.
export function missing(chosen: Chosen): string | undefined {
  const row = rowFor(chosen.mode);
  if (row.names === "box" && !chosen.name.trim()) return "Name the work.";
  if (row.names === "ticket" && !ref(chosen.ticket))
    return "Give a ticket: FIX-623, or #12 for a GitHub issue.";
  // The repository first where a design is picked: the list is home's, so
  // asking for a document before there is one to list asks the impossible.
  if (row.picks && chosen.repos.length === 0) return "Tick a repository.";
  if (row.names === "design" && !chosen.design) return "Pick a design.";
  if (row.names === "branch" && !chosen.branch) return "Pick a branch.";
  return undefined;
}

// summary is the line under the button: what Start would do, in one sentence.
export function summary(chosen: Chosen, plan: Plan): string {
  const row = rowFor(chosen.mode);
  if (!row.picks || plan.makes.length === 0) return row.detail;
  const where =
    plan.makes.length === 1
      ? plan.makes[0].repo
      : `${plan.makes[0].repo} and ${plan.makes.length - 1} more`;
  if (!plan.branch && row.names === "ticket") return `Issue #${issueOf(chosen)} in ${where}.`;
  if (!plan.branch) return `A conversation in ${where}.`;
  return `${plan.branch} in ${where}.`;
}

export function issueOf(chosen: Chosen): number | undefined {
  const one = ref(chosen.ticket);
  return one && "issue" in one ? one.issue : undefined;
}
