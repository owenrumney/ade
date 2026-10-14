import * as vscode from "vscode";
import type { BusMember, BusOffer, BusReply, BusSent, BusState } from "./client";

// The two group headings, named once so the tree and its twisty agree.
export const ANSWERS = "Answers";
export const WAITING = "Waiting on them";
export const ONLINE = "Online";
export const REVIEWS = "Review requests";

// Fresh is what arrived since the last state, for the notifications.
export interface Fresh {
  offers: BusOffer[];
  replies: BusReply[];
  // back is who became reachable again, for a watch set on their row.
  back: string[];
}

// returned is who woke up: awake now and not awake before, which includes
// somebody who was not in the roster at all. A watch fires on that edge.
export function returned(before: BusMember[], after: BusMember[]): string[] {
  const was = new Map(before.map((m) => [m.user, m.presence]));
  const out = new Set<string>();
  for (const m of after) {
    if (m.you) continue;
    if (m.presence === "active" && was.get(m.user) !== "active") out.add(m.user);
  }
  return [...out];
}

// PRESENCES are what you can set yourself to, and what each means. Automatic
// is the way back: it follows your editor window again.
export const PRESENCES: { label: string; detail: string; to: string }[] = [
  { label: "$(circle-filled) Active", detail: "here, whatever the window says", to: "active" },
  { label: "$(circle-outline) Away", detail: "not here, whatever the window says", to: "idle" },
  { label: "$(sync) Automatic", detail: "follow the editor window again", to: "" },
];

// replyLine is how an answer reads in a notification: who, and what they said.
export function replyLine(reply: BusReply): string {
  const who = reply.byAgent ? `${reply.from} (their agent)` : reply.from;
  if (reply.kind === "review") return `${who} reviewed it: ${reply.verdict}`;
  const said = [reply.option, reply.text].filter(Boolean).join(" ").trim();
  return `${who} answered: ${said}`;
}

// A row of the Team tree: a group header, a request waiting on you, or
// somebody else's harness.
export type TeamNode =
  | { kind: "group"; title: string; count: number; open: boolean }
  | { kind: "offer"; offer: BusOffer }
  | { kind: "reply"; reply: BusReply }
  | { kind: "sent"; sent: BusSent }
  | { kind: "member"; member: BusMember };

// shortEnough is the question that fits a quick pick. Anything longer gets a
// panel, where it is read rather than guessed at from an elided middle.
export const QUESTION_CAP = 140;
export const OPTION_CAP = 60;

export function shortEnough(offer: BusOffer): boolean {
  const options = offer.options ?? [];
  if (!options.length) return false;
  if ((offer.question ?? "").length > QUESTION_CAP) return false;
  return options.every((option) => option.length <= OPTION_CAP);
}

// busLine is what a row says beside the question: who is waiting, and the
// agent of theirs that is actually blocked on it.
export function busLine(offer: BusOffer, me = ""): string {
  const bits = [offer.from];
  if (offer.agent) bits.push(offer.agent);
  else if (offer.harness) bits.push(offer.harness);
  if (offer.kind === "review") {
    const who = askedOf(offer, me);
    if (who) bits.push(`(${who})`);
    if (offer.claimed) bits.push("yours");
  }
  return bits.filter(Boolean).join(" · ");
}

// askedOf says whether a request came to you by name or was offered around,
// which decides whether ignoring it is rude or routine.
export function askedOf(offer: BusOffer, me: string): string {
  const to = (offer.to ?? "").trim();
  if (!to) return "";
  if (to === "anyone" || to === "*") return "team";
  if (me && to.toLowerCase() === me.toLowerCase()) return "direct";
  return to.includes("@") ? "direct" : to;
}

// reviews are the pull requests waiting on you; asks are everything else and
// stay at the top level, where a question is not behind a twisty.
export function reviews(offers: BusOffer[]): BusOffer[] {
  return offers.filter((o) => o.kind === "review");
}

export function asks(offers: BusOffer[]): BusOffer[] {
  return offers.filter((o) => o.kind !== "review");
}

export function busLabel(offer: BusOffer): string {
  if (offer.kind === "review") return `${offer.repo}#${offer.number}`;
  return offer.question ?? "a question";
}

export function busIcon(offer: BusOffer): string {
  if (offer.kind === "review") return offer.claimed ? "git-pull-request-go-to-changes" : "git-pull-request";
  return "comment-discussion";
}

// busTooltip is the whole of it. A row elides; hovering should not.
export function busTooltip(offer: BusOffer): string {
  const lines = [`**${offer.from}**`, ""];
  if (offer.kind === "review") {
    lines.push(offer.note || "asked for a review", "", offer.pr ?? "");
  } else {
    lines.push(offer.question ?? "");
    if (offer.options?.length) lines.push("", offer.options.map((o) => `- ${o}`).join("\n"));
  }
  if (offer.agent) lines.push("", `_${offer.agent}${offer.model ? ` · ${offer.model}` : ""}_`);
  return lines.join("\n");
}

