import * as vscode from "vscode";
import { input } from "./pick";
import { quoted } from "./detail";
import { join as joinPath } from "node:path";
import type { Client, State } from "./client";
import {
  ahead,
  committable,
  groups,
  icon,
  join,
  rejected,
  type Row,
  stageFirst,
  split,
} from "./commit";
import type { GitRepository } from "./gitapi";
import { rel } from "./picked";

// ScmLabel names ade's provider in the repository picker. VS Code's own Git
// provider is already there under the folder's name, so this must not be one.
export const ScmLabel = "ade session";

// Index is where the changeset is drawn and moved: Git's own repository when
// this window has it, ade's provider for a root it cannot see.
export interface Index extends vscode.Disposable {
  readonly box: { value: string };
  show(staged: Row[], changed: Row[], line: string): void;
  stage(paths: string[]): Promise<unknown>;
  unstage(paths: string[]): Promise<unknown>;
}

// GitIndex drives Git's repository; its rows and gestures stay Git's own.
export class GitIndex implements Index {
  constructor(
    private repo: GitRepository,
    private root: string,
  ) {}

  get box(): { value: string } {
    return this.repo.inputBox;
  }

  show(): void {}

  stage(paths: string[]): Promise<void> {
    return this.repo.add(paths.map((p) => joinPath(this.root, p)));
  }

  unstage(paths: string[]): Promise<void> {
    return this.repo.revert(paths.map((p) => joinPath(this.root, p)));
  }

  dispose(): void {}
}

// AdeIndex is ade's own provider, for a root this window's Git cannot reach.
export class AdeIndex implements Index {
  private control: vscode.SourceControl;
  private stagedGroup: vscode.SourceControlResourceGroup;
  private changedGroup: vscode.SourceControlResourceGroup;

  constructor(
    private client: Client,
    private root: string,
  ) {
    this.control = vscode.scm.createSourceControl(
      "ade",
      ScmLabel,
      vscode.Uri.file(root),
    );
    this.control.inputBox.placeholder = PLACEHOLDER;
    this.control.acceptInputCommand = {
      command: "ade.commit",
      title: "Commit",
    };
    this.control.quickDiffProvider = undefined;

    this.stagedGroup = this.control.createResourceGroup(
      "staged",
      "Staged Changes",
    );
    this.changedGroup = this.control.createResourceGroup("changed", "Changes");
    this.stagedGroup.hideWhenEmpty = true;
    this.changedGroup.hideWhenEmpty = true;
  }

  get box(): { value: string } {
    return this.control.inputBox;
  }

  show(staged: Row[], changed: Row[], line: string): void {
    this.stagedGroup.resourceStates = staged.map((row) => this.row(row, true));
    this.changedGroup.resourceStates = changed.map((row) =>
      this.row(row, false),
    );
    this.control.count = staged.length + changed.length;
    this.control.inputBox.placeholder = line
      ? `Message · ${line}`
      : PLACEHOLDER;
  }

  stage(paths: string[]): Promise<unknown> {
    return this.client.stage(paths);
  }

  unstage(paths: string[]): Promise<unknown> {
    return this.client.unstage(paths);
  }

  dispose(): void {
    this.control.dispose();
  }

  private row(row: Row, staged: boolean): vscode.SourceControlResourceState {
    const uri = vscode.Uri.file(joinPath(this.root, row.path));
    return {
      resourceUri: uri,
      contextValue: staged ? "ade.staged" : "ade.changed",
      command: { command: "vscode.open", title: "Open", arguments: [uri] },
      decorations: {
        strikeThrough: row.status === "deleted",
        tooltip: row.status || (staged ? "staged" : ""),
        iconPath: new vscode.ThemeIcon(icon(row.status || "modified")),
      },
    };
  }
}

const PLACEHOLDER = "Message (⌘Enter to commit what is staged)";

// Scm is the session's changeset: the agent's draft, commit and push through
// ade, and stage and unstage through whichever index holds it.
export class Scm implements vscode.Disposable {
  private staged: Row[] = [];
  private changed: Row[] = [];
  private draft = "";
  // The last draft put in the box, so a state that repeats it does not
  // overwrite what has been typed since.
  private wrote = "";
  // Set while a message this window asked for is still being written.
  private waiting = false;

  constructor(
    private client: Client,
    private root: string,
    private index: Index,
  ) {}

  dispose(): void {
    this.index.dispose();
  }

  // use moves the changeset to another index, as the Diff view's root moves.
  // The root moves with it: a row's path is relative to the root it came from.
  use(index: Index, root: string): void {
    this.index.dispose();
    this.index = index;
    this.root = root;
    this.wrote = "";
    this.index.show(this.staged, this.changed, "");
  }

  apply(state: State): void {
    const { staged, changed } = groups(state.worktree);
    this.staged = staged;
    this.changed = changed;
    this.index.show(staged, changed, ahead(state.worktree));

    // A draft is what the agent wrote for what is staged. It only lands in an
    // untouched box: the message being typed is the person's, not the model's.
    const draft = state.draft ? join(state.draft) : "";
    this.draft = draft;
    if (draft && draft !== this.wrote && this.untouched()) {
      this.index.box.value = draft;
      this.wrote = draft;
      if (this.waiting) {
        this.waiting = false;
        void this.offer();
      }
    }
  }

