import "./bridge";
import { REPORTS } from "./reports";
import * as real from "./real";
import type { Dash, Design, Repo, Review } from "./real";
import { icon } from "./icons";
import { docRef } from "./docref";
import {
  counted,
  grouped,
  inBarOrder,
  type At,
  type Place,
  type Row,
  type Rules,
  type View,
  type World,
} from "./model";
import { Omnibox, el } from "./omnibox";
import { sheet, type Asked } from "./sheets";
import { build, flatten, squashed, toggled, type Node, type Shown } from "./tree";
import { fill, owed, wired, type Ask, type Doing } from "./doing";
import { SOURCES, type Choice, type Held, type Pickable } from "./sources";
import { changesIn, drawnFrom, listed, toneOf, unsent, type Change, type Thread } from "./changes";
import { needFor, Sources } from "./fresh";
import { flip, pruned, read as readMarks, write as writeMarks, type Marks } from "./viewed";
import { frame, pushTo, shown, stirred, tell } from "./panel";
import {
  AGENT,
  dismissable,
  groupOf,
  held,
  idOf,
  inGroup,
  keepable,
  live,
  moved,
  opened,
  newSessionPanel,
  rebuilt,
  settled,
  shut,
  shutMany,
  startIn,
  sweeping,
  trimmed,
  withAgent,
  type Group,
  type Layout,
  type Sweep,
  type Tab,
} from "./tabs";
import { lastAgent, newSeed, setLastAgent, setTerminalMode, terminalMode } from "./terminalmode";
import { isChat, sideWidth } from "./dock";
import { menu, type Row as MenuRow } from "./menu";
import { edits, focusEditor, mount, mountDiff, reaching, saved, selected } from "./code";
import { close as closeTerm, lost as termLost, open as openTerm } from "./term";
import { newSession, written, WHICH, type Reach, type Written } from "./panels";
import { page as prPage } from "../pages/prpage";
import { prActs, type PrActId } from "./prpage";
import { plan, type Usage } from "../pages/usage";
import { refreshFor } from "./refresh";
import { inSpaces, keyOf, spaces as spacesPlace, type Space } from "./spaces";
import {
  designsOf,
  drawnFrom as spaceDrawnFrom,
  sessionsOf,
  type Seen,
  type SpaceAct,
} from "./spacepage";
import type { Repo as Stats } from "../pages/repo/view";
import type { BranchPR, Check } from "../pages/client";

// The shell. It owns the keyboard outright and hands the window named
// commands, never keystrokes. Absent only under `make app-shots`.

// How long a server gets to come up before this is a failure.
const startingFor = 8_000;

// What every list says while that server is on its way up.
const coming = "Starting ade for this checkout…";

interface Rows<T> {
  rows: T;
  why?: string;
}

const EMPTY_DIFF: { rows: Change[]; why: string } = { rows: [], why: "" };
const EMPTY_TREE: { rows: { path: string }[]; why: string } = { rows: [], why: "" };

// Where the files ticked off in a review are kept between reloads.
const markKey = "ade.diff.viewed";

// The dock asks a panel tab's question — is there a server to fetch from.
const CHAT_TAB: Tab = { id: "chat", title: "Chat", icon: "session", what: { panel: "ade.openChat" } };

interface Gathered {
  offline?: string;
  dash?: Dash & { error?: string };
  designs?: Design[];
  repos?: Repo[];
  hosts?: real.Machine[];
  active?: real.Active[];
  todos?: real.Todo[];
  jobs?: real.Job[];
  spaces?: Space[];
  limits?: Usage;
  trouble?: Record<string, string>;
}

class Ade {
  private world: World = real.offline("Reading ade…");
  private place = "prs";
  private picked = new Map<string, string>();
  // The pick the sidebar was last scrolled to, so a redraw does not do it again.
  private revealed = "";
  private listening = false;
  private unfolded = new Set<string>(["internal", "internal/dashboard"]);
  private folded = new Set<string>();
  private filters = new Map<string, string>();
  private trees = new Map<string, Node[]>();
  private said = "";
  private homeDir = "";
  private session = "";
  private repos: Repo[] = [];
  private openSpaces: Space[] = [];
  private changes: Change[] = [];
  private threads: Thread[] = [];
  private mine: real.Listed[] = [];
  private bus?: real.Bus;
  private pr?: { number: number; title: string; body: string };
  private reviews: Review[] = [];
  private pickTrouble = "";
  // Which threads are unfolded: a thread shows one comment, and the findings
  // behind it are a click away.
  private openThreads = new Set<string>();
  // How much of the review is waiting on GitHub, ready here, or going nowhere.
  private draft: real.DraftState = { inDraft: 0, ready: 0, notGoing: 0 };
  // The plan windows, as the launcher last read them.
  private limits?: Usage;
  private prShown?: { row: Row; at: { repo: string; number: number }; url: string };
  // The space whose page is open, for the acts drawn on it.
  private spaceShown?: Space;
  // Archived from a space page, until a read brings a list that agrees.
  private goneSessions = new Set<string>();
  private goneSpaces = new Set<string>();
  // Where the picked session's server is up to. "starting" is one
  // this window has in flight, which is not yet a failure.
  private serving: "ok" | "starting" | "failed" = "ok";
  private startedAt = 0;
  // Every announced session as of the last reading: how the window
  // knows whether a session it is about to show is served.
  private announced: real.Active[] = [];
  // Sessions whose agent has ended, and those whose agent needs you, as last read.
  private endedIn = new Set<string>();
  private needing: Set<string> | undefined;
  private resumed = "";
  private todoList: real.Todo[] = [];
  private jobs: real.Job[] = [];
  // Every read the window keeps, by age. See fresh.ts.
  private sources = new Sources();
  // The version the server publishes for exactly this diff.
  private diffNow = "";
  private diffRead = "";
  // Which checkout holds a changed copy of each design in the store, so the
  // row's Re-import knows where to read it from again.
  private designChanged = new Map<string, string>();
  // The store as the last read listed it.
  private storeDesigns: real.Design[] = [];
  private faultsSaid = 0;

  // The checkout the window was opened on, when it was opened on one.
  // Only until a session is picked: after that the pick is the answer.
  private openedOn = "";
  private asked = false;
  private woke = new Set<string>();
  // A session you just picked, until its server is on it. Cleared once
  // acted on, so a conversation moved by hand afterwards stays put.
  private wants = "";
  // A checkout just made for a review: the window moves onto it as soon
  // as the ade in it announces a session.
  private landing = "";
  // What to show once it announces: the diff, or the conversation.
  private landingFor: "review" | "chat" = "review";
  private landingSince = 0;
  // An Add to review in flight. Left live, a second press files the
  // comments into the draft twice.
  private adding = false;
  private where = new Map<string, string>();
  // What is open in the editor area. A pane is kept once built: moving
  // an iframe reloads it, so switching hides rather than rebuilds.
  private tabs: Tab[] = [];
  // One active tab per half, and which half a verb lands in.
  private actives: [string, string] = ["", ""];
  private focus: Group = 0;
  private sideBySide = false;
  private splitAt = Number(localStorage.getItem("ade.split.at")) || 0.5;
  // A tab is being carried. The strip holds still until it is put down.
  private dragging = false;
  private panes = new Map<string, { node: HTMLElement; key: string; waiting: boolean }>();
  // What each conversation had open, and which one the tabs now belong
  // to. Switching closes them and brings back the ones for the new.
  private keptTabs = new Map<string, Layout>();
  private tabsFor = "";
  private launcher?: { base: string; token: string };
  private omnibox: Omnibox;
  private sidebarWidth = Number(localStorage.getItem("ade.sidebar")) || 300;
  // The chat dock: its own width, and shut only because you shut it.
  private chatWidth = Number(localStorage.getItem("ade.chat")) || 400;
  private chatShut = localStorage.getItem("ade.chat.shut") === "1";
  // The agent's own terminal holds the conversation, and there is no chat.
  private terminal = terminalMode();
  // What the sidebar last drew as, so an unchanged poll leaves it alone.
  private sidebarHTML = "";
  // Expanded: the chat takes the editor area, out to the sidebar.
  private chatWide = localStorage.getItem("ade.chat.wide") === "1";
  // The sidebar folded away, by the arrows under the activity bar.
  private sideShut = localStorage.getItem("ade.side.shut") === "1";
  private split = localStorage.getItem("ade.diff.split") === "1";
  // Files ticked off in the review. Switching session starts a fresh set.
  private marks: Marks = {};
  private marksFor = "";
  private started = false;
  private timer = 0 as unknown as ReturnType<typeof setTimeout>;
  private watching = "";
  private root = document.getElementById("window")!;

  constructor() {
    this.written();
    this.omnibox = new Omnibox(
      () => this.world.places,
      (hit) => {
        if (hit.kind === "row") this.open(hit.row, placeOf(this.world.places, hit.row.id));
        else this.run(hit.verb.command);
      },
    );
    document.documentElement.style.setProperty("--sidebar-w", `${this.sidebarWidth}px`);
    document.documentElement.style.setProperty("--chat-w", `${this.chatWidth}px`);
    this.saySplit();
    // How the editor reaches a language server. The window holds the
    // session, so it does the asking; the editor only says where.
    reaching(
      {
        hover: async (path, line, col) =>
          (await this.language<string>("hover", path, line, col, "hover")) ?? "",
        definition: async (path, line, col) => {
          const at = await this.language<{ path?: string; line?: number }>(
            "definition",
            path,
            line,
            col,
          );
          return at?.path ? { path: at.path, line: at.line ?? 1 } : undefined;
        },
      },
      (to) => {
        this.openFile(to.path);
        this.say(`${to.path}:${to.line}`);
      },
    );
    // Arrows are not chords, so the shell does not carry them.
    document.addEventListener("keydown", (e) => this.key(e));
    // Coming back to the window is looking at it, which is what clears a badge.
    window.addEventListener("focus", () => void this.read());
    if (window.ade) {
      window.ade.onCommand((command) => this.run(command));
      window.ade.onEvent((name) => {
        tell({ kind: "event", name });
        // A page draws from what its host pushes and has no poll, so an
        // event is also the moment to hand it a fresh state.
        void stirred();
        this.soon(300);
      });
      window.ade.onStream((open) => tell({ kind: "stream", open }));
    } else {
      shotHarness((command) => this.run(command));
    }
    if (this.only) {
      this.session = new URLSearchParams(location.search).get("session") ?? "";
      this.launcher = undefined;
      void window.ade?.where().then((at) => {
        this.launcher = at ?? undefined;
        this.draw();
      });
      return;
    }
    // ade's lists change because of other people. Each read sets the
    // next one; see soon().
    void this.read();
  }

  // read is ade's own answer, drawn. A part that failed says so in its
  // own list rather than taking the window down.
  private async read(): Promise<void> {
    if (!window.ade) return;
    // Asked once: `ade .` says which checkout, and the first read is
    // where that decides anything.
    if (!this.asked) {
      this.asked = true;
      this.openedOn = (await window.ade.opened().catch(() => "")) || "";
      this.homeDir ||= this.openedOn;
    }
    const got = (await window.ade
      .world(this.homeDir)
      .catch((e: Error) => ({ offline: e.message }))) as Gathered;
    if (got.offline) {
      // No launcher is useless, and nothing else here starts one. Once,
      // then the message says what to do.
      if (!this.started) {
        this.started = true;
        this.world = real.offline("Starting ade…");
        this.draw();
        const up: { ok?: boolean; error?: string } = await window.ade
          .start()
          .catch((e: Error) => ({ error: e.message }));
        if (up.ok) return this.read();
        this.world = real.offline(up.error ?? got.offline);
        this.soon(10_000);
        return this.draw();
      }
      this.world = real.offline(got.offline);
      this.soon(10_000);
      return this.draw();
    }
    const dash = got.dash ?? {};
    const repos = got.repos ?? [];
    const trouble = got.trouble ?? {};
    this.repos = repos;
    this.reviews = got.dash?.reviews ?? [];
    const listed = got.spaces ?? [];
    this.goneSpaces = new Set(listed.map((s) => s.repo).filter((r) => this.goneSpaces.has(r)));
    this.openSpaces = listed.filter((s) => !this.goneSpaces.has(s.repo));
    this.goneSessions = real.stillListed(repos, this.goneSessions);
    this.limits = got.limits;
    this.where = real.whereSessionsAre(repos, got.active ?? []);
    // Only when the read brought a list back: a failed scan knows of
    // nothing, and landing off that moved you out of the conversation.
    const known = real.knownSessions(repos, got.active ?? [], got.hosts ?? []);
    if (known.size > 0 && !known.has(this.session)) {
      this.goTo(real.landOn(repos, this.openedOn));
    }
    this.landing = this.landed();
    this.homeDir = this.where.get(this.session) ?? repos[0]?.path ?? "";
    if (this.session !== this.watching) {
      this.watching = this.session;
      void window.ade.watch(this.session);
    }
    this.launcher = (await window.ade.where()) ?? undefined;
    // Once, and only once there is a launcher to connect to.
    if (this.launcher && !this.listening) {
      this.listening = true;
      void window.ade.listen();
    }
    this.announced = got.active ?? [];
    this.todoList = got.todos ?? [];
    this.jobs = got.jobs ?? [];
    this.refreshMyDay();
    // This read is the one that finds out whether anything is serving
    // the session, so the other three do not repeat its failure.
    const doing = await this.doing();
    this.badged(doing, got.active ?? []);
    // A pass while nothing serves the session shows what was last read.
    const starting = this.serving === "starting";
    const [changes, files] = starting
      ? [this.held(this.diffKey, EMPTY_DIFF, coming), this.held(this.treeKey, EMPTY_TREE, coming)]
      : await this.looking();
    this.draft = starting ? this.draft : await this.draftState();
    this.changes = changes.rows;
    if (this.marksFor !== this.session) {
      this.marksFor = this.session;
      this.marks = readMarks(localStorage.getItem(markKey), this.session);
    }
    this.marks = pruned(this.marks, this.changes);
    this.world = {
      places: inBarOrder([
        real.waiting(dash, got.active ?? []),
        spacesPlace(this.openSpaces, this.designCounts()),
        note(
          without(
            real.sessions(
              repos,
              "",
              Date.now(),
              got.hosts ?? [],
              got.active ?? [],
              inSpaces(this.openSpaces),
            ),
            this.goneSessions,
          ),
          trouble.sessions,
          "No session on this machine yet.",
        ),
        real.designs(this.noteChanged(got.designs ?? [])),
        real.prs(dash, this.mine, this.repoOf(this.session)),
        note(real.agents(got.active ?? []), trouble.agents),
        real.issues(dash),
        note(real.todos(got.todos ?? []), trouble.todos, "Nothing on the list."),
        note(real.jobs(got.jobs ?? []), trouble.jobs, "No jobs on this machine."),
        real.inbox(dash),
        real.team(this.bus),
        note(
          {
            id: "review",
            title: "Review",
            icon: "review",
            rows: [],
            files: this.changes.map((c) => ({ path: c.path, status: toneOf(c.status) })),
            count: unsent(this.threads),
            note: real.reviewNote(this.draft),
          },
          changes.why,
        ),
        note({ id: "code", title: "Code", icon: "code", rows: [], files: files.rows }, files.why),
      ]),
      status: {
        ...real.status(dash, repos, doing),
        // One server per checkout: the conversation it is in is not
        // always the row that was picked, and the chat shows its own.
        elsewhere: doing.on && this.session && doing.on !== this.session ? doing.on : "",
      },
      blame: this.world.blame,
    };
    if (this.serving === "starting") this.soon(1_000);
    else this.soon(real.busy(doing) ? 3_000 : 30_000);
    // By id: the places are in the bar's order by now, and the index this
    // was written with pointed at Sessions.
    this.world.places = this.world.places.map((place) =>
      place.id === "designs" ? note(place, trouble.designs, "The designs store is empty.") : place,
    );
    this.trees.clear();
    void this.heard();
    this.reopen();
    this.draw();
    // One server per checkout, and it comes up in whatever conversation
    // it was last in. Picking a session means moving it onto that one.
    if (this.wants === this.session && doing.on && doing.on !== this.session) {
      this.wants = "";
      void this.resume();
    }
  }

  private async up(): Promise<void> {
    if (!window.ade) return;
    this.say("starting ade…");
    const got = await window.ade.start().catch((e: Error) => ({ error: e.message }));
    if (got.error) return this.say(got.error);
    this.started = true;
    await this.read();
    this.say("ade is running");
  }