// Questions first: somebody's agent is stopped until one is answered, while a
// review can wait. Then oldest first, which is who has waited longest.
export function rankOffer(a: BusOffer, b: BusOffer): number {
  if (a.kind !== b.kind) return a.kind === "ask" ? -1 : 1;
  return (a.createdAt ?? "").localeCompare(b.createdAt ?? "");
}

const PRESENCE: Record<string, { icon: string; word: string }> = {
  active: { icon: "circle-filled", word: "here" },
  idle: { icon: "circle-outline", word: "away" },
  dnd: { icon: "circle-slash", word: "answering their agent" },
};

export function memberIcon(member: BusMember): string {
  return PRESENCE[member.presence]?.icon ?? "circle-outline";
}

// memberLine is what a person's row says: how present they are, what their
// harness says it is doing, and how many agents are running under it.
export function memberLine(member: BusMember): string {
  const bits = [PRESENCE[member.presence]?.word ?? member.presence];
  bits.push(member.status || member.label || member.harness || "");
  if (member.agents) bits.push(`${member.agents} agent${member.agents > 1 ? "s" : ""}`);
  return bits.filter(Boolean).join(" · ");
}

// takes is what they will be offered, which is why somebody who is here can
// still be no use for a review.
export function takes(member: BusMember): string {
  const both = [member.takesAsk ? "questions" : "", member.takesPr ? "reviews" : ""].filter(Boolean);
  return both.length ? `takes ${both.join(" and ")}` : "sends only";
}

// Inbox is what the team has asked of this window. A tree, not a toast: a
// question has to stay put until somebody answers it.
export class Inbox implements vscode.TreeDataProvider<TeamNode> {
  private changed = new vscode.EventEmitter<void>();
  readonly onDidChangeTreeData = this.changed.event;
  private offers: BusOffer[] = [];
  private replies: BusReply[] = [];
  private sent: BusSent[] = [];
  private team: BusMember[] = [];
  private online = false;
  private me = "";
  private shut = new Set<string>();

  // What arrived since the last state: questions put to you, and answers to
  // what you asked. Both are news exactly once.
  get all(): BusOffer[] {
    return this.offers;
  }

  apply(bus: BusState | null | undefined): Fresh {
    const asked = new Set(this.offers.map((offer) => offer.id));
    const now = [...(bus?.offers ?? [])].sort(rankOffer);
    const answered = new Set(this.replies.map((reply) => reply.id));
    const back = [...(bus?.replies ?? [])];

    const fresh = {
      offers: now.filter((offer) => !asked.has(offer.id)),
      replies: back.filter((reply) => !answered.has(reply.id)),
      back: returned(this.team, bus?.team ?? []),
    };
    this.offers = now;
    this.replies = back;
    this.sent = [...(bus?.sent ?? [])];
    this.team = [...(bus?.team ?? [])];
    this.online = Boolean(bus?.online);
    this.me = bus?.user ?? "";
    this.changed.fire();
    return fresh;
  }

  get count(): number {
    return this.offers.length;
  }

  get joined(): boolean {
    return this.online;
  }

  // user is who this ade is on the bus, so a reviewer list can leave you off.
  get user(): string {
    return this.me;
  }

  find(id: string): BusOffer | undefined {
    return this.offers.find((offer) => offer.id === id);
  }

  // Only the people are a group: the things waiting on you are the point of
  // the view and should never be behind a twisty.
  getChildren(node?: TeamNode): TeamNode[] {
    if (!node) {
      const rows: TeamNode[] = asks(this.offers).map((offer) => ({ kind: "offer", offer }));
      // Pull requests are their own group: a stack of them used to push the
      // questions waiting on you off the top of the view.
      const waiting = reviews(this.offers);
      if (waiting.length > 0) {
        rows.push({
          kind: "group",
          title: REVIEWS,
          count: waiting.length,
          open: !this.shut.has(REVIEWS),
        });
      }
      // Answers outlive the popup that announced them, so a missed toast is
      // not a lost answer. They go when you dismiss one.
      if (this.replies.length > 0) {
        rows.push({
          kind: "group",
          title: ANSWERS,
          count: this.replies.length,
          open: !this.shut.has(ANSWERS),
        });
      }
      // What you are waiting on somebody else for. Not actionable here, but
      // the thing you came to the view to check.
      if (this.sent.length > 0) {
        rows.push({
          kind: "group",
          title: WAITING,
          count: this.sent.length,
          open: !this.shut.has(WAITING),
        });
      }
      if (this.online) {
        rows.push({
          kind: "group",
          title: ONLINE,
          count: this.team.length,
          open: !this.shut.has(ONLINE),
        });
      }
      return rows;
    }
    if (node.kind !== "group") return [];
    if (node.title === ANSWERS) {
      return this.replies.map((reply) => ({ kind: "reply", reply }));
    }
    if (node.title === WAITING) {
      return this.sent.map((sent) => ({ kind: "sent", sent }));
    }
    if (node.title === REVIEWS) {
      return reviews(this.offers).map((offer) => ({ kind: "offer", offer }));
    }
    return this.team.map((member) => ({ kind: "member", member }));
  }

