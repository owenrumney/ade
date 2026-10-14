// What a conversation is made of. The server renders prose to html already, so
// nothing here parses markdown.
export interface Message {
  id: string;
  kind: "you" | "agent" | "tool" | "note" | string;
  author?: string;
  model?: string;
  body?: string;
  html?: string;
  run?: string;
  files?: Attached[];
  images?: Pasted[];
  tool?: string;
  toolId?: string;
  doing?: string;
  steps?: number;
  input?: string;
  output?: string;
  target?: string;
  failed?: boolean;
  // A reply to a review of the diff, whose output is the comments it filed.
  review?: boolean;
  // undo names the snapshot taken before your message ran; undone is set
  // once it has been undone, and is the way back.
  undo?: string;
  undone?: string;
  // foldedInto names the prompt this one was sent with, as one turn.
  foldedInto?: string;
  at: string;
}

// Attached mirrors chat.File: something dropped on a message, written beside
// the conversation and served back by name.
export interface Attached {
  name: string;
  path: string;
  image?: boolean;
}

// Pasted mirrors chat.Image: the bytes inline. The outbox puts a turn's
// pictures on the reply this way, and old conversations hold pasted ones.
export interface Pasted {
  mediaType: string;
  data: string;
}

// Shown is one thing a message carries besides its words.
export interface Shown {
  src: string;
  name: string;
  image: boolean;
  path?: string;
}

// Where the server serves an attachment from. It keys them by base name, so a
// path recorded inside a sandbox still resolves from here.
export function attachmentPath(path: string): string {
  return `/attachments/${encodeURIComponent(path.split("/").pop() || path)}`;
}

// shown is what to draw under a message. Both carriers are read: a prompt's
// attachments are files on disk, and an outbox picture is bytes on the reply.
export function shown(msg: Message, src: (path: string) => string): Shown[] {
  const out: Shown[] = (msg.files || []).map((file) => ({
    src: src(file.path),
    name: file.name,
    image: Boolean(file.image),
    path: file.path,
  }));
  for (const [i, img] of (msg.images || []).entries()) {
    out.push({
      src: `data:${img.mediaType};base64,${img.data}`,
      name: `image ${i + 1}`,
      image: true,
    });
  }
  return out;
}

// shotsOf is every picture in the conversation, in the order it was said. The
// lightbox walks the whole thread, not the message it was opened from.
export function shotsOf(
  messages: Message[],
  src: (path: string) => string,
): Shown[] {
  return messages.flatMap((msg) => shown(msg, src).filter((one) => one.image));
}

// foldable is a named agent's reply, with a document open to fold it into.
// Only a named one: the main agent's own words already wrote the document.
// Never a reviewer of the diff: its output is the comments it filed.
// Act is a verb a message carries. Copy is for prose worth taking away, replay
// for your own words, fold for a named agent's reply into an open document,
// hide for a reply you have read and want out of the way, fixture to save your
// prompt and the tree it ran on as a prompt fixture.
export type Act = "copy" | "hide" | "replay" | "fold" | "undo" | "fixture";

// Change is one file an undo would touch: put back, deleted or recreated.
export interface Change {
  path: string;
  do: "restore" | "delete" | "recreate" | string;
}

// turnsAfter is how many of your messages came after this one. Undoing it
// undoes those too: the tree goes back to when it was sent.
export function turnsAfter(messages: Message[], id: string): number {
  const at = messages.findIndex((msg) => msg.id === id);
  if (at < 0) return 0;
  return messages.slice(at + 1).filter((msg) => msg.kind === "you").length;
}

// undoSummary says what confirming will do, before anything is done.
export function undoSummary(changes: Change[], later: number): string {
  const count = (what: string) => changes.filter((c) => c.do === what).length;
  const files = (n: number) => `${n} file${n === 1 ? "" : "s"}`;
  const parts = [
    count("restore") ? `put ${files(count("restore"))} back` : "",
    count("delete") ? `delete ${files(count("delete"))}` : "",
    count("recreate") ? `recreate ${files(count("recreate"))}` : "",
  ].filter(Boolean);
  const also = later
    ? ` This also undoes ${later} later turn${later === 1 ? "" : "s"}.`
    : "";
  return `This will ${parts.join(", ")}.${also}`;
}

