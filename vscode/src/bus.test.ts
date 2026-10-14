import { describe, expect, it } from "vitest";
import type { BusMember, BusOffer, BusReply } from "./client";
import {
  ANSWERS,
  busIcon,
  busLabel,
  busLine,
  busTooltip,
  Inbox,
  memberIcon,
  memberContext,
  memberItem,
  memberLine,
  PRESENCES,
  ONLINE,
  WAITING,
  rankOffer,
  replyItem,
  replyLine,
  sentItem,
  userOf,
  shortEnough,
  takes,
  askedOf,
  asks,
  reviews,
  REVIEWS,
  returned,
} from "./bus";

function ask(over: Partial<BusOffer> = {}): BusOffer {
  return {
    id: "msg_1",
    kind: "ask",
    from: "liam@infracost.io",
    question: "Is dev RDS safe to resize now?",
    options: ["yes", "no"],
    agent: "claude-code",
    createdAt: "2026-10-02T09:00:00Z",
    ...over,
  };
}

function review(over: Partial<BusOffer> = {}): BusOffer {
  return {
    id: "msg_2",
    kind: "review",
    from: "liam@infracost.io",
    repo: "infracost/infra",
    number: 123,
    pr: "https://github.com/infracost/infra/pull/123",
    createdAt: "2026-10-02T09:01:00Z",
    ...over,
  };
}

describe("which way to put a question", () => {
  it("takes a quick pick for a short one with options", () => {
    expect(shortEnough(ask())).toBe(true);
  });

  it("wants a panel when there are no options to pick from", () => {
    expect(shortEnough(ask({ options: [] }))).toBe(false);
  });

  it("wants a panel when the question will not fit a line", () => {
    expect(shortEnough(ask({ question: "why ".repeat(60) }))).toBe(false);
  });

  it("wants a panel when an option is a paragraph", () => {
    expect(shortEnough(ask({ options: ["yes", "x".repeat(80)] }))).toBe(false);
  });
});

describe("a row", () => {
  it("names the question and who is waiting on it", () => {
    expect(busLabel(ask())).toBe("Is dev RDS safe to resize now?");
    expect(busLine(ask())).toBe("liam@infracost.io · claude-code");
    expect(busIcon(ask())).toBe("comment-discussion");
  });

  it("names a review by its pull request", () => {
    expect(busLabel(review())).toBe("infracost/infra#123");
    expect(busIcon(review())).toBe("git-pull-request");
  });

  it("says when a review is already yours", () => {
    expect(busLine(review({ claimed: true }))).toContain("yours");
  });

  it("puts the whole question in the tooltip, which does not elide", () => {
    const tip = busTooltip(ask({ question: "a long one", options: ["yes", "no"] }));
    expect(tip).toContain("a long one");
    expect(tip).toContain("- yes");
    expect(tip).toContain("claude-code");
  });
});

describe("the inbox", () => {
  it("puts questions before reviews, oldest first", () => {
    const offers = [review(), ask({ id: "a2", createdAt: "2026-10-02T09:02:00Z" }), ask({ id: "a1" })];
    expect([...offers].sort(rankOffer).map((o) => o.id)).toEqual(["a1", "a2", "msg_2"]);
  });

  it("reports only what is new, so a poll does not nag about the same question", () => {
    const inbox = new Inbox();
    expect(inbox.apply({ online: true, offers: [ask()] }).offers.map((o) => o.id)).toEqual(["msg_1"]);
    expect(
      inbox.apply({ online: true, offers: [ask(), review()] }).offers.map((o) => o.id),
    ).toEqual(["msg_2"]);
    expect(inbox.count).toBe(2);
    expect(inbox.joined).toBe(true);
  });

  // The answer to your own question is the other half, and the same rule: a
  // poll that sees it again must not announce it again.
  it("reports an answer once, however often the state is read", () => {
    const inbox = new Inbox();
    const answer = { id: "msg_9", kind: "answer", from: "liam@infracost.io", option: "yes" };
    expect(inbox.apply({ online: true, replies: [answer] }).replies).toHaveLength(1);
    expect(inbox.apply({ online: true, replies: [answer] }).replies).toHaveLength(0);
  });

  it("empties when the offer is taken by somebody else", () => {
    const inbox = new Inbox();
    inbox.apply({ online: true, offers: [ask()] });
    inbox.apply({ online: true, offers: [] });
    expect(inbox.count).toBe(0);
    expect(inbox.find("msg_1")).toBeUndefined();
  });

  it("is empty and quiet when there is no bus at all", () => {
    const inbox = new Inbox();
    expect(inbox.apply(undefined)).toEqual({ offers: [], replies: [], back: [] });
    expect(inbox.joined).toBe(false);
  });

  it("gives a row the command that answers it", () => {
    const inbox = new Inbox();
    inbox.apply({ online: true, offers: [ask(), review()] });
    const top = inbox.getChildren();
    const first = inbox.getTreeItem(top[0]);
    expect(first.command?.command).toBe("ade.answerAsk");
    expect(first.contextValue).toBe("ade.bus.ask");

    // The review is under its own group now, not beside the question.
    const group = top.find((n) => n.kind === "group" && n.title === REVIEWS);
    expect(group).toBeDefined();
    const [row] = inbox.getChildren(group).map((n) => inbox.getTreeItem(n));
    expect(row.command?.command).toBe("ade.takeReview");
  });
});