  // VS Code remembers a twisty across a redraw only if we do.
  held(title: string, open: boolean): void {
    if (open) this.shut.delete(title);
    else this.shut.add(title);
  }

  getTreeItem(node: TeamNode): vscode.TreeItem {
    if (node.kind === "group") return groupItem(node.title, node.count, node.open);
    if (node.kind === "member") return memberItem(node.member);
    if (node.kind === "reply") return replyItem(node.reply);
    if (node.kind === "sent") return sentItem(node.sent);
    return offerItem(node.offer, this.me);
  }
}

// An answer you have not cleared. Clicking it is how it goes, which is also
// what tells the launcher to stop holding it.
export function replyItem(reply: BusReply): vscode.TreeItem {
  const item = new vscode.TreeItem(replyLine(reply), vscode.TreeItemCollapsibleState.None);
  item.description = reply.question || reply.pr || "";
  item.iconPath = new vscode.ThemeIcon(
    reply.kind === "review" ? "git-pull-request" : "comment",
  );
  item.tooltip = new vscode.MarkdownString(
    [`**${reply.from}**`, reply.question ?? reply.pr ?? "", replyLine(reply)]
      .filter(Boolean)
      .join("\n\n"),
  );
  item.contextValue = "ade.bus.reply";
  item.command = {
    command: "ade.dismissReply",
    title: "Clear it",
    arguments: [reply.id],
  };
  return item;
}

// Something you asked that has no answer yet. Nothing to click: the only move
// is to wait, or to chase it somewhere that is not this tree.
export function sentItem(sent: BusSent): vscode.TreeItem {
  const item = new vscode.TreeItem(
    sent.question || sent.pr || "no words",
    vscode.TreeItemCollapsibleState.None,
  );
  item.description = sent.to || "anyone";
  item.iconPath = new vscode.ThemeIcon(sent.kind === "review" ? "git-pull-request" : "clock");
  item.tooltip = new vscode.MarkdownString(
    [
      `**${sent.forAgent ? "a run here" : "you"} asked ${sent.to || "anyone"}**`,
      sent.question ?? sent.pr ?? "",
      "no answer yet",
    ]
      .filter(Boolean)
      .join("\n\n"),
  );
  item.contextValue = "ade.bus.sent";
  return item;
}

export function groupItem(title: string, count: number, open: boolean): vscode.TreeItem {
  const item = new vscode.TreeItem(
    title,
    open
      ? vscode.TreeItemCollapsibleState.Expanded
      : vscode.TreeItemCollapsibleState.Collapsed,
  );
  item.description = count > 0 ? String(count) : "nobody";
  item.iconPath = new vscode.ThemeIcon("organization");
  item.contextValue = "ade.bus.group";
  return item;
}

export function memberItem(member: BusMember): vscode.TreeItem {
  const item = new vscode.TreeItem(
    member.you ? `${member.user} (you)` : member.user,
    vscode.TreeItemCollapsibleState.None,
  );
  item.description = memberLine(member);
  item.iconPath = new vscode.ThemeIcon(
    memberIcon(member),
    member.presence === "active"
      ? new vscode.ThemeColor("charts.green")
      : undefined,
  );
  item.tooltip = new vscode.MarkdownString(
    [`**${member.user}**`, "", member.instance, member.node ?? "", takes(member)]
      .filter(Boolean)
      .join("\n\n"),
  );
  item.contextValue = memberContext(member);
  return item;
}

// What a member row offers. Your own row offers nothing: the bus refuses your
// own instances, and asking yourself is not a feature.
export function memberContext(member: BusMember): string {
  if (member.you) return "ade.bus.you";
  if (member.takesAsk && member.takesPr) return "ade.bus.member.both";
  if (member.takesAsk) return "ade.bus.member.ask";
  if (member.takesPr) return "ade.bus.member.reviews";
  return "ade.bus.member";
}

// userOf is who a row stands for, for a command handed the node.
export function userOf(node: unknown): string {
  const row = node as TeamNode | undefined;
  return row?.kind === "member" ? row.member.user : "";
}

function offerItem(offer: BusOffer, me = ""): vscode.TreeItem {
  {
    const item = new vscode.TreeItem(busLabel(offer), vscode.TreeItemCollapsibleState.None);
    item.description = busLine(offer, me);
    item.iconPath = new vscode.ThemeIcon(busIcon(offer));
    item.tooltip = new vscode.MarkdownString(busTooltip(offer));
    item.contextValue = offer.kind === "ask" ? "ade.bus.ask" : "ade.bus.review";
    item.command = {
      command: offer.kind === "ask" ? "ade.answerAsk" : "ade.takeReview",
      title: offer.kind === "ask" ? "Answer" : "Take the review",
      arguments: [offer],
    };
    return item;
  }
}
