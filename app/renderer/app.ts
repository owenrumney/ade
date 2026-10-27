import "./bridge";
import { REPORTS } from "./reports";
import * as real from "./real";
import type { Dash, Design, Repo, Review } from "./real";
import { icon } from "./icons";
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
import { owed, wired, type Ask, type Doing } from "./doing";
import { SOURCES, type Choice, type Held, type Pickable } from "./sources";
import { changesIn, drawnFrom, listed, toneOf, unsent, type Change, type Thread } from "./changes";
import { Kept } from "./kept";
import { frame, pushTo, stirred, tell } from "./panel";
import {
  dismissable,
  held,
  idOf,
  keepable,
  live,
  opened,
  rebuilt,
  shut,
  shutMany,
  sweeping,
  trimmed,
  type Layout,
  type Sweep,
  type Tab,
} from "./tabs";
import { isChat, sideWidth } from "./dock";
import { menu, type Row as MenuRow } from "./menu";
import { edits, mount, mountDiff, reaching, saved, selected } from "./code";
import { close as closeTerm, open as openTerm } from "./term";
import { newSession, written, WHICH, type Reach, type Written } from "./panels";
import { page as prPage } from "../../vscode/src/prpage";
import { prActs, type PrActId } from "./prpage";
import { plan, type Usage } from "../../vscode/src/usage";
import { refreshFor } from "./refresh";
import { inSpaces, keyOf, spaces as spacesPlace, type Space } from "./spaces";
import {
  designsOf,
  drawnFrom as spaceDrawnFrom,
  sessionsOf,
  type Seen,
  type SpaceAct,
} from "./spacepage";
import type { Repo as Stats } from "../../vscode/src/repo/view";
import type { BranchPR, Check } from "../../vscode/src/client";

// The shell. It owns the keyboard outright and hands the window named
// commands, never keystrokes. Absent only under `make app-shots`.

// How long a server gets to come up before this is a failure.
const startingFor = 8_000;

