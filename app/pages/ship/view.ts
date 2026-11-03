// What the Ship page draws, worked out from what /api/ship answers. The DOM
// is main.ts.

export interface Rev {
  sha: string;
  subject: string;
}

export interface Check {
  name: string;
  workflow?: string;
  state?: string;
  bucket?: string;
}

export interface PR {
  number: number;
  title?: string;
  url?: string;
  state?: string;
  draft?: boolean;
  baseRef?: string;
  reviewDecision?: string;
  checks?: Check[];
  failing?: number;
  failLine?: string;
  mergeBlocked?: string;
}

export interface Project {
  root: string;
  name: string;
  branch?: string;
  home?: boolean;
  base?: string;
  files?: number;
  staged?: number;
  upstream?: string;
  ahead?: number;
  behind?: number;
  unpushed?: Rev[];
  pr?: PR;
  note?: string;
}

export interface Gate {
  name: string;
  state: string;
  note: string;
  button?: { label: string; do: string };
}

export interface Draft {
  title?: string;
  body?: string;
}

export interface State {
  projects?: Project[];
  gates?: Gate[];
  home?: string;
  view?: string;
  draft?: Draft;
  running?: boolean;
}

// Act is the one verb a step offers. root is which repository it acts on, so
// the host can point the diff there first.
export interface Act {
  label: string;
  do: string;
  root: string;
}

// Step is one card. "done" is behind you, "now" is where the change is,
// "waiting" is ahead of it, and "off" is a step this repository cannot take.
export interface Step {
  key: string;
  title: string;
  said: string;
  state: "done" | "now" | "waiting" | "off";
  act?: Act;
  // Why this step cannot be taken here, shown instead of a button.
  because?: string;
}

// ELSEWHERE is why the steps that reach GitHub are only ever home's: the
// routes behind them refuse a diff pointed at another repository.
const ELSEWHERE = "only this session's own checkout can do this";

export function steps(project: Project, draft?: Draft): Step[] {
  const out: Step[] = [work(project, draft), base(project), push(project)];
  out.push(raise(project));
  if (project.pr) out.push(checks(project), merge(project));
  return out;
}

function work(project: Project, draft?: Draft): Step {
  const files = project.files ?? 0;
  const staged = project.staged ?? 0;
  if (files === 0) {
    return { key: "work", title: "Uncommitted", said: "nothing uncommitted", state: "done" };
  }
  const said = `${plural(files, "file")}, ${staged} staged`;
  if (staged === 0) {
    return { key: "work", title: "Uncommitted", said, state: "now", because: "nothing is staged" };
  }
  if (!draft?.title) {
    return {
      key: "work",
      title: "Uncommitted",
      said,
      state: "now",
      act: { label: "Write the message", do: "message", root: project.root },
    };
  }
  return {
    key: "work",
    title: "Uncommitted",
    said: `${said} · ${draft.title}`,
    state: "now",
    act: { label: "Commit", do: "commit", root: project.root },
  };
}

function base(project: Project): Step {
  const behind = project.behind ?? 0;
  const on = project.base || "the base";
  if (behind === 0) {
    return { key: "base", title: "Base", said: `up to date with ${on}`, state: "done" };
  }
  return {
    key: "base",
    title: "Base",
    said: `${plural(behind, "commit")} behind ${on}`,
    state: "now",
    act: { label: "Rebase", do: "rebase", root: project.root },
  };
}

function push(project: Project): Step {
  const ahead = project.ahead ?? 0;
  const unpushed = project.unpushed?.length ?? 0;
  if (!project.upstream) {
    return {
      key: "push",
      title: "Pushed",
      said: unpushed === 0 ? "no branch on the remote" : `${plural(unpushed, "commit")}, none pushed`,
      state: unpushed === 0 ? "waiting" : "now",
      act: unpushed === 0 ? undefined : { label: "Push", do: "push", root: project.root },
    };
  }
  if (ahead === 0) {
    return { key: "push", title: "Pushed", said: `up to date with ${project.upstream}`, state: "done" };
  }
  return {
    key: "push",
    title: "Pushed",
    said: `${plural(ahead, "commit")} unpushed`,
    state: "now",
    act: { label: "Push", do: "push", root: project.root },
  };
}

function raise(project: Project): Step {
  const pr = project.pr;
  if (pr) {
    const what = pr.draft ? "draft" : (pr.state || "open").toLowerCase();
    return {
      key: "pr",
      title: "Pull request",
      said: `#${pr.number} ${what}${pr.title ? ` · ${pr.title}` : ""}`,
      state: "done",
    };
  }
  const ready = (project.unpushed?.length ?? 0) > 0;
  return {
    key: "pr",
    title: "Pull request",
    said: ready ? "none yet" : "nothing committed to raise one for",
    state: ready ? "now" : "waiting",
    because: ready && !project.home ? ELSEWHERE : undefined,
    act: ready && project.home ? { label: "Raise", do: "raise", root: project.root } : undefined,
  };
}

function checks(project: Project): Step {
  const pr = project.pr as PR;
  const failing = pr.failing ?? 0;
  const ran = pr.checks?.length ?? 0;
  if (ran === 0) {
    return { key: "checks", title: "Checks", said: "none have reported", state: "waiting" };
  }
  if (failing === 0) {
    return { key: "checks", title: "Checks", said: `${plural(ran, "check")} passing`, state: "done" };
  }
  return {
    key: "checks",
    title: "Checks",
    said: pr.failLine || `${plural(failing, "check")} failing`,
    state: "now",
    because: project.home ? undefined : ELSEWHERE,
    act: project.home ? { label: "Fix CI", do: "fix", root: project.root } : undefined,
  };
}

function merge(project: Project): Step {
  const pr = project.pr as PR;
  if ((pr.state || "").toUpperCase() === "MERGED") {
    return { key: "merge", title: "Merged", said: `#${pr.number} is in`, state: "done" };
  }
  if (pr.mergeBlocked) {
    return { key: "merge", title: "Merge", said: pr.mergeBlocked, state: "waiting" };
  }
  return {
    key: "merge",
    title: "Merge",
    said: "ready to merge",
    state: "now",
    because: project.home ? undefined : ELSEWHERE,
    act: project.home ? { label: "Merge", do: "merge", root: project.root } : undefined,
  };
}

// Column is one repository, in the order the page lays them out: home first.
export interface Column {
  project: Project;
  steps: Step[];
  note: string;
}

export function columns(state: State): Column[] {
  const view = state.view ?? "";
  return (state.projects ?? []).map((project) => ({
    project,
    // The draft describes the index of the repository the diff is on, and
    // claiming it for another's card would offer somebody else's message.
    steps: project.note ? [] : steps(project, drafting(project, view) ? state.draft : undefined),
    note: project.note ?? "",
  }));
}

function drafting(project: Project, view: string): boolean {
  return view === "" ? Boolean(project.home) : project.root === view;
}

// rail is the gates, worst first: a rail read top to bottom should start with
// what is in the way.
export function rail(state: State): Gate[] {
  const order = ["fail", "warn", "pending", "pass", "optional"];
  return [...(state.gates ?? [])].sort(
    (a, b) => rank(order, a.state) - rank(order, b.state),
  );
}

function rank(order: string[], state: string): number {
  const at = order.indexOf(state);
  return at === -1 ? order.length : at;
}

function plural(n: number, word: string): string {
  return n === 1 ? `1 ${word}` : `${n} ${word}s`;
}
