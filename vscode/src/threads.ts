import * as vscode from "vscode";
import { join } from "node:path";
import type { BranchPR, Client, Thread } from "./client";
import { idFrom, threadOf } from "./comments";
import { claimState, REPLY, shownComments, unsent } from "./commentrow";
import { badge, label, plan, threadState } from "./reconcile";

// Threads renders ade's review threads inline and sends replies back. It holds
// no state of its own: what is on screen is what the last fetch said.
export class Threads implements vscode.Disposable {
  private controller: vscode.CommentController;
  private held = new Map<string, vscode.CommentThread>();
  // The threads you asked to see in full, by id. Per window, not saved.
  private open = new Set<string>();
  private last: Thread[] = [];
  private pr?: BranchPR;
  // A comment with a state throws in the extension host without the proposal.
  private drafts = granted("commentsDraftState");

  constructor(private client: Client) {
    this.controller = vscode.comments.createCommentController(
      "ade",
      "ade review",
    );
    this.controller.commentingRangeProvider = {
      provideCommentingRanges: (doc) =>
        this.rel(doc.uri) ? [new vscode.Range(0, 0, doc.lineCount - 1, 0)] : [],
    };
  }

  dispose(): void {
    for (const thread of this.held.values()) thread.dispose();
    this.held.clear();
    this.controller.dispose();
  }

  // apply reconciles the threads on screen against a fetch. The pull request
  // comes with them: whether a comment can be sent depends on there being one.
  apply(threads: Thread[], pr?: BranchPR): void {
    this.pr = pr;
    this.last = threads;
    const { create, update, dispose } = plan(this.held, threads);

    for (const held of dispose) {
      this.forget(held);
      held.dispose();
    }
    for (const thread of create) {
      const made = this.draw(thread);
      if (made) this.held.set(thread.id, made);
    }
    for (const { held, thread } of update) {
      held.comments = shownComments(thread, this.open.has(thread.id)).map(
        (c, i) => this.comment(thread, c, i),
      );
      held.label = label(thread, Boolean(this.pr));
      held.contextValue = threadState(thread, Boolean(this.pr), this.open.has(thread.id));
      held.state = thread.resolved
        ? vscode.CommentThreadState.Resolved
        : vscode.CommentThreadState.Unresolved;
    }
  }

  // reply posts what was typed, and says so when it is refused: a rejected
  // POST used to leave the box on screen looking posted.
  async reply(reply: vscode.CommentReply): Promise<void> {
    const id = this.idOf(reply.thread);
    if (id) {
      await this.say(() => this.client.reply(id, reply.text));
      return;
    }
    const file = this.rel(reply.thread.uri);
    const at = reply.thread.range?.start.line;
    if (!file || at === undefined) {
      void vscode.window.showWarningMessage(
        "ade: comments go on a file in this checkout; this one is outside it.",
      );
      return;
    }
    const made = await this.say(() =>
      this.client.create(file, at + 1, reply.text),
    );
    if (made) reply.thread.dispose();
  }

  // say reports a refusal. The box comes down only once it is really filed.
  private async say<T>(call: () => Promise<T>): Promise<boolean> {
    try {
      await call();
      return true;
    } catch (err) {
      void vscode.window.showErrorMessage(`ade: ${String(err)}`);
      return false;
    }
  }

  // remove deletes the whole thread. ade has no per-comment delete: a comment is
  // a reply inside a thread, and the store only ever drops the thread.
  async remove(arg: unknown): Promise<void> {
    const id = this.which(arg);
    if (!id) return;
    await this.client.deleteThread(id);
    // Gone now rather than on the next poll, so the widget does not linger.
    const held = this.held.get(id);
    this.held.delete(id);
    held?.dispose();
  }

  async setResolved(arg: unknown, resolved: boolean): Promise<void> {
    const id = this.which(arg);
    if (id) await this.client.resolve(id, resolved);
  }

  // which thread a menu meant, from either side: a row in ade's comments tree
  // carries the id, a comment widget has to be looked up by identity.
  private which(arg: unknown): string | undefined {
    const carried = idFrom(arg);
    if (carried) return carried;
    const thread = threadOf<vscode.CommentThread>(arg);
    return thread && this.idOf(thread);
  }

  private draw(thread: Thread): vscode.CommentThread | undefined {
    const uri = vscode.Uri.file(join(this.client.root, thread.file));
    const at = new vscode.Range(thread.line - 1, 0, thread.line - 1, 0);
    const made = this.controller.createCommentThread(
      uri,
      at,
      shownComments(thread, this.open.has(thread.id)).map((c, i) =>
        this.comment(thread, c, i),
      ),
    );
    made.label = label(thread, Boolean(this.pr));
    made.contextValue = threadState(thread, Boolean(this.pr), this.open.has(thread.id));
    made.collapsibleState = thread.resolved
      ? vscode.CommentThreadCollapsibleState.Collapsed
      : vscode.CommentThreadCollapsibleState.Expanded;
    made.state = thread.resolved
      ? vscode.CommentThreadState.Resolved
      : vscode.CommentThreadState.Unresolved;
    return made;
  }

  // Only the claim carries a contextValue. The verbs on it act on the whole
  // thread, so a reply offering them again would be the same button twice.
  private comment(
    thread: Thread,
    c: Thread["comments"][number],
    at: number,
  ): vscode.Comment {
    return {
      author: { name: c.author === "you" ? "you" : c.author },
      label: badge(thread, c, at),
      body: new vscode.MarkdownString(c.body),
      mode: vscode.CommentMode.Preview,
      contextValue: at === 0 ? claimState(thread, Boolean(this.pr)) : REPLY,
      ...(this.drafts && at === 0 && unsent(thread)
        ? { state: vscode.CommentState.Draft }
        : {}),
    };
  }

  // The pull request the send would go to, for the command that needs its
  // number to confirm.
  get branchPR(): BranchPR | undefined {
    return this.pr;
  }

  // unfold shows a folded thread in full, or folds it back. The state is this
  // window's: the store knows nothing about how much of it you are reading.
  unfold(arg: unknown, open: boolean): void {
    const id = this.which(arg);
    if (!id) return;
    if (open) this.open.add(id);
    else this.open.delete(id);
    this.apply(this.last, this.pr);
  }

  get all(): Thread[] {
    return this.last;
  }

  // threadId is which thread a menu meant, for a command outside this class.
  threadId(arg: unknown): string | undefined {
    return this.which(arg);
  }

  private idOf(thread: vscode.CommentThread): string | undefined {
    for (const [id, held] of this.held) if (held === thread) return id;
    return undefined;
  }

  private forget(thread: vscode.CommentThread): void {
    const id = this.idOf(thread);
    if (id) this.held.delete(id);
  }

  // rel is the path as ade names it, or undefined for a file outside the root
  // the diff is of.
  private rel(uri: vscode.Uri): string | undefined {
    if (uri.scheme !== "file") return undefined;
    const rel = vscode.workspace.asRelativePath(uri, false);
    return rel.startsWith("..") ? undefined : rel;
  }
}

// VS Code empties enabledApiProposals unless argv.json enables this extension.
function granted(name: string): boolean {
  const pkg = vscode.extensions.getExtension("owenrumney.ade")?.packageJSON as
    | { enabledApiProposals?: string[] }
    | undefined;
  return pkg?.enabledApiProposals?.includes(name) ?? false;
}