// acts is what a bubble offers. A tool call and a note are not prose and are
// not yours, so they offer nothing.
export function acts(msg: Message, doc: string | undefined): Act[] {
  if (msg.foldedInto) return [];
  if (msg.kind === "you") {
    const mine: Act[] = (msg.body ?? "").trim() ? ["replay"] : [];
    if (msg.undo) mine.push("undo", "fixture");
    return mine;
  }
  if (msg.kind !== "agent") return [];
  const out: Act[] = (msg.body ?? "").trim() ? ["copy", "hide"] : [];
  if (foldable(msg, doc)) out.push("fold");
  return out;
}

export function foldable(msg: Message, doc: string | undefined): boolean {
  return (
    Boolean(doc) && msg.kind === "agent" && Boolean(msg.author) && !msg.review
  );
}

// line is a tool call folded to one: what it did, and to what. A tool with no
// target is still worth a line, because the agent doing nothing looks the same.
// A sub-agent still at work adds its own step, so the fold shows it moving.
export function line(msg: Message): string {
  const what =
    [msg.tool, msg.target].filter(Boolean).join(" ").trim() || "working";
  if (!msg.doing) return what;
  return `${what} · ${msg.steps ?? 0} steps · ${msg.doing}`;
}

// who names the speaker. A named agent is not the main one and has to say so,
// or @security's verdict reads as the conversation's.
export function who(msg: Message): string {
  if (msg.kind === "you") return "you";
  return msg.author || "agent";
}