  // space closes one or clones the checkout an open one is missing.
  // Closing writes the list and nothing else: no checkout is touched.
  private async space(what: "close" | "clone", repo: string): Promise<void> {
    if (!window.ade) return;
    if (what === "close") {
      this.goneSpaces.add(repo);
      this.openSpaces = this.openSpaces.filter((s) => s.repo !== repo);
      this.redrawPlace(spacesPlace(this.openSpaces, this.designCounts()));
      this.say(`closing ${repo}…`);
    }
    const got =
      what === "close"
        ? await window.ade.act("launcher", "/api/spaces", { repo }, undefined, "DELETE")
        : await window.ade.act("launcher", "/api/spaces/clone", { repo });
    const back = real.answered(got as real.Answer);
    if (back.error) {
      this.goneSpaces.delete(repo);
      this.say(back.error);
      return void this.read();
    }
    this.say(what === "close" ? `closed ${repo}` : `cloning ${repo}…`);
    await this.read();
  }

  // again re-reads the showing place. What it asks first is the service
  // behind that place, if any: Issues is Linear, Team is agentbus.
  private async again(place = this.place): Promise<void> {
    // Asked for by hand, so nothing held is trusted.
    this.sources.stale();
    const want = refreshFor(place);
    this.say(want.say);
    let trouble = "";
    if (want.at && want.path) {
      const got = await window.ade
        ?.act(want.at, want.path, {})
        .catch((e: Error) => ({ error: e.message }));
      trouble = real.answered(got as real.Answer).error;
    }
    await this.read();
    this.say(trouble || "refreshed");
  }

  private async doing(): Promise<real.Doing> {
    if (!window.ade || !this.session) {
      this.threads = [];
      this.mine = [];
      return real.NOTHING;
    }
    const got = (await window.ade
      .read("session", "/api/state", this.session)
      .catch((e: Error) => ({ error: e.message }))) as { error?: string };
    const unserved = real.unserved(got.error);
    const was = this.serving;
    if (!unserved) {
      this.serving = "ok";
      // A chat built while nothing answered gave up on its own. The pane
      // is kept across a restart, so it is told rather than rebuilt.
      if (was !== "ok") pushTo("chat", { kind: "reconnect" });
    } else if (!this.woke.has(this.session)) {
      // Once per session: one that will not come up would otherwise be
      // started again every three seconds.
      this.woke.add(this.session);
      this.serving = "starting";
      void this.wake();
    } else if (Date.now() - this.startedAt > startingFor) {
      this.serving = "failed";
    }
    this.faulted(got as { fault?: { count?: number; latest?: string } });
    this.mine = ((got as { openPrs?: real.Listed[] | null })?.openPrs ?? []).filter((p) => p.number);
    this.bus = (got as { bus?: real.Bus })?.bus;
    this.threads = ((got as { threads?: Thread[] | null })?.threads ?? []).filter((t) => t.id);
    this.diffNow = String((got as { diffVersion?: string }).diffVersion ?? "");
    // The version makes the diff stale, not the event that brought it.
    const at = `${this.session}:${this.diffNow}`;
    if (this.diffNow !== "" && at !== this.diffRead) {
      this.diffRead = at;
      this.sources.stale(this.diffKey);
      this.sources.stale(this.treeKey);
    }
    return real.doingIn(got);
  }

  // landed moves onto the checkout a review was put in, and shows the
  // diff. Until its ade announces, there is nothing to move onto.
  private landed(): string {
    if (!this.landing) return "";
    const id = real.servingDir(this.where, this.landing);
    if (!id) {
      // A wait with nothing on screen reads as a verb that did nothing.
      if (Date.now() - this.landingSince > 30_000) {
        this.say(`started in ${this.landing}, but it has not announced yet: pick it in Sessions`);
        this.landingFor = "review";
        return "";
      }
      this.soon(1_000);
      return this.landing;
    }
    this.goTo(id);
    this.wants = id;
    this.picked.set("sessions", id);
    if (!this.servedAt(id)) this.serving = "starting";
    if (this.landingFor === "chat") this.run(this.terminal ? "ade.openAgent" : "ade.openChat");
    else this.place = "review";
    this.landingFor = "review";
    return "";
  }

  // follow is a session the New session panel just started.
  private follow(dir: string): void {
    for (const tab of [...this.tabs]) {
      if ("panel" in tab.what && newSessionPanel(tab.what.panel)) this.close(tab.id);
    }
    this.landing = dir;
    this.landingFor = "chat";
    this.landingSince = Date.now();
    this.say(`started in ${dir}; waiting for it`);
    this.landing = this.landed();
    this.draw();
  }

  private soon(ms: number): void {
    clearTimeout(this.timer);
    this.timer = setTimeout(() => void this.read(), ms);
  }

  private get diffKey(): string {
    return `diff:${this.session}`;
  }

  private get treeKey(): string {
    return `tree:${this.session}`;
  }

  // looking is one pass's look at the diff and the file list.
  private async looking(): Promise<[Rows<Change[]>, Rows<{ path: string }[]>]> {
    if (!window.ade) return [{ rows: [] }, { rows: [] }];
    if (!this.session) {
      return [
        { rows: [], why: "Pick a session: the diff is one checkout's." },
        { rows: [], why: "Pick a session: Code is one checkout's." },
      ];
    }
    this.sources.watch(this.diffKey, () => this.readDiff(), "diff");
    this.sources.watch(this.treeKey, () => this.readTree(), "diff");
    // A source nothing looked at is dropped: a session left behind stops being read.
    await this.sources.settled();
    this.sources.looked();
    return [this.held(this.diffKey, EMPTY_DIFF), this.held(this.treeKey, EMPTY_TREE)];
  }

  // held is a reading as a place takes it: rows, and a line for having none.
  private held<T>(key: string, empty: { rows: T; why: string }, instead = ""): Rows<T> {
    const got = this.sources.reading(key, empty);
    return { rows: got.value.rows, why: instead || got.error || got.value.why };
  }

  private async readDiff(): Promise<{ rows: Change[]; why: string }> {
    const got = await window.ade?.read("session", "/api/diff", this.session);
    const bad = (got as { error?: string } | undefined)?.error;
    if (bad) throw new Error(`/api/diff — ${bad}`);
    const rows = changesIn(got);
    const d = (got as { diff?: { mode?: string; base?: string } }).diff ?? {};
    const where = this.where.get(this.session) ?? "the picked session";
    const view = [d.mode, d.base && `against ${d.base}`].filter(Boolean).join(" ");
    const why =
      rows.length > 0
        ? ""
        : `Nothing in ${where}${view ? ` · ${view}` : ""}. ⌘K “scope” to change the view.`;
    return { rows, why };
  }

  private async readTree(): Promise<{ rows: { path: string }[]; why: string }> {
    const got = (await window.ade?.read("session", "/api/tree", this.session)) as {
      files?: string[];
      error?: string;
    };
    if (got.error) throw new Error(`/api/tree — ${got.error}`);
    return { rows: (got.files ?? []).map((path) => ({ path })), why: "" };
  }

  // draftState is how far the review has got, which the Review place says and
  // the verbs act on.
  // faulted says a panic the session contained, once per fault.
  private faulted(got: { fault?: { count?: number; latest?: string } }): void {
    const count = got.fault?.count ?? 0;
    if (count <= this.faultsSaid) return;
    this.faultsSaid = count;
    this.say(`ade hit a fault and carried on: ${got.fault?.latest ?? "see the Logs panel"}`);
  }

  private async draftState(): Promise<real.DraftState> {
    const none = { inDraft: 0, ready: 0, notGoing: 0 };
    if (!window.ade || !this.session) return none;
    const got = (await window.ade
      .read("session", "/api/review/draft", this.session)
      .catch((e: Error) => ({ error: e.message }))) as real.DraftState & { error?: string };
    // A read that failed is said: zeros read as "nothing to say" over a
    // review with thirteen findings in it.
    if (got.error) return { ...none, error: got.error };
    return got;
  }

  run(command: string): void {
    this.noted("command", command);
    const what = wired(command);
    if (!what || what.at === "soon") return this.say(owed(command));
    if (what.at === "panel") {
      this.omnibox.close();
      if (isChat(what.panel)) return this.chat();
      // Run from a workspace, New session opens with that repository ticked.
      const from = what.panel === "ade.newSession" ? startIn(held(this.tabs, this.active)) : "";
      if (from) return this.show({ panel: `in:${from}` }, "New session", "session");
      const made = WHICH[what.panel];
      const title = made ? made(this.reach(), this.sessionAt()).title : what.panel;
      return this.show({ panel: what.panel }, title, "session");
    }
    if (what.at === "report") {
      this.omnibox.close();
      void this.report(what.from, what.path, what.render, what.also);
      return;
    }
    if (what.at === "rules") {
      this.omnibox.close();
      void this.rules();
      return;
    }
    if (what.at === "find") {
      void this.find(what);
      return;
    }
    if (what.at === "edit") {
      void this.editPR();
      return;
    }
    if (what.at === "verdict") {
      void this.giveVerdict(what.which);
      return;
    }
    if (what.at === "window") {
      if (what.verb === "dismiss") {
        if (this.omnibox.showing()) return this.omnibox.close();
        if (!dismissable(held(this.tabs, this.active))) return;
        return this.close();
      }
      this.omnibox.close();
      if (what.verb === "omnibox") return this.omnibox.toggle();
      if (what.verb === "refresh") return void this.again();
      if (what.verb === "save") return void this.save();
      if (what.verb === "review-draft") return void this.addToReview();
      if (what.verb === "review-finish") return void this.finishReview();
      if (what.verb === "open-pr") return void this.openPRLink();
      if (what.verb === "start") return void this.up();
      if (what.verb === "resume") return void this.resume();
      if (what.verb === "new-chat") return void this.newChat();
      if (what.verb === "ask") return void this.ask();
      if (what.verb === "dictate") return this.dictate();
      if (what.verb === "history") return void this.history();
      if (what.verb === "float") {
        void window.ade?.float("ade.concierge", this.session);
        return;
      }
      if (what.verb === "terminal") {
        return this.show({ kind: "term", title: "Shell" }, "Shell", "code");
      }
      if (what.verb === "agent") {
        return this.show({ kind: "agent", title: "Agent" }, "Agent", "session");
      }
      if (what.verb === "terminal-mode") {
        setTerminalMode(!this.terminal);
        return location.reload();
      }
      if (what.verb === "sidebar") return this.shutSide(!this.sideShut);
      if (what.place) {
        this.place = what.place;
        if (this.sideShut) return this.shutSide(false);
      }
      return this.draw();
    }
    void this.perform(command, what);
  }

