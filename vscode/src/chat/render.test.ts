import { describe, expect, it } from "vitest";
import {
  acts,
  attachmentPath,
  escape,
  foldNote,
  foldable,
  folds,
  group,
  line,
  opens,
  selected,
  shotsOf,
  shown,
  isQuestion,
  age,
  questionsOf,
  queuedLabel,
  redraws,
  sideLabel,
  step,
  summary,
  type Message,
  who,
  working,
  turnsAfter,
  undoSummary,
} from "./render";

function msg(over: Partial<Message> = {}): Message {
  return { id: "1", kind: "agent", at: "2026-09-30T17:00:00Z", ...over };
}

describe("a tool call folded to one line", () => {
  it("says what it did and to what", () => {
    expect(line(msg({ kind: "tool", tool: "Read", target: "web.go" }))).toBe(
      "Read web.go",
    );
  });

  it("still says something with no target", () => {
    // The agent doing nothing looks the same as a blank line.
    expect(line(msg({ kind: "tool", tool: "Bash" }))).toBe("Bash");
    expect(line(msg({ kind: "tool" }))).toBe("working");
  });
});

describe("who spoke", () => {
  it("names a side agent, because its verdict is not the conversation's", () => {
    expect(who(msg({ author: "security" }))).toBe("security");
  });

  it("is you or the agent otherwise", () => {
    expect(who(msg({ kind: "you" }))).toBe("you");
    expect(who(msg())).toBe("agent");
  });
});

describe("a prompt", () => {
  it("is never html, whatever it happens to contain", () => {
    expect(escape('<img src=x onerror="alert(1)">')).not.toContain("<img");
  });
});

describe("the thinking line", () => {
  it("shows while a reply is owed", () => {
    expect(working([msg({ kind: "you" })], true)).toBe(true);
    expect(working([], true)).toBe(true);
  });

  it("does not show once the agent has answered", () => {
    expect(working([msg({ kind: "agent" })], true)).toBe(false);
  });

  it("names the tool it is on and how long it has been there", () => {
    const at = Date.parse("2026-09-30T17:00:00Z");
    const tool = msg({ kind: "tool", tool: "Bash", target: "go test ./..." });
    expect(step([tool], at + 72_000)).toBe("Bash go test ./... · 1m12s");
    expect(step([msg({ kind: "you" })], at + 5000)).toBe("thinking… · 5s");
    expect(step([], at)).toBe("thinking…");
  });

  it("does not show when nothing is running", () => {
    expect(working([msg({ kind: "you" })], false)).toBe(false);
  });
});

describe("group", () => {
  const tool = (id: string, over: Partial<Message> = {}): Message => ({
    id,
    kind: "tool",
    tool: "grep",
    at: "2026-09-30T18:00:00Z",
    ...over,
  });
  const said = (id: string): Message => ({
    id,
    kind: "agent",
    body: "hi",
    at: "x",
  });

  it("folds a run of tool calls into one block", () => {
    const got = group([said("a"), tool("1"), tool("2"), tool("3"), said("b")]);
    expect(got.map((b) => b.kind)).toEqual(["msg", "tools", "msg"]);
    expect(got[1].kind === "tools" && got[1].msgs).toHaveLength(3);
  });

  it("lifts a held call out of the run it follows", () => {
    const held = tool("3", { toolId: "toolu_3" });
    const got = group([tool("1"), tool("2"), held], new Set(["toolu_3"]));
    expect(got.map((b) => b.kind)).toEqual(["tools", "msg"]);
    expect(got[1].kind === "msg" && got[1].msg.id).toBe("3");
  });

  it("keeps two runs separated by something said apart", () => {
    const got = group([tool("1"), said("a"), tool("2")]);
    expect(got.map((b) => b.kind)).toEqual(["tools", "msg", "tools"]);
  });

  it("is empty for an empty conversation", () => {
    expect(group([])).toEqual([]);
  });

  it("leaves a conversation with no tools alone", () => {
    expect(group([said("a"), said("b")]).map((b) => b.kind)).toEqual([
      "msg",
      "msg",
    ]);
  });
});

describe("summary", () => {
  const tool = (t: string, over: Partial<Message> = {}): Message => ({
    id: t,
    kind: "tool",
    tool: t,
    at: "x",
    ...over,
  });

  it("is the call itself when there is only one", () => {
    expect(summary([tool("grep", { target: "web.go" })])).toBe("grep web.go");
  });

  // What it is on now says more than what it started with.
  it("counts, and names the last thing it did", () => {
    expect(summary([tool("grep"), tool("read"), tool("edit")])).toBe(
      "3 steps · edit",
    );
  });

  it("says how many failed, since a count alone hides that", () => {
    expect(summary([tool("grep"), tool("edit", { failed: true })])).toContain(
      "1 failed",
    );
  });
});

