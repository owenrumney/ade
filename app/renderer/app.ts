import "./bridge";
import { REPORTS } from "./reports";
import * as real from "./real";
import type { Dash, Repo } from "./real";
import { icon } from "./icons";
import {
  BAR,
  SIDE,
  counted,
  matches,
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
import { fill, owed, wired, type Ask, type Doing, type Verb } from "./doing";
import { SOURCES, type Choice, type Held, type Pickable } from "./sources";
import { changesIn, drawnFrom, listed, toneOf, unsent, type Change, type Thread } from "./changes";
import { CommitBox, NOTHING as NO_COMMIT, standingIn, type Does } from "./commitbox";
import { needFor, Sources } from "./fresh";
import { flip, pruned, read as readMarks, write as writeMarks, type Marks } from "./viewed";
import { frame, pushTo, shown, stirred, tell } from "./panel";
import {
  AGENT,
  DIFF,
  diffTab,
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
  seated,
  settled,
  shut,
  shutMany,
  standing,
  startIn,
  sweeping,
  trimmed,
  type Group,
  type Layout,
  type Sweep,
  type Tab,
} from "./tabs";
import { lastAgent, newSeed, setLastAgent } from "./terminalmode";
import { WIDTHS, sideWidth, type Edge } from "./dock";
import { menu, type Row as MenuRow } from "./menu";
import { edits, focusEditor, mountDiff, saved } from "./code";
import { close as closeTerm, lost as termLost, open as openTerm } from "./term";
import { newSession, WHICH, type Reach } from "./panels";
import { refreshFor } from "./refresh";
import { keyOf, spaces as spacesPlace, type Space } from "./spaces";
import { drawnFrom as spaceDrawnFrom, sessionsOf, type Seen, type SpaceAct } from "./spacepage";

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

// Where the files ticked off in a review are kept between reloads.
const markKey = "ade.diff.viewed";

interface Gathered {
  offline?: string;
  dash?: Dash & { error?: string };
  repos?: Repo[];
  active?: real.Active[];
  jobs?: real.Job[];
  spaces?: Space[];
  trouble?: Record<string, string>;
}

class Ade {
  private world: World = real.offline("Reading ade…");
  // The list last acted in: where the arrows move and what a pick belongs to.
  private place = "sessions";
  // The list open beside the bar on the right, or none.
  private side = localStorage.getItem("ade.side") ?? "";
  private picked = new Map<string, string>();
  // The pick each list was last scrolled to, so a redraw does not do it again.
  private revealed = new Map<HTMLElement, string>();
  private listening = false;
  private unfolded = new Set<string>();
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
  private commit = new CommitBox(this.commitDoes(), (text) => this.say(text));
  // My Day: the day this window last built it unasked, and a build in flight.
  private myDayAsked = "";
  private myDayBuilding = false;
  private mine: real.Listed[] = [];
  private bus?: real.Bus;
  private pickTrouble = "";
  // Which threads are unfolded: a thread shows one comment, and the findings
  // behind it are a click away.
  private openThreads = new Set<string>();
  // How much of the review is waiting on GitHub, ready here, or going nowhere.
  private draft: real.DraftState = { inDraft: 0, ready: 0, notGoing: 0 };
  // The space whose page is open, for the acts drawn on it.
  private spaceShown?: Space;
  // Closed from here, until a read brings a list that agrees.
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
  private jobs: real.Job[] = [];
  // Every read the window keeps, by age. See fresh.ts.
  private sources = new Sources();
  // The version the server publishes for exactly this diff.
  private diffNow = "";
  private diffRead = "";
  private faultsSaid = 0;

  // The checkout the window was opened on, when it was opened on one.
  // Only until a session is picked: after that the pick is the answer.
  private openedOn = "";
  // The conversation the last run was in, read once on start.
  private wasOn = "";
  private asked = false;
  private woke = new Set<string>();
  // A checkout just made: the window moves onto it as soon as the ade in
  // it announces a session.
  private landing = "";
  // What to show once it announces: the diff, or the agent.
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
  // What each session had open, and which one the tabs now belong
  // to. Switching closes them and brings back the ones for the new.
  private keptTabs = new Map<string, Layout>();
  private tabsFor = "";
  private launcher?: { base: string; token: string };
  private omnibox: Omnibox;
  // The three columns a grip drags: sessions, the Diff's files, the list on the right.
  private widths: Record<Edge, number> = {
    sidebar: Number(localStorage.getItem(WIDTHS.sidebar.key)) || 220,
    files: Number(localStorage.getItem(WIDTHS.files.key)) || 280,
    side: Number(localStorage.getItem(WIDTHS.side.key)) || 340,
  };
  // What each list last drew as, so an unchanged poll leaves it alone.
  private drawnHTML = new Map<HTMLElement, string>();
  // The sessions folded away by ⌘B, for this run: a kept fold hid them for good.
  private sideShut = false;
  // The last review by named agents, and the one put away by its start.
  private reviewRun?: real.ReviewRun;
  private reviewShut = "";
  private split = localStorage.getItem("ade.diff.split") === "1";
  // Files ticked off in the review. Switching session starts a fresh set.
  private marks: Marks = {};
  private marksFor = "";
  private started = false;
  private timer = 0 as unknown as ReturnType<typeof setTimeout>;
  private watching = "";
  private root = document.getElementById("window")!;
  private shell?: Shell;

  constructor() {
    this.written();
    this.omnibox = new Omnibox(
      () => this.world.places,
      (hit) => {
        if (hit.kind === "row") this.open(hit.row, placeOf(this.world.places, hit.row.id));
        else this.run(hit.verb.command);
      },
    );
    for (const edge of Object.keys(WIDTHS) as Edge[]) {
      document.documentElement.style.setProperty(WIDTHS[edge].css, `${this.widths[edge]}px`);
    }
    this.saySplit();
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
    const listed = got.spaces ?? [];
    this.goneSpaces = new Set(listed.map((s) => s.repo).filter((r) => this.goneSpaces.has(r)));
    this.openSpaces = listed.filter((s) => !this.goneSpaces.has(s.repo));
    this.goneSessions = real.stillListed(repos, this.goneSessions);
    this.where = real.whereSessionsAre(repos, got.active ?? []);
    // Only when the read brought a list back: a failed scan knows of
    // nothing, and landing off that moved you out of the conversation.
    const known = real.knownSessions(repos, got.active ?? []);
    if (known.size > 0 && !known.has(this.session)) {
      this.goTo(real.landAgain(repos, known, this.openedOn, this.wasOn));
      // Spent: a session closed later must not send the window back here.
      this.wasOn = "";
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
    this.jobs = got.jobs ?? [];
    if (real.myDayDue(real.myDay(this.jobs), new Date())) void this.buildMyDay(false);
    this.refreshMyDay();
    // This read is the one that finds out whether anything is serving
    // the session, so the other three do not repeat its failure.
    const doing = await this.doing();
    this.reviewRun = doing.review;
    this.badged(doing, got.active ?? []);
    // A pass while nothing serves the session shows what was last read.
    const starting = this.serving === "starting";
    const changes = starting ? this.held(this.diffKey, EMPTY_DIFF, coming) : await this.looking();
    this.draft = starting ? this.draft : await this.draftState();
    this.changes = changes.rows;
    if (this.marksFor !== this.session) {
      this.marksFor = this.session;
      this.marks = readMarks(localStorage.getItem(markKey), this.session);
    }
    this.marks = pruned(this.marks, this.changes);
    this.world = {
      places: inBarOrder([
        real.waiting(dash, this.bus),
        spacesPlace(this.openSpaces),
        note(
          without(real.sessions(repos, Date.now(), got.active ?? []), this.goneSessions),
          trouble.sessions,
          "No session yet. ⌘N starts one.",
        ),
        real.prs(dash, this.mine, this.repoOf(this.session)),
        real.issues(dash),
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
      ]),
      status: real.status(dash, repos, doing, this.bus),
    };
    if (this.serving === "starting") this.soon(1_000);
    else this.soon(real.busy(doing) ? 3_000 : 30_000);
    this.trees.clear();
    void this.heard();
    this.reopen();
    this.draw();
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
      this.redrawPlace(spacesPlace(this.openSpaces));
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
      this.commit.show(NO_COMMIT);
      return real.NOTHING;
    }
    const got = (await window.ade
      .read("session", "/api/state", this.session)
      .catch((e: Error) => ({ error: e.message }))) as { error?: string };
    const unserved = real.unserved(got.error);
    if (!unserved) {
      this.serving = "ok";
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
    this.commit.show(standingIn(got));
    this.diffNow = String((got as { diffVersion?: string }).diffVersion ?? "");
    // The version makes the diff stale, not the event that brought it.
    const at = `${this.session}:${this.diffNow}`;
    if (this.diffNow !== "" && at !== this.diffRead) {
      this.diffRead = at;
      this.sources.stale(this.diffKey);
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
    this.picked.set("sessions", id);
    if (!this.servedAt(id)) this.serving = "starting";
    this.run(this.landingFor === "chat" ? "ade.openAgent" : "ade.diffTurn");
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

  // looking is one pass's look at the diff.
  private async looking(): Promise<Rows<Change[]>> {
    if (!window.ade) return { rows: [] };
    if (!this.session) return { rows: [], why: "Pick a session: the diff is one checkout's." };
    this.sources.watch(this.diffKey, () => this.readDiff(), "diff");
    // A source nothing looked at is dropped: a session left behind stops being read.
    await this.sources.settled();
    this.sources.looked();
    return this.held(this.diffKey, EMPTY_DIFF);
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
    const what = wired(command);
    if (!what || what.at === "soon") return this.say(owed(command));
    if (what.at === "panel") {
      this.omnibox.close();
      // Run from a workspace, New session opens with that repository ticked.
      const from = what.panel === "ade.newSession" ? startIn(held(this.tabs, this.active)) : "";
      if (from) return this.show({ panel: `in:${from}` }, "New session", "session");
      const made = WHICH[what.panel];
      const title = made ? made(this.reach()).title : what.panel;
      return this.show({ panel: what.panel }, title, "session");
    }
    if (what.at === "report") {
      this.omnibox.close();
      void this.report(what.from, what.path, what.render);
      return;
    }
    if (what.at === "rules") {
      this.omnibox.close();
      void this.rules();
      return;
    }
    if (what.at === "verdict") {
      void this.giveVerdict(what.which);
      return;
    }
    if (what.at === "window") return this.windowVerb(what.verb, what.place);
    void this.perform(command, what);
  }

  // windowVerb is a command the window does itself, with no request behind it.
  private windowVerb(verb: Verb | undefined, place: string | undefined): void {
    if (verb === "dismiss") {
      if (this.omnibox.showing()) return this.omnibox.close();
      if (!dismissable(held(this.tabs, this.active))) return;
      return this.close();
    }
    this.omnibox.close();
    switch (verb) {
      case "omnibox":
        return this.omnibox.toggle();
      case "refresh":
        return void this.again();
      case "save":
        return void this.save();
      case "review-draft":
        return void this.addToReview();
      case "review-finish":
        return void this.finishReview();
      case "open-pr":
        return void this.openPRLink();
      case "start":
        return void this.up();
      case "terminal":
        return this.show({ kind: "term", title: "Shell" }, "Shell", "terminal");
      case "agent":
        return this.show({ kind: "agent", title: "Agent" }, "Agent", "session");
      case "diff":
        return this.toDiff();
      case "findings":
        return void this.sendFindings();
      case "close-session":
        return void this.endSession(this.session);
      case "my-day":
        return this.openMyDay();
      case "brief":
        return void this.openBrief();
      case "brief-send":
        return void this.sendBrief();
      case "import":
        return void this.importConversation();
      case "tags":
        return void this.tagSession();
      case "sidebar":
        return this.shutSide(!this.sideShut);
    }
    // The left is the sessions, always: any other list opens on the right.
    if (place === "sessions") return this.shutSide(false);
    if (place) return this.sideTo(place, true);
    this.draw();
  }

  // toDiff brings the Diff tab forward on the file it had, or the first changed.
  private toDiff(): void {
    const held = this.tabs.find((t) => t.id === DIFF);
    const empty = !held || ("kind" in held.what && held.what.kind === "diff");
    const path = this.picked.get("review") ?? this.changes[0]?.path;
    if (empty && path && this.changes.some((c) => c.path === path)) return this.openFile(path);
    if (!held) return this.show(diffTab().what, "Diff", "diff");
    this.active = DIFF;
    this.remember();
    this.draw();
  }

  // sideTo opens a list beside the bar, or shuts it when it is the one showing.
  private sideTo(place: string, keep = false): void {
    this.side = this.side === place && !keep ? "" : place;
    localStorage.setItem("ade.side", this.side);
    this.draw();
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
  ): Promise<void> {
    if (!window.ade) return;
    this.say("reading…");
    const got = await window.ade
      .read(from, path, this.session)
      .catch((e: Error) => ({ error: e.message }));
    const err = (got as { error?: string }).error;
    const body = err
      ? `# ${render}\n\n${err}`
      : REPORTS[render]?.(got) ?? `# ${render}\n\nNothing to draw.`;
    this.said = "";
    this.show({ kind: "doc", title: render, body }, render, "eye");
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
      rules: { deny: got.deny ?? [], file: got.file ?? "" },
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
      link: (href) => void window.ade!.link(href),
      push: (id, said) => pushTo(id, said),
      follow: (dir) => this.follow(dir),
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

  // servedAt is whether an ade already answers for a session. Its
  // checkout counts: one server answers for every conversation in it.
  private servedAt(session: string): boolean {
    const dir = this.where.get(session);
    return this.announced.some(
      (each) => each.id === session || (Boolean(dir) && each.dir === dir),
    );
  }

  private repoOf(session: string): string {
    const dir = this.where.get(session);
    const repo = this.repos.find((r) => (r.checkouts ?? []).some((c) => c.path === dir));
    return repo ? `${repo.owner ? repo.owner + "/" : ""}${repo.name}` : "";
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


  // open is a row clicked, in the place it was clicked in: a pull
  // request is in two lists at once.
  private open(row: Row, from = this.place): void {
    this.place = from;
    this.picked.set(from, row.id);
    if (from !== "sessions") return this.openRow(row);
    // A different session is a different diff, and its own set of tabs.
    if (this.session !== row.id) {
      this.goTo(row.id);
      if (!this.servedAt(row.id)) this.serving = "starting";
      void this.read();
    }
    this.draw();
  }

  // What a row opens as.
  private openRow(row: Row): void {
    if (row.id.startsWith("who:")) return void this.conversation(row.id.slice("who:".length));
    if (this.place === "spaces") return this.openSpace(row.id.slice("space:".length));
    // A pull request opens as a session on its diff, as one from a link does.
    const pr = this.place === "prs" ? (row.pr ?? prAt(row.id)) : undefined;
    if (pr) return void this.openPR(pr, row.name);
    // Not something ade holds: it lives on a website, so it opens there.
    if (row.url) {
      void window.ade?.link(row.url);
      this.say(`opened ${row.url}`);
      return;
    }
    this.say(`${row.name}: nothing opens this`);
  }

  // markJobSeen puts My Day's reports away, which takes the alert off its button.
  private async markJobSeen(name: string): Promise<void> {
    await window.ade?.act("launcher", "/api/jobs/seen", { name }).catch(() => undefined);
    void this.read();
  }

  // openSpace draws a repository: the sessions in it, and a way to start one.
  private openSpace(key: string): void {
    // The space shown stands in when a read came back without it.
    const space =
      this.openSpaces.find((s) => s.repo === key) ??
      (this.spaceShown?.repo === key ? this.spaceShown : undefined);
    if (!space) return;
    this.spaceShown = space;
    this.show({ kind: "space", title: space.repo, seen: this.seenOf(space) }, space.repo, "space");
  }

  // nowSeen is a space page's content as it is now: the lists move while it is open.
  private nowSeen(was: Seen): Seen {
    return this.seenOf(this.openSpaces.find((s) => s.repo === was.space.repo) ?? was.space);
  }

  private seenOf(space: Space): Seen {
    const repo = this.repos.find((r) => keyOf(r) === space.repo);
    return { space, sessions: sessionsOf(repo, Date.now(), this.goneSessions) };
  }

  // spaceAct is a link on that page.
  private spaceAct(did: SpaceAct): void {
    const space = this.spaceShown;
    if (!space) return;
    if (did.what === "new-session") {
      if (!space.path) return this.say(`${space.repo} is not cloned on this machine`);
      return this.show({ panel: `in:${space.path}` }, "New session", "session");
    }
    if (did.what === "close-session") return void this.endSession(did.id);
    const row = this.world.places.find((p) => p.id === "sessions")?.rows.find((r) => r.id === did.id);
    if (row) this.open(row, "sessions");
  }

  // openPR checks a listed pull request out and lands on its diff.
  private async openPR(at: { repo: string; number: number }, name: string): Promise<void> {
    const agent = await this.whichAgent();
    if (!agent) return;
    this.say(`checking out ${name}…`, true);
    const got = await window.ade
      ?.act("launcher", "/api/reviews/open", { repo: at.repo, number: at.number })
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got as real.Answer);
    if (back.error) return this.say(back.error);
    await this.landOnReview((back.body as { path?: string } | undefined)?.path ?? "", agent);
  }

  // mutePR puts a review request aside, or brings it back.
  private async mutePR(at: { repo: string; number: number }, muted: boolean): Promise<void> {
    const got = await window.ade
      ?.act("launcher", "/api/reviews/mute", { repo: at.repo, number: at.number, muted })
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got as real.Answer);
    this.say(back.error || (muted ? "put aside" : "brought back"));
    await this.again("prs");
  }

  // openPRLink checks a pasted pull request out and lands on it. The
  // launcher does the cloning, the worktree and the space.
  private async openPRLink(): Promise<void> {
    const link = await this.omnibox.type("Paste a pull request link", "");
    this.omnibox.close();
    if (!link?.trim()) return;
    const agent = await this.whichAgent();
    if (!agent) return;
    this.say(`checking out ${link.trim()}; cloning first if you do not have it…`, true);
    const got = await window.ade!
      .act("launcher", "/api/reviews/open", { link: link.trim() })
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got as real.Answer);
    if (back.error) return this.say(back.error);
    await this.landOnReview((back.body as { path?: string } | undefined)?.path ?? "", agent);
  }

  // landOnReview is a pull request just checked out: an agent seated in it,
  // and the window landing on its diff once it announces.
  private async landOnReview(path: string, agent: string): Promise<void> {
    if (!path) return this.say("it was opened, but nothing said where");
    if (agent) await this.seatAgent(path, agent);
    this.landing = path;
    this.landingFor = "review";
    this.landingSince = Date.now();
    this.say(`checked out in ${path}`);
    await this.read();
  }

  // claimReview takes a review a teammate asked for over agentbus and opens
  // it exactly as Open PR from link does.
  private async claimReview(id: string): Promise<void> {
    const agent = await this.whichAgent();
    if (!agent) return;
    this.say("claiming the review and checking it out…", true);
    const got = await window.ade
      ?.act("launcher", "/api/bus/claim", { id, open: true })
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got as real.Answer);
    if (back.error) return this.say(back.error);
    await this.landOnReview((back.body as { path?: string } | undefined)?.path ?? "", agent);
  }

  // answerOffer is a teammate's question, answered in the omnibox: one of
  // its options when it gave some, your own words otherwise.
  private async answerOffer(id: string): Promise<void> {
    const offer = this.bus?.offers?.find((one) => one.id === id);
    if (!offer) return this.say("that question is no longer waiting");
    const title = `${offer.from} asks: ${offer.question ?? ""}`;
    let option = "";
    if (offer.options?.length) {
      const rows = offer.options.map((one) => ({ label: one, value: one }));
      const picked = await this.omnibox.ask(title, [...rows, { label: "Something else…", value: "" }]);
      this.omnibox.close();
      if (typeof picked !== "string") return;
      option = picked;
    }
    const text = option ? "" : ((await this.omnibox.type(title, ""))?.trim() ?? "");
    this.omnibox.close();
    if (!option && !text) return;
    const got = await window.ade
      ?.act("launcher", "/api/bus/answer", { id, option, text })
      .catch((e: Error) => ({ error: e.message }));
    this.say(real.answered(got as real.Answer).error || `answered ${offer.from}`);
    void this.read();
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
    if (!this.shell) this.shell = this.build();
    const { titlebar, activity, sidebar, files, side, head, sheet, statusbar } = this.shell;
    const { sideGrip, filesGrip, listGrip } = this.shell;
    // With nothing being dragged nothing should be shaded, whatever left it:
    // the poll redraws every few seconds, so a stuck hint cannot outlive one.
    if (!this.dragging) this.hint(undefined);
    titlebar.replaceChildren(...this.titlebar());
    activity.replaceChildren(...this.activity());

    // The left is the sessions, whatever else is clicked.
    sidebar.hidden = sideGrip.hidden = this.sideShut;
    this.fill(sidebar, this.listOf(this.placeAt("sessions")));
    // The Diff's files, comments and commit box, beside the diff they are of.
    const diffing = Boolean(this.session) && this.actives.includes(DIFF);
    files.hidden = filesGrip.hidden = !diffing;
    if (diffing) this.fill(files, [...this.listOf(this.placeAt("review")), this.reviewStrip(), this.commentList()], this.commit.el);
    // One of the bar's lists, or nothing.
    const beside = this.world.places.find((p) => p.id === this.side && SIDE.includes(p.id));
    side.hidden = listGrip.hidden = !beside;
    if (beside) this.fill(side, this.listOf(beside));

    if (!this.dragging) head.replaceChildren(...this.head());
    statusbar.replaceChildren(...this.statusbar());

    // A built pane is kept: hidden, not taken out, because an iframe reloads
    // when it moves and a terminal reconnects.
    for (const tab of this.tabs) {
      const want = this.paneKey(tab);
      const wait = Boolean(this.waiting(tab));
      const g = groupOf(tab);
      const front = tab.id === this.actives[g];
      let held = this.panes.get(tab.id);
      let was = 0;
      if (held && rebuilt(held, want, wait)) {
        // Where you had read to: a redrawn diff must not throw it away.
        was = held.node.scrollTop;
        held.node.remove();
        held = undefined;
      }
      // Built when first looked at, so a shell starts when it is wanted. The
      // agent is the exception: picking its session is what resumes it.
      if (!held && !front && tab.id !== AGENT) continue;
      if (!held) {
        const node = el("div", "pane");
        node.append(this.inPane(tab));
        held = { node, key: want, waiting: wait };
        this.panes.set(tab.id, held);
        sheet.append(node);
        if (was) node.scrollTop = was;
      }
      // Only the pane's edges move: a reparented iframe would reload.
      const hid = held.node.hidden;
      held.node.hidden = !front;
      // Back in front, so its panel reads the stir it sat out while hidden.
      if (hid && !held.node.hidden) void shown(held.node);
      held.node.classList.toggle("left", this.sideBySide && g === 0);
      held.node.classList.toggle("right", this.sideBySide && g === 1);
    }
    sheet.classList.toggle("two", this.sideBySide);
    head.classList.toggle("two", this.sideBySide);
    // A save is about the focused half's editor.
    focusEditor(this.focus);
    // The sheet's own line, not the one a waiting pane holds.
    const empty = sheet.querySelector(":scope > .nothing");
    if (this.tabs.length === 0 && !empty) sheet.append(nothingOpen());
    if (this.tabs.length > 0 && empty) empty.remove();
  }

  // fill redraws one list only when it draws differently: a rebuild under the
  // pointer takes the node a press started on, and the click never lands.
  private fill(host: HTMLElement, nodes: HTMLElement[], keep?: HTMLElement): void {
    const on = document.activeElement;
    const typing = host.contains(on) && Boolean(on?.classList.contains("find"));
    // Where you had scrolled to is kept across a rebuild.
    const read = host.querySelector(".list")?.scrollTop ?? 0;
    const drawn = el("div");
    drawn.append(...nodes);
    if (drawn.innerHTML !== this.drawnHTML.get(host)) {
      this.drawnHTML.set(host, drawn.innerHTML);
      // A kept node stays where it is: moving it would take its focus.
      for (const old of [...host.children]) if (old !== keep) old.remove();
      if (keep) keep.before(...drawn.childNodes);
      else host.append(...drawn.childNodes);
      const list = host.querySelector(".list");
      if (list && read) list.scrollTop = read;
      if (typing) {
        const find = host.querySelector<HTMLInputElement>(".find");
        find?.focus();
        find?.setSelectionRange(find.value.length, find.value.length);
      }
    }
    // Only when the pick moved. On every draw it dragged the list back
    // under the pointer a second after you scrolled away from it.
    const pick = host.querySelector(".row.on, .twig.on");
    const key = pick?.textContent ?? "";
    if (key === this.revealed.get(host)) return;
    this.revealed.set(host, key);
    pick?.scrollIntoView({ block: "nearest" });
  }

  private placeAt(id: string): Place {
    return this.world.places.find((p) => p.id === id) ?? { id, title: id, icon: "dot", rows: [] };
  }

  // paneKey is what a pane was built with: a session and an address for a
  // live one, and for a diff everything it draws beyond the diff itself.
  private paneKey(tab: Tab): string {
    // A terminal that would not start is redrawn when that changes.
    const off = this.stuck(tab) ? ":off" : "";
    if (!("panel" in tab.what) && tab.what.kind === "agent") {
      return `${this.session}:${this.launcher?.base ?? ""}:${this.agentEnded() ? "ended" : "up"}${off}`;
    }
    if (live(tab)) return `${this.session}:${this.launcher?.base ?? ""}${off}`;
    const view = tab.what;
    if ("panel" in view) return "";
    // A space is its documents and its conversations, which move while the
    // page is open: archiving one is the whole point of the Archive on it.
    if (view.kind === "space") return spaceDrawnFrom(this.nowSeen(view.seen));
    if (view.kind === "doc" && view.again) return `${view.again}:${view.body}`;
    if (view.kind !== "hunks") return "";
    if (view.editing) return `editing:${view.file.path}`;
    const how = this.split ? "split" : "unified";
    return `${how}:${drawnFrom(view.file.path, this.threads, this.openThreads, Boolean(this.world.status.pr))}`;
  }

  // waiting is why a panel tab cannot draw its page yet, or empty when
  // it can: a page built now would fetch the proxy's 404 and print it.
  private waiting(tab: Tab): string {
    if (!live(tab)) return "";
    if (!this.sessionAt()) return this.session ? "Waiting for ade…" : "Pick a session.";
    if (this.stuck(tab)) return "This session would not start. Click “session would not start” in the status bar to try again.";
    return this.serving === "starting" ? "Starting ade for this checkout…" : "";
  }

  // stuck is a terminal whose ade did not come up: a dial now is a 404 and nothing else.
  private stuck(tab: Tab): boolean {
    return this.serving === "failed" && !("panel" in tab.what);
  }

  // show opens something in the editor area. The same thing twice is
  // the same tab, with whatever it now says.
  private show(what: Tab["what"], title: string, icon = "file"): void {
    const id = idOf(what);
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
    this.commit.reset();
    this.remember();
    for (const pane of this.panes.values()) pane.node.remove();
    this.panes.clear();
    this.tabs = [];
    this.actives = ["", ""];
    this.focus = 0;
    this.sideBySide = false;
    this.session = session;
    this.tabsFor = session;
    // Until its ade answers, no pane dials it. Picking it earns one more start.
    if (session && !this.servedAt(session)) {
      this.serving = "starting";
      this.woke.delete(session);
    }
    this.reopen();
  }

  // reopen brings back what a session had open: its three tabs, the panels it
  // can open from an id alone, and which half each was in.
  private reopen(): void {
    if (this.tabs.length > 0) return;
    if (!this.session) return;
    this.seat();
    const kept = this.keptTabs.get(this.tabsFor);
    const right = kept?.right ?? [];
    for (const id of [...(kept?.tabs ?? []), ...right]) {
      if (id.startsWith("panel:")) this.run(id.slice("panel:".length));
    }
    for (const id of right) this.tabs = moved(this.tabs, id, 1);
    if (kept?.at) {
      this.splitAt = kept.at;
      this.saySplit();
    }
    const here = (id: string | undefined) => (id && this.tabs.some((t) => t.id === id) ? id : "");
    const last = (g: Group) => inGroup(this.tabs, g).slice(-1)[0]?.id ?? "";
    this.actives = [here(kept?.active) || AGENT, here(kept?.rightOn) || last(1)];
    this.settle();
    this.remember();
    this.draw();
  }

  // badged follows the agents' terminals: one looked at is seen, and one
  // that needs you while you are elsewhere says so.
  private badged(doing: real.Doing, active: real.Active[]): void {
    const looking = document.hasFocus() && this.actives.includes(AGENT);
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

  // sendFindings hands the open comments to the session's agent, then shows it.
  private async sendFindings(): Promise<void> {
    const got = await window.ade
      ?.act("session", "/api/agent/findings", {}, this.session)
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got as real.Answer);
    if (back.error) return this.say(back.error);
    this.say(`sent ${(back.body as { sent?: number })?.sent ?? 0} findings to the agent`);
    this.run("ade.openAgent");
  }

  // commitDoes is the commit box's reach into the picked session.
  private commitDoes(): Does {
    const post = async (path: string, body: Record<string, unknown>, said: (back: Record<string, unknown>) => string) => {
      const got = await window.ade
        ?.act("session", path, body, this.session)
        .catch((e: Error) => ({ error: e.message }));
      const back = real.answered(got as real.Answer);
      if (back.error) return back.error;
      const text = said((back.body ?? {}) as Record<string, unknown>);
      if (text) this.say(text);
      void this.read();
      return "";
    };
    return {
      write: () => post("/api/commit-message", {}, () => "writing a commit message"),
      commit: (title, body) => post("/api/commit", { title, body }, (back) => `committed ${String(back.sha ?? "")}`),
      push: () => post("/api/push", {}, (back) => `pushed to ${String(back.upstream || "the remote")}`),
      // No paths: the session's own git decides what "all" is, at the moment of the press.
      stageAll: () => post("/api/stage", { all: true }, (back) => `staged ${String(back.staged ?? 0)} file(s)`),
    };
  }

  // endedCard stands where an agent that exited was: resuming is asked for.
  private endedCard(): HTMLElement {
    const card = el("div", "nothing");
    const again = el("span", "btn", "Resume");
    again.addEventListener("click", () => void this.resumeAgent());
    card.append(el("p", "", "The agent in this session has ended."), again);
    return card;
  }

  // seat gives a session its three tabs, first in the strip.
  private seat(): void {
    if (!this.session) return;
    this.tabs = seated(this.tabs);
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
    this.wasOn = kept.session ?? "";
    for (const [session, one] of kept.kept ?? []) this.keptTabs.set(session, one);
    // What a run before this change wrote: one set, for whichever
    // conversation was open when it was written.
    if (!kept.kept && kept.tabs) {
      this.keptTabs.set(kept.session ?? "", { tabs: kept.tabs, active: kept.active ?? "" });
    }
  }

  private close(id = this.active): void {
    const tab = this.tabs.find((t) => t.id === id);
    if (!tab || standing(id)) return;
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
      const ids = sweeping(mine, id, which).filter((one) => !standing(one));
      const label = { others: "Close others", right: "Close to the right", left: "Close to the left", all: "Close all" };
      return { label: label[which], off: ids.length === 0, run: () => this.closeMany(ids) };
    };
    menu(at, [
      { label: "Close", off: standing(id), run: () => this.close(id) },
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
    const sidebar = el("div", "sidebar left");
    const files = el("div", "sidebar files");
    files.append(this.commit.el);
    const side = el("div", "sidebar side");
    const head = el("div", "head");
    const sheet = el("div", "sheet");
    const statusbar = el("div", "statusbar");
    const main = el("div", "main");
    const landing = el("div", "landing");
    landing.hidden = true;
    sheet.append(this.splitGrip(), landing);
    const sideGrip = this.grip("sidebar");
    const filesGrip = this.grip("files");
    const listGrip = this.grip("side");
    // The Diff's files sit under the strip, beside the diff they are of.
    const work = el("div", "work");
    work.append(files, filesGrip, sheet);
    main.append(head, work);
    const body = el("div", "body");
    body.append(sidebar, sideGrip, main, listGrip, side, activity);
    this.root.replaceChildren(titlebar, body, statusbar);
    this.root.className = "window";
    return { titlebar, activity, sidebar, files, side, head, sheet, statusbar, sideGrip, filesGrip, listGrip };
  }

  private titlebar(): HTMLElement[] {
    const box = el("div", "box", "⌘K  ·  anything");
    box.addEventListener("click", () => this.omnibox.show());
    return [el("div", "where", this.repoOf(this.session) || "ade"), box];
  }

  // The bar on the right: a button opens its list beside it, and closes it again.
  private activity(): HTMLElement[] {
    const bar: HTMLElement[] = [];
    for (const id of BAR) {
      const place = this.world.places.find((p) => p.id === id);
      if (!place) continue;
      const button = document.createElement("button");
      button.className = id === this.side ? "on" : "";
      if (place.alert) button.classList.add("alert");
      button.title = place.title;
      button.append(icon(place.icon, 21));
      const n = counted(place);
      if (n > 0) button.append(el("span", "count", String(n)));
      button.addEventListener("click", () => this.sideTo(id));
      bar.push(button);
      // Under the bell, where what the morning holds belongs.
      if (id === "waiting") bar.push(this.myDayButton());
    }
    return bar;
  }

  // Not a list: one click opens this morning's report as a tab.
  private myDayButton(): HTMLElement {
    const job = real.myDay(this.jobs);
    const unread = job ? real.jobUnread(job) : 0;
    const button = document.createElement("button");
    button.className = this.tabs.some((tab) => tab.id === "doc:My Day") ? "on" : "";
    if (unread) button.classList.add("alert");
    button.title = job ? real.myDayTitle(job) : "My Day";
    button.append(icon("calendar", 21));
    if (unread) button.append(el("span", "count", String(unread)));
    button.addEventListener("click", () => this.openMyDay());
    return button;
  }

  // refreshMyDay puts the next fire into the tab already showing the last one.
  // In place rather than through show(), which would pull focus to it.
  private refreshMyDay(): void {
    const body = real.myDayReport(real.myDay(this.jobs));
    const again = this.myDayAgain();
    this.tabs = this.tabs.map((tab) =>
      tab.id === "doc:My Day" && "kind" in tab.what && tab.what.kind === "doc" &&
      (tab.what.body !== body || tab.what.again !== again)
        ? { ...tab, what: { ...tab.what, body, again } }
        : tab,
    );
  }

  // openMyDay draws the newest fire and takes the alert off it. The title is
  // the same every day so the tab is reused; the report dates its own heading.
  private openMyDay(): void {
    const job = real.myDay(this.jobs);
    const view = { kind: "doc" as const, title: "My Day", body: real.myDayReport(job), again: this.myDayAgain() };
    this.show(view, "My Day", "calendar");
    if (job && real.jobUnread(job)) void this.markJobSeen(job.name);
  }

  private myDayAgain(): "idle" | "busy" {
    return this.myDayBuilding || real.myDay(this.jobs)?.running ? "busy" : "idle";
  }

  // buildMyDay runs the report now. Unasked is the first open of a weekday,
  // once a day from this window, and says nothing if it cannot.
  private async buildMyDay(asked: boolean): Promise<void> {
    const today = new Date().toDateString();
    if (this.myDayBuilding || (!asked && this.myDayAsked === today)) return;
    this.myDayAsked = today;
    this.myDayBuilding = true;
    // Unasked is called from inside a read, which draws when it is done.
    if (asked) {
      this.refreshMyDay();
      this.draw();
    }
    const got = await window.ade
      ?.act("launcher", "/api/jobs/run", { name: real.MY_DAY })
      .catch((e: Error) => ({ error: e.message }));
    this.myDayBuilding = false;
    const back = real.answered(got as real.Answer);
    if (!asked) return this.soon(300);
    if (back.error) this.say(back.error);
    await this.read();
  }

  // grip drags one edge wider, by one variable on the root. The lists on the
  // right grow leftwards, so a pointer moving left widens them.
  private grip(which: Edge): HTMLElement {
    const grip = el("div", "grip");
    const { key, css, back } = WIDTHS[which];
    // Pointer capture, and the panes made deaf: an iframe hit-tests the
    // pointer itself, so a drag over one never saw the mouse come up.
    grip.addEventListener("pointerdown", (down) => {
      down.preventDefault();
      grip.setPointerCapture(down.pointerId);
      grip.classList.add("held");
      document.body.classList.add("dragging");
      const from = this.widths[which];
      const at = down.clientX;
      const move = (e: PointerEvent) => {
        this.widths[which] = sideWidth(which, from + (back ? at - e.clientX : e.clientX - at));
        document.documentElement.style.setProperty(css, `${this.widths[which]}px`);
      };
      const done = () => {
        grip.classList.remove("held");
        document.body.classList.remove("dragging");
        grip.removeEventListener("pointermove", move);
        grip.removeEventListener("lostpointercapture", done);
        localStorage.setItem(key, String(this.widths[which]));
      };
      grip.addEventListener("pointermove", move);
      // Not pointerup: a capture lost any other way has to clear the drag too.
      grip.addEventListener("lostpointercapture", done);
    });
    return grip;
  }

  // listOf is one place drawn as a list: its heading and filter, then its lines.
  private listOf(place: Place): HTMLElement[] {
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
      const rows = want ? place.rows.filter((r) => matches(r, want)) : place.rows;
      for (const group of grouped(rows)) {
        const shut = this.folded.has(`${place.id}:${group.name}`);
        if (group.name) list.append(this.heading(place.id, group, shut));
        if (shut) continue;
        for (const row of group.rows) list.append(this.row(row, place.id));
      }
      if (rows.length === 0) list.append(this.note(this.empty(place, want)));
    }
    return [this.head2(place), list];
  }

  // reviewStrip is a review by named agents as it goes: each reviewer, then
  // the stage. It stays when the review is over, until it is shut.
  private reviewStrip(): HTMLElement {
    const out = el("div", "review-run");
    const run = this.reviewRun;
    if (!run || run.startedAt === this.reviewShut) return out;
    const head = el("div", "head", real.reviewHead(run));
    if (!run.running) {
      const shut = el("button", "shut", "×");
      shut.title = "Put this away";
      shut.addEventListener("click", () => {
        this.reviewShut = run.startedAt;
        this.draw();
      });
      head.append(shut);
    }
    out.append(head);
    for (const one of real.reviewLines(run)) {
      const row = el("div", `who ${one.state}`);
      row.append(el("span", "dot"), el("span", "name", one.name), el("span", "note", one.note));
      row.title = `${one.name} ${one.note}`;
      out.append(row);
    }
    if (run.running && run.stage) out.append(el("div", "stage", run.stage));
    if (run.summary) out.append(el("div", "summary", run.summary));
    return out;
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
      this.place = place.id;
      this.filters.set(place.id, find.value);
      this.draw();
    });
    const again = el("button", "again");
    again.title = "Refresh: ask GitHub and ade again";
    again.append(icon("refresh", 14));
    again.addEventListener("click", () => void this.again(place.id));
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
      ? place.rows.filter((r) => matches(r, want))
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
    const open = listed(this.threads).length;
    if (open > 0) {
      const send = el("span", "btn quiet", `Send ${open} to the agent`);
      send.title = "Writes the open comments to a file and asks the agent to work through it.";
      send.addEventListener("click", () => void this.sendFindings());
      bar.append(send);
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
    if (this.omnibox.showing()) return;
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
      ? place.rows.filter((r) => matches(r, want))
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
    if (this.picked.get("review") === node.path) row.className += " on";
    if (this.marks[node.path]) row.className += " viewed";
    for (let i = 0; i < depth; i++) row.append(el("span", "pad guide"));

    const chev = el("span", node.dir ? (open ? "chev open" : "chev") : "chev leaf");
    chev.append(icon("chevron", 13));
    const ico = el("span", node.dir ? "ico dir" : "ico");
    ico.append(icon(node.dir ? (open ? "folder-open" : "folder") : "file", 15));
    row.append(chev, ico, el("span", "nm", node.name));
    if (node.status) row.append(el("span", "mark", node.status[0].toUpperCase()));

    row.addEventListener("click", () => {
      this.place = "review";
      this.picked.set("review", node.path);
      if (node.dir) {
        this.unfolded = toggled(this.unfolded, node.path);
        this.draw();
        return;
      }
      this.openFile(node.path);
    });
    return row;
  }

  // openFile puts a changed file in the Diff tab: one tab, whatever it shows.
  private openFile(path: string, at?: At): void {
    const file = this.changes.find((c) => c.path === path);
    if (!file) return this.say(`${path} is not in this diff`);
    this.picked.set("review", path);
    this.show({ kind: "hunks", title: path, file, threads: this.threads, at }, "Diff", "diff");
  }

  private shutSide(on: boolean): void {
    this.sideShut = on;
    this.draw();
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
    const editable = view?.kind === "hunks" && view.editing;
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

  private row(row: Row, place: string): HTMLElement {
    // The session the window landed on is the pick until another is made.
    const pick = this.picked.get(place) ?? (place === "sessions" ? this.session : "");
    const node = el("div", pick === row.id ? "row on" : "row");
    const ico = el("span", row.tone ? `ico ${row.tone}` : "ico");
    ico.append(icon(row.icon));
    node.append(ico, el("span", "name", row.name));
    // A session's name keeps its line: its tags and facts go under it.
    const more = place === "sessions" ? el("span", "more") : node;
    if (more !== node) node.classList.add("session");
    if (row.tags?.length) more.append(this.tags(row));
    if (row.meta) more.append(el("span", "meta", row.meta));
    if (row.tag) node.append(el("span", `tag ${row.tag.kind}`, row.tag.text));
    // What the line does besides open, shown on hover.
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
    if (more !== node) node.append(more);
    node.addEventListener("click", () => this.open(row, place));
    return node;
  }

  // tags sit beside a session's name. The brief's is a button: it opens the brief.
  private tags(row: Row): HTMLElement {
    const box = el("span", "tags");
    for (const tag of row.tags ?? []) {
      const node = el("span", `tag ${tag.kind}`, tag.text);
      if (tag.kind === "brief") {
        node.title = "Open the brief";
        node.addEventListener("pointerdown", (e) => {
          e.preventDefault();
          e.stopPropagation();
          void this.openBrief(row.id);
        });
        node.addEventListener("click", (e) => e.stopPropagation());
      }
      box.append(node);
    }
    return box;
  }

  // rowAct is the one thing a line does besides open: which verb, and on what.
  private async rowAct(act: NonNullable<Row["acts"]>[number]): Promise<void> {
    switch (act.what) {
      case "session-end":
        return this.endSession(act.name);
      case "session-tags":
        return this.tagSession(act.name);
      case "brief-open":
        return this.openBrief(act.name);
      case "brief-send":
        return this.sendBrief(act.name);
      case "space-close":
        return this.space("close", act.name);
      case "space-clone":
        return this.space("clone", act.name);
      case "pr-web":
        return void window.ade?.link(act.name);
      case "pr-mute":
      case "pr-unmute": {
        const at = prAt(act.name);
        return at ? this.mutePR(at, act.what === "pr-mute") : undefined;
      }
      case "bus-claim":
        return this.claimReview(act.name);
      case "bus-answer":
        return this.answerOffer(act.name);
      case "bus-dismiss": {
        const team = this.world.places.find((p) => p.id === "team");
        if (team) this.redrawPlace(without(team, new Set([act.name])));
        const got = await window.ade
          ?.act("launcher", "/api/bus/dismiss", { id: act.name })
          .catch((e: Error) => ({ error: e.message }));
        this.say(real.answered(got as real.Answer).error || "dismissed");
        return void this.read();
      }
    }
  }

  // endSession closes a session: its ade stops and its tab goes. Asked first
  // only when its agent is in the middle of something.
  private async endSession(id: string): Promise<void> {
    if (!window.ade || !id) return;
    const up = this.announced.find((a) => a.id === id);
    if (real.running(real.stateOf(up?.agent))) {
      const picked = await this.omnibox.ask("Its agent is still working. Close the session anyway?", [
        { label: "Close it", value: "yes" },
        { label: "Leave it running", value: "" },
      ]);
      this.omnibox.close();
      if (picked !== "yes") return;
    }
    this.goneSessions.add(id);
    const sessions = this.world.places.find((p) => p.id === "sessions");
    if (sessions) this.redrawPlace(without(sessions, this.goneSessions));
    // Only a served one has an ade to stop: its checkout may hold older sessions.
    const dir = up ? (up.dir ?? this.where.get(id) ?? "") : "";
    const back = real.answered((await this.stopAndPutAway(id, dir)) as real.Answer);
    if (back.error) {
      this.goneSessions.delete(id);
      this.say(back.error);
    } else if (id === this.session) {
      this.goTo("");
    }
    void this.read();
  }

  // onSession moves to a session an act names, so what follows is about it.
  private async onSession(id: string): Promise<void> {
    if (!id || id === this.session) return;
    this.goTo(id);
    this.picked.set("sessions", id);
    if (!this.servedAt(id)) this.serving = "starting";
    await this.read();
  }

  // briefRead asks the session's ade for its brief, waiting out one just started.
  private async briefRead(): Promise<{ path?: string; body?: string; error?: string }> {
    let got: { path?: string; body?: string; error?: string } = {};
    for (let tries = 0; tries < 10; tries++) {
      got = (await window.ade
        ?.read("session", "/api/brief", this.session)
        .catch((e: Error) => ({ error: e.message }))) as typeof got;
      if (!real.unserved(got?.error)) break;
      await new Promise((done) => setTimeout(done, 1_000));
    }
    return got ?? {};
  }

  // openBrief shows a session's brief as a tab, with the button that sends it.
  private async openBrief(id = this.session): Promise<void> {
    if (!window.ade) return;
    await this.onSession(id);
    if (!this.session) return this.say("pick a session first");
    const got = await this.briefRead();
    if (got.error || !got.path) return this.say(got.error || "this session has no brief yet");
    const name = got.path.split("/").pop() ?? "Brief";
    this.show({ kind: "doc", title: `Brief: ${name}`, body: got.body ?? "", brief: true }, "Brief", "file");
  }

  // sendBrief types one line at the agent: read the brief, say where it leaves off.
  private async sendBrief(id = this.session): Promise<void> {
    if (!window.ade) return;
    await this.onSession(id);
    if (!this.session) return this.say("pick a session first");
    const got = await window.ade
      .act("session", "/api/agent/brief", {}, this.session)
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got as real.Answer);
    if (back.error) return this.say(back.error);
    this.say("asked the agent to read the brief");
    this.run("ade.openAgent");
  }

  // importConversation takes up a conversation claude holds, wherever it was
  // had: a session in its checkout, or the one already there moved onto it.
  private async importConversation(): Promise<void> {
    if (!window.ade) return;
    const first = encodeURIComponent(this.where.get(this.session) ?? "");
    const got = await window.ade
      .read("launcher", `/api/conversations?first=${first}`)
      .catch((e: Error) => ({ error: e.message }));
    const listed = real.answered(got as real.Answer);
    if (listed.error) return this.say(listed.error);
    const held = (listed.body as { conversations?: real.Conversation[] })?.conversations ?? [];
    if (held.length === 0) return this.say("claude holds no conversation on this machine");
    const picked = await this.omnibox.ask("Import which conversation?", real.conversationRows(held));
    this.omnibox.close();
    if (typeof picked !== "string") return;
    this.say("importing the conversation…", true);
    const moved = await window.ade
      .act("launcher", "/api/conversations/import", { id: picked })
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(moved as real.Answer);
    if (back.error) return this.say(back.error);
    const at = (back.body ?? {}) as { session?: string; dir?: string };
    const id = at.session || picked;
    if (at.dir) this.where.set(id, at.dir);
    this.say("imported");
    await this.onSession(id);
    this.run("ade.openAgent");
  }

  // tagSession edits the tags typed by hand: the ticket and the brief are ade's own.
  private async tagSession(id = this.session): Promise<void> {
    if (!window.ade) return;
    if (!id) return this.say("pick a session first");
    const typed = await this.omnibox.type("Tags, with spaces between", real.ownTags(this.repos, id).join(" "));
    this.omnibox.close();
    if (typed === undefined) return;
    const got = await window.ade
      .act("launcher", "/api/session/tags", { id, tags: real.typedTags(typed) })
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got as real.Answer);
    if (back.error) return this.say(back.error);
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

  // The strip, or one strip per half. Each is its own scroller, so a
  // long row of tabs on one side does not push the other off screen.
  private head(): Element[] {
    if (this.tabs.length === 0) return [el("span", "", "Nothing open")];
    return (this.sideBySide ? [0, 1] : [0]).map((g) => {
      const side = g === 0 ? "left" : "right";
      const strip = el("div", g === this.focus ? `strip ${side} focus` : `strip ${side}`);
      strip.append(...this.strip(g as Group));
      strip.addEventListener("pointerdown", (e) => {
        if (e.target === strip) this.focusOn(g as Group);
      });
      return strip;
    });
  }

  private focusOn(g: Group): void {
    if (this.focus === g) return;
    this.focus = g;
    this.draw();
  }

  private strip(group: Group): Element[] {
    return inGroup(this.tabs, group).map((tab) => {
      const node = el("div", tab.id === this.actives[group] ? "tab on" : "tab");
      if (standing(tab.id)) node.classList.add("standing");
      node.append(icon(tab.icon, 13), el("span", "nm", tab.title));
      node.title = "kind" in tab.what && tab.what.kind === "hunks" ? tab.what.title : tab.title;
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
      // The three every session has do not close.
      if (!standing(tab.id)) {
        const x = el("span", "x", "✕");
        x.title = "Close";
        x.addEventListener("click", (e) => {
          e.stopPropagation();
          this.close(tab.id);
        });
        node.append(x);
      }
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
        // A capture never granted is never lost, and the hint stayed drawn
        // over the panes with nothing left to clear it.
        window.removeEventListener("pointerup", done);
        window.removeEventListener("pointercancel", done);
        if (!this.dragging) return;
        this.dragging = false;
        document.body.classList.remove("tabbing");
        this.hint(undefined);
        this.dropTab(id, x);
        this.draw();
      };
      head.addEventListener("pointermove", move);
      // Not pointerup alone: a capture lost any other way has to clear it too.
      head.addEventListener("lostpointercapture", done);
      window.addEventListener("pointerup", done);
      window.addEventListener("pointercancel", done);
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

  // inPane is what one tab holds, built once.
  private inPane(tab: Tab): HTMLElement {
    if ("panel" in tab.what) {
      const which = tab.what.panel;
      // New session opened from a space starts with that repository ticked.
      const inDir = which.startsWith("in:") ? which.slice(3) : "";
      const make = inDir
        ? (reach: Reach) => newSession(reach, { ...newSeed(), mode: "work", repos: [inDir] })
        : WHICH[which];
      if (!make) return el("div", "nothing", `no panel called ${which}`);
      const panel = make(this.reach());
      // A launcher page draws with no session picked: New session is how
      // you get one, and it used to say "Pick a session." instead.
      const wait = panel.launcher ? "" : this.waiting(tab);
      if (wait) return el("div", "nothing", wait);
      return frame(panel, `${tab.id}:${this.session}:${this.launcher?.base ?? ""}`);
    }
    const view = tab.what;
    if (view.kind === "term") {
      const wait = this.waiting(tab);
      if (wait) return el("div", "nothing", wait);
      return openTerm(this.session, (why) => this.say(why));
    }
    if (view.kind === "agent") {
      // Opening its pane is what starts the agent, so not before ade is up.
      const wait = this.waiting(tab);
      if (wait) return el("div", "nothing", wait);
      if (this.agentEnded()) return this.endedCard();
      return openTerm(this.session, (why) => this.say(why), AGENT);
    }
    if (view.kind === "hunks" && view.editing) {
      return mountDiff(groupOf(tab), view.base ?? "", view.source ?? "", view.title);
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
      {
        forget: (rule) => void this.forget(rule),
        thread: (asked) => void this.onThread(asked),
        ask: (who, text) => void this.askPerson(who, text),
        space: (did) => void this.spaceAct(did),
        again: () => void this.buildMyDay(true),
        send: () => void this.sendBrief(),
      },
      {
        hasPR: Boolean(this.world.status.pr),
        open: this.openThreads,
        split: this.split,
        viewed: new Set(Object.keys(this.marks)),
      },
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
      where.title = `${dir}: click to copy`;
      where.addEventListener("click", () => void this.copy(dir));
      bar.push(where);
    }

    // With no branch to name, the icon alone says nothing.
    if (status.branch) {
      const branch = el("div", "item branch");
      branch.append(icon("branch", 13), el("span", "", status.branch));
      bar.push(branch);
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
      bar.push(checks);
    }
    // What is running, by name: a review by three agents is three names.
    if (status.doing) {
      const run = el("div", "item running");
      run.append(el("span", "pulse"), el("span", "", status.doing));
      run.title = "⌘K “stop” to stop it";
      bar.push(run);
    }
    // Nothing serving the session fails every pane at once, so it is
    // one line, and not red until a start has been tried.
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
    if (this.said) {
      const saying = el("div", "item saying", this.said);
      saying.title = "Click to copy";
      saying.addEventListener("click", () => void this.copy(this.said));
      bar.push(saying);
    }
    bar.push(el("div", "gap"));
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
  files: HTMLElement;
  side: HTMLElement;
  head: HTMLElement;
  sheet: HTMLElement;
  statusbar: HTMLElement;
  sideGrip: HTMLElement;
  filesGrip: HTMLElement;
  listGrip: HTMLElement;
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