function member(over: Partial<BusMember> = {}): BusMember {
  return {
    user: "liam@infracost.io",
    node: "liams-mbp",
    instance: "liam@infracost.io/crit-1",
    harness: "crit",
    presence: "active",
    takesAsk: true,
    takesPr: true,
    ...over,
  };
}

describe("a person's row", () => {
  it("says how present they are in words, not jargon", () => {
    expect(memberLine(member())).toContain("here");
    expect(memberLine(member({ presence: "idle" }))).toContain("away");
    expect(memberLine(member({ presence: "dnd" }))).toContain("answering their agent");
  });

  it("prefers what their harness says it is doing", () => {
    expect(memberLine(member({ status: "deep in FIX-702", label: "infra on main" })))
      .toBe("here · deep in FIX-702");
    expect(memberLine(member({ label: "infra on main" }))).toBe("here · infra on main");
  });

  it("counts the agents running under them", () => {
    expect(memberLine(member({ agents: 1 }))).toContain("1 agent");
    expect(memberLine(member({ agents: 3 }))).toContain("3 agents");
  });

  it("says what they will actually be offered", () => {
    expect(takes(member())).toBe("takes questions and reviews");
    expect(takes(member({ takesPr: false }))).toBe("takes questions");
    expect(takes(member({ takesAsk: false, takesPr: false }))).toBe("sends only");
  });

  // Your own row carries its own value so no verb matches it. The bus refuses
  // your own instances, and asking yourself is not a feature.
  it("marks you as you, and offers you nothing", () => {
    const item = memberItem(member({ you: true }));
    expect(item.label).toContain("(you)");
    expect(item.contextValue).toBe("ade.bus.you");
  });

  // A row says what it takes, so a verb is offered only where it would work.
  it("says what a person takes, so only the verbs that work are offered", () => {
    const takes = (over: Partial<BusMember>) => memberContext(member(over));
    expect(takes({ takesAsk: true, takesPr: true })).toBe("ade.bus.member.both");
    expect(takes({ takesAsk: true, takesPr: false })).toBe("ade.bus.member.ask");
    expect(takes({ takesAsk: false, takesPr: true })).toBe(
      "ade.bus.member.reviews",
    );
    expect(takes({ takesAsk: false, takesPr: false })).toBe("ade.bus.member");
  });

  it("fills a green dot only for somebody who is here", () => {
    expect(memberIcon(member())).toBe("circle-filled");
    expect(memberIcon(member({ presence: "idle" }))).toBe("circle-outline");
    expect(memberIcon(member({ presence: "unheard-of" }))).toBe("circle-outline");
  });
});