describe("opens", () => {
  const tool = (over: Partial<Message> = {}): Message => ({
    id: "t",
    kind: "tool",
    at: "x",
    ...over,
  });

  it("stays shut for a run that worked", () => {
    expect(opens([tool(), tool()])).toBe(false);
  });

  it("opens itself when something failed, which is the line to read", () => {
    expect(opens([tool(), tool({ failed: true })])).toBe(true);
  });
});

describe("what a message carries besides its words", () => {
  const src = (path: string) =>
    `http://127.0.0.1:1/x${attachmentPath(path)}?t=k`;

  // The outbox puts a turn's pictures on the reply as bytes. The editor drew
  // neither carrier, so a screenshot the Electron app showed was missing here.
  it("draws an outbox picture inline", () => {
    const got = shown(
      {
        id: "1",
        kind: "agent",
        at: "",
        images: [{ mediaType: "image/png", data: "AAA" }],
      },
      src,
    );
    expect(got).toEqual([
      { src: "data:image/png;base64,AAA", name: "image 1", image: true },
    ]);
  });

  it("serves a dropped file by its base name", () => {
    const got = shown(
      {
        id: "1",
        kind: "you",
        at: "",
        files: [
          {
            name: "shot.png",
            path: "/w/.git/ade/attachments/0001-shot.png",
            image: true,
          },
        ],
      },
      src,
    );
    expect(got[0].src).toBe(
      "http://127.0.0.1:1/x/attachments/0001-shot.png?t=k",
    );
    expect(got[0].path).toBe("/w/.git/ade/attachments/0001-shot.png");
  });

  it("marks a file that is not a picture", () => {
    const got = shown(
      {
        id: "1",
        kind: "you",
        at: "",
        files: [{ name: "run.log", path: "/w/run.log" }],
      },
      src,
    );
    expect(got[0].image).toBe(false);
  });

  it("carries nothing when the message has nothing", () => {
    expect(shown({ id: "1", kind: "agent", at: "" }, src)).toEqual([]);
  });

  it("escapes a name that would otherwise break the url", () => {
    expect(attachmentPath("/w/a b&c.png")).toBe("/attachments/a%20b%26c.png");
  });
});

describe("the pictures the lightbox walks", () => {
  const src = (path: string) => `/x${attachmentPath(path)}`;
  const png = (data: string) => ({ mediaType: "image/png", data });

  // The app's gallery walks the whole thread, so this one has to as well: the
  // arrows are useless if they only see the message that was clicked.
  it("is every picture in the conversation, in the order it was said", () => {
    const got = shotsOf(
      [
        {
          id: "1",
          kind: "you",
          at: "",
          files: [{ name: "a.png", path: "/w/a.png", image: true }],
        },
        { id: "2", kind: "agent", at: "", body: "no picture here" },
        { id: "3", kind: "agent", at: "", images: [png("B"), png("C")] },
      ],
      src,
    );
    expect(got.map((s) => s.name)).toEqual(["a.png", "image 1", "image 2"]);
  });

  it("leaves out an attachment that is not a picture", () => {
    const got = shotsOf(
      [
        {
          id: "1",
          kind: "you",
          at: "",
          files: [{ name: "run.log", path: "/w/run.log" }],
        },
      ],
      src,
    );
    expect(got).toEqual([]);
  });

  it("is empty for a conversation with nothing in it", () => {
    expect(shotsOf([], src)).toEqual([]);
  });
});

describe("folding a reply into the design document", () => {
  // @architect's verdict is worth keeping; the main agent's is already in the
  // document, since it is what wrote it.
  it("is offered on a named agent's reply", () => {
    expect(foldable(msg({ author: "architect" }), "fix-739.md")).toBe(true);
  });

  it("is not offered on the main agent's own words", () => {
    expect(foldable(msg(), "fix-739.md")).toBe(false);
  });

  it("is not offered on your own message", () => {
    expect(foldable(msg({ kind: "you", author: "architect" }), "x.md")).toBe(
      false,
    );
  });

  it("is not offered with no document open", () => {
    expect(foldable(msg({ author: "architect" }), "")).toBe(false);
    expect(foldable(msg({ author: "architect" }), undefined)).toBe(false);
  });

  // A reviewer of the diff files comments; there is no verdict to fold.
  it("is not offered on a review of the diff", () => {
    expect(
      foldable(msg({ author: "security", review: true }), "fix-739.md"),
    ).toBe(false);
  });
});