  // offer is what to do with the message that has just arrived. Without it the
  // draft lands in a box on a view you are not looking at.
  private async offer(): Promise<void> {
    const said = await vscode.window.showInformationMessage(
      "ade: commit message ready.",
      "Commit",
      "Commit and push",
      "Edit it first",
    );
    if (said === "Edit it first") {
      await vscode.commands.executeCommand("workbench.view.scm");
      return;
    }
    if (!said) return;
    if (!(await this.commit(false))) return;
    if (said === "Commit and push") await this.push();
  }

  async push(force = false): Promise<void> {
    try {
      await this.client.push(force);
      vscode.window.setStatusBarMessage("ade: pushed", 3000);
    } catch (err) {
      const why = String(err);
      if (!force && rejected(why)) {
        await this.offerForce(why);
        return;
      }
      void vscode.window.showErrorMessage(`ade: ${why}`);
    }
  }

  // The refusal after an amend is the ordinary case, and git's message names it
  // without saying what to do. Modal, because a force replaces the remote branch.
  private async offerForce(why: string): Promise<void> {
    const said = await vscode.window.showWarningMessage(
      "ade: the remote has the branch at a commit this push would drop.",
      {
        modal: true,
        detail: `${quoted(why)}\n\nForce pushing replaces what is on the remote. An amended commit needs it; anyone who pulled the old one will have to reset.`,
      },
      "Force push",
    );
    if (said === "Force push") await this.push(true);
  }

  async stage(uris: vscode.Uri[]): Promise<void> {
    await this.move(uris, (paths) => this.index.stage(paths), "stage");
  }

  async unstage(uris: vscode.Uri[]): Promise<void> {
    await this.move(uris, (paths) => this.index.unstage(paths), "unstage");
  }

  // Everything not staged, and everything staged, as the two buttons the
  // Diff view needs. The groups are already worked out for the rows.
  async stageAll(): Promise<void> {
    await this.stage(
      this.changed.map((row) => vscode.Uri.file(joinPath(this.root, row.path))),
    );
  }

  async unstageAll(): Promise<void> {
    await this.unstage(
      this.staged.map((row) => vscode.Uri.file(joinPath(this.root, row.path))),
    );
  }

  get stagedCount(): number {
    return this.staged.length;
  }

  get changedCount(): number {
    return this.changed.length;
  }

  // The Diff view has no input box, so an empty one is asked for rather than
  // refused. The agent's draft is what it starts from.
  private async ask(amend: boolean): Promise<string | undefined> {
    const said = await input({
      title: amend
        ? "Amend the last commit"
        : `Commit ${this.staged.length} staged file(s)`,
      prompt: "First line is the title; the rest is the body",
      value: amend ? await this.lastMessage() : this.draft,
    });
    return said?.trim() ? said : undefined;
  }

  // lastMessage is what an amend starts from: the commit's own words, not the
  // agent's draft for a change that is not this one.
  private async lastMessage(): Promise<string> {
    try {
      const head = await this.client.headMessage();
      return join({ ...head, staged: null });
    } catch {
      return this.draft;
    }
  }

  async commit(amend: boolean): Promise<boolean> {
    let words = this.index.box.value;
    if (!words.trim() && this.staged.length > 0)
      words = (await this.ask(amend)) ?? "";
    if (!committable(words, this.staged, amend)) {
      void vscode.window.showWarningMessage(
        this.staged.length === 0
          ? "ade: nothing is staged."
          : "ade: a commit needs a title.",
      );
      return false;
    }
    const { title, body } = split(words);
    try {
      await this.client.commit(title, body, amend);
    } catch (err) {
      void vscode.window.showErrorMessage(`ade: ${String(err)}`);
      return false;
    }
    this.index.box.value = "";
    this.wrote = "";
    return true;
  }

  // writeMessage asks the agent for one. It answers before the message exists,
  // so the box fills on the state that follows rather than here.
  async writeMessage(): Promise<void> {
    this.index.box.value = "";
    this.wrote = "";
    if (stageFirst(this.stagedCount, this.changedCount)) await this.stageAll();
    let queued = 0;
    try {
      queued = (await this.client.writeCommitMessage()).queued ?? 0;
    } catch (err) {
      void vscode.window.showErrorMessage(`ade: ${String(err)}`);
      return;
    }
    if (queued > 0) {
      vscode.window.setStatusBarMessage(
        "ade: commit message queued behind the current turn",
        4000,
      );
    }
    // Here, not the chat: the message is going to land in this box, and the
    // progress belongs where the result will be.
    this.waiting = true;
    await vscode.commands.executeCommand("workbench.view.scm");
  }

  private untouched(): boolean {
    const now = this.index.box.value.trim();
    return now === "" || now === this.wrote.trim();
  }

  private async move(
    uris: vscode.Uri[],
    call: (paths: string[]) => Promise<unknown>,
    verb: string,
  ): Promise<void> {
    const paths = uris
      .map((uri) => rel(this.root, uri.fsPath))
      .filter((p): p is string => p !== undefined);
    if (paths.length === 0) {
      if (uris.length > 0) {
        void vscode.window.showWarningMessage(
          `ade: that is outside ${this.root}, which is what this session diffs.`,
        );
      }
      return;
    }
    try {
      await call(paths);
    } catch (err) {
      void vscode.window.showErrorMessage(
        `ade: could not ${verb} that (${String(err)})`,
      );
    }
  }
}
