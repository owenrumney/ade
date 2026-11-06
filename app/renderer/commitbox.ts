import { menu } from "./menu";
import { el } from "./omnibox";

// The commit box in Diff: a title, a body, and the buttons under them. Built
// once and kept out of the sidebar's redraw, which would take the typing.

export interface Standing {
  staged: number;
  unstaged: number;
  ahead: number;
  drafting: boolean;
  draft?: { title: string; body: string };
  // The branch and its pull request: Create PR shows for a branch without one.
  branch?: string;
  pr?: number;
}

// How a commit is made: folded into the last one, and pushed after.
export interface Commit {
  amend?: boolean;
  push?: boolean;
}

// Each answers with why it could not, or nothing when it did.
export interface Does {
  write(): Promise<string>;
  commit(title: string, body: string, how: Commit): Promise<string>;
  push(): Promise<string>;
  stageAll(): Promise<string>;
  createPR(): Promise<string>;
}

export const NOTHING: Standing = { staged: 0, unstaged: 0, ahead: 0, drafting: false };

// standingIn reads what the box shows out of a session's /api/state.
export function standingIn(state: unknown): Standing {
  const got = (state ?? {}) as {
    worktree?: { staged?: string[] | null; unstaged?: unknown[] | null; ahead?: number } | null;
    draft?: { title?: string; body?: string; raw?: string } | null;
    agent?: { running?: boolean; kind?: string } | null;
    meta?: { branch?: string } | null;
    branchPr?: { number?: number } | null;
  };
  const draft = got.draft;
  return {
    staged: got.worktree?.staged?.length ?? 0,
    unstaged: got.worktree?.unstaged?.length ?? 0,
    ahead: got.worktree?.ahead ?? 0,
    drafting: Boolean(got.agent?.running && got.agent.kind === "commit-message"),
    branch: got.meta?.branch || undefined,
    pr: got.branchPr?.number,
    // A reply that did not parse is still shown, as the body, to be edited.
    draft: draft ? { title: draft.title ?? "", body: draft.title ? (draft.body ?? "") : (draft.raw ?? "") } : undefined,
  };
}

// headline is what a commit would take, in one line.
export function headline(at: Standing): string {
  const left = at.unstaged > 0 ? ` · ${at.unstaged} not staged` : "";
  if (at.staged === 0) return left ? `nothing staged${left}` : "nothing to commit";
  return `${at.staged} staged${left}`;
}

// refusal is why a press cannot commit, or empty when it can. An amend may
// carry no title, keeping the old message, and nothing staged, changing it.
export function refusal(at: Standing, how: Commit, title: string): string {
  if (how.amend) return "";
  if (at.staged === 0) return "nothing is staged to commit";
  if (!title) return "a commit needs a title";
  return "";
}

export class CommitBox {
  readonly el = el("div", "commitbox");
  private readonly line = el("span", "standing");
  private readonly stage = el("span", "link", "Stage all");
  private readonly title = document.createElement("input");
  private readonly body = document.createElement("textarea");
  private readonly write = el("span", "btn quiet", "Write message");
  private readonly commit = el("span", "btn go", "Commit & push");
  private readonly more = el("span", "btn more", "▾");
  private readonly push = el("span", "btn quiet", "Push");
  private readonly pr = el("span", "btn quiet", "Create PR");
  private at = NOTHING;
  private applied = "";
  private busy = false;

  constructor(
    private readonly does: Does,
    private readonly say: (text: string) => void,
  ) {
    this.title.className = "title";
    this.title.placeholder = "Title";
    this.body.className = "body";
    this.body.placeholder = "Body";
    this.body.rows = 4;
    const top = el("div", "top");
    top.append(this.line, this.stage, this.push, this.pr);
    const split = el("span", "split");
    split.append(this.commit, this.more);
    const row = el("div", "acts");
    row.append(this.write, split);
    this.el.append(top, this.title, this.body, row);
    this.stage.addEventListener("click", () => void this.run(() => this.does.stageAll()));
    this.write.addEventListener("click", () => void this.drafted());
    this.commit.addEventListener("click", () => void this.committed({ push: true }));
    this.more.title = "Commit without pushing, or amend the last commit";
    this.more.addEventListener("click", () => {
      const box = this.more.getBoundingClientRect();
      menu({ x: box.left, y: box.bottom + 4 }, [
        { label: "Commit", run: () => void this.committed({}) },
        { label: "Amend the last commit", run: () => void this.committed({ amend: true }) },
        { label: "Amend and push", run: () => void this.committed({ amend: true, push: true }) },
      ]);
    });
    this.push.addEventListener("click", () => void this.run(() => this.does.push()));
    this.pr.title = "Pushes the branch and asks the agent to open a draft pull request, written up for a reviewer.";
    this.pr.addEventListener("click", () => void this.run(() => this.does.createPR()));
    this.show(NOTHING);
  }

  // show takes each read: the counts, what the buttons can do, and a new draft.
  show(at: Standing): void {
    this.at = at;
    this.line.textContent = headline(at);
    this.stage.hidden = at.unstaged === 0;
    this.write.textContent = at.drafting ? "Writing…" : "Write message";
    this.write.classList.toggle("off", at.drafting || at.staged === 0);
    this.commit.classList.toggle("off", at.staged === 0);
    // Push stands alone only once there is a commit to push.
    this.push.textContent = `Push · ${at.ahead}`;
    this.push.hidden = at.ahead === 0;
    this.pr.hidden = !at.branch || Boolean(at.pr);
    // Once per draft: one already shown is yours to edit, or to clear.
    const key = at.draft ? `${at.draft.title}\n${at.draft.body}` : "";
    if (at.draft && key !== this.applied) {
      this.title.value = at.draft.title;
      this.body.value = at.draft.body;
    }
    this.applied = key;
  }

  // reset empties the box for another session, whose own draft may then fill it.
  reset(): void {
    this.title.value = "";
    this.body.value = "";
    this.applied = "";
    this.show(NOTHING);
  }

  message(): { title: string; body: string } {
    return { title: this.title.value.trim(), body: this.body.value.trim() };
  }

  private async drafted(): Promise<void> {
    if (this.at.drafting) return;
    if (this.at.staged === 0) return this.say("nothing is staged to describe");
    await this.run(() => this.does.write());
  }

  private async committed(how: Commit): Promise<void> {
    const { title, body } = this.message();
    const no = refusal(this.at, how, title);
    if (no) return this.say(no);
    const why = await this.run(() => this.does.commit(title, body, how));
    if (why !== "") return;
    this.title.value = "";
    this.body.value = "";
  }

  // run is one press at a time; what went wrong is said, and handed back.
  private async run(what: () => Promise<string>): Promise<string | undefined> {
    if (this.busy) return undefined;
    this.busy = true;
    this.el.classList.add("busy");
    const why = await what().catch((e: Error) => e.message || "that failed");
    this.busy = false;
    this.el.classList.remove("busy");
    if (why) this.say(why);
    return why;
  }
}