describe("the two levels", () => {
  it("keeps what is waiting on you at the top, with the people in a group", () => {
    const inbox = new Inbox();
    inbox.apply({ online: true, offers: [ask()], team: [member()] });

    const rows = inbox.getChildren();
    expect(rows.map((r) => r.kind)).toEqual(["offer", "group"]);
    expect(inbox.getChildren(rows[1]).map((r) => r.kind)).toEqual(["member"]);
  });

  it("shows the group with nothing waiting, and none of it when offline", () => {
    const inbox = new Inbox();
    inbox.apply({ online: true, offers: [], team: [member()] });
    expect(inbox.getChildren().map((r) => r.kind)).toEqual(["group"]);

    inbox.apply({ online: false, offers: [], team: [] });
    expect(inbox.getChildren()).toEqual([]);
  });

  it("remembers a group somebody shut", () => {
    const inbox = new Inbox();
    inbox.apply({ online: true, offers: [], team: [member()] });
    expect(inbox.getChildren()[0]).toMatchObject({ open: true });

    inbox.held("Online", false);
    inbox.apply({ online: true, offers: [], team: [member()] });
    expect(inbox.getChildren()[0]).toMatchObject({ open: false });
  });

  it("badges the work, not the people", () => {
    const inbox = new Inbox();
    inbox.apply({ online: true, offers: [ask()], team: [member(), member({ user: "v@x" })] });
    expect(inbox.count).toBe(1);
  });
});

describe("who a row stands for", () => {
  it("is the person on a member row", () => {
    expect(userOf({ kind: "member", member: member() })).toBe(
      "liam@infracost.io",
    );
  });

  // A command reached from the palette has no row, and a group header is not
  // a person. Both have to come back empty rather than guess.
  it("is nobody for anything else", () => {
    expect(userOf({ kind: "group", title: "Online", count: 2, open: true })).toBe("");
    expect(userOf(undefined)).toBe("");
  });
});

describe("what an answer says in a notification", () => {
  const answer = (over: Partial<BusReply> = {}): BusReply => ({
    id: "msg_9",
    kind: "answer",
    from: "liam@infracost.io",
    ...over,
  });

  it("names who answered and what they said", () => {
    expect(replyLine(answer({ option: "yes", text: "loud and clear" }))).toBe(
      "liam@infracost.io answered: yes loud and clear",
    );
  });

  // An agent answering for an away human is weaker evidence, and the line has
  // to say so or it reads as the person.
  it("says when their agent stood in for them", () => {
    expect(replyLine(answer({ text: "probably", byAgent: true }))).toContain(
      "(their agent)",
    );
  });

  it("gives a review its verdict rather than its prose", () => {
    expect(replyLine(answer({ kind: "review", verdict: "approved" }))).toBe(
      "liam@infracost.io reviewed it: approved",
    );
  });
});

describe("setting how the team sees you", () => {
  it("offers active, away and the way back to automatic", () => {
    expect(PRESENCES.map((p) => p.to)).toEqual(["active", "idle", ""]);
  });

  // Empty is not a presence on the wire: it clears the override so the window
  // decides again. Without it an override would be permanent.
  it("ends with automatic, so the override is escapable", () => {
    expect(PRESENCES.at(-1)?.to).toBe("");
  });
});

describe("answers you have not cleared", () => {
  const answered = (id: string): BusReply => ({
    id,
    kind: "answer",
    from: "liam@infracost.io",
    question: "can you hear me?",
    option: "yes",
  });

  // A notification fires once. Without a row, missing it loses the answer
  // while the launcher goes on holding it.
  it("keeps a row for each, so a missed popup is not a lost answer", () => {
    const inbox = new Inbox();
    inbox.apply({ online: true, replies: [answered("msg_1"), answered("msg_2")] });

    const groups = inbox.getChildren().filter((n) => n.kind === "group");
    const answers = groups.find((n) => n.kind === "group" && n.title === ANSWERS);
    expect(answers).toBeTruthy();
    expect(answers?.kind === "group" && answers.count).toBe(2);
  });

  it("shows no group at all when there is nothing waiting to be read", () => {
    const inbox = new Inbox();
    inbox.apply({ online: true, replies: [] });
    const titles = inbox.getChildren().map((n) => (n.kind === "group" ? n.title : ""));
    expect(titles).not.toContain(ANSWERS);
    expect(titles).toContain(ONLINE);
  });

  it("puts answers above the people, because they are what needs reading", () => {
    const inbox = new Inbox();
    inbox.apply({ online: true, replies: [answered("msg_1")] });
    const titles = inbox
      .getChildren()
      .filter((n) => n.kind === "group")
      .map((n) => (n.kind === "group" ? n.title : ""));
    expect(titles).toEqual([ANSWERS, ONLINE]);
  });

  // Clicking it is how it goes, which is also what stops the launcher holding
  // it. A row with no command would strand the answer.
  it("clears itself when clicked", () => {
    const item = replyItem(answered("msg_1"));
    expect(item.command?.command).toBe("ade.dismissReply");
    expect(item.command?.arguments).toEqual(["msg_1"]);
    expect(item.contextValue).toBe("ade.bus.reply");
  });
});