  // perform sends one verb to ade. The table's body is merged with what
  // the window knows: the checkout, or the session.
  private async perform(command: string, what: Doing): Promise<void> {
    if (!window.ade || (what.at !== "launcher" && what.at !== "session")) return;
    const body: Record<string, unknown> = {
      dir: this.homeDir,
      id: this.session,
      ...(what.body ?? {}),
    };
    let path = what.path;
    for (const need of what.needs ?? []) {
      const said = await this.askFor(need);
      if (said === undefined) return this.omnibox.close();
      path = fill(body, path, need.as, said);
    }
    this.omnibox.close();
    this.say("working…");
    // An answer named `session` says which session to send to, so a verb
    // can act on one other than the sidebar's.
    const to = typeof body.session === "string" ? body.session : this.session;
    const got = await window.ade
      .act(what.at, path, body, to, what.how)
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got);
    this.say(back.error || said(command, back.body));
    await this.read();
    // Opening a space is asking to look at it. Without this the list gains
    // a row and the workspace already showing stays put.
    if (!back.error && command === "ade.openSpace" && typeof body.repo === "string") {
      this.openSpace(body.repo);
    }
  }

  private async report(
    from: "launcher" | "session",
    path: string,
    render: string,
    also?: string,
  ): Promise<void> {
    if (!window.ade) return;
    this.say("reading…");
    const got = await window.ade
      .read(from, path, this.session)
      .catch((e: Error) => ({ error: e.message }));
    const extra = also
      ? await window.ade.read(from, also, this.session).catch(() => ({}))
      : undefined;
    const err = (got as { error?: string }).error;
    const body = err
      ? `# ${render}\n\n${err}`
      : REPORTS[render]?.(got, extra) ?? `# ${render}\n\nNothing to draw.`;
    this.said = "";
    this.show({ kind: "doc", title: render, body }, render, "eye");
  }

  private async find(what: Extract<Doing, { at: "find" }>): Promise<void> {
    if (!window.ade) return;
    const q = await this.omnibox.type(what.title);
    if (!q?.trim()) return this.omnibox.close();
    const found = (await window.ade
      .read(what.from, `${what.path}?q=${encodeURIComponent(q.trim())}`, this.session)
      .catch((e: Error) => ({ error: e.message }))) as {
      hits?: { path: string; line: number; snippet: string }[];
      error?: string;
    };
    if (found.error) return this.say(found.error);
    const hits = found.hits ?? [];
    if (hits.length === 0) return this.say(`nothing matches "${q.trim()}"`);
    const path = await this.omnibox.ask(
      `${hits.length} for "${q.trim()}"`,
      hits.map((hit) => ({ label: hit.path, note: hit.snippet, value: hit.path })),
    );
    this.omnibox.close();
    if (typeof path !== "string") return;
    const read = (await window.ade
      .read(what.from, what.open + path.split("/").map(encodeURIComponent).join("/"), this.session)
      .catch((e: Error) => ({ error: e.message }))) as { markdown?: string; error?: string };
    this.show(
      { kind: "doc", title: path, body: read.markdown ?? `# ${path}\n\n${read.error ?? ""}` },
      path,
      "design",
    );
  }

  private async history(): Promise<void> {
    const path = this.picked.get(this.place);
    if (!window.ade || !path || !this.session) return this.say("pick a file in Code first");
    const got = (await window.ade
      .read("session", `/api/history?path=${encodeURIComponent(path)}`, this.session)
      .catch((e: Error) => ({ error: e.message }))) as {
      commits?: { sha: string; author: string; at: string; subject: string }[];
      error?: string;
    };
    if (got.error) return this.say(got.error);
    const rows = got.commits ?? [];
    const body = rows.length
      ? `# ${path}\n\n` +
        rows
          .map((c) => `- \`${c.sha.slice(0, 7)}\` ${real.ago(c.at)} · ${c.author} — ${c.subject}`)
          .join("\n") +
        "\n"
      : `# ${path}\n\nNo commits touch this file.`;
    this.show({ kind: "doc", title: `${path} · history`, body }, `${path} · history`, "ring");
  }

  private async ask(): Promise<void> {
    const pick = selected();
    if (!pick) return this.say("highlight something in Code first");
    const got = await window.ade
      ?.act("session", "/api/attach", pick, this.session)
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got);
    if (back.error) return this.say(back.error);
    this.chat();
    this.say(`${pick.from} is on the next message`);
  }

  private dictate(): void {
    this.chat();
    tell({ kind: "dictate" });
  }

  // Chat is the dock on the right, not a tab: every command that used to
  // open one opens that instead.
  private chat(): void {
    if (this.only) return;
    this.chatShut = false;
    localStorage.removeItem("ade.chat.shut");
    this.draw();
    pushTo("chat", { kind: "focus" });
  }

  // Flex on one class, not a width this would have to keep in step.
  private wideChat(on: boolean): void {
    this.chatWide = on;
    if (on) localStorage.setItem("ade.chat.wide", "1");
    else localStorage.removeItem("ade.chat.wide");
    this.draw();
  }

  private shutChat(): void {
    this.chatShut = true;
    localStorage.setItem("ade.chat.shut", "1");
    this.draw();
  }

  // The head redraws every pass; the page below it only when the
  // conversation or the server it reaches has moved.
  private drawDock(): void {
    if (!this.shell) return;
    const { dock, dockHead, dockBody, chatGrip } = this.shell;
    const shut = this.chatShut || this.terminal;
    dock.hidden = shut;
    chatGrip.hidden = shut || this.chatWide;
    this.root.classList.toggle("chatwide", !shut && this.chatWide);
    this.root.classList.toggle("terminal", this.terminal);
    if (shut) return;
    dockHead.replaceChildren(...this.dockHead());
    const wait = this.waiting(CHAT_TAB);
    if (wait) {
      const now = dockBody.firstElementChild;
      if (now?.className !== "nothing" || now.textContent !== wait) {
        dockBody.replaceChildren(el("div", "nothing", wait));
      }
      return;
    }
    const made = WHICH["ade.openChat"](this.reach(), this.sessionAt());
    const node = frame(made, `chat:${this.session}:${this.launcher?.base ?? ""}`);
    if (dockBody.firstElementChild !== node) dockBody.replaceChildren(node);
  }

  private dockHead(): HTMLElement[] {
    const name = el("div", "nm", "Chat");
    const wide = el("div", "x");
    wide.append(icon(this.chatWide ? "narrower" : "wider", 12));
    wide.title = this.chatWide ? "Back to the dragged width" : "Widen it to the sidebar";
    wide.addEventListener("click", () => this.wideChat(!this.chatWide));
    const again = el("div", "x");
    again.append(icon("refresh", 12));
    again.title = "Read the conversation again";
    again.addEventListener("click", () => pushTo("chat", { kind: "reconnect" }));
    const shut = el("div", "x", "✕");
    shut.title = "Hide the chat";
    shut.addEventListener("click", () => this.shutChat());
    return [name, wide, again, shut];
  }

  // In the tab strip, so the dock comes back by mouse as well as omnibox.
  private chatToggle(): HTMLElement {
    const node = el("div", this.chatShut ? "chattoggle" : "chattoggle on");
    node.append(icon("session", 13));
    node.title = this.chatShut ? "Show the chat" : "Hide the chat";
    node.addEventListener("click", () => (this.chatShut ? this.chat() : this.shutChat()));
    return node;
  }

  // wake starts the ade that serves the picked session's checkout.
  // `/api/open` starts one, or hands the session to the one already there.
  private async wake(again = false): Promise<void> {
    const dir = this.where.get(this.session);
    if (!window.ade || !dir) return this.say("pick a session first");
    if (again) this.woke.delete(this.session);
    this.serving = "starting";
    this.startedAt = Date.now();
    this.say("starting ade for this checkout…");
    const got = await window.ade
      .act("launcher", "/api/open", { dir, id: this.session })
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got);
    if (back.error) {
      this.serving = "failed";
      return this.say(back.error);
    }
    // Shortly, not now: the proxy reads a record the server writes as
    // it comes up. Scheduled, not awaited — this is inside a read.
    this.say("started; waiting for it");
    this.soon(900);
  }

  // The provider is carried over, not defaulted: a codex session asked to
  // start again on claude is not the conversation that was asked for.
  private async newChat(): Promise<void> {
    if (!window.ade) return;
    // Nothing picked, so there is no conversation to replace: the panel
    // that starts one is the answer.
    if (!this.session) return this.run("ade.newSession");
    const message = await this.omnibox.type("What is the new conversation for?");
    this.omnibox.close();
    if (!message?.trim()) return;
    const doing = await this.doing();
    this.say("starting a new conversation…");
    const got = await window.ade
      .act(
        "session",
        "/api/chat/new",
        { message: message.trim(), provider: doing.provider || "claude" },
        this.session,
      )
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got);
    if (back.error) return this.say(back.error);
    this.run("ade.openChat");
    await this.read();
  }

  private async resume(): Promise<void> {
    if (!window.ade || !this.session) return;
    const doing = await this.doing();
    this.say("moving the conversation…");
    const got = await window.ade
      .act(
        "session",
        "/api/chat/resume",
        { id: this.session, provider: doing.provider || "claude" },
        this.session,
      )
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got);
    if (back.error) return this.say(back.error);
    await this.read();
    this.say("resumed");
  }

  private async onThread(asked: Asked): Promise<void> {
    if (!window.ade) return;
    const send = (path: string, body: Record<string, unknown>, how?: "PUT" | "DELETE") =>
      window.ade!.act("session", path, body, this.session, how);
    const id = encodeURIComponent(asked.id ?? "");
    let got: { error?: string } | undefined;
    if (asked.what === "new" || asked.what === "reply") {
      const said = await this.omnibox.type(
        asked.what === "new" ? `Comment on ${asked.file}:${asked.line}` : "Reply…",
      );
      this.omnibox.close();
      if (!said?.trim()) return;
      got =
        asked.what === "new"
          ? await send("/api/threads", {
              scope: "line",
              file: asked.file,
              side: asked.side,
              line: asked.line,
              body: said.trim(),
            })
          : await send(`/api/threads/${id}/reply`, { body: said.trim() });
    } else if (asked.what === "resolve") {
      const now = this.threads.find((t) => t.id === asked.id);
      got = await send(`/api/threads/${id}/resolve`, { resolved: !now?.resolved });
    } else if (asked.what === "split") {
      this.split = !this.split;
      localStorage.setItem("ade.diff.split", this.split ? "1" : "0");
      return this.draw();
    } else if (asked.what === "viewed") {
      const file = this.changes.find((c) => c.path === asked.file);
      if (!file) return;
      this.marks = flip(this.marks, file);
      localStorage.setItem(markKey, writeMarks(this.marks, this.session));
      return this.draw();
    } else if (asked.what === "unfold") {
      this.openThreads = toggled(this.openThreads, asked.id ?? "");
      return this.draw();
    } else if (asked.what === "convert") {
      this.say("writing it for the author…");
      got = await send(`/api/threads/${id}/convert`, {});
    } else if (asked.what === "draft") {
      const pr = this.world.status.pr;
      // A pressed verb says it is working, so every way out of here has
      // to redraw it or the button stays dead.
      if (!pr) {
        this.say("this branch has no pull request");
        return this.redrawThreads();
      }
      got = await send("/api/review/draft", { confirm: pr, thread: asked.id });
    } else if (asked.what === "take-out") {
      got = await send(`/api/review/draft/${id}`, {}, "DELETE");
    } else if (asked.what === "drop") {
      got = await send(`/api/threads/${id}`, {}, "DELETE");
    }
    const back = real.answered(got);
    if (back.error) {
      this.say(back.error);
      return this.redrawThreads();
    }
    // GitHub refusing one comment is not a failed request, so the answer
    // has to be read rather than assumed.
    if (asked.what === "draft") {
      this.say(real.draftSaid(back.body as real.Drafted, this.world.status.pr ?? 0));
    }
    await this.read();
    this.redrawThreads();
  }

  private redrawThreads(): void {
    const view = this.showing();
    if (view?.kind !== "hunks") return;
    this.show({ ...view, threads: this.threads }, view.title, "diff");
  }

  private async rules(): Promise<void> {
    if (!window.ade) return;
    const got = (await window.ade
      .read("session", "/api/permissions", this.session)
      .catch((e: Error) => ({ error: e.message }))) as Partial<Rules> & { error?: string };
    if (got.error) return this.say(got.error);
    const view: View = {
      kind: "rules",
      title: "Permissions",
      rules: {
        allow: got.allow ?? [],
        deny: got.deny ?? [],
        session: got.session ?? [],
        free: Boolean(got.free),
        file: got.file ?? "",
      },
    };
    this.show(view, "Permissions", "shield");
  }

  private async forget(rule: string): Promise<void> {
    if (!window.ade) return;
    const got = await window.ade
      .act("session", "/api/permissions", { rule, forget: true }, this.session)
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got);
    if (back.error) return this.say(back.error);
    await this.rules();
  }

  private async editPR(): Promise<void> {
    if (!window.ade) return;
    const state = (await window.ade
      .read("session", "/api/state", this.session)
      .catch((e: Error) => ({ error: e.message }))) as {
      branchPr?: { number: number; title: string; body?: string };
      error?: string;
    };
    if (state.error) return this.say(state.error);
    const pr = state.branchPr;
    if (!pr) return this.say("this branch has no pull request");
    const title = await this.omnibox.type(`#${pr.number} — the title`, pr.title);
    this.omnibox.close();
    if (!title) return;
    this.pr = { number: pr.number, title, body: pr.body ?? "" };
    this.show({ panel: `pr:${pr.number}` }, `#${pr.number}`, "pr");
  }

  // sessionAt is the picked session's address, through the proxy: one
  // origin for every session, here or on another machine.
  private sessionAt(): { base: string; token: string } | undefined {
    if (!this.launcher || !this.session) return undefined;
    return {
      base: `${this.launcher.base}/s/${this.session}`,
      token: this.launcher.token,
    };
  }

  private reach(): Reach {
    // Read when the call is made: a panel outlives the pick.
    return {
      get: (at, path) => this.reached(at, path),
      call: (at, path, method, body) => this.acted(window.ade!.call(at, path, method, body, this.session)),
      send: (at, path, body, method) =>
        this.acted(window.ade!.act(at, path, body, this.session, method)),
      open: (path) => this.fromPanel(path),
      link: (href) => void window.ade!.link(href),
      run: (command) => this.run(command),
      push: (id, said) => pushTo(id, said),
      follow: (dir) => this.follow(dir),
      ask: (title, rows) => this.omnibox.ask(title, rows),
    };
  }

  // reached is a panel's read, by age: one /api/ship pass is four git
  // commands per repository, and a stir arrives on every agent step.
  private async reached(at: "launcher" | "session", path: string): Promise<unknown> {
    const need = needFor(path);
    const read = () => window.ade!.read(at, path, this.session);
    if (need === 0) return read();
    const key = `panel:${at}:${this.session}:${path}`;
    this.sources.watch<unknown>(key, read, need);
    await this.sources.settled();
    const got = this.sources.reading<unknown>(key, undefined);
    return got.error ? { error: got.error } : got.value;
  }

  // acted is a panel's press: what it changed is anybody's guess.
  private async acted<T>(doing: Promise<T>): Promise<T> {
    const got = await doing;
    this.sources.stale();
    return got;
  }

  private todoAt(id: string): string {
    return this.todoList.find((each) => each.id === id)?.text ?? "";
  }

  // servedAt is whether an ade already answers for a session. Its
  // checkout counts: one server answers for every conversation in it.
  private servedAt(session: string): boolean {
    const dir = this.where.get(session);
    return this.announced.some(
      (each) => !each.machine && (each.id === session || (Boolean(dir) && each.dir === dir)),
    );
  }

  private repoOf(session: string): string {
    const dir = this.where.get(session);
    const repo = this.repos.find((r) => (r.checkouts ?? []).some((c) => c.path === dir));
    return repo ? `${repo.owner ? repo.owner + "/" : ""}${repo.name}` : "";
  }

  private fromPanel(path: string): void {
    const dir = this.where.get(this.session) ?? "";
    // A design document is a document, not a file to edit as text.
    const doc = docRef(path, dir);
    if (doc) return this.show({ panel: doc.panel }, doc.title, "design");
    const inside = dir && path.startsWith(dir + "/") ? path.slice(dir.length + 1) : path;
    this.place = "code";
    this.picked.set("code", inside);
    this.openFile(inside);
  }

  // askFor puts a verb's question through the omnibox, the only place
  // anything is put to a person.
  private async askFor(first: Ask): Promise<unknown> {
    if (first.ask === "one") {
      return this.omnibox.ask(
        first.title,
        first.of.map((o) => (typeof o === "string" ? { label: o, value: o } : o)),
      );
    }
    if (first.ask === "type") {
      const said = await this.omnibox.type(first.title);
      if (said === undefined) return undefined;
      if (said === "" && !first.blank) return this.say(`${first.as}: nothing typed`);
      return /^\d+$/.test(said) ? Number(said) : said;
    }
    const from = first.from;
    const held = from === "repo" || from === "session" || from === "scope";
    const list = held ? this.choices(from) : await this.fetched(from);
    if (list.length === 0) {
      return this.say(this.pickTrouble || `nothing to pick: no ${first.from} to offer`);
    }
    if (first.ask === "many") return this.omnibox.many(first.title, list, first.ticks);
    return this.omnibox.ask(first.title, list);
  }

  // fetched is a list ade holds. Read as the box opens: a stale picker
  // is worse than a slow one.
  private async fetched(from: Choice): Promise<Pickable[]> {
    if (!window.ade) return [];
    const of = SOURCES[from];
    const got = (await window.ade
      .read(of.at, of.path, this.session)
      .catch((e: Error) => ({ error: e.message }))) as Record<string, unknown>;
    // A route this ade has not got reads as an empty list, and an empty
    // picker says "nothing to offer" when the truth is "nothing answered".
    if (typeof got.error === "string") {
      this.pickTrouble = got.error;
      return [];
    }
    this.pickTrouble = "";
    return of.rows(got);
  }

  private choices(from: Held): Pickable[] {
    if (from === "scope") {
      return ["branch", "committed", "uncommitted"].map((s) => ({ label: s, value: s }));
    }
    if (from === "session") {
      const place = this.world.places.find((p) => p.id === "sessions");
      return (place?.rows ?? []).map((r) => ({ label: r.name, note: r.meta, value: r.id }));
    }
    return this.repos.map((r) => ({ label: r.name, note: r.path, value: r.path }));
  }

  // say is the status bar line, which stays until the next replaces it.
  private redrawPlace(place: Place): void {
    this.world.places = this.world.places.map((p) => (p.id === place.id ? place : p));
    this.draw();
  }

  // hold keeps the line up for a verb that runs for minutes. Cleared after
  // six seconds, a clone in progress reads as a verb that did nothing.
  private say(said: string, hold = false): void {
    this.said = said;
    this.draw();
    if (hold) return;
    setTimeout(() => {
      if (this.said === said) {
        this.said = "";
        this.draw();
      }
    }, 6000);
  }

  private at(): Place {
    return this.world.places.find((p) => p.id === this.place) ?? this.world.places[0];
  }

  // shut closes whatever is framed over the editor area.
  // Set when this window is a float: one panel, named in the url.
  private only = new URLSearchParams(location.search).get("only") ?? "";
  private shell?: Shell;

  // open is a row clicked, in the place it was clicked in: a pull
  // request is in two lists at once.
  private open(row: Row, from = this.place): void {
    this.place = from;
    this.picked.set(this.place, row.id);
    // A different session is a different diff. A sub-agent's id
    // carries the session it belongs to.
    const want = this.place === "agents" ? row.id.split(":")[0] : row.id;
    if ((this.place === "sessions" || this.place === "agents") && this.session !== want) {
      this.goTo(want);
      // Picking a conversation means that conversation: one server per
      // checkout, so the read that follows moves it onto this one.
      this.wants = want;
      // Said here, not when the first read lands: openRow() draws the
      // chat at once, and it would fetch a 404 and print it.
      if (!this.servedAt(want)) this.serving = "starting";
      void this.read();
    }
    this.openRow(row);
  }

  // What a row opens as.
  private openRow(row: Row): void {
    // A session opens as its conversation, which is what it is.
    if (this.place === "sessions" || this.place === "agents") return this.chat();
    // A todo is a thing to do: opening one offers to start it. The
    // omnibox opens rows, so this is its way in as well as the list's.
    if (this.place === "todos") return this.fromTodo(row.id);
    if (row.id.startsWith("who:")) return void this.conversation(row.id.slice("who:".length));
    const reading = { kind: "doc" as const, title: row.name, body: `# ${row.name}\n\nReading…` };
    if (this.place === "designs") {
      return this.show({ panel: `store:${row.id}` }, row.name, "design");
    }
    if (this.place === "jobs") {
      const job = this.jobs.find((one) => `job:${one.name}` === row.id);
      this.show(
        {
          kind: "doc",
          title: row.name,
          body: job ? real.jobReport(job) : `# ${row.name}\n\nNo run history.`,
        },
        row.name,
        "eye",
      );
      // Reading the reports is the whole of reviewing them, and what takes
      // the alert off the Jobs icon.
      if (job && real.jobUnread(job)) void this.markJobSeen(job.name);
      return;
    }
    if (this.place === "spaces") return this.openSpace(row.id.slice("space:".length));
    // The row says which pull request it is, not the list it was in. A
    // notification's id is its own, so the number comes off its url.
    const pr = row.pr ?? prAt(row.id);
    if (pr) {
      this.show(reading, row.name, "pr");
      void this.readPR(row, pr);
      return;
    }
    // Not something ade holds: it lives on a website, so it opens there.
    if (row.url) {
      void window.ade?.link(row.url);
      this.say(`opened ${row.url}`);
      return;
    }
    this.show(
      { kind: "doc", title: row.name, body: `# ${row.name}\n\nNothing opens this yet.` },
      row.name,
      "file",
    );
  }

  // markJobSeen puts a job's reports away. No time named marks every outcome
  // it holds, which is what the page just drew.
  private async markJobSeen(name: string): Promise<void> {
    await window.ade?.act("launcher", "/api/jobs/seen", { name }).catch(() => undefined);
    void this.read();
  }

  // openSpace draws a repository. The numbers are a second read, so the
  // page is shown without them and drawn again when they land.
  private openSpace(key: string): void {
    // The space shown stands in when a read came back without it.
    const space =
      this.openSpaces.find((s) => s.repo === key) ??
      (this.spaceShown?.repo === key ? this.spaceShown : undefined);
    if (!space) return;
    this.spaceShown = space;
    this.show({ kind: "space", title: space.repo, seen: this.seenOf(space) }, space.repo, "space");
    if (space.path) void this.spaceStats(space);
  }

  // nowSeen is a space page's content as it is now, keeping the numbers it
  // has already read: only the lists move while the page is open.
  private nowSeen(was: Seen): Seen {
    const space = this.openSpaces.find((s) => s.repo === was.space.repo) ?? was.space;
    return this.seenOf(space, was.stats, was.note);
  }

  private seenOf(space: Space, stats?: Stats, note?: string): Seen {
    const repo = this.repos.find((r) => keyOf(r) === space.repo);
    return {
      space,
      sessions: sessionsOf(repo, Date.now(), this.goneSessions),
      designs: designsOf(this.storeDesigns, space.repo),
      stats,
      note,
    };
  }

  private async spaceStats(space: Space): Promise<void> {
    if (!window.ade) return;
    const at = `/api/repo?dir=${encodeURIComponent(space.path ?? "")}`;
    const got = (await window.ade
      .read("launcher", at)
      .catch((e: Error) => ({ error: e.message }))) as Stats & { error?: string };
    // Gone elsewhere while it was reading: the page it would redraw is
    // not the one showing.
    if (this.spaceShown !== space) return;
    const seen = this.seenOf(space, got.error ? undefined : got, got.error);
    // Numbers landing must not take the tab back: New session opened while
    // they were reading is the one you asked for.
    this.restate({ kind: "space", title: space.repo, seen }, space.repo, "space");
  }

  // restate redraws a tab already open, leaving which one is showing alone.
  private restate(what: Tab["what"], title: string, icon: string): void {
    const id = idOf(what);
    if (!this.tabs.some((tab) => tab.id === id)) return;
    this.tabs = opened(this.tabs, { id, title, icon, what });
    this.remember();
    this.draw();
  }

  // spaceAct is a link or an Archive on that page.
  private async spaceAct(did: SpaceAct): Promise<void> {
    const space = this.spaceShown;
    if (!window.ade || !space) return;
    if (did.what === "open-session") {
      const row = this.world.places
        .find((p) => p.id === "sessions")
        ?.rows.find((r) => r.id === did.id);
      if (row) this.open(row, "sessions");
      return;
    }
    if (did.what === "open-design") {
      return this.show({ panel: `store:${did.name}` }, did.name, "design");
    }
    if (did.what === "new-session") {
      if (!space.path) return this.say(`${space.repo} is not cloned on this machine`);
      return this.show({ panel: `in:${space.path}` }, "New session", "session");
    }
    if (did.what === "import-session") return this.importSession(space.repo);
    // Off the page first, then the request: a row that stays put reads as a
    // button that did nothing.
    this.goneSessions.add(did.id);
    const listed = this.world.places.find((p) => p.id === "sessions");
    if (listed) this.redrawPlace(without(listed, this.goneSessions));
    this.say("archiving the session…");
    this.openSpace(space.repo);

    const got = await window.ade.act("launcher", "/api/archive", { id: did.id, archived: true });
    const back = real.answered(got as real.Answer);
    if (back.error) {
      this.goneSessions.delete(did.id);
      this.openSpace(space.repo);
      return this.say(back.error);
    }
    this.say("archived");
    // Not awaited: the read is the whole world, and the page is already right.
    void this.read();
  }

  // importSession pins a conversation into this space by id, for one the list
  // window drops: older than a fortnight, or past the row cap.
  private async importSession(repo: string): Promise<void> {
    const id = await this.omnibox.type("Session id to import");
    this.omnibox.close();
    if (!id?.trim() || !window.ade) return;

    this.say("looking for that session…");
    const got = await window.ade.act("launcher", "/api/sessions/import", { repo, id: id.trim() });
    const back = real.answered(got as real.Answer) as real.Answer & {
      session?: real.Session;
      elsewhere?: string;
    };
    if (back.error) return this.say(back.error);

    // Recorded in another repository: adopting is the only way in, and it is
    // the reader's call rather than ours.
    if (back.elsewhere) return this.offerAdopt(repo, back.elsewhere, back.session);

    this.say(`imported ${back.session?.title || back.session?.id || "the session"}`);
    this.openSpace(repo);
    void this.read();
  }

  // offerAdopt asks before taking a conversation off another repository. The
  // transcript stays where it was written; only where it is listed changes.
  private async offerAdopt(repo: string, elsewhere: string, found?: real.Session): Promise<void> {
    const name = found?.title || found?.id || "that session";
    const pick = await this.omnibox.ask(`${name} belongs to ${elsewhere}`, [
      { label: `Open it in ${elsewhere}`, value: "open" },
      { label: `Adopt it into ${repo}`, note: "it remembers the directory it was recorded in", value: "adopt" },
    ]);
    this.omnibox.close();
    if (pick === "open") {
      if (found) await window.ade?.act("launcher", "/api/open", { dir: found.dir, id: found.id });
      return;
    }
    if (pick !== "adopt" || !window.ade) return;

    const into = this.spaceShown?.path;
    if (!into) return this.say("that space has no checkout here to adopt into");
    const got = await window.ade.act("launcher", "/api/sessions/import", {
      repo,
      id: found?.id,
      into,
    });
    const back = real.answered(got as real.Answer);
    if (back.error) return this.say(back.error);
    this.say(`adopted into ${repo}`);
    this.openSpace(repo);
    void this.read();
  }

  // readPR renders the pull request with `app/pages/prpage.ts`, and
  // what can be done with it as buttons above.
  private async readPR(row: Row, at: { repo: string; number: number }): Promise<void> {
    if (!window.ade) return;
    const [got, branch] = await Promise.all([
      window.ade
        .pr(at.repo, at.number)
        .catch((e: Error) => ({ error: e.message })) as Promise<{
        pr?: BranchPR;
        checks?: Check[] | null;
        error?: string;
      }>,
      this.branchPR(at),
    ]);
    if (this.picked.get(this.place) !== row.id) return;
    const requested = this.reviews.find(
      (r) => r.repo === at.repo && r.number === at.number,
    );
    const blocked = branch && {
      approveBlocked: branch.approveBlocked,
      requestChangesBlocked: branch.requestChangesBlocked,
      mergeBlocked: branch.mergeBlocked,
    };
    const pr = got.pr ? { ...got.pr, ...blocked, checks: got.checks ?? null } : undefined;
    const url = pr?.url ?? requested?.url ?? row.url ?? "";
    const acts = prActs({ url, requested, branch });
    const body = pr ? prPage(pr) : `# ${row.name}\n\n${got.error ?? "Nothing came back."}`;
    this.prShown = { row, at, url };
    this.show({ kind: "pr", title: row.name, body, acts }, row.name, "pr");
  }

  // branchPR is the session's own pull request when it is this one: the
  // session holds why a verdict or a merge would be refused.
  private async branchPR(at: { repo: string; number: number }): Promise<BranchPR | undefined> {
    if (!window.ade || !this.session) return undefined;
    const mine = this.repoOf(this.session);
    if (!mine || !(at.repo === mine || at.repo.endsWith(`/${mine}`))) return undefined;
    const state = (await window.ade.read("session", "/api/state", this.session).catch(() => ({}))) as {
      branchPr?: BranchPR;
    };
    return state.branchPr?.number === at.number ? state.branchPr : undefined;
  }

  // prAct is one button on the pull request's page.
  private async prAct(id: PrActId): Promise<void> {
    const shown = this.prShown;
    if (!window.ade || !shown) return;
    const { at, url } = shown;
    if (id === "web") return void window.ade.link(url);
    let got: unknown;
    if (id === "ade") {
      this.say(`checking out #${at.number}…`);
      got = await window.ade.act("launcher", "/api/reviews/open", { repo: at.repo, number: at.number });
      this.landing = (real.answered(got as real.Answer).body as { path?: string })?.path ?? "";
      this.landingSince = Date.now();
    } else if (id === "mute" || id === "unmute") {
      got = await window.ade.act("launcher", "/api/reviews/mute", {
        repo: at.repo,
        number: at.number,
        muted: id === "mute",
      });
    } else {
      const ask = await this.verdict(id, at.number);
      if (!ask) return;
      got = await window.ade.act("session", ask.path, ask.body, this.session);
    }
    const back = real.answered(got as real.Answer);
    if (back.error) return this.say(back.error);
    const path = (back.body as { path?: string } | undefined)?.path;
    this.say(id === "ade" && path ? `#${at.number} is checked out in ${path}` : `#${at.number}: done`);
    await this.again();
    if (this.prShown === shown) await this.readPR(shown.row, at);
  }

  // openPRLink checks a pasted pull request out and lands on it. The
  // launcher does the cloning, the worktree and the space.
  private async openPRLink(): Promise<void> {
    const link = await this.omnibox.type("Paste a pull request link", "");
    this.omnibox.close();
    if (!link?.trim()) return;
    const agent = this.terminal ? await this.whichAgent() : "";
    if (this.terminal && !agent) return;
    this.say(`checking out ${link.trim()}; cloning first if you do not have it…`, true);
    const got = await window.ade!
      .act("launcher", "/api/reviews/open", { link: link.trim() })
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got as real.Answer);
    if (back.error) return this.say(back.error);
    const path = (back.body as { path?: string } | undefined)?.path ?? "";
    if (!path) return this.say("it was opened, but nothing said where");
    if (agent) await this.seatAgent(path, agent);
    this.landing = path;
    this.landingFor = "review";
    this.landingSince = Date.now();
    this.say(`checked out in ${path}`);
    await this.read();
  }

  // whichAgent asks which CLI a session runs, the last one used first.
  private async whichAgent(): Promise<string> {
    const rows = ["claude", "codex"].map((one) => ({ label: one, value: one }));
    if (lastAgent() === "codex") rows.reverse();
    const picked = await this.omnibox.ask("Which agent?", rows);
    this.omnibox.close();
    if (typeof picked !== "string") return "";
    setLastAgent(picked);
    return picked;
  }

  // seatAgent puts an open checkout on the picked CLI; a running agent keeps its own.
  private async seatAgent(dir: string, agent: string): Promise<void> {
    const got = await window.ade!
      .act("launcher", "/api/open", { dir, fresh: true, direct: true, agent, terminal: true, sandbox: false })
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got as real.Answer);
    if (back.error) this.say(back.error);
  }

  // giveVerdict is the omnibox's Approve, Request changes and Merge. They
  // go through verdict() because the server refuses one that does not name
  // the pull request, and only the window knows its number.
  private async giveVerdict(which: "approve" | "request-changes" | "merge"): Promise<void> {
    if (!window.ade) return;
    const number = this.world.status.pr;
    if (!number) {
      this.omnibox.close();
      return this.say("this branch has no pull request");
    }
    const ask = await this.verdict(which, number);
    if (!ask) return;
    this.say("working…");
    const got = await window.ade
      .act("session", ask.path, ask.body, this.session)
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got as real.Answer);
    this.say(back.error || `#${number}: done`);
    void this.read();
  }

  // verdict asks for what a review or a merge needs, through the box.
  // Each is public and posted as you, so each names the pull request.
  private async verdict(
    id: "approve" | "request-changes" | "merge",
    number: number,
  ): Promise<{ path: string; body: Record<string, unknown> } | undefined> {
    if (id === "merge") {
      const method = await this.omnibox.type(`Merge #${number}: rebase or merge`, "rebase");
      this.omnibox.close();
      if (method !== "rebase" && method !== "merge") return undefined;
      return { path: "/api/pr/merge", body: { method } };
    }
    const words = await this.omnibox.type(
      id === "approve" ? `Approve #${number}: a comment, or empty` : `Request changes on #${number}: what needs changing`,
    );
    this.omnibox.close();
    if (words === undefined || (id === "request-changes" && !words.trim())) return undefined;
    return { path: `/api/pr/${id}`, body: { body: words, confirm: number } };
  }

  // Built once and updated in place: moving an iframe in the DOM
  // reloads it, and a read happens every few seconds.
  draw(): void {
    // A floating window is one panel and no chrome.
    if (this.only) {
      if (!this.launcher) return;
      const make = WHICH[this.only];
      if (!make) return;
      if (this.root.firstChild) return;
      this.root.className = "window only";
      this.root.replaceChildren(frame(make(this.reach(), this.sessionAt()), `only:${this.only}`));
      return;
    }
    if (!this.shell) this.shell = this.build();
    const { titlebar, activity, sidebar, head, sheet, statusbar, sideGrip } = this.shell;
    sidebar.hidden = this.sideShut;
    sideGrip.hidden = this.sideShut;
    this.drawDock();
    titlebar.replaceChildren(...this.titlebar());
    activity.replaceChildren(...this.activity());
    // The filter keeps the keyboard across a redraw.
    const typing = document.activeElement?.classList.contains("find");
    // The list is rebuilt every few seconds, so where you had scrolled to
    // is kept across it, as the panes below keep theirs.
    const read = sidebar.querySelector(".list")?.scrollTop ?? 0;
    // Only when it draws differently. A rebuild under the pointer takes
    // the node a press started on, and the click never lands.
    const drawn = el("div");
    drawn.append(...this.sidebar());
    const moved = drawn.innerHTML !== this.sidebarHTML;
    if (moved) {
      this.sidebarHTML = drawn.innerHTML;
      sidebar.replaceChildren(...drawn.childNodes);
    }
    const list = sidebar.querySelector(".list");
    if (moved && list && read) list.scrollTop = read;
    if (moved && typing) {
      const find = sidebar.querySelector<HTMLInputElement>(".find");
      find?.focus();
      find?.setSelectionRange(find.value.length, find.value.length);
    }
    // Only when the pick moved. On every draw it dragged the list back
    // under the pointer a second after you scrolled away from it.
    const pick = `${this.place}:${this.picked.get(this.place) ?? ""}`;
    if (pick !== this.revealed) {
      this.revealed = pick;
      sidebar.querySelector(".row.on, .twig.on")?.scrollIntoView({ block: "nearest" });
    }
    if (!this.dragging) head.replaceChildren(...this.head());
    statusbar.replaceChildren(...this.statusbar());

    // Every open tab keeps its pane; hidden, not taken out, because an
    // iframe reloads when it moves and the terminal reconnects.
    for (const tab of this.tabs) {
      const want = this.paneKey(tab);
      const wait = Boolean(this.waiting(tab));
      let held = this.panes.get(tab.id);
      let was = 0;
      if (held && rebuilt(held, want, wait)) {
        // Where you had read to: a redrawn diff must not throw it away.
        was = held.node.scrollTop;
        held.node.remove();
        held = undefined;
      }
      if (!held) {
        const node = el("div", "pane");
        node.append(this.inPane(tab));
        held = { node, key: want, waiting: wait };
        this.panes.set(tab.id, held);
        sheet.append(node);
        if (was) node.scrollTop = was;
      }
      // Only the pane's edges move: a reparented iframe would reload.
      const g = groupOf(tab);
      const hid = held.node.hidden;
      held.node.hidden = tab.id !== this.actives[g];
      // Back in front, so its panel reads the stir it sat out while hidden.
      if (hid && !held.node.hidden) void shown(held.node);
      held.node.classList.toggle("left", this.sideBySide && g === 0);
      held.node.classList.toggle("right", this.sideBySide && g === 1);
    }
    sheet.classList.toggle("two", this.sideBySide);
    head.classList.toggle("two", this.sideBySide);
    // A save or a selection is about the focused half's editor.
    focusEditor(this.focus);
    // The sheet's own line, not the one a waiting pane holds.
    const empty = sheet.querySelector(":scope > .nothing");
    if (this.tabs.length === 0 && !empty) sheet.append(nothingOpen());
    if (this.tabs.length > 0 && empty) empty.remove();
  }

  // paneKey is what a pane was built with: a session and an address for a
  // live one, and for a diff everything it draws beyond the diff itself.
  private paneKey(tab: Tab): string {
    if (!("panel" in tab.what) && tab.what.kind === "agent") {
      return `${this.session}:${this.launcher?.base ?? ""}:${this.agentEnded() ? "ended" : "up"}`;
    }
    if (live(tab)) return `${this.session}:${this.launcher?.base ?? ""}`;
    const view = tab.what;
    if ("panel" in view) return "";
    // A space is its documents and its conversations, which move while the
    // page is open: archiving one is the whole point of the Archive on it.
    if (view.kind === "space") return spaceDrawnFrom(this.nowSeen(view.seen));
    if (view.kind !== "hunks") return "";
    if (view.editing) return `editing:${view.file.path}`;
    const how = this.split ? "split" : "unified";
    return `${how}:${drawnFrom(view.file.path, this.threads, this.openThreads, Boolean(this.world.status.pr))}`;
  }

  // waiting is why a panel tab cannot draw its page yet, or empty when
  // it can: a page built now would fetch the proxy's 404 and print it.
  private waiting(tab: Tab): string {
    if (!("panel" in tab.what) && tab.what.kind !== "agent") return "";
    if (!this.sessionAt()) return this.session ? "Waiting for ade…" : "Pick a session.";
    return this.serving === "starting" ? "Starting ade for this checkout…" : "";
  }

  // show opens something in the editor area. The same thing twice is
  // the same tab, with whatever it now says.
  private show(what: Tab["what"], title: string, icon = "file"): void {
    const id = idOf(what);
    // The expanded chat covers the editor area, so opening something new
    // into it is a verb that does nothing at all. Opening wins.
    if (this.chatWide) {
      this.chatWide = false;
      localStorage.removeItem("ade.chat.wide");
    }
    // Already open stays in its own half; a new one lands in the focused.
    const was = this.tabs.find((t) => t.id === id);
    const group = was ? groupOf(was) : this.focus;
    this.tabs = opened(this.tabs, { id, title, icon, what, group });
    this.active = id;
    this.remember();
    if (!live({ id, title, icon, what })) {
      this.panes.get(id)?.node.remove();
      this.panes.delete(id);
    }
    this.draw();
  }

  // Setting it follows the tab over, so a verb lands in its half.
  private get active(): string {
    return this.actives[this.focus];
  }

  private set active(id: string) {
    const tab = this.tabs.find((t) => t.id === id);
    if (tab) this.focus = groupOf(tab);
    this.actives[this.focus] = id;
  }

  // A half that empties is no split: the other takes the area back.
  private settle(): void {
    const done = settled(this.tabs);
    this.tabs = done.tabs;
    this.sideBySide = done.split;
    if (!done.split) {
      this.actives = [this.actives[0] || this.actives[1], ""];
      this.focus = 0;
    } else if (!this.actives[this.focus]) {
      this.focus = this.focus === 0 ? 1 : 0;
    }
  }

  private saySplit(): void {
    document.documentElement.style.setProperty("--split-at", `${this.splitAt * 100}%`);
  }

  // kept is what the window picks up again: where you were, and the
  // tabs it can open from an id alone.
  private remember(): void {
    if (this.only) return;
    this.keptTabs.set(this.tabsFor, {
      tabs: keepable(inGroup(this.tabs, 0)),
      active: this.actives[0],
      right: keepable(inGroup(this.tabs, 1)),
      rightOn: this.actives[1],
      at: this.splitAt,
    });
    localStorage.setItem(
      "ade.window",
      JSON.stringify({
        place: this.place,
        session: this.session,
        kept: trimmed(this.keptTabs, this.tabsFor),
      }),
    );
  }

  // goTo moves to a conversation: what is open belongs to the one being
  // left, so it is written down and closed, and that one's comes back.
  private goTo(session: string): void {
    if (session === this.session) return;
    if (this.only) {
      this.session = session;
      return;
    }
    this.remember();
    for (const pane of this.panes.values()) pane.node.remove();
    this.panes.clear();
    this.tabs = [];
    this.actives = ["", ""];
    this.focus = 0;
    this.sideBySide = false;
    this.session = session;
    this.tabsFor = session;
    this.reopen();
  }

  // again opens what was open, after the first read: a file tab needs
  // the session its checkout belongs to.
  private reopen(): void {
    if (this.only || this.tabs.length > 0) return;
    const kept = this.keptTabs.get(this.tabsFor);
    const right = kept?.right ?? [];
    if (!kept || kept.tabs.length + right.length === 0) return this.seat();
    // Replaying a file tab moves the sidebar to Code, which is how
    // openFile knows what it is opening. Where you were is not its to change.
    const was = this.place;
    for (const id of [...kept.tabs, ...right]) {
      const [kind, ...rest] = id.split(":");
      const path = rest.join(":");
      // A layout kept before the dock still names the chat, which the dock
      // remembers for itself.
      if (kind === "panel") {
        if (!isChat(path)) this.run(path);
      }
      else if (kind === "term") this.run("ade.openTerminal");
      else if (kind === "code" || kind === "hunks") {
        this.place = kind === "hunks" ? "review" : "code";
        this.picked.set(this.place, path);
        this.openFile(path);
      }
    }
    this.place = was;
    this.seat();
    for (const id of right) this.tabs = moved(this.tabs, id, 1);
    if (kept.at) {
      this.splitAt = kept.at;
      this.saySplit();
    }
    const here = (id: string | undefined) => (id && this.tabs.some((t) => t.id === id) ? id : "");
    const last = (g: Group) => inGroup(this.tabs, g).slice(-1)[0]?.id ?? "";
    this.actives = [here(kept.active) || last(0), here(kept.rightOn) || last(1)];
    this.settle();
    this.remember();
    this.draw();
  }

  // badged follows the agents' terminals: one looked at is seen, and one
  // that needs you while you are elsewhere says so.
  private badged(doing: real.Doing, active: real.Active[]): void {
    const looking = this.terminal && document.hasFocus() && this.actives.includes(AGENT);
    const needing = new Set<string>();
    this.endedIn.clear();
    for (const one of active) {
      const at = one.id === this.session ? doing.terminal : (one.agent?.terminal ?? "");
      if (at === "ended") this.endedIn.add(one.id);
      if (at !== "needs") continue;
      needing.add(one.id);
      const here = one.id === this.session && looking;
      if (this.needing && !this.needing.has(one.id) && !here) this.notify(one);
    }
    if (doing.terminal === "ended") this.endedIn.add(this.session);
    if (!this.endedIn.has(this.session)) this.resumed = "";
    this.needing = needing;
    if (doing.terminal === "needs" && looking) {
      void window.ade?.act("session", "/api/agent/seen", {}, this.session).catch(() => undefined);
    }
    // A socket the server dropped under a living agent is picked up again.
    const up = doing.terminal !== "" && doing.terminal !== "ended";
    if (up && this.panes.has(AGENT) && termLost(AGENT)) {
      openTerm(this.session, (why) => this.say(why), AGENT);
    }
  }

  private notify(one: real.Active): void {
    try {
      const note = new Notification(one.name || one.branch || "ade", { body: "The agent needs you." });
      note.onclick = () => {
        window.focus();
        this.goTo(one.id);
        this.wants = one.id;
        this.picked.set("sessions", one.id);
        this.run("ade.openAgent");
        void this.read();
      };
    } catch {
      // A window that cannot notify still shows the badge.
    }
  }

  private agentEnded(): boolean {
    return this.endedIn.has(this.session) && this.resumed !== this.session;
  }

  private async resumeAgent(): Promise<void> {
    const got = await window.ade
      ?.act("session", "/api/agent/terminal", {}, this.session)
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got as real.Answer);
    if (back.error) return this.say(back.error);
    this.resumed = this.session;
    this.draw();
  }

  // endedCard stands where an agent that exited was: resuming is asked for.
  private endedCard(): HTMLElement {
    const card = el("div", "nothing");
    const again = el("span", "btn", "Resume");
    again.addEventListener("click", () => void this.resumeAgent());
    card.append(el("p", "", "The agent in this session has ended."), again);
    return card;
  }

  // seat gives a terminal session its agent, first in the strip.
  private seat(): void {
    if (!this.terminal || !this.session || this.only) return;
    this.tabs = withAgent(this.tabs);
    if (!this.actives[0]) this.actives[0] = AGENT;
  }

  // written is what the last run left: the place it was in, and the
  // tabs each conversation had open.
  private written(): void {
    let kept: {
      place?: string;
      session?: string;
      tabs?: string[];
      active?: string;
      kept?: [string, Layout][];
    };
    try {
      kept = JSON.parse(localStorage.getItem("ade.window") ?? "{}") as typeof kept;
    } catch {
      return;
    }
    if (kept.place) this.place = kept.place;
    for (const [session, one] of kept.kept ?? []) this.keptTabs.set(session, one);
    // What a run before this change wrote: one set, for whichever
    // conversation was open when it was written.
    if (!kept.kept && kept.tabs) {
      this.keptTabs.set(kept.session ?? "", { tabs: kept.tabs, active: kept.active ?? "" });
    }
  }

  private close(id = this.active): void {
    const tab = this.tabs.find((t) => t.id === id);
    if (!tab) return;
    // Where it lands is its own half's business: the other half is not
    // disturbed by a tab closing over here.
    const from = groupOf(tab);
    const gone = shut(inGroup(this.tabs, from), id, this.actives[from]);
    this.tabs = this.tabs.filter((t) => t.id !== id);
    this.actives[from] = gone.active;
    this.dropPanes([id]);
    this.settle();
    this.remember();
    this.draw();
  }

  // tabMenu is the strip's context menu. A sweep that would take nothing
  // is listed and dead, so the rows are always in the same places.
  private tabMenu(id: string, at: { x: number; y: number }): void {
    const tab = this.tabs.find((t) => t.id === id);
    // A sweep takes the tabs beside this one, which are its own half's.
    const mine = inGroup(this.tabs, tab ? groupOf(tab) : 0);
    const sweep = (which: Sweep): MenuRow => {
      const ids = sweeping(mine, id, which);
      const label = { others: "Close others", right: "Close to the right", left: "Close to the left", all: "Close all" };
      return { label: label[which], off: ids.length === 0, run: () => this.closeMany(ids) };
    };
    menu(at, [
      { label: "Close", run: () => this.close(id) },
      sweep("others"),
      sweep("right"),
      sweep("left"),
      { ...sweep("all"), apart: true },
    ]);
  }

  private closeMany(ids: string[]): void {
    const gone: string[] = [];
    for (const g of [0, 1] as Group[]) {
      const mine = inGroup(this.tabs, g);
      const done = shutMany(mine, ids.filter((id) => mine.some((t) => t.id === id)), this.actives[g]);
      this.actives[g] = done.active;
      gone.push(...done.gone);
    }
    this.tabs = this.tabs.filter((t) => !gone.includes(t.id));
    this.dropPanes(gone);
    this.settle();
    this.remember();
    this.draw();
  }

  // A pane is held by tab id, so every id that closed has to let its own
  // go: a live one leaves an iframe or a terminal behind otherwise.
  private dropPanes(ids: string[]): void {
    for (const id of ids) {
      this.panes.get(id)?.node.remove();
      this.panes.delete(id);
      if (id === "term") closeTerm();
      if (id === AGENT) closeTerm(AGENT);
    }
  }

  private showing(): View | undefined {
    const tab = held(this.tabs, this.active);
    return tab && !("panel" in tab.what) ? tab.what : undefined;
  }

  private build(): Shell {
    const titlebar = el("div", "titlebar");
    const activity = el("div", "activity");
    const sidebar = el("div", "sidebar");
    const head = el("div", "head");
    const sheet = el("div", "sheet");
    const statusbar = el("div", "statusbar");
    const main = el("div", "main");
    const landing = el("div", "landing");
    landing.hidden = true;
    sheet.append(this.splitGrip(), landing);
    main.append(head, sheet);
    // Built once and filled in place: its iframe reloads if it is moved.
    const dockHead = el("div", "dockhead");
    const dockBody = el("div", "dockbody");
    const dock = el("div", "dock");
    dock.append(dockHead, dockBody);
    const chatGrip = this.grip("chat");
    const sideGrip = this.grip();
    const body = el("div", "body");
    body.append(activity, sidebar, sideGrip, main, chatGrip, dock);
    this.root.replaceChildren(titlebar, body, statusbar);
    this.root.className = "window";
    return { titlebar, activity, sidebar, head, sheet, statusbar, dock, dockHead, dockBody, chatGrip, sideGrip };
  }

  private titlebar(): HTMLElement[] {
    const box = el("div", "box", "⌘K  ·  anything");
    box.addEventListener("click", () => this.omnibox.show());
    const bell = el("div", "bell");
    bell.append(icon("bell", 15));
    if (this.world.status.waiting > 0)
      bell.append(el("span", "count", String(this.world.status.waiting)));
    return [el("div", "where", this.world.status.repo), box, bell];
  }

  private activity(): HTMLElement[] {
    const bar: HTMLElement[] = [];
    for (const place of this.world.places) {
      const button = document.createElement("button");
      button.className = place.id === this.place ? "on" : "";
      if (place.alert) button.classList.add("alert");
      button.title = place.title;
      button.append(icon(place.icon, 21));
      const n = counted(place);
      if (n > 0) button.append(el("span", "count", String(n)));
      button.addEventListener("click", () => {
        // The same place twice folds the sidebar away; another opens it.
        this.sideShut = place.id === this.place ? !this.sideShut : false;
        this.place = place.id;
        localStorage.setItem("ade.side.shut", this.sideShut ? "1" : "0");
        this.remember();
        this.draw();
      });
      if (place.id === "review") bar.push(el("div", "gap"));
      bar.push(button);
      // Under the bell, where what the morning holds belongs.
      if (place.id === "waiting") {
        const day = this.myDayButton();
        if (day) bar.push(day);
      }
    }
    bar.push(this.terminalButton(), this.foldButton());
    return bar;
  }

  // Not a place: one click opens this morning's report. The Jobs place keeps
  // the older fires. Absent when the machine has no my-day job to open.
  private myDayButton(): HTMLElement | undefined {
    const job = real.myDay(this.jobs);
    if (!job) return undefined;
    const unread = real.jobUnread(job);
    const button = document.createElement("button");
    button.className = this.tabs.some((tab) => tab.id === "doc:My Day") ? "on" : "";
    if (unread) button.classList.add("alert");
    button.title = real.myDayTitle(job);
    button.append(icon("calendar", 21));
    if (unread) button.append(el("span", "count", String(unread)));
    button.addEventListener("click", () => this.openMyDay());
    return button;
  }

  // refreshMyDay puts the next fire into the tab already showing the last one.
  // In place rather than through show(), which would pull focus to it.
  private refreshMyDay(): void {
    const body = real.myDayReport(real.myDay(this.jobs));
    this.tabs = this.tabs.map((tab) =>
      tab.id === "doc:My Day" && "kind" in tab.what && tab.what.kind === "doc" && tab.what.body !== body
        ? { ...tab, what: { ...tab.what, body } }
        : tab,
    );
  }

  // openMyDay draws the newest fire and takes the alert off it. The title is
  // the same every day so the tab is reused; the report dates its own heading.
  private openMyDay(): void {
    const job = real.myDay(this.jobs);
    this.show({ kind: "doc", title: "My Day", body: real.myDayReport(job) }, "My Day", "calendar");
    if (job && real.jobUnread(job)) void this.markJobSeen(job.name);
  }

  private foldButton(): HTMLElement {
    const button = document.createElement("button");
    button.className = "fold";
    button.title = this.sideShut ? "Show the sidebar" : "Hide the sidebar";
    button.append(icon(this.sideShut ? "unfold" : "fold", 18));
    button.addEventListener("click", () => this.shutSide(!this.sideShut));
    return button;
  }

  private shutSide(on: boolean): void {
    this.sideShut = on;
    localStorage.setItem("ade.side.shut", on ? "1" : "0");
    this.draw();
  }

  // Not a place: it opens a tab rather than filling the sidebar.
  private terminalButton(): HTMLElement {
    const button = document.createElement("button");
    button.className = this.tabs.some((tab) => tab.id === "term") ? "on" : "";
    button.title = "Terminal";
    button.append(icon("terminal", 21));
    button.addEventListener("click", () => this.run("ade.openTerminal"));
    return button;
  }

  // grip drags one side wider, by one variable on the root. The chat is
  // on the right, so a pointer moving left widens it.
  private grip(which: "sidebar" | "chat" = "sidebar"): HTMLElement {
    const grip = el("div", "grip");
    const chat = which === "chat";
    // Pointer capture, and the panes made deaf: an iframe hit-tests the
    // pointer itself, so a drag over one never saw the mouse come up.
    grip.addEventListener("pointerdown", (down) => {
      down.preventDefault();
      grip.setPointerCapture(down.pointerId);
      grip.classList.add("held");
      document.body.classList.add("dragging");
      const from = chat ? this.chatWidth : this.sidebarWidth;
      const at = down.clientX;
      const move = (e: PointerEvent) => {
        const width = sideWidth(which, from + (chat ? at - e.clientX : e.clientX - at));
        if (chat) this.chatWidth = width;
        else this.sidebarWidth = width;
        document.documentElement.style.setProperty(
          chat ? "--chat-w" : "--sidebar-w",
          `${width}px`,
        );
      };
      const done = () => {
        grip.classList.remove("held");
        document.body.classList.remove("dragging");
        grip.removeEventListener("pointermove", move);
        grip.removeEventListener("lostpointercapture", done);
        localStorage.setItem(
          chat ? "ade.chat" : "ade.sidebar",
          String(chat ? this.chatWidth : this.sidebarWidth),
        );
      };
      grip.addEventListener("pointermove", move);
      // Not pointerup: a capture lost any other way — the window
      // blurring, the pointer cancelled — has to clear the drag too.
      grip.addEventListener("lostpointercapture", done);
    });
    return grip;
  }

  private sidebar(): HTMLElement[] {
    const place = this.at();
    const want = (this.filters.get(place.id) ?? "").toLowerCase();
    const list = el("div", "list");
    if (place.files) {
      const files = want
        ? (place.files ?? []).filter((f) => f.path.toLowerCase().includes(want))
        : place.files;
      const tree = squashed(build(files));
      const open = want ? everyDir(tree) : this.unfolded;
      for (const shown of flatten(tree, open)) list.append(this.twig(shown));
      if (files.length === 0) list.append(this.note(this.empty(place, want)));
    } else {
      const rows = want
        ? place.rows.filter((r) => `${r.name} ${r.meta ?? ""}`.toLowerCase().includes(want))
        : place.rows;
      for (const group of grouped(rows)) {
        const shut = this.folded.has(`${place.id}:${group.name}`);
        if (group.name) list.append(this.heading(place.id, group, shut));
        if (shut) continue;
        for (const row of group.rows) list.append(this.row(row));
      }
      if (rows.length === 0) list.append(this.note(this.empty(place, want)));
    }
    if (place.id !== "review") return [this.head2(place), list];
    return [this.head2(place), list, this.commentList()];
  }

  // commentList is the review's open comments under its files, worst first:
  // the whole of what is to say, without opening each file to find it.
  private commentList(): HTMLElement {
    const out = el("div", "comments");
    const all = listed(this.threads);
    if (all.length === 0) return out;
    out.append(el("div", "head", `Comments · ${all.length}`));
    for (const one of all) {
      const row = el("div", "comment");
      row.append(el("span", `sev ${one.severity}`, `${one.severity}:`), el("span", "title", one.title));
      row.title = `${one.file}:${one.line}`;
      row.addEventListener("click", () => {
        this.picked.set("review", one.file);
        this.openFile(one.file, { thread: one.id, line: one.line });
      });
      out.append(row);
    }
    return out;
  }

  // The heading and its filter. One field per place, kept while away.
  private head2(place: Place): HTMLElement {
    const head = el("div", "side-head");
    head.append(el("h2", "", place.title));
    const find = document.createElement("input");
    find.className = "find";
    find.placeholder = "filter";
    find.spellcheck = false;
    find.value = this.filters.get(place.id) ?? "";
    find.addEventListener("input", () => {
      this.filters.set(place.id, find.value);
      this.draw();
    });
    const again = el("button", "again");
    again.title = "Refresh: ask GitHub and ade again";
    again.append(icon("refresh", 14));
    again.addEventListener("click", () => void this.again());
    head.append(find);
    const all = this.foldAll(place);
    if (all) head.append(all);
    head.append(again);
    if (place.id !== "review") return head;
    const heads = el("div", "heads");
    heads.append(head, this.reviewBar());
    return heads;
  }

  // foldAll shuts every group in a place, or opens them all once they are
  // shut. Nothing to fold means no button: a place without headings would
  // otherwise carry a control that does nothing.
  private foldAll(place: Place): HTMLElement | undefined {
    const names = this.groupNames(place);
    if (names.length === 0) return undefined;
    const shut = names.every((name) => this.folded.has(`${place.id}:${name}`));
    const button = el("button", "again fold-all");
    button.title = shut ? "Expand every group" : "Collapse every group";
    button.append(icon(shut ? "expand" : "collapse", 14));
    button.addEventListener("click", () => {
      for (const name of names) {
        const key = `${place.id}:${name}`;
        if (shut) this.folded.delete(key);
        else this.folded.add(key);
      }
      this.draw();
    });
    return button;
  }

  // The headings a place is showing. The filter counts: collapsing all of
  // them should not reach a group the filter has taken off the screen.
  private groupNames(place: Place): string[] {
    if (place.files) return [];
    const want = (this.filters.get(place.id) ?? "").toLowerCase();
    const rows = want
      ? place.rows.filter((r) => `${r.name} ${r.meta ?? ""}`.toLowerCase().includes(want))
      : place.rows;
    return grouped(rows)
      .map((g) => g.name)
      .filter(Boolean);
  }

  // reviewBar is where the review stands and the one thing that ends it.
  private reviewBar(): HTMLElement {
    const bar = el("div", "review-bar");
    const said = real.reviewNote(this.draft);
    bar.append(el("span", "quiet", said || "nothing to say yet"));
    const behind = this.world.status.behind ?? 0;
    if (behind > 0) {
      const up = el("span", "btn quiet", `Catch up · ${behind} behind`);
      up.title = "Stash, rebase onto the base, bring your work back, settle what conflicts";
      up.addEventListener("click", () => void this.catchUp());
      bar.append(up);
    }
    if (this.draft.ready > 0) {
      const add = el("span", this.adding ? "btn quiet add off" : "btn quiet add", `Add ${this.draft.ready} to the review`);
      add.title = this.adding ? "Adding them…" : "Into the draft review, which only you can see.";
      if (!this.adding) add.addEventListener("click", () => void this.addToReview());
      bar.append(add);
    }
    if (this.draft.inDraft > 0) {
      const finish = el("span", "btn", `Send the review · ${this.draft.inDraft}`);
      finish.title = "Submits the draft with a verdict. This is what the author sees.";
      finish.addEventListener("click", () => void this.finishReview());
      bar.append(finish);
    }
    return bar;
  }

  // note is what an empty list says, selectable and one click from the
  // clipboard: a message about a failure is usually read to someone.
  private note(text: string): HTMLElement {
    const node = el("div", "empty", text);
    node.title = "Click to copy";
    node.addEventListener("click", () => void this.copy(text));
    return node;
  }

  // copy puts something on the clipboard and says it did.
  private async copy(text: string): Promise<void> {
    if (!text.trim()) return;
    await navigator.clipboard.writeText(text).catch(() => undefined);
    this.say("copied");
  }

  // What an empty list says: the filter, if one is on, before the
  // reason it would be empty anyway.
  private empty(place: Place, want: string): string {
    if (want) return `Nothing here matches "${want}".`;
    return place.note || "Nothing here.";
  }

  // key moves in the list, when the keyboard is the window's: a filter,
  // the omnibox or a panel owns its own.
  private key(e: KeyboardEvent): void {
    if (this.only || this.omnibox.showing()) return;
    const on = document.activeElement;
    if (on instanceof HTMLInputElement || on instanceof HTMLTextAreaElement) {
      // The filter is the one input the arrows still move under: you
      // narrow a list and then walk it without reaching for the mouse.
      if (!on.classList.contains("find") || !/^Arrow/.test(e.key)) return;
    }
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    const place = this.at();
    const ids = this.walkable(place);
    if (ids.length === 0) return;
    const at = ids.indexOf(this.picked.get(place.id) ?? "");

    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      const next = e.key === "ArrowDown" ? Math.min(at + 1, ids.length - 1) : Math.max(at - 1, 0);
      this.picked.set(place.id, ids[at < 0 ? 0 : next]);
      return this.draw();
    }
    if (e.key === "Enter" && at >= 0) {
      e.preventDefault();
      return this.enter(place, ids[at]);
    }
    // A directory folds and unfolds under the arrows, as a tree does.
    if ((e.key === "ArrowRight" || e.key === "ArrowLeft") && place.files && at >= 0) {
      const path = ids[at];
      if (!this.treeOf(place).length) return;
      e.preventDefault();
      const open = new Set(this.unfolded);
      if (e.key === "ArrowRight") open.add(path);
      else open.delete(path);
      this.unfolded = open;
      this.draw();
    }
  }

  // walkable is what the arrows move through: the rows as drawn, or
  // the twigs that are open, filter and folding included.
  private walkable(place: Place): string[] {
    const want = (this.filters.get(place.id) ?? "").toLowerCase();
    if (place.files) {
      const files = want
        ? place.files.filter((f) => f.path.toLowerCase().includes(want))
        : place.files;
      const tree = squashed(build(files));
      return flatten(tree, want ? everyDir(tree) : this.unfolded).map((s) => s.node.path);
    }
    const rows = want
      ? place.rows.filter((r) => `${r.name} ${r.meta ?? ""}`.toLowerCase().includes(want))
      : place.rows;
    return grouped(rows)
      .filter((g) => !this.folded.has(`${place.id}:${g.name}`))
      .flatMap((g) => g.rows.map((r) => r.id));
  }

  // enter opens what the arrows landed on, which is a row in a list
  // and a file or a directory in a tree.
  private enter(place: Place, id: string): void {
    if (!place.files) {
      const row = place.rows.find((r) => r.id === id);
      if (row) this.open(row, place.id);
      return;
    }
    const node = flatten(this.treeOf(place), this.unfolded).find((s) => s.node.path === id)?.node;
    if (!node) return;
    if (node.dir) this.unfolded = toggled(this.unfolded, node.path);
    else this.openFile(node.path);
    this.draw();
  }

  // heading is a group of rows, folded or not: deep enough to want
  // folding, shallow enough not to want a tree widget.
  private heading(
    place: string,
    group: { name: string; rows: Row[] },
    shut: boolean,
  ): HTMLElement {
    const head = el("div", shut ? "group shut" : "group");
    const chev = el("span", "chev");
    chev.append(icon("chevron", 12));
    head.append(chev, el("span", "nm", group.name), el("span", "count", String(group.rows.length)));
    head.addEventListener("click", () => {
      this.folded = toggled(this.folded, `${place}:${group.name}`);
      this.draw();
    });
    return head;
  }

  // treeOf is a place's files as a tree, squashed and built once per place.
  private treeOf(place: Place): Node[] {
    const held = this.trees.get(place.id);
    if (held) return held;
    const made = squashed(build(place.files ?? []));
    this.trees.set(place.id, made);
    return made;
  }

  private twig(shown: Shown): HTMLElement {
    const { node, depth, open } = shown;
    const row = el("div", `twig${node.status ? " " + node.status : ""}`);
    if (this.picked.get(this.place) === node.path) row.className += " on";
    if (this.place === "review" && this.marks[node.path]) row.className += " viewed";
    for (let i = 0; i < depth; i++) row.append(el("span", "pad guide"));

    const chev = el("span", node.dir ? (open ? "chev open" : "chev") : "chev leaf");
    chev.append(icon("chevron", 13));
    const ico = el("span", node.dir ? "ico dir" : "ico");
    ico.append(icon(node.dir ? (open ? "folder-open" : "folder") : "file", 15));
    row.append(chev, ico, el("span", "nm", node.name));
    if (node.status) row.append(el("span", "mark", node.status[0].toUpperCase()));

    row.addEventListener("click", () => {
      this.picked.set(this.place, node.path);
      if (node.dir) {
        this.unfolded = toggled(this.unfolded, node.path);
        this.draw();
        return;
      }
      this.openFile(node.path);
    });
    return row;
  }

  // openFile puts a file in a tab of its own: the diff when it is under
  // review, the file otherwise. The path is the title, since names repeat.
  private openFile(path: string, at?: At): void {
    const name = path.split("/").pop() ?? path;
    if (this.place === "review") {
      const file = this.changes.find((c) => c.path === path);
      if (file) {
        this.show({ kind: "hunks", title: path, file, threads: this.threads, at }, name, "diff");
        return;
      }
    }
    this.show({ kind: "code", title: path, source: "" }, name, "file");
    void this.readFile(path);
  }

  // language asks one of the session's language routes. A file with no
  // server answers empty, which is most of them.
  private async language<T>(
    route: string,
    path: string,
    line: number,
    col: number,
    field?: string,
  ): Promise<T | undefined> {
    if (!window.ade || !this.session) return undefined;
    const at = `/api/${route}?path=${encodeURIComponent(path)}&line=${line}&col=${col}`;
    const got = (await window.ade.read("session", at, this.session).catch(() => undefined)) as
      | Record<string, unknown>
      | undefined;
    if (!got || got.error) return undefined;
    return (field ? got[field] : got) as T;
  }

  // blame is who last touched the picked line. git answers one line at
  // a time, so this is asked for the line you click and no other.
  private async blame(line: number): Promise<void> {
    const view = this.showing();
    if (view?.kind !== "code") return;
    const path = view.title;
    if (!window.ade) return;
    const at = `/api/blame?path=${encodeURIComponent(path)}&line=${line}`;
    const got = (await window.ade
      .read("session", at, this.session)
      .catch((e: Error) => ({ error: e.message }))) as {
      author?: string;
      short?: string;
      summary?: string;
      at?: string;
      error?: string;
    };
    this.world = {
      ...this.world,
      blame: got.error
        ? got.error
        : [got.author, real.ago(got.at), got.short, got.summary].filter(Boolean).join(" · "),
    };
    this.draw();
  }

  // readFile is a file of the picked session's checkout, off disk.
  private async readFile(path: string): Promise<void> {
    if (!window.ade || !this.session) return this.say("pick a session: a file is one checkout's");
    const got = (await window.ade
      .read("session", `/api/source?path=${encodeURIComponent(path)}`, this.session)
      .catch((e: Error) => ({ error: e.message }))) as { source?: string; sum?: string; error?: string };
    if (this.picked.get(this.place) !== path) return;
    const view: View = got.error
      ? { kind: "doc", title: path, body: `# ${path}\n\n${got.error}` }
      : { kind: "code", title: path, source: got.source ?? "", sum: got.sum };
    this.show(view, path.split("/").pop() ?? path, "file");
  }

  // conversation is everything said with one person, which this machine kept:
  // the bus keeps none. Read-only until the bus has a message that is not a question.
  private async conversation(who: string): Promise<void> {
    if (!window.ade) return;
    const got = (await window.ade
      .read("launcher", `/api/bus/history?with=${encodeURIComponent(who)}`)
      .catch((e: Error) => ({ error: e.message }))) as { said?: real.Said[]; error?: string };
    const said = got.said ?? [];
    const body = got.error ? `# ${who}\n\n${got.error}` : real.historyPage(who, said, Date.now());
    this.show({ kind: "person", title: who, who, body, count: said.length }, who, "people");
  }

  // askPerson sends what was typed. The bus has ask and answer and nothing
  // plainer, so it goes as a question, and the conversation is redrawn with it.
  private async askPerson(who: string, text: string): Promise<void> {
    if (!window.ade) return;
    const got = await window.ade
      .act("launcher", "/api/bus/ask", { to: who, question: text })
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got as real.Answer);
    this.say(back.error || `asked ${who}`);
    await this.conversation(who);
  }

  // heard redraws an open conversation when something new was said in it, and
  // not while you are typing: a redraw would take the words out of the box.
  private async heard(): Promise<void> {
    const open = this.showing();
    if (open?.kind !== "person" || !window.ade) return;
    const typing = document.querySelector<HTMLTextAreaElement>(".pane:not([hidden]) .ask-box textarea");
    if (typing?.value.trim()) return;
    const got = (await window.ade
      .read("launcher", `/api/bus/history?with=${encodeURIComponent(open.who)}`)
      .catch(() => ({}))) as { said?: real.Said[] };
    if ((got.said ?? []).length !== open.count) await this.conversation(open.who);
  }

  // catchUp replays the branch on a fresh base over the top of whatever is
  // uncommitted. The server undoes a rebase it cannot settle.
  private async catchUp(): Promise<void> {
    if (!window.ade || !this.session) return;
    this.say("catching up…");
    const got = await window.ade
      .act("session", "/api/base/catch-up", {}, this.session)
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got as real.Answer);
    if (back.error) return this.say(back.error);
    this.say(real.caughtUp(back.body as real.CaughtUp));
    await this.read();
  }

  // addToReview puts every converted comment into the pull request's draft
  // review, which is where you read what would be submitted.
  private async addToReview(): Promise<void> {
    const pr = this.world.status.pr;
    if (!window.ade || !pr) return this.say("this branch has no pull request");
    if (this.adding) return;
    this.adding = true;
    this.draw();
    const got = await window.ade
      .act("session", "/api/review/draft", { confirm: pr }, this.session)
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got as real.Answer);
    this.adding = false;
    if (back.error) return this.say(back.error);
    this.say(real.draftSaid(back.body as real.Drafted, pr));
    await this.read();
  }

  // finishReview submits the draft with a verdict, which is the moment the
  // author can see any of it.
  private async finishReview(): Promise<void> {
    const pr = this.world.status.pr;
    if (!window.ade || !pr) return this.say("this branch has no pull request");
    const n = this.draft.inDraft;
    if (n === 0) return this.say("nothing is in the draft review yet");
    const event = await this.omnibox.ask(`Finish the review of #${pr}`, [
      { label: "Approve", value: "approve" },
      { label: "Request changes", value: "request-changes" },
      { label: "Comment", value: "comment" },
    ]);
    this.omnibox.close();
    if (typeof event !== "string") return;
    const body = await this.omnibox.type(
      event === "request-changes" ? `#${pr} — the summary, which GitHub requires` : `#${pr} — the summary`,
      event === "approve" ? `Approved with ${n} comment${n === 1 ? "" : "s"}` : "",
    );
    this.omnibox.close();
    if (body === undefined) return;
    // GitHub refuses a request for changes with no words; the other two it takes.
    if (event === "request-changes" && !body.trim())
      return this.say(`#${pr}: requesting changes needs a summary`);
    const got = await window.ade
      .act("session", "/api/review/finish", { confirm: pr, event, body }, this.session)
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got as real.Answer);
    if (back.error) return this.say(back.error);
    this.say(`#${pr}: ${n} comment${n === 1 ? "" : "s"} are on the pull request`);
    await this.read();
  }

  // editInReview swaps a diff tab for the file over the diff's left side,
  // and back. The editor keeps a model per file, so an unsaved edit survives.
  private async editInReview(id: string): Promise<void> {
    const tab = this.tabs.find((t) => t.id === id);
    const view = tab?.what;
    if (!tab || !view || "panel" in view || view.kind !== "hunks") return;
    if (view.editing) {
      tab.what = { ...view, editing: false };
      this.active = id;
      return this.draw();
    }
    if (!window.ade || !this.session) return this.say("pick a session: a file is one checkout's");
    const path = encodeURIComponent(view.file.path);
    const [got, left] = await Promise.all([
      window.ade
        .read("session", `/api/source?path=${path}`, this.session)
        .catch((e: Error) => ({ error: e.message })) as Promise<{
        source?: string;
        sum?: string;
        error?: string;
      }>,
      window.ade
        .call("session", `/api/blob?path=${path}&side=left`, "GET", undefined, this.session)
        .catch((e: Error) => ({ status: 0, error: e.message })) as Promise<{
        ok?: boolean;
        text?: string;
        error?: string;
      }>,
    ]);
    if (got.error) return this.say(got.error);
    if (left.error || !left.ok) return this.say(left.error ?? `the diff has no left side for ${view.file.path}`);
    tab.what = { ...view, editing: true, base: left.text ?? "", source: got.source ?? "", sum: got.sum };
    this.active = id;
    this.draw();
  }

  // save writes the showing file back to the checkout. The server
  // refuses it when the file changed under you since it was read.
  private async save(): Promise<void> {
    const view = this.showing();
    const changed = edits();
    const editable = view?.kind === "code" || (view?.kind === "hunks" && view.editing);
    if (!view || !editable || !changed || changed.path !== view.title)
      return this.say("nothing to save");
    if (!window.ade || !this.session) return;
    const got = await window.ade
      .act("session", "/api/source", { path: changed.path, source: changed.source, was: view.sum ?? "" }, this.session, "PUT")
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got as real.Answer);
    if (back.error) return this.say(back.error);
    view.sum = (back.body as { sum?: string }).sum;
    view.source = changed.source;
    saved(changed.path, changed.source);
    this.say(`saved ${changed.path}`);
    // The diff under the tab is now out of date by exactly what was saved.
    if (view.kind === "hunks") await this.read();
  }

  private row(row: Row): HTMLElement {
    const on = this.picked.get(this.place) === row.id;
    const node = el("div", on ? "row on" : "row");
    if (row.depth) node.classList.add("under");
    const ico = el("span", row.tone ? `ico ${row.tone}` : "ico");
    ico.append(icon(row.icon));
    node.append(ico, el("span", "name", row.name));
    if (row.meta) node.append(el("span", "meta", row.meta));
    if (row.tag) node.append(el("span", `tag ${row.tag.kind}`, row.tag.text));
    // What the row does besides open, shown on hover.
    for (const act of row.acts ?? []) {
      if (act.menu) continue;
      const node2 = el("span", "act", act.label);
      // On the press: a redraw between down and up takes the node the
      // click would have needed, and the button did nothing at all.
      node2.addEventListener("pointerdown", (e) => {
        e.preventDefault();
        e.stopPropagation();
        void this.rowAct(act);
      });
      node2.addEventListener("click", (e) => e.stopPropagation());
      node.append(node2);
    }
    if (row.acts?.length) {
      node.addEventListener("contextmenu", (e) => {
        e.preventDefault();
        menu(
          { x: e.clientX, y: e.clientY },
          row.acts!.map((act) => ({ label: act.label, run: () => void this.rowAct(act) })),
        );
      });
    }
    node.addEventListener("click", () => this.open(row));
    return node;
  }

  // rowAct is the one thing a row does besides open. Each is a verb
  // ade already has; the row only says which and on what.
  private async rowAct(act: NonNullable<Row["acts"]>[number]): Promise<void> {
    this.noted("verb", `${act.what} ${act.name}`);
    if (act.what === "design-start") return this.startDesign(act.name);
    if (act.what === "design-again") return this.againDesign(act.name);
    if (act.what === "space-close" || act.what === "space-clone") {
      return this.space(act.what === "space-close" ? "close" : "clone", act.name);
    }
    if (act.what === "todo-start") return this.fromTodo(act.name);
    if (act.what === "todo-done" || act.what === "todo-drop") return this.todoOff(act);
    if (act.what === "bus-dismiss") {
      const team = this.world.places.find((p) => p.id === "team");
      if (team) this.redrawPlace(without(team, new Set([act.name])));
      const got = await window.ade
        ?.act("launcher", "/api/bus/dismiss", { id: act.name })
        .catch((e: Error) => ({ error: e.message }));
      const back = real.answered(got as real.Answer);
      this.say(back.error || "dismissed");
      return void this.read();
    }
    if (act.what === "queue-first") {
      await window.ade
        ?.act(
          "session",
          `/api/queue/${encodeURIComponent(act.name)}/urgent`,
          {},
          act.on ?? this.session,
        )
        .catch((e: Error) => ({ error: e.message }));
      return void this.read();
    }
    if (act.what === "session-end") return this.endSession(act.name);
    // Both are the launcher's, and row-only verbs in the fork too. The
    // label says which way it goes: Pause stops it, Resume starts.
    const paused = act.label === "Pause";
    const at = act.what === "job-run" ? "/api/jobs/run" : "/api/jobs/pause";
    const got = await window.ade
      ?.act("launcher", at, act.what === "job-run" ? { name: act.name } : { name: act.name, paused })
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got);
    if (back.error) return this.say(back.error);
    this.say(act.what === "job-run" ? `${act.name} started` : paused ? "paused" : "resumed");
    void this.read();
  }

  // endSession is the one verb on a session row, in Sessions and in
  // Agents: stop the run, stop the ade under it, or put it away for good.
  private async endSession(id: string): Promise<void> {
    if (!window.ade) return;
    // Every place, not just Agents: it is offered from Sessions too, and
    // without the row the dialog is titled with a uuid.
    const row = this.world.places.flatMap((p) => p.rows).find((r) => r.id === id);
    const state = this.announced.find((a) => a.id === id);
    const what = await this.omnibox.ask(
      `${row?.name ?? id}`,
      real.endings(real.stateOf(state?.agent), real.spoken(this.repos, id)),
    );
    this.omnibox.close();
    if (!what) return;
    // Deleting is the one that cannot be undone, so it is asked twice.
    if (what === "delete") {
      const sure = await this.omnibox.ask(`Delete ${row?.name ?? id}?`, [
        { label: "Delete it", value: "yes" },
        { label: "Keep it", value: "no" },
      ]);
      this.omnibox.close();
      if (sure !== "yes") return;
    }
    const dir = this.where.get(id) ?? "";
    const leaves = what === "archive" || what === "delete" || what === "done";
    if (leaves) {
      this.goneSessions.add(id);
      const listed = this.world.places.find((p) => p.id === "sessions");
      if (listed) this.redrawPlace(without(listed, this.goneSessions));
    }
    this.say(`${real.underway(what)}…`);
    const got = await this.ending(what, id, dir);
    const back = real.answered(got as real.Answer);
    if (back.error) {
      this.goneSessions.delete(id);
      this.say(back.error);
      return void this.read();
    }
    this.say(real.ended(what, back.body));
    await this.read();
  }

  // stopAndPutAway is the whole decision in one press. Forgetting first would
  // leave an ade serving a session no list admits to, so the server goes first.
  private async stopAndPutAway(id: string, dir: string): Promise<unknown> {
    const ade = window.ade!;
    if (dir) {
      const stopped = real.answered(
        (await ade
          .act("launcher", "/api/graph/stop", { dir })
          .catch((e: Error) => ({ error: e.message }))) as real.Answer,
      );
      // Nothing serving it is the state this asks for, not a failure.
      if (stopped.error && !real.unserved(stopped.error)) return stopped;
    }
    return ade
      .act("launcher", "/api/sessions/delete", { ids: [id] })
      .catch((e: Error) => ({ error: e.message }));
  }

  private ending(what: string, id: string, dir: string): Promise<unknown> {
    const ade = window.ade!;
    switch (what) {
      case "agent":
        return ade.act("session", "/api/cancel", {}, id).catch((e: Error) => ({ error: e.message }));
      case "ade":
        return ade.act("launcher", "/api/graph/stop", { dir }).catch((e: Error) => ({ error: e.message }));
      case "done":
        return this.stopAndPutAway(id, dir);
      case "archive":
        return ade
          .act("launcher", "/api/archive", { id, archived: true })
          .catch((e: Error) => ({ error: e.message }));
      default:
        return ade
          .act("launcher", "/api/sessions/delete", { ids: [id] })
          .catch((e: Error) => ({ error: e.message }));
    }
  }

  // fromTodo opens the New session screen with the todo in it. Not
  // started here: what it runs against is a choice.
  private fromTodo(id: string): void {
    // Always a tab, seeded when the list has the todo: a button that
    // answers with nothing reads as broken.
    if (!this.todoAt(id)) this.say("that todo is not on the list any more");
    this.show({ panel: `todo:${id}` }, "New session", "session");
  }

  // todoOff takes a todo off the list: done keeps it as history, dropped
  // deletes it. The row goes at once, before the next read confirms it.
  private async todoOff(act: NonNullable<Row["acts"]>[number]): Promise<void> {
    if (!window.ade) return;
    const done = act.what === "todo-done";
    const todos = this.world.places.find((p) => p.id === "todos");
    if (todos) this.redrawPlace(without(todos, new Set([act.name])));
    const at = `/api/todos/${encodeURIComponent(act.name)}`;
    const got = await window.ade
      .act(
        "launcher",
        done ? at : `${at}?dir=${encodeURIComponent(this.homeDir)}`,
        done ? { dir: this.homeDir, done: true } : {},
        undefined,
        done ? "PATCH" : "DELETE",
      )
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got as real.Answer);
    this.say(back.error || (done ? "marked as done" : "dropped"));
    await this.read();
  }

  // startDesign checks out every repository the document names, on one
  // branch, and opens a session across them.
  // noted puts one line in the audit trail: what was set off, and where
  // from. What came of it is written by the reach it makes.
  private noted(kind: "command" | "verb" | "panel", what: string): void {
    window.ade?.note({
      kind,
      what,
      where: this.place,
      session: this.session,
      dir: this.where.get(this.session) ?? "",
    });
  }

  private async startDesign(asked: string): Promise<void> {
    if (!window.ade) return;
    const name = await this.intoStore(asked);
    if (!name) return;
    const machine = await this.whichMachine(name);
    if (machine === undefined) return;
    this.say(`starting ${name}…`);
    const got = await window.ade
      .act("launcher", "/api/designs/start", { name, machine: machine || undefined })
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got);
    if (back.error) return this.say(back.error);
    await this.read();
    this.say(machine ? `started ${name} on ${machine}` : `started ${name}`);
  }

  // noteChanged remembers where each design's changed copy is, and hands the
  // list back as it came.
  // designCounts is the store's designs per repository, for the Spaces rows.
  private designCounts(): Map<string, number> {
    const out = new Map<string, number>();
    for (const one of this.storeDesigns) {
      const repo = one.repo ?? "";
      if (repo) out.set(repo, (out.get(repo) ?? 0) + 1);
    }
    return out;
  }

  private noteChanged(all: real.Design[]): real.Design[] {
    this.designChanged = new Map(all.filter((d) => d.changed).map((d) => [d.name, d.changed!]));
    this.storeDesigns = all;
    return all;
  }

  // Re-import reads the checkout's copy over the store's, for a design the two
  // have been edited apart.
  private async againDesign(name: string): Promise<void> {
    const dir = this.designChanged.get(name);
    if (!dir) return this.say("nothing to re-import");
    this.say(`re-importing ${name}…`);
    const got = await window
      .ade!.act("launcher", `/api/designs/import?dir=${encodeURIComponent(dir)}`, {
        file: `${name}.md`,
        again: true,
      })
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got);
    if (back.error) return this.say(back.error);
    await this.read();
    this.say(`re-imported ${name}`);
  }

  // Only the store's documents can start, so one written in a checkout is
  // imported on the way. A name already in the store is handed straight back.
  private async intoStore(asked: string): Promise<string | undefined> {
    const space = asked.startsWith("space:");
    if (!space && !asked.startsWith("here:")) return asked;
    const rest = asked.slice(asked.indexOf(":") + 1);
    const cut = space ? rest.indexOf("|") : -1;
    const dir = space ? rest.slice(0, cut) : this.homeDir;
    const file = space ? rest.slice(cut + 1) : rest;
    if (!dir) {
      this.say("that document has no checkout on this machine");
      return undefined;
    }
    this.say(`importing ${file}…`);
    const got = await window
      .ade!.act("launcher", `/api/designs/import?dir=${encodeURIComponent(dir)}`, { file })
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got);
    if (back.error) {
      this.say(back.error);
      return undefined;
    }
    return (back.body as { name?: string }).name;
  }

  // Connected machines only; "" is here, undefined a cancelled pick.
  private async whichMachine(name: string): Promise<string | undefined> {
    type Host = { name: string; label?: string; connected?: boolean; local?: boolean };
    const got = (await window.ade
      ?.read("launcher", "/api/hosts")
      .catch(() => ({}))) as { hosts?: Host[] } | undefined;
    const away = (got?.hosts ?? [])
      .filter((h) => h.connected && !h.local)
      .map((h) => ({ label: h.label || h.name, note: h.name, value: h.name }));
    if (away.length === 0) return "";
    return this.omnibox.ask<string>(`Where to start ${name}?`, [
      { label: "Here", note: "this machine", value: "" },
      ...away,
    ]);
  }

  // The strip, or one strip per half. Each is its own scroller, so a
  // long row of tabs on one side does not push the other off screen.
  private head(): Element[] {
    if (this.tabs.length === 0) return [el("span", "", "Nothing open"), this.chatToggle()];
    const strips = (this.sideBySide ? [0, 1] : [0]).map((g) => {
      const side = g === 0 ? "left" : "right";
      const strip = el("div", g === this.focus ? `strip ${side} focus` : `strip ${side}`);
      strip.append(...this.strip(g as Group));
      strip.addEventListener("pointerdown", (e) => {
        if (e.target === strip) this.focusOn(g as Group);
      });
      return strip;
    });
    return [...strips, this.chatToggle()];
  }

  private focusOn(g: Group): void {
    if (this.focus === g) return;
    this.focus = g;
    this.draw();
  }

  private strip(group: Group): Element[] {
    return inGroup(this.tabs, group).map((tab) => {
      const node = el("div", tab.id === this.actives[group] ? "tab on" : "tab");
      node.append(icon(tab.icon, 13), el("span", "nm", tab.title));
      node.title = tab.title;
      node.dataset.tab = tab.id;
      const what = tab.what;
      if (!("panel" in what) && what.kind === "hunks") {
        const swap = el("span", "x swap");
        swap.append(icon(what.editing ? "diff" : "edit", 12));
        swap.title = what.editing ? "Back to the diff" : "Edit this file";
        swap.addEventListener("click", (e) => {
          e.stopPropagation();
          void this.editInReview(tab.id);
        });
        node.append(swap);
      }
      const x = el("span", "x", "✕");
      x.title = "Close";
      x.addEventListener("click", (e) => {
        e.stopPropagation();
        this.close(tab.id);
      });
      node.append(x);
      node.addEventListener("contextmenu", (e) => {
        e.preventDefault();
        this.tabMenu(tab.id, { x: e.clientX, y: e.clientY });
      });
      this.dragTab(node, tab.id);
      return node;
    });
  }

  // A tab is picked up where it is pressed and put down where it is let
  // go: the half under the pointer, or the right edge, which splits.
  private dragTab(node: HTMLElement, id: string): void {
    node.addEventListener("pointerdown", (down) => {
      if (down.button !== 0 || (down.target as HTMLElement).closest(".x")) return;
      down.preventDefault();
      this.active = id;
      this.draw();
      const head = this.shell?.head;
      if (!head) return;
      // The strip, not the tab: a poll redraw takes every tab out of the
      // document, and the capture goes with whichever one held it.
      head.setPointerCapture(down.pointerId);
      const held = head.querySelector<HTMLElement>(`.tab[data-tab="${CSS.escape(id)}"]`);
      let x = down.clientX;
      const move = (e: PointerEvent) => {
        x = e.clientX;
        if (!this.dragging && Math.abs(x - down.clientX) < 6) return;
        this.dragging = true;
        held?.classList.add("held");
        document.body.classList.add("tabbing");
        this.hint(this.dropHalf(x));
      };
      const done = () => {
        head.removeEventListener("pointermove", move);
        head.removeEventListener("lostpointercapture", done);
        if (!this.dragging) return;
        this.dragging = false;
        document.body.classList.remove("tabbing");
        this.hint(undefined);
        this.dropTab(id, x);
        this.draw();
      };
      head.addEventListener("pointermove", move);
      // Not pointerup: a capture lost any other way has to clear it too.
      head.addEventListener("lostpointercapture", done);
    });
  }

  // dropHalf is the half a tab let go here would land in. Unsplit, only
  // the right of the area opens a second half.
  private dropHalf(x: number): Group {
    const box = this.shell?.sheet.getBoundingClientRect();
    if (!box || box.width === 0) return 0;
    const across = (x - box.left) / box.width;
    if (this.sideBySide) return across < this.splitAt ? 0 : 1;
    return across > 0.6 ? 1 : 0;
  }

  // hint is the shaded half saying where the tab would go.
  private hint(where: Group | undefined): void {
    const node = this.shell?.sheet.querySelector<HTMLElement>(".landing");
    if (!node) return;
    node.hidden = where === undefined;
    node.classList.toggle("right", where === 1);
    node.classList.toggle("whole", !this.sideBySide && where === 0);
  }

  private dropTab(id: string, x: number): void {
    const tab = this.tabs.find((t) => t.id === id);
    if (!tab) return;
    const want = this.dropHalf(x);
    const from = groupOf(tab);
    if (from === want) return;
    const rest = shut(inGroup(this.tabs, from), id, this.actives[from]);
    this.actives[from] = rest.active;
    this.tabs = moved(this.tabs, id, want);
    this.actives[want] = id;
    this.focus = want;
    this.settle();
    this.remember();
  }

  // splitGrip drags the divider. The halves are measured as a fraction
  // of the area, so the window resizing keeps the proportion.
  private splitGrip(): HTMLElement {
    const grip = el("div", "splitgrip");
    grip.addEventListener("pointerdown", (down) => {
      down.preventDefault();
      grip.setPointerCapture(down.pointerId);
      grip.classList.add("held");
      document.body.classList.add("dragging");
      const move = (e: PointerEvent) => {
        const box = grip.parentElement?.getBoundingClientRect();
        if (!box || box.width === 0) return;
        this.splitAt = Math.min(0.85, Math.max(0.15, (e.clientX - box.left) / box.width));
        this.saySplit();
      };
      const done = () => {
        grip.classList.remove("held");
        document.body.classList.remove("dragging");
        grip.removeEventListener("pointermove", move);
        grip.removeEventListener("lostpointercapture", done);
        localStorage.setItem("ade.split.at", String(this.splitAt));
        this.remember();
      };
      grip.addEventListener("pointermove", move);
      grip.addEventListener("lostpointercapture", done);
    });
    return grip;
  }

  // writable is the document a panel id names: a design, or a pull
  // request's description. Both are markdown edited as it reads.
  private writable(which: string): Written | undefined {
    if (which.startsWith("design:")) {
      const file = which.slice("design:".length);
      return {
        id: which,
        title: file.replace(/\.md$/, ""),
        // `raw`, not `body`: this route answers with both, and `body`
        // is the document already rendered to HTML.
        read: async (reach) =>
          ((await reach.get("session", `/api/design?file=${encodeURIComponent(file)}`)) as {
            raw?: string;
          }).raw ?? "",
        write: async (reach, text) => {
          await reach.send("session", "/api/design", { file, body: text }, "PUT");
        },
        notes: true,
        file,
      };
    }
    // A design in a space other than the session's: read and written
    // through the launcher, which takes the checkout it is in.
    if (which.startsWith("spacedesign:")) {
      const [dir, file] = which.slice("spacedesign:".length).split("|");
      const at = `dir=${encodeURIComponent(dir)}&file=${encodeURIComponent(file)}`;
      return {
        id: which,
        title: file.replace(/\.md$/, ""),
        read: async (reach) =>
          ((await reach.get("launcher", `/api/design?${at}`)) as { body?: string }).body ?? "",
        write: async (reach, text) => {
          await reach.send("launcher", `/api/design?dir=${encodeURIComponent(dir)}`, { file, body: text }, "PUT");
        },
      };
    }
    if (which.startsWith("store:")) {
      const name = which.slice("store:".length);
      return {
        id: which,
        title: name,
        read: async (reach) =>
          ((await reach.get("launcher", `/api/designs/read?name=${encodeURIComponent(name)}`)) as {
            body?: string;
          }).body ?? "",
        write: async (reach, text) => {
          await reach.send("launcher", "/api/designs/write", { name, body: text }, "PUT");
        },
      };
    }
    if (which.startsWith("pr:") && this.pr) {
      const pr = this.pr;
      return {
        id: which,
        title: `#${pr.number} ${pr.title}`,
        read: async () => pr.body,
        write: async (reach, text) => {
          pr.body = text;
          await reach.send("session", "/api/pr", { title: pr.title, body: text }, "PUT");
        },
      };
    }
    return undefined;
  }

  // inPane is what one tab holds, built once.
  private inPane(tab: Tab): HTMLElement {
    if ("panel" in tab.what) {
      // A design names its own file, so it is a panel per document
      // rather than one of the fixed set.
      const which = tab.what.panel;
      const doc = this.writable(which);
      const fromTodo = which.startsWith("todo:");
      const todo = fromTodo ? this.todoAt(which.slice(5)) : "";
      const inDir = which.startsWith("in:") ? which.slice(3) : "";
      const make = doc
        ? (reach: Reach, at?: { base: string; token: string }) => written(reach, at, doc)
        : fromTodo
          ? (reach: Reach) => newSession(reach, { ...newSeed(), message: todo })
          : inDir
            ? (reach: Reach) => newSession(reach, { ...newSeed(), mode: "work", repos: [inDir] })
            : WHICH[which];
      if (!make) return el("div", "nothing", `no panel called ${which}`);
      // The chat reaches the session's own server, so it is given that
      // address rather than the launcher's.
      const panel = make(this.reach(), this.sessionAt());
      // A launcher page draws with no session picked: New session is how
      // you get one, and it used to say "Pick a session." instead.
      const wait = panel.launcher ? "" : this.waiting(tab);
      if (wait) return el("div", "nothing", wait);
      return frame(panel, `${tab.id}:${this.session}:${this.launcher?.base ?? ""}`);
    }
    const view = tab.what;
    if (view.kind === "term") return openTerm(this.session, (why) => this.say(why));
    if (view.kind === "agent") {
      // Opening its pane is what starts the agent, so not before ade is up.
      const wait = this.waiting(tab);
      if (wait) return el("div", "nothing", wait);
      if (this.agentEnded()) return this.endedCard();
      return openTerm(this.session, (why) => this.say(why), AGENT);
    }
    // Code is monaco: one instance, moved into the tab that is showing
    // a file. Moving a div is free; each file keeps its own model.
    if (view.kind === "code") {
      return mount(groupOf(tab), view.source, view.title, view.line ?? 0, (line) =>
        void this.blame(line),
      );
    }
    if (view.kind === "hunks" && view.editing) {
      return mountDiff(groupOf(tab), view.base ?? "", view.source ?? "", view.title, 0, (line) =>
        void this.blame(line),
      );
    }
    // A diff is drawn from what is true now, not from what was true when
    // the file was opened, and the jump it carried happens once.
    let drawn: View = view;
    if (view.kind === "space") drawn = { ...view, seen: this.nowSeen(view.seen) };
    if (view.kind === "hunks") {
      const file = this.changes.find((c) => c.path === view.file.path) ?? view.file;
      drawn = { ...view, file, threads: this.threads };
      view.at = undefined;
    }
    return sheet(
      drawn,
      (line) => this.blame(line),
      (rule) => void this.forget(rule),
      (asked) => void this.onThread(asked),
      (id) => void this.prAct(id),
      {
        hasPR: Boolean(this.world.status.pr),
        open: this.openThreads,
        split: this.split,
        viewed: new Set(Object.keys(this.marks)),
      },
      (who, text) => void this.askPerson(who, text),
      (did) => void this.spaceAct(did),
    );
  }

  private statusbar(): HTMLElement[] {
    const bar: HTMLElement[] = [];
    const status = this.world.status;

    // First, before the branch: two worktrees of one repository are the
    // same branch name, and which one you are in is the other half.
    const dir = this.where.get(this.session) ?? "";
    if (dir) {
      const where = el("div", "item");
      where.append(icon("folder", 13), el("span", "", dir.split("/").pop() ?? dir));
      where.title = `${dir} — click for the repository`;
      where.addEventListener("click", () => this.run("ade.showRepo"));
      bar.push(where);
    }

    const branch = el("div", "item branch");
    branch.append(icon("branch", 13), el("span", "", status.branch));
    branch.title = "Switch branch, or move this session to another worktree";
    branch.addEventListener("click", () => this.run("ade.switchWorktree"));

    bar.push(branch);
    const spent = plan(this.limits, Date.now());
    if (spent) {
      const item = el("div", spent.warn ? "item bad" : "item");
      item.append(icon("gauge", 13), el("span", "", `${spent.stale ? "~" : ""}${spent.bars}`));
      item.title = spent.tooltip;
      item.addEventListener("click", () => this.run("ade.showUsage"));
      bar.push(item);
    }
    // Which view of the diff you are on. Review's empty list says the
    // scope too, but by then you have already been surprised by it.
    if (status.scope) {
      const scope = el("div", "item");
      scope.append(icon("diff", 13), el("span", "", status.scope));
      scope.title = "What the diff is of. Click to change it.";
      scope.addEventListener("click", () => this.run("ade.setScope"));
      bar.push(scope);
    }
    // The checks, when the repository has any: an empty tick said
    // nothing and took the room to say it.
    if (status.checks) {
      const bad = /failed|stopped/.test(status.checks);
      const checks = el("div", bad ? "item bad" : "item");
      checks.append(icon(bad ? "warn" : "check", 13), el("span", "", status.checks));
      checks.title = "⌘K “checks” to run them";
      checks.addEventListener("click", () => this.run("ade.checks"));
      bar.push(checks);
    }
    if (status.sandboxed) {
      const boxed = el("div", "item boxed");
      boxed.append(icon("shield", 13), el("span", "", "sandboxed"));
      boxed.title = "The agent runs in a container. Click for its mounts.";
      boxed.addEventListener("click", () => this.run("ade.sandboxPanel"));
      bar.push(boxed);
    }
    // What is running, by name: a review by three agents is three names.
    if (status.doing) {
      const run = el("div", "item running");
      run.append(el("span", "pulse"), el("span", "", status.doing));
      run.title = "⌘K “stop” to stop it";
      bar.push(run);
    }
    // Nothing serving the session fails every pane at once, so it is
    // one line — and not red until a start has been tried.
    if (this.serving === "starting") {
      const up = el("div", "item");
      up.append(el("span", "pulse"), el("span", "", "starting ade for this checkout…"));
      bar.push(up);
    }
    if (this.serving === "failed") {
      const off = el("div", "item bad");
      off.append(icon("warn", 13), el("span", "", "session would not start"));
      off.title = "It did not come up. Click to try again.";
      off.addEventListener("click", () => void this.wake(true));
      bar.push(off);
    }
    if (status.elsewhere) {
      const other = el("div", "item elsewhere");
      other.append(icon("warn", 13), el("span", "", "chat is on another conversation"));
      other.title = "Click to move this checkout's ade onto the session you picked";
      other.addEventListener("click", () => void this.resume());
      bar.push(other);
    }
    if (this.said) {
      const saying = el("div", "item saying", this.said);
      saying.title = "Click to copy";
      saying.addEventListener("click", () => void this.copy(this.said));
      bar.push(saying);
    }
    bar.push(el("div", "gap"));
    if (this.showing()?.kind === "code" && this.world.blame)
      bar.push(el("div", "item blame", this.world.blame));
    if (status.model) {
      const model = el("div", "item", status.model);
      model.title = "Which model this session answers with. Click to change it.";
      model.addEventListener("click", () => this.run("ade.pickModel"));
      bar.push(model);
    }
    return bar;
  }
}