export function clock(at: string): string {
  const when = new Date(at);
  return Number.isNaN(when.getTime())
    ? ""
    : when.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

// escape is for the text the server did not render: a prompt is yours and is
// never html, whatever it happens to contain.
export function escape(text: string): string {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

// Asked is one question the agent put to you, out of AskUserQuestion's input.
export interface Asked {
  question: string;
  header?: string;
  multiSelect?: boolean;
  options?: { label: string; description?: string }[];
}

// isQuestion is the one tool call that is not a step: the turn stops on it.
export function isQuestion(msg: Message): boolean {
  return msg.kind === "tool" && msg.tool === "AskUserQuestion";
}

export function questionsOf(input?: string): Asked[] {
  try {
    const parsed = JSON.parse(input || "{}") as { questions?: Asked[] };
    return Array.isArray(parsed.questions) ? parsed.questions : [];
  } catch {
    return [];
  }
}

// A run of tool calls is one block, collapsed: twenty greps between two
// sentences push the sentences off the screen.
export type Block =
  { kind: "msg"; msg: Message } | { kind: "tools"; msgs: Message[] };

export function group(
  messages: Message[],
  held?: ReadonlySet<string>,
): Block[] {
  const out: Block[] = [];
  for (const msg of messages) {
    const last = out[out.length - 1];
    // A question or a held call is never folded into a run: "3 steps" is no
    // way to show the thing the turn is waiting on.
    const stopped = isQuestion(msg) || held?.has(msg.toolId || "");
    if (msg.kind === "tool" && !stopped) {
      if (last?.kind === "tools") last.msgs.push(msg);
      else out.push({ kind: "tools", msgs: [msg] });
      continue;
    }
    out.push({ kind: "msg", msg });
  }
  return out;
}

// summary is the one line a collapsed run shows. The count, and the last
// thing it did: what the agent is on now is worth more than what it started.
export function summary(msgs: Message[]): string {
  const failed = msgs.filter((m) => m.failed).length;
  const what =
    msgs.length === 1
      ? line(msgs[0])
      : `${msgs.length} steps · ${line(msgs[msgs.length - 1])}`;
  return failed ? `${what} · ${failed} failed` : what;
}

// A run holding a failure opens itself: the count alone hides the one line
// somebody needs to read.
export function opens(msgs: Message[]): boolean {
  return msgs.some((m) => m.failed);
}

// working reports whether the agent owes a reply: it is running and the last
// thing said was not its own.
export function working(
  messages: Message[],
  running: boolean,
  asking = 0,
): boolean {
  // A paused turn owes nothing until you answer: a spinner there is a lie.
  if (!running || asking) return false;
  const last = messages[messages.length - 1];
  return !last || last.kind !== "agent";
}

// selected is whether text is selected inside root. A caret is not.
export function selected(root: Node, now: Selection | null): boolean {
  if (!now || now.isCollapsed || now.rangeCount === 0) return false;
  const at = now.getRangeAt(0).commonAncestorContainer;
  const node = at.nodeType === 3 ? at.parentNode : at;
  return Boolean(node && root.contains(node));
}

// step is what the agent is on and for how long: the last tool call, or
// thinking, aged from the last thing anyone said.
export function step(messages: Message[], now: number): string {
  const last = messages[messages.length - 1];
  const what =
    last?.kind === "tool"
      ? `${last.tool || "tool"} ${last.target || ""}`.trim()
      : "thinking…";
  const since = last ? now - Date.parse(last.at) : NaN;
  if (Number.isNaN(since) || since < 1000) return what;
  const secs = Math.floor(since / 1000);
  const age = secs < 60 ? `${secs}s` : `${Math.floor(secs / 60)}m${secs % 60}s`;
  return `${what} · ${age}`;
}

// Folds is a conversation with each welded prompt under the one it went with.
export interface Folds {
  top: Message[];
  under: Map<string, Message[]>;
}

export function folds(messages: Message[]): Folds {
  const ids = new Set(messages.map((m) => m.id));
  const into = new Map<string, string>();
  for (const m of messages) {
    // A mark naming a prompt that is gone is ignored, not a prompt lost.
    if (m.foldedInto && m.foldedInto !== m.id && ids.has(m.foldedInto)) into.set(m.id, m.foldedInto);
  }
  const top: Message[] = [];
  const under = new Map<string, Message[]>();
  for (const msg of messages) {
    const at = rootOf(msg.id, into);
    if (at === msg.id) {
      top.push(msg);
      continue;
    }
    under.set(at, [...(under.get(at) ?? []), msg]);
  }
  return { top, under };
}

// rootOf is the top-level prompt a fold is drawn under, however deep the
// marks nest. A loop in the marks puts the message at the top rather than nowhere.
function rootOf(id: string, into: Map<string, string>): string {
  const seen = new Set<string>();
  let at = id;
  for (let next = into.get(at); next !== undefined; next = into.get(at)) {
    if (seen.has(at)) return id;
    seen.add(at);
    at = next;
  }
  return at;
}

// foldNote is the line under a welded prompt.
export function foldNote(count: number): string {
  return `${count} prompts sent as one turn · undo takes back all of them`;
}

const redrawn = new Set(["chat", "agent", "state", "resync"]);

// An unnamed event counts: a frame we cannot read is not one to drop.
export function redraws(name?: string): boolean {
  return !name || redrawn.has(name);
}

// Side is a named agent at work beside the main one, as /api/state reports it.
export interface Side {
  id: string;
  name: string;
  startedAt: string;
}

export function age(ms: number): string {
  if (!Number.isFinite(ms) || ms < 1000) return "";
  const secs = Math.floor(ms / 1000);
  return secs < 60 ? `${secs}s` : `${Math.floor(secs / 60)}m${secs % 60}s`;
}

// sideLabel is the line a running named agent shows while it works.
export function sideLabel(side: Side, now: number): string {
  const since = age(now - Date.parse(side.startedAt));
  return `@${side.name} is working${since ? ` · ${since}` : ""}`;
}

// queuedLabel is the line saying your message is waiting its turn.
export function queuedLabel(n: number): string {
  if (n <= 0) return "";
  return n === 1 ? "1 message waiting its turn" : `${n} messages waiting their turn`;
}