// What every list says while that server is on its way up.
const coming = "Starting ade for this checkout…";

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
  private goneDesigns = new Set<string>();
  private goneSpaces = new Set<string>();
  // Where the picked session's server is up to. "starting" is one
  // this window has in flight, which is not yet a failure.
  private serving: "ok" | "starting" | "failed" = "ok";
  private startedAt = 0;
  // Every announced session as of the last reading: how the window
  // knows whether a session it is about to show is served.
  private announced: real.Active[] = [];
  private todoList: real.Todo[] = [];
  private jobs: real.Job[] = [];
  // The diff is the largest thing the server holds, kept until the
  // version it publishes for exactly this moves.
  private diffNow = "";
  private diffHeld = new Kept<Change[]>([]);
  // The file list and the checkout's documents, kept the same way.
  private filesHeld = new Kept<{ path: string }[]>([]);
  private docsFor = "";
  private docsHeld: string[] = [];

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
  private where = new Map<string, string>();
  // What is open in the editor area. A pane is kept once built: moving
  // an iframe reloads it, so switching hides rather than rebuilds.
  private tabs: Tab[] = [];
  private active = "";
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
    this.announced = got.active ?? [];
    this.todoList = got.todos ?? [];
    this.jobs = got.jobs ?? [];
    // This read is the one that finds out whether anything is serving
    // the session, so the other three do not repeat its failure.
    const doing = await this.doing();
    // A pass while nothing serves the session shows what was last read.
    // See `stale` in kept.ts for why it may not write an empty one.
    const starting = this.serving === "starting";
    const [changes, files, docs] = starting
      ? [this.diffHeld.stale(coming), this.filesHeld.stale(coming), this.docsHeld]
      : await Promise.all([this.diff(), this.tree(), this.docs()]);
    this.draft = starting ? this.draft : await this.draftState();
    this.changes = changes.rows;
    this.world = {
      places: inBarOrder([
        real.waiting(dash, got.active ?? []),
        spacesPlace(this.openSpaces),
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
        real.designs(
          got.designs ?? [],
          docs,
          this.where.get(this.session) ?? "",
          this.openSpaces,
        ),
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
    this.world.places[2] = note(this.world.places[2], trouble.designs, "The designs store is empty.");
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
    // Asked for by hand, so nothing kept is trusted.
    this.diffHeld.stale("");
    this.filesHeld.stale("");
    this.docsFor = "";
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
    this.mine = ((got as { openPrs?: real.Listed[] | null })?.openPrs ?? []).filter((p) => p.number);
    this.bus = (got as { bus?: real.Bus })?.bus;
    this.threads = ((got as { threads?: Thread[] | null })?.threads ?? []).filter((t) => t.id);
    this.diffNow = String((got as { diffVersion?: string }).diffVersion ?? "");
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
    if (this.landingFor === "chat") this.run("ade.openChat");
    else this.place = "review";
    this.landingFor = "review";
    return "";
  }

  // follow is a session the New session panel just started.
  private follow(dir: string): void {
    this.close(idOf({ panel: "ade.newSession" }));
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

  private async diff(): Promise<{ rows: Change[]; why?: string }> {
    if (!window.ade) return { rows: [] };
    if (!this.session) return { rows: [], why: "Pick a session: the diff is one checkout's." };
    // Only when it has moved: a run writes a step a second and the diff
    // behind it does not change nearly that often.
    const kept = this.diffHeld.good(`${this.session}:${this.diffNow}`);
    if (kept) return kept;
    const got = await window.ade
      .read("session", "/api/diff", this.session)
      .catch((e: Error) => ({ error: e.message }));
    const bad = (got as { error?: string }).error;
    // Not remembered: a failure is worth asking about again.
    if (bad) return { rows: [], why: `/api/diff — ${bad}` };
    const rows = changesIn(got);
    const d = (got as { diff?: { mode?: string; base?: string } }).diff ?? {};
    const where = this.where.get(this.session) ?? "the picked session";
    const view = [d.mode, d.base && `against ${d.base}`].filter(Boolean).join(" ");
    const why =
      rows.length > 0
        ? ""
        : `Nothing in ${where}${view ? ` · ${view}` : ""}. ⌘K “scope” to change the view.`;
    return this.diffHeld.keep(`${this.session}:${this.diffNow}`, rows, why);
  }

  // draftState is how far the review has got, which the Review place says and
  // the verbs act on.
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

  private async docs(): Promise<string[]> {
    const dir = this.where.get(this.session);
    if (!window.ade || !dir) return [];
    // A checkout's documents are a directory walk that changes when
    // somebody writes one, which is not every three seconds.
    if (dir === this.docsFor) return this.docsHeld;
    const at = `/api/designs?dir=${encodeURIComponent(dir)}`;
    const got = (await window.ade.read("launcher", at).catch(() => ({}))) as {
      designs?: string[];
    };
    this.docsFor = dir;
    this.docsHeld = got.designs ?? [];
    return this.docsHeld;
  }

  private async tree(): Promise<{ rows: { path: string }[]; why?: string }> {
    if (!window.ade || !this.session) {
      return { rows: [], why: "Pick a session: Code is one checkout's." };
    }
    // On the diff's version too: a file added or removed moves it, and
    // nothing else changes what git lists.
    const at = `${this.session}:${this.diffNow}`;
    const kept = this.filesHeld.good(at);
    if (kept) return kept;
    const got = (await window.ade
      .read("session", "/api/tree", this.session)
      .catch((e: Error) => ({ error: e.message }))) as { files?: string[]; error?: string };
    if (got.error) return { rows: [], why: `/api/tree — ${got.error}` };
    return this.filesHeld.keep(at, (got.files ?? []).map((path) => ({ path })));
  }

  run(command: string): void {
    const what = wired(command);
    if (!what || what.at === "soon") return this.say(owed(command));
    if (what.at === "panel") {
      this.omnibox.close();
      if (isChat(what.panel)) return this.chat();
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
        return this.show({ kind: "term", title: "Terminal" }, "Terminal", "code");
      }
      if (what.place) this.place = what.place;
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
      const slot = `{${need.as}}`;
      // A row whose answer is an object names its own fields: the switcher
      // offers worktrees, which take a path, and branches, which take a name.
      if (said !== null && typeof said === "object") Object.assign(body, said);
      else if (path.includes(slot)) path = path.replace(slot, encodeURIComponent(String(said)));
      else body[need.as] = said;
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
    void this.read();
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
    dock.hidden = this.chatShut;
    chatGrip.hidden = this.chatShut;
    if (this.chatShut) return;
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
    const again = el("div", "x");
    again.append(icon("refresh", 12));
    again.title = "Read the conversation again";
    again.addEventListener("click", () => pushTo("chat", { kind: "reconnect" }));
    const shut = el("div", "x", "✕");
    shut.title = "Hide the chat";
    shut.addEventListener("click", () => this.shutChat());
    return [name, again, shut];
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
    } else if (asked.what === "unfold") {
      this.openThreads = toggled(this.openThreads, asked.id ?? "");
      return this.draw();
    } else if (asked.what === "convert") {
      this.say("writing it for the author…");
      got = await send(`/api/threads/${id}/convert`, {});
    } else if (asked.what === "draft") {
      const pr = this.world.status.pr;
      if (!pr) return this.say("this branch has no pull request");
      got = await send("/api/review/draft", { confirm: pr, thread: asked.id });
    } else if (asked.what === "take-out") {
      got = await send(`/api/review/draft/${id}`, {}, "DELETE");
    } else if (asked.what === "drop") {
      got = await send(`/api/threads/${id}`, {}, "DELETE");
    }
    const back = real.answered(got);
    if (back.error) return this.say(back.error);
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
      get: (at, path) => window.ade!.read(at, path, this.session),
      call: (at, path, method, body) => window.ade!.call(at, path, method, body, this.session),
      send: (at, path, body, method) => window.ade!.act(at, path, body, this.session, method),
      open: (path) => this.fromPanel(path),
      link: (href) => void window.ade!.link(href),
      run: (command) => this.run(command),
      push: (id, said) => pushTo(id, said),
      follow: (dir) => this.follow(dir),
      ask: (title, rows) => this.omnibox.ask(title, rows),
    };
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

  private say(said: string): void {
    this.said = said;
    this.draw();
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

  // viewFor is what a row opens as: a pull request through the
  // extension's own page builder, anything else as its document.
  private openRow(row: Row): void {
    // A session opens as its conversation, which is what it is.
    if (this.place === "sessions" || this.place === "agents") return this.chat();
    // A todo is a thing to do: opening one offers to start it. The
    // omnibox opens rows, so this is its way in as well as the list's.
    if (this.place === "todos") return this.fromTodo(row.id);
    if (row.id.startsWith("who:")) return void this.conversation(row.id.slice("who:".length));
    const reading = { kind: "doc" as const, title: row.name, body: `# ${row.name}\n\nReading…` };
    if (this.place === "designs") {
      // Either way it opens in the editor: one in this checkout through
      // the session serving it, one from the store through the launcher.
      const which = row.id.startsWith("space:")
        ? `spacedesign:${row.id.slice("space:".length)}`
        : row.id.startsWith("here:")
          ? `design:${row.id.slice("here:".length)}`
          : `store:${row.id}`;
      return this.show({ panel: which }, row.name, "design");
    }
    if (this.place === "jobs") {
      const job = this.jobs.find((one) => `job:${one.name}` === row.id);
      return this.show(
        {
          kind: "doc",
          title: row.name,
          body: job ? real.jobReport(job) : `# ${row.name}\n\nNo run history.`,
        },
        row.name,
        "eye",
      );
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
      designs: designsOf(space, this.goneDesigns),
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
    this.show({ kind: "space", title: space.repo, seen }, space.repo, "space");
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
      const name = did.id.split("|").pop() ?? did.id;
      return this.show({ panel: `spacedesign:${did.id}` }, name.replace(/\.md$/, ""), "design");
    }
    if (did.what === "import-session") return this.importSession(space.repo);
    // Off the page first, then the request: a row that stays put reads as a
    // button that did nothing.
    const session = did.what === "archive-session";
    const gone = session ? this.goneSessions : this.goneDesigns;
    gone.add(did.id);
    const listed = this.world.places.find((p) => p.id === "sessions");
    if (session && listed) this.redrawPlace(without(listed, gone));
    this.say(session ? "archiving the session…" : "archiving the design…");
    this.openSpace(space.repo);

    const got = session
      ? await window.ade.act("launcher", "/api/archive", { id: did.id, archived: true })
      : await window.ade.act("launcher", "/api/designs/archive", {
          repo: space.repo,
          file: did.id,
        });
    const back = real.answered(got as real.Answer);
    if (back.error) {
      gone.delete(did.id);
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

  // readPR renders the pull request with `vscode/src/prpage.ts`, and
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
    const { titlebar, activity, sidebar, head, sheet, statusbar } = this.shell;
    this.drawDock();
    titlebar.replaceChildren(...this.titlebar());
    activity.replaceChildren(...this.activity());
    // The filter keeps the keyboard across a redraw.
    const typing = document.activeElement?.classList.contains("find");
    sidebar.replaceChildren(...this.sidebar());
    if (typing) {
      const find = sidebar.querySelector<HTMLInputElement>(".find");
      find?.focus();
      find?.setSelectionRange(find.value.length, find.value.length);
    }
    sidebar.querySelector(".row.on, .twig.on")?.scrollIntoView({ block: "nearest" });
    head.replaceChildren(...this.head());
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
      held.node.hidden = tab.id !== this.active;
    }
    // The sheet's own line, not the one a waiting pane holds.
    const empty = sheet.querySelector(":scope > .nothing");
    if (this.tabs.length === 0 && !empty) sheet.append(nothingOpen());
    if (this.tabs.length > 0 && empty) empty.remove();
  }

  // paneKey is what a pane was built with: a session and an address for a
  // live one, and for a diff everything it draws beyond the diff itself.
  private paneKey(tab: Tab): string {
    if (live(tab)) return `${this.session}:${this.launcher?.base ?? ""}`;
    const view = tab.what;
    if ("panel" in view) return "";
    // A space is its documents and its conversations, which move while the
    // page is open: archiving one is the whole point of the Archive on it.
    if (view.kind === "space") return spaceDrawnFrom(this.nowSeen(view.seen));
    if (view.kind !== "hunks") return "";
    if (view.editing) return `editing:${view.file.path}`;
    return drawnFrom(view.file.path, this.threads, this.openThreads, Boolean(this.world.status.pr));
  }

  // waiting is why a panel tab cannot draw its page yet, or empty when
  // it can: a page built now would fetch the proxy's 404 and print it.
  private waiting(tab: Tab): string {
    if (!("panel" in tab.what)) return "";
    if (!this.sessionAt()) return this.session ? "Waiting for ade…" : "Pick a session.";
    return this.serving === "starting" ? "Starting ade for this checkout…" : "";
  }

  // show opens something in the editor area. The same thing twice is
  // the same tab, with whatever it now says.
  private show(what: Tab["what"], title: string, icon = "file"): void {
    const id = idOf(what);
    this.tabs = opened(this.tabs, { id, title, icon, what });
    this.active = id;
    this.remember();
    if (!live({ id, title, icon, what })) {
      this.panes.get(id)?.node.remove();
      this.panes.delete(id);
    }
    this.draw();
  }

  // kept is what the window picks up again: where you were, and the
  // tabs it can open from an id alone.
  private remember(): void {
    if (this.only) return;
    this.keptTabs.set(this.tabsFor, { tabs: keepable(this.tabs), active: this.active });
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
    this.active = "";
    this.session = session;
    this.tabsFor = session;
    this.reopen();
  }

  // again opens what was open, after the first read: a file tab needs
  // the session its checkout belongs to.
  private reopen(): void {
    if (this.only || this.tabs.length > 0) return;
    const kept = this.keptTabs.get(this.tabsFor);
    if (!kept || kept.tabs.length === 0) return;
    // Replaying a file tab moves the sidebar to Code, which is how
    // openFile knows what it is opening. Where you were is not its to change.
    const was = this.place;
    for (const id of kept.tabs) {
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
    if (kept.active && this.tabs.some((t) => t.id === kept.active)) this.active = kept.active;
    this.remember();
    this.draw();
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
    const gone = shut(this.tabs, id, this.active);
    this.tabs = gone.tabs;
    this.active = gone.active;
    this.dropPanes([id]);
    this.remember();
    this.draw();
  }

  // tabMenu is the strip's context menu. A sweep that would take nothing
  // is listed and dead, so the rows are always in the same places.
  private tabMenu(id: string, at: { x: number; y: number }): void {
    const sweep = (which: Sweep): MenuRow => {
      const ids = sweeping(this.tabs, id, which);
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
    const gone = shutMany(this.tabs, ids, this.active);
    this.tabs = gone.tabs;
    this.active = gone.active;
    this.dropPanes(gone.gone);
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
    main.append(head, sheet);
    // Built once and filled in place: its iframe reloads if it is moved.
    const dockHead = el("div", "dockhead");
    const dockBody = el("div", "dockbody");
    const dock = el("div", "dock");
    dock.append(dockHead, dockBody);
    const chatGrip = this.grip("chat");
    const body = el("div", "body");
    body.append(activity, sidebar, this.grip(), main, chatGrip, dock);
    this.root.replaceChildren(titlebar, body, statusbar);
    this.root.className = "window";
    return { titlebar, activity, sidebar, head, sheet, statusbar, dock, dockHead, dockBody, chatGrip };
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
        this.place = place.id;
        this.remember();
        this.draw();
      });
      if (place.id === "review") bar.push(el("div", "gap"));
      bar.push(button);
    }
    bar.push(this.terminalButton());
    return bar;
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
    head.append(find, again);
    if (place.id !== "review") return head;
    const heads = el("div", "heads");
    heads.append(head, this.reviewBar());
    return heads;
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
      const add = el("span", "btn quiet", `Add ${this.draft.ready} to the review`);
      add.addEventListener("click", () => void this.addToReview());
      bar.append(add);
    }
    if (this.draft.inDraft > 0) {
      const finish = el("span", "btn", "Finish the review");
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
    const got = await window.ade
      .act("session", "/api/review/draft", { confirm: pr }, this.session)
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got as real.Answer);
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
      `#${pr} — the summary`,
      event === "approve" ? `Approved with ${n} comment${n === 1 ? "" : "s"}` : "",
    );
    this.omnibox.close();
    if (body === undefined) return;
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
      const node2 = el("span", "act", act.label);
      node2.addEventListener("click", (e) => {
        e.stopPropagation();
        void this.rowAct(act);
      });
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
    if (act.what === "design-start") return this.startDesign(act.name);
    if (act.what === "space-close" || act.what === "space-clone") {
      return this.space(act.what === "space-close" ? "close" : "clone", act.name);
    }
    if (act.what === "todo-start") return this.fromTodo(act.name);
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
    if (act.what === "session-stop") {
      const got = await window.ade
        ?.act("session", "/api/cancel", {}, act.name)
        .catch((e: Error) => ({ error: e.message }));
      const stop = real.answered(got);
      // Nothing behind the row to cancel: the listing is stale, not the
      // press, so it refreshes rather than refusing.
      if (stop.error && !real.unserved(stop.error)) return this.say(stop.error);
      this.say(stop.error ? "no ade was serving it" : "stopped");
      return void this.read();
    }
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

  // endSession is the one verb on an agent row: stop the run, stop the ade
  // under it, or put the conversation away for good.
  private async endSession(id: string): Promise<void> {
    if (!window.ade) return;
    // Offered from the Sessions sidebar too, whose rows the Agents place
    // does not hold: without the fallback the dialog is titled with a uuid.
    const row = ["agents", "sessions"]
      .map((place) => this.world.places.find((p) => p.id === place)?.rows.find((r) => r.id === id))
      .find(Boolean);
    const state = this.announced.find((a) => a.id === id);
    const what = await this.omnibox.ask(
      `${row?.name ?? id}`,
      real.endings(real.stateOf(state?.agent)),
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
    this.say(real.ended(what));
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
    const text = this.todoAt(id);
    if (!text) return this.say("that todo is not on the list any more");
    this.show({ panel: `todo:${id}` }, "New session", "session");
  }

  // startDesign checks out every repository the document names, on one
  // branch, and opens a session across them.
  private async startDesign(name: string): Promise<void> {
    if (!window.ade) return;
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

  private head(): Element[] {
    if (this.tabs.length === 0) return [el("span", "", "Nothing open"), this.chatToggle()];
    const tabs = this.tabs.map((tab) => {
      const node = el("div", tab.id === this.active ? "tab on" : "tab");
      node.append(icon(tab.icon, 13), el("span", "nm", tab.title));
      node.title = tab.title;
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
      node.addEventListener("click", () => {
        this.active = tab.id;
        this.draw();
      });
      node.addEventListener("contextmenu", (e) => {
        e.preventDefault();
        this.tabMenu(tab.id, { x: e.clientX, y: e.clientY });
      });
      return node;
    });
    return [...tabs, this.chatToggle()];
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
      // `todo:<id>` is the New session screen with that todo in it.
      const todo = which.startsWith("todo:") ? this.todoAt(which.slice(5)) : "";
      const make = doc
        ? (reach: Reach, at?: { base: string; token: string }) => written(reach, at, doc)
        : todo
          ? (reach: Reach) => newSession(reach, { message: todo })
          : WHICH[which];
      if (!make) return el("div", "nothing", `no panel called ${which}`);
      const wait = this.waiting(tab);
      if (wait) return el("div", "nothing", wait);
      // The chat reaches the session's own server, so it is given that
      // address rather than the launcher's.
      return frame(
        make(this.reach(), this.sessionAt()),
        `${tab.id}:${this.session}:${this.launcher?.base ?? ""}`,
      );
    }
    const view = tab.what;
    if (view.kind === "term") return openTerm(this.session, (why) => this.say(why));
    // Code is monaco: one instance, moved into the tab that is showing
    // a file. Moving a div is free; each file keeps its own model.
    if (view.kind === "code") {
      return mount(view.source, view.title, view.line ?? 0, (line) => void this.blame(line));
    }
    if (view.kind === "hunks" && view.editing) {
      return mountDiff(view.base ?? "", view.source ?? "", view.title, 0, (line) => void this.blame(line));
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
      { hasPR: Boolean(this.world.status.pr), open: this.openThreads },
      (who, text) => void this.askPerson(who, text),
      (did) => void this.spaceAct(did),
    );
  }

  private statusbar(): HTMLElement[] {
    const bar: HTMLElement[] = [];
    const status = this.world.status;

    const branch = el("div", "item");
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

// said is what a verb reports: ade answers a start with what it
// started, and "done" for a two-minute review says nothing.
function said(command: string, got: unknown): string {
  const back = (got ?? {}) as { agents?: string[]; running?: boolean; to?: string };
  const verb = command.replace(/^ade\./, "");
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
    const of: Record<string, string> = { k: "omnibox", b: "sidebar.toggle" };
    const command = of[e.key.toLowerCase()];
    if (!command) return;
    e.preventDefault();
    run(command);
  });
}

new Ade().draw();