// The verbs that sweep comments, and what each says it was looking for.
const SWEPT: Record<string, string> = {
  "ade.pruneOrphans": "orphaned comments",
  "ade.removeResolved": "resolved comments",
  "ade.removeDeclined": "declined comments",
  "ade.removeAllComments": "comments",
};

// said is what a verb reports: ade answers a start with what it
// started, and "done" for a two-minute review says nothing.
function said(command: string, got: unknown): string {
  const back = (got ?? {}) as { agents?: string[]; running?: boolean; to?: string };
  const verb = command.replace(/^ade\./, "");
  const sweep = SWEPT[command];
  if (sweep !== undefined) return real.sweptSaid(got, sweep);
  if (back.agents?.length) return `${back.agents.map((a) => `@${a}`).join(", ")} reviewing`;
  if (back.to) return `asked ${back.to}`;
  if (back.running) return `${verb} started`;
  return `${verb} done`;
}

// Shell is the window's frame: built once, each region updated in
// place, because an iframe reloads when it is moved.
interface Shell {
  titlebar: HTMLElement;
  activity: HTMLElement;
  sidebar: HTMLElement;
  head: HTMLElement;
  sheet: HTMLElement;
  statusbar: HTMLElement;
  dock: HTMLElement;
  dockHead: HTMLElement;
  dockBody: HTMLElement;
  chatGrip: HTMLElement;
  sideGrip: HTMLElement;
}