describe("what is still out", () => {
  it("groups sent requests under their own heading", () => {
    const inbox = new Inbox();
    inbox.apply({
      online: true,
      sent: [{ id: "msg_1", kind: "ask", to: "liam@infracost.io", question: "safe to resize?" }],
    });

    const groups = inbox.getChildren().filter((n) => n.kind === "group");
    const waiting = groups.find((n) => n.kind === "group" && n.title === WAITING);
    expect(waiting).toBeDefined();

    const rows = inbox.getChildren(waiting);
    expect(rows).toHaveLength(1);
    expect(rows[0].kind).toBe("sent");
  });

  it("leaves the heading out when nothing is waiting", () => {
    const inbox = new Inbox();
    inbox.apply({ online: true, sent: [] });
    const titles = inbox.getChildren().map((n) => (n.kind === "group" ? n.title : ""));
    expect(titles).not.toContain(WAITING);
  });

  it("names who it went to and says there is no answer", () => {
    const item = sentItem({ id: "msg_1", kind: "ask", to: "liam@infracost.io", question: "safe?" });
    expect(item.label).toBe("safe?");
    expect(item.description).toBe("liam@infracost.io");
    expect(String((item.tooltip as { value: string }).value)).toContain("no answer yet");
  });
});

describe("who a review was asked of", () => {
  const me = "owen@infracost.io";
  const at = (to?: string) => review({ to });

  it("is direct when it names you", () => {
    expect(askedOf(at(me), me)).toBe("direct");
    expect(askedOf(at("OWEN@Infracost.io"), me)).toBe("direct");
  });

  it("is the team when it was offered around", () => {
    expect(askedOf(at("anyone"), me)).toBe("team");
    expect(askedOf(at("*"), me)).toBe("team");
  });

  it("names the group when it was asked of one", () => {
    expect(askedOf(at("engineering"), me)).toBe("engineering");
  });

  it("says nothing when the sender addressed nobody", () => {
    expect(askedOf(at(undefined), me)).toBe("");
  });

  it("reads as direct when it names someone and you do not know yourself", () => {
    expect(askedOf(at("kim@infracost.io"), "")).toBe("direct");
  });

  it("goes in brackets on the row", () => {
    expect(busLine(at("anyone"), me)).toContain("(team)");
    expect(busLine(at(me), me)).toContain("(direct)");
  });
});

describe("pull requests get their own group", () => {
  it("keeps questions at the top level and reviews apart", () => {
    const rows = [ask(), review({}), ask(), review({})];
    expect(asks(rows)).toHaveLength(2);
    expect(reviews(rows)).toHaveLength(2);
  });
});

describe("telling you somebody is back", () => {
  const at = (user: string, presence: string): BusMember =>
    member({ user, presence, instance: user });

  it("fires on the edge from away to active", () => {
    expect(returned([at("kim", "idle")], [at("kim", "active")])).toEqual(["kim"]);
  });

  it("fires for somebody who was not in the roster at all", () => {
    expect(returned([], [at("kim", "active")])).toEqual(["kim"]);
  });

  it("stays quiet while they are already active", () => {
    expect(returned([at("kim", "active")], [at("kim", "active")])).toEqual([]);
  });

  it("stays quiet when they go away", () => {
    expect(returned([at("kim", "active")], [at("kim", "idle")])).toEqual([]);
  });

  it("never fires for you", () => {
    expect(returned([], [{ ...at("me", "active"), you: true }])).toEqual([]);
  });
});