describe("a question the agent asked", () => {
  const asked = msg({
    id: "q",
    kind: "tool",
    tool: "AskUserQuestion",
    toolId: "toolu_1",
    input: JSON.stringify({
      questions: [
        {
          question: "Tea or coffee?",
          options: [{ label: "Tea" }, { label: "Coffee" }],
        },
      ],
    }),
  });

  it("is never folded into the run of steps around it", () => {
    // "3 steps · AskUserQuestion" is no way to show what the turn stopped on.
    const blocks = group([
      msg({ id: "a", kind: "tool", tool: "Read" }),
      asked,
      msg({ id: "b", kind: "tool", tool: "Bash" }),
    ]);
    expect(blocks.map((b) => b.kind)).toEqual(["tools", "msg", "tools"]);
    expect(isQuestion(asked)).toBe(true);
    expect(isQuestion(msg({ kind: "tool", tool: "Read" }))).toBe(false);
  });

  it("reads its questions out of the tool's input", () => {
    expect(questionsOf(asked.input)[0].options).toHaveLength(2);
    expect(questionsOf("not json")).toEqual([]);
    expect(questionsOf(undefined)).toEqual([]);
  });

  it("stops the spinner, because the turn owes nothing until you answer", () => {
    expect(working([msg({ kind: "you" })], true)).toBe(true);
    expect(working([msg({ kind: "you" })], true, 1)).toBe(false);
  });
});

describe("the verbs on a bubble", () => {
  const you = { id: "1", kind: "you", body: "do the thing", at: "" };
  const said = { id: "2", kind: "agent", body: "done", at: "" };

  it("offers replay on your own words and nothing else", () => {
    expect(acts(you, "cart.md")).toEqual(["replay"]);
  });

  it("offers copy and hide on a reply, and fold with a document", () => {
    expect(acts(said, undefined)).toEqual(["copy", "hide"]);
    expect(
      acts({ ...said, author: "architect", run: "subagent" }, "cart.md"),
    ).toEqual(["copy", "hide", "fold"]);
  });

  it("offers undo and a fixture on your message once it has a snapshot", () => {
    expect(acts({ ...you, undo: "ab12" }, undefined)).toEqual([
      "replay",
      "undo",
      "fixture",
    ]);
  });

  it("counts the later turns an undo takes with it", () => {
    const all = [you, said, { ...you, id: "3" }, { ...you, id: "4" }];
    expect(turnsAfter(all, "1")).toBe(2);
    expect(turnsAfter(all, "4")).toBe(0);
    expect(turnsAfter(all, "nope")).toBe(0);
  });

  it("says what an undo will do before it does it", () => {
    const changes = [
      { path: "a.go", do: "restore" },
      { path: "b.go", do: "restore" },
      { path: "new.go", do: "delete" },
    ];
    expect(undoSummary(changes, 0)).toBe(
      "This will put 2 files back, delete 1 file.",
    );
    expect(undoSummary([{ path: "gone.go", do: "recreate" }], 2)).toBe(
      "This will recreate 1 file. This also undoes 2 later turns.",
    );
  });

  it("offers nothing on an empty bubble, a tool call or a note", () => {
    expect(acts({ ...you, body: "  " }, undefined)).toEqual([]);
    expect(acts({ ...said, body: "" }, undefined)).toEqual([]);
    expect(acts({ id: "3", kind: "tool", tool: "Bash", at: "" }, "x")).toEqual(
      [],
    );
    expect(
      acts({ id: "4", kind: "note", body: "cancelled", at: "" }, "x"),
    ).toEqual([]);
  });
});

// Selecting a word while a redraw landed left the range spanning whatever
// replaced the nodes it was anchored in, so a copy took a random amount.
describe("whether a redraw would break a selection", () => {
  const inside = { nodeType: 1 };
  const text = { nodeType: 3, parentNode: inside };
  const outside = { nodeType: 1 };
  const list = { contains: (n: unknown) => n === inside } as unknown as Node;

  function pick(at: unknown, collapsed = false): Selection {
    return {
      isCollapsed: collapsed,
      rangeCount: 1,
      getRangeAt: () => ({ commonAncestorContainer: at }),
    } as unknown as Selection;
  }

  it("is true for text selected in the list", () => {
    expect(selected(list, pick(text))).toBe(true);
    expect(selected(list, pick(inside))).toBe(true);
  });

  it("is false for a caret, which is not a selection", () => {
    expect(selected(list, pick(text, true))).toBe(false);
  });

  it("is false with nothing selected at all", () => {
    expect(selected(list, null)).toBe(false);
    expect(
      selected(list, { isCollapsed: false, rangeCount: 0 } as Selection),
    ).toBe(false);
  });

  it("is false for a selection in the composer, which no redraw touches", () => {
    expect(selected(list, pick(outside))).toBe(false);
  });
});