// prAt reads a pull request out of a row id of the window's own
// making: `owner/name#412`, which is how the lists it fills name one.
function prAt(id: string): { repo: string; number: number } | undefined {
  const [repo, number] = id.split("#");
  return repo && /^\d+$/.test(number ?? "") ? { repo, number: Number(number) } : undefined;
}

// everyDir is every directory in a tree, for a filtered one: a match
// hidden behind a closed folder is not a match you can see.
function everyDir(nodes: Node[]): Set<string> {
  const out = new Set<string>();
  const walk = (all: Node[]) => {
    for (const node of all) {
      if (!node.dir) continue;
      out.add(node.path);
      walk(node.children ?? []);
    }
  };
  walk(nodes);
  return out;
}

// placeOf is the list a row belongs to, for a pick that came from the
// box rather than from a click in one.
function placeOf(places: Place[], id: string): string {
  return places.find((p) => p.rows.some((r) => r.id === id))?.id ?? "prs";
}

// nothingOpen is the editor area with no tabs in it.
function nothingOpen(): HTMLElement {
  const out = el("div", "nothing");
  const hint = el("div");
  hint.append(
    document.createTextNode("Or press "),
    el("span", "key", "⌘K"),
    document.createTextNode(" for anything."),
  );
  out.append(el("div", "", "Pick something on the left."), hint);
  return out;
}

// note is what an empty list says. A failed read names itself; an empty
// one that worked says so plainly. Silence is what reads as broken.
function without(place: Place, gone: ReadonlySet<string>): Place {
  return { ...place, rows: place.rows.filter((r) => !gone.has(r.id)) };
}

function note(place: Place, why: string | undefined, instead = ""): Place {
  const empty = place.files ? place.files.length === 0 : place.rows.length === 0;
  if (!empty) return place;
  return { ...place, note: why ?? instead };
}

// shotHarness is how `make app-shots` drives the page in a browser,
// where there is no shell to own the keyboard. Not a way to run ade.
function shotHarness(run: (command: string) => void): void {
  document.addEventListener("keydown", (e) => {
    if (!(e.metaKey || e.ctrlKey)) return;
    const of: Record<string, string> = { k: "omnibox", b: "sidebar.toggle", n: "ade.newSession" };
    const command = of[e.key.toLowerCase()];
    if (!command) return;
    e.preventDefault();
    run(command);
  });
}

new Ade().draw();
