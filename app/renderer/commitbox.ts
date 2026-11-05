import { el } from "./omnibox";

// The commit box in Diff: a title, a body, and Write message, Commit, Push.
// Built once and kept out of the sidebar's redraw, which would take the typing.

export interface Standing {
  staged: number;
  unstaged: number;
  ahead: number;
  drafting: boolean;
  draft?: { title: string; body: string };
}

// Each answers with why it could not, or nothing when it did.
export interface Does {
  write(): Promise<string>;
  commit(title: string, body: string): Promise<string>;
  push(): Promise<string>;
  stageAll(): Promise<string>;
}

export const NOTHING: Standing = { staged: 0, unstaged: 0, ahead: 0, drafting: false };

// standingIn reads what the box shows out of a session's /api/state.
export function standingIn(state: unknown): Standing {
  const got = (state ?? {}) as {
    worktree?: { staged?: string[] | null; unstaged?: unknown[] | null; ahead?: number } | null;
    draft?: { title?: string; body?: string; raw?: string } | null;
    agent?: { running?: boolean; kind?: string } | null;
  };
  const draft = got.draft;
  return {
    staged: got.worktree?.staged?.length ?? 0,
    unstaged: got.worktree?.unstaged?.length ?? 0,
    ahead: got.worktree?.ahead ?? 0,
    drafting: Boolean(got.agent?.running && got.agent.kind === "commit-message"),
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

export class CommitBox {
  readonly el = el("div", "commitbox");
  private readonly line = el("span", "standing");
  private readonly stage = el("span", "link", "Stage all");
  private readonly title = document.createElement("input");
  private readonly body = document.createElement("textarea");
  private readonly write = el("span", "btn quiet", "Write message");
  private readonly commit = el("span", "btn", "Commit");
  private readonly push = el("span", "btn quiet", "Push");
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
    top.append(this.line, this.stage);
    const row = el("div", "row");
    row.append(this.write, this.commit, this.push);
    this.el.append(top, this.title, this.body, row);
    this.stage.addEventListener("click", () => void this.run(() => this.does.stageAll()));
    this.write.addEventListener("click", () => void this.drafted());
    this.commit.addEventListener("click", () => void this.committed());
    this.push.addEventListener("click", () => void this.run(() => this.does.push()));
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
    this.push.textContent = at.ahead > 0 ? `Push · ${at.ahead}` : "Push";
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

  private async committed(): Promise<void> {
    const { title, body } = this.message();
    if (this.at.staged === 0) return this.say("nothing is staged to commit");
    if (!title) return this.say("a commit needs a title");
    const why = await this.run(() => this.does.commit(title, body));
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