describe("prompts welded into one turn", () => {
  const you = (id: string, over: Partial<Message> = {}): Message => ({
    id,
    kind: "you",
    body: id,
    at: "x",
    ...over,
  });

  it("sit under the prompt they were sent with", () => {
    const got = folds([you("a"), you("b", { foldedInto: "a" }), you("c", { foldedInto: "a" }), you("d")]);
    expect(got.top.map((m) => m.id)).toEqual(["a", "d"]);
    expect(got.under.get("a")?.map((m) => m.id)).toEqual(["b", "c"]);
  });

  it("stay in the line when the prompt they name is gone", () => {
    const got = folds([you("b", { foldedInto: "a" })]);
    expect(got.top.map((m) => m.id)).toEqual(["b"]);
    expect(got.under.size).toBe(0);
  });

  it("sit under the top of a chain, so none is drawn nowhere", () => {
    const got = folds([you("a"), you("b", { foldedInto: "a" }), you("c", { foldedInto: "b" })]);
    expect(got.top.map((m) => m.id)).toEqual(["a"]);
    expect(got.under.get("a")?.map((m) => m.id)).toEqual(["b", "c"]);
  });

  it("stay in the line when the marks loop", () => {
    const got = folds([you("a", { foldedInto: "b" }), you("b", { foldedInto: "a" }), you("c", { foldedInto: "c" })]);
    expect(got.top.map((m) => m.id)).toEqual(["a", "b", "c"]);
    expect(got.under.size).toBe(0);
  });

  it("carry no verbs: the turn, and its snapshot, are the first prompt's", () => {
    expect(acts(you("b", { foldedInto: "a", undo: "s1" }), undefined)).toEqual([]);
  });

  it("say how many went and what undo takes back", () => {
    expect(foldNote(2)).toBe("2 prompts sent as one turn · undo takes back all of them");
  });
});

describe("which events are worth a refetch", () => {
  it("takes the ones that change the conversation", () => {
    for (const name of ["chat", "agent", "state", "resync"]) {
      expect(redraws(name)).toBe(true);
    }
  });

  // ade logs far faster than anyone reads, and the pane is sent every event
  // it broadcasts. Refetching on a log line was a redraw twice a second.
  it("ignores the ones that do not", () => {
    for (const name of ["logs", "activity", "threads", "project", "lineage"]) {
      expect(redraws(name)).toBe(false);
    }
  });

  it("takes an event it cannot name rather than dropping it", () => {
    expect(redraws(undefined)).toBe(true);
    expect(redraws("")).toBe(true);
  });
});

describe("a sub-agent's row", () => {
  it("carries its step while it works, and only its call after", () => {
    expect(
      line(msg({ kind: "tool", tool: "Agent", target: "Inventory ade features", doing: "Read: agent.go", steps: 48 })),
    ).toBe("Agent Inventory ade features · 48 steps · Read: agent.go");
    expect(line(msg({ kind: "tool", tool: "Agent", target: "Inventory ade features", steps: 48 }))).toBe(
      "Agent Inventory ade features",
    );
  });
});

describe("a named agent at work beside the main one", () => {
  const side = { id: "s1", name: "security", startedAt: "2026-10-12T12:00:00Z" };
  const at = (secs: number) => Date.parse(side.startedAt) + secs * 1000;

  it("says who is working", () => {
    expect(sideLabel(side, at(0))).toBe("@security is working");
  });

  it("carries its age once there is one worth showing", () => {
    expect(sideLabel(side, at(9))).toBe("@security is working · 9s");
    expect(sideLabel(side, at(125))).toBe("@security is working · 2m5s");
  });

  it("says nothing about age it cannot work out", () => {
    expect(sideLabel({ ...side, startedAt: "nonsense" }, at(60))).toBe("@security is working");
    expect(age(NaN)).toBe("");
    expect(age(-5)).toBe("");
  });
});

describe("messages waiting their turn", () => {
  it("counts them", () => {
    expect(queuedLabel(1)).toBe("1 message waiting its turn");
    expect(queuedLabel(3)).toBe("3 messages waiting their turn");
  });

  it("says nothing when the queue is empty", () => {
    expect(queuedLabel(0)).toBe("");
  });
});
