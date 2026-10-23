import "./bridge";
import { REPORTS } from "./reports";
import * as real from "./real";
import type { Dash, Design, Repo } from "./real";
import { icon } from "./icons";
import {
  counted,
  grouped,
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
import { changesIn, toneOf, unsent, type Change, type Thread } from "./changes";
import { frame, stirred, tell } from "./panel";
import { held, idOf, keepable, live, opened, shut, type Tab } from "./tabs";
import { mount, reaching, selected } from "./code";
import { close as closeTerm, open as openTerm } from "./term";
import { newSession, written, WHICH, type Reach, type Written } from "./panels";
import { page as prPage } from "../../vscode/src/prpage";
import type { BranchPR, Check } from "../../vscode/src/client";

// The shell. It owns the keyboard outright and hands the window named
// commands, never keystrokes. Absent only under `make app-shots`.

// How long a server gets to come up before this is a failure.
const startingFor = 8_000;

// What every list says while that server is on its way up.
const coming = "Starting ade for this checkout…";

interface Gathered {
  offline?: string;
  dash?: Dash & { error?: string };
  designs?: Design[];
  repos?: Repo[];
  hosts?: real.Machine[];
  active?: real.Active[];
  todos?: real.Todo[];
  jobs?: real.Job[];
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
  private changes: Change[] = [];
  private threads: Thread[] = [];
  private mine: real.Listed[] = [];
  private bus?: real.Bus;
  private pr?: { number: number; title: string; body: string };
  // Where the picked session's server is up to. "starting" is one
  // this window has in flight, which is not yet a failure.
  private serving: "ok" | "starting" | "failed" = "ok";
  private startedAt = 0;
  // Every announced session as of the last reading: how the window
  // knows whether a session it is about to show is served.
  private announced: real.Active[] = [];
  private todoList: real.Todo[] = [];
  // The diff is the largest thing the server holds, kept until the
  // version it publishes for exactly this moves.
  private diffNow = "";
  private diffAt = "";
  private diffFor = "";
  private diffWhy = "";
  // The file list and the checkout's documents, kept the same way.
  private files: { path: string }[] = [];
  private filesAt = "";
  private filesWhy = "";
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
  private where = new Map<string, string>();
  // What is open in the editor area. A pane is kept once built: moving
  // an iframe reloads it, so switching hides rather than rebuilds.
  private tabs: Tab[] = [];
  private active = "";
  private panes = new Map<string, { node: HTMLElement; key: string }>();
  private launcher?: { base: string; token: string };
  private omnibox: Omnibox;
  private sidebarWidth = Number(localStorage.getItem("ade.sidebar")) || 300;
  private started = false;
  private timer = 0 as unknown as ReturnType<typeof setTimeout>;
  private watching = "";
  private root = document.getElementById("window")!;

  constructor() {
    this.omnibox = new Omnibox(
      () => this.world.places,
      (hit) => {
        if (hit.kind === "row") this.open(hit.row, placeOf(this.world.places, hit.row.id));
        else this.run(hit.verb.command);
      },
    );
    document.documentElement.style.setProperty("--sidebar-w", `${this.sidebarWidth}px`);
    // How the editor reaches a language server. The window holds the
    // session, so it does the asking; the editor only says where.
    reaching(
      {
        hover: (path, line, col) => this.language<string>("hover", path, line, col, "hover"),
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
    const got = await window.ade
      .world(this.homeDir)
      .catch((e: Error) => ({ offline: e.message }));
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
    this.where = real.whereSessionsAre(repos, got.active ?? []);
    // Only when the read brought a list back: a failed scan knows of
    // nothing, and landing off that moved you out of the conversation.
    const known = real.knownSessions(repos, got.active ?? [], got.hosts ?? []);
    if (known.size > 0 && !known.has(this.session)) {
      this.session = real.landOn(repos, this.openedOn);
    }
    this.homeDir = this.where.get(this.session) ?? repos[0]?.path ?? "";
    if (this.session !== this.watching) {
      this.watching = this.session;
      void window.ade.watch(this.session);
    }
    this.launcher = (await window.ade.where()) ?? undefined;
    this.announced = got.active ?? [];
    this.todoList = got.todos ?? [];
    // This read is the one that finds out whether anything is serving
    // the session, so the other three do not repeat its failure.
    const doing = await this.doing();
    const starting = this.serving === "starting";
    const [changes, files, docs] = starting
      ? [{ rows: [], why: coming }, { rows: [], why: coming }, []]
      : await Promise.all([this.diff(), this.tree(), this.docs()]);
    this.changes = changes.rows;
    this.world = {
      places: [
        real.waiting(dash, got.active ?? []),
        real.prs(dash, this.mine, this.repoOf(this.session)),
        real.designs(got.designs ?? [], docs, this.where.get(this.session) ?? ""),
        note(
          real.sessions(repos, "", Date.now(), got.hosts ?? []),
          trouble.sessions,
          "No session on this machine yet.",
        ),
        note(real.agents(got.active ?? []), trouble.agents),
        note(real.todos(got.todos ?? []), trouble.todos, "Nothing on the list."),
        note(real.jobs(got.jobs ?? []), trouble.jobs, "No jobs on this machine."),
        real.issues(dash),
        real.inbox(dash),
        real.team(this.bus),
        note(
          {
            id: "review",
            title: "Review",
            icon: "diff",
            rows: [],
            files: this.changes.map((c) => ({ path: c.path, status: toneOf(c.status) })),
            count: unsent(this.threads),
          },
          changes.why,
        ),
        note({ id: "code", title: "Code", icon: "code", rows: [], files: files.rows }, files.why),
      ],
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

  private async again(): Promise<void> {
    // Asked for by hand, so nothing kept is trusted.
    this.diffAt = "";
    this.filesAt = "";
    this.docsFor = "";
    this.say("asking GitHub…");
    await window.ade?.act("launcher", "/api/dash/refresh", {}).catch(() => undefined);
    await this.read();
    this.say("refreshed");
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
    const unserved = /no ade is serving/.test(got.error ?? "");
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
    this.mine = ((got as { openPrs?: real.Listed[] | null })?.openPrs ?? []).filter((p) => p.number);
    this.bus = (got as { bus?: real.Bus })?.bus;
    this.threads = ((got as { threads?: Thread[] | null })?.threads ?? []).filter((t) => t.id);
    this.diffNow = String((got as { diffVersion?: string }).diffVersion ?? "");
    return real.doingIn(got);
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
    if (this.session === this.diffFor && this.diffNow !== "" && this.diffNow === this.diffAt) {
      return { rows: this.changes, why: this.diffWhy };
    }
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
    this.diffAt = this.diffNow;
    this.diffFor = this.session;
    this.diffWhy = why;
    return { rows, why };
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
    if (at === this.filesAt && this.diffNow !== "") {
      return { rows: this.files, why: this.filesWhy };
    }
    const got = (await window.ade
      .read("session", "/api/tree", this.session)
      .catch((e: Error) => ({ error: e.message }))) as { files?: string[]; error?: string };
    if (got.error) return { rows: [], why: `/api/tree — ${got.error}` };
    this.files = (got.files ?? []).map((path) => ({ path }));
    this.filesAt = at;
    this.filesWhy = "";
    return { rows: this.files };
  }

  run(command: string): void {
    const what = wired(command);
    if (!what || what.at === "soon") return this.say(owed(command));
    if (what.at === "panel") {
      this.omnibox.close();
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
        const tab = held(this.tabs, this.active);
        if (!tab || "panel" in tab.what) return;
        return this.close();
      }
      this.omnibox.close();
      if (what.verb === "omnibox") return this.omnibox.toggle();
      if (what.verb === "refresh") return void this.again();
      if (what.verb === "start") return void this.up();
      if (what.verb === "resume") return void this.resume();
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
      if (path.includes(slot)) path = path.replace(slot, encodeURIComponent(String(said)));
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

  private chat(): void {
    this.show({ panel: "ade.openChat" }, "Chat", "session");
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
              scope: "diff",
              file: asked.file,
              side: asked.side,
              line: asked.line,
              body: said.trim(),
            })
          : await send(`/api/threads/${id}/reply`, { body: said.trim() });
    } else if (asked.what === "resolve") {
      const now = this.threads.find((t) => t.id === asked.id);
      got = await send(`/api/threads/${id}/resolve`, { resolved: !now?.resolved });
    } else if (asked.what === "send") {
      got = await send(`/api/threads/${id}/send-reply`, {});
    } else if (asked.what === "drop") {
      got = await send(`/api/threads/${id}`, {}, "DELETE");
    }
    const back = real.answered(got);
    if (back.error) return this.say(back.error);
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
    if (list.length === 0) return this.say(`nothing to pick: no ${first.from} to offer`);
    if (first.ask === "many") return this.omnibox.many(first.title, list);
    return this.omnibox.ask(first.title, list);
  }

  // fetched is a list ade holds. Read as the box opens: a stale picker
  // is worse than a slow one.
  private async fetched(from: Choice): Promise<Pickable[]> {
    if (!window.ade) return [];
    const of = SOURCES[from];
    const got = (await window.ade
      .read(of.at, of.path, this.session)
      .catch(() => ({}))) as Record<string, unknown>;
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
      this.session = want;
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
    const reading = { kind: "doc" as const, title: row.name, body: `# ${row.name}\n\nReading…` };
    if (this.place === "designs") {
      // Either way it opens in the editor: one in this checkout through
      // the session serving it, one from the store through the launcher.
      const which = row.id.startsWith("here:")
        ? `design:${row.id.slice("here:".length)}`
        : `store:${row.id}`;
      return this.show({ panel: which }, row.name, "design");
    }
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

  // readPR renders the pull request with `vscode/src/prpage.ts`.
  private async readPR(row: Row, at: { repo: string; number: number }): Promise<void> {
    if (!window.ade) return;
    const got: { pr?: BranchPR; checks?: Check[] | null; error?: string } = await window.ade
      .pr(at.repo, at.number)
      .catch((e: Error) => ({ error: e.message }));
    if (this.picked.get(this.place) !== row.id) return;
    const body = got.error
      ? `# ${row.name}\n\n${got.error}`
      : got.pr
        ? prPage({ ...got.pr, checks: got.checks ?? null })
        : "";
    if (body) this.show({ kind: "doc", title: row.name, body }, row.name, "pr");
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
      let held = this.panes.get(tab.id);
      // Built again when the session or the address has moved on.
      if (held && held.key !== want) {
        held.node.remove();
        held = undefined;
      }
      if (!held) {
        const node = el("div", "pane");
        node.append(this.inPane(tab));
        held = { node, key: want };
        this.panes.set(tab.id, held);
        sheet.append(node);
      }
      held.node.hidden = tab.id !== this.active;
    }
    const empty = sheet.querySelector(".nothing");
    if (this.tabs.length === 0 && !empty) sheet.append(nothingOpen());
    if (this.tabs.length > 0 && empty) empty.remove();
  }

  // paneKey is what a pane was built with: a session and an address.
  private paneKey(tab: Tab): string {
    return live(tab) ? `${this.session}:${this.launcher?.base ?? ""}` : "";
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
    localStorage.setItem(
      "ade.window",
      JSON.stringify({
        place: this.place,
        session: this.session,
        tabs: keepable(this.tabs),
        active: this.active,
      }),
    );
  }

  // again opens what was open, after the first read: a file tab needs
  // the session its checkout belongs to.
  private reopen(): void {
    if (this.only || this.tabs.length > 0) return;
    let kept: { place?: string; tabs?: string[]; active?: string };
    try {
      kept = JSON.parse(localStorage.getItem("ade.window") ?? "{}") as typeof kept;
    } catch {
      return;
    }
    if (kept.place) this.place = kept.place;
    for (const id of kept.tabs ?? []) {
      const [kind, ...rest] = id.split(":");
      const path = rest.join(":");
      if (kind === "panel") this.run(path === "ade.openChat" ? "ade.openChat" : path);
      else if (kind === "term") this.run("ade.openTerminal");
      else if (kind === "code" || kind === "hunks") {
        this.place = kind === "hunks" ? "review" : "code";
        this.picked.set(this.place, path);
        this.openFile(path);
      }
    }
    if (kept.active && this.tabs.some((t) => t.id === kept.active)) this.active = kept.active;
    if (kept.place) this.place = kept.place;
    this.draw();
  }

  private close(id = this.active): void {
    const gone = shut(this.tabs, id, this.active);
    this.tabs = gone.tabs;
    this.active = gone.active;
    this.panes.get(id)?.node.remove();
    this.panes.delete(id);
    if (id === "term") closeTerm();
    this.remember();
    this.draw();
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
    const body = el("div", "body");
    body.append(activity, sidebar, this.grip(), main);
    this.root.replaceChildren(titlebar, body, statusbar);
    this.root.className = "window";
    return { titlebar, activity, sidebar, head, sheet, statusbar };
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
    return bar;
  }

  // grip drags the sidebar wider, by one variable on the root.
  private grip(): HTMLElement {
    const grip = el("div", "grip");
    // Pointer capture, and the panes made deaf: an iframe hit-tests the
    // pointer itself, so a drag over one never saw the mouse come up.
    grip.addEventListener("pointerdown", (down) => {
      down.preventDefault();
      grip.setPointerCapture(down.pointerId);
      grip.classList.add("held");
      document.body.classList.add("dragging");
      const from = this.sidebarWidth;
      const at = down.clientX;
      const move = (e: PointerEvent) => {
        this.sidebarWidth = Math.min(720, Math.max(180, from + e.clientX - at));
        document.documentElement.style.setProperty("--sidebar-w", `${this.sidebarWidth}px`);
      };
      const done = () => {
        grip.classList.remove("held");
        document.body.classList.remove("dragging");
        grip.removeEventListener("pointermove", move);
        grip.removeEventListener("lostpointercapture", done);
        localStorage.setItem("ade.sidebar", String(this.sidebarWidth));
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
    return [this.head2(place), list];
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
    head.append(find);
    return head;
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
  private openFile(path: string): void {
    const name = path.split("/").pop() ?? path;
    if (this.place === "review") {
      const file = this.changes.find((c) => c.path === path);
      if (file) {
        this.show({ kind: "hunks", title: path, file, threads: this.threads }, name, "diff");
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
      .catch((e: Error) => ({ error: e.message }))) as { source?: string; error?: string };
    if (this.picked.get(this.place) !== path) return;
    const view: View = got.error
      ? { kind: "doc", title: path, body: `# ${path}\n\n${got.error}` }
      : { kind: "code", title: path, source: got.source ?? "" };
    this.show(view, path.split("/").pop() ?? path, "file");
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
    node.addEventListener("click", () => this.open(row));
    return node;
  }

  // rowAct is the one thing a row does besides open. Each is a verb
  // ade already has; the row only says which and on what.
  private async rowAct(act: NonNullable<Row["acts"]>[number]): Promise<void> {
    if (act.what === "design-start") return this.startDesign(act.name);
    if (act.what === "todo-start") return this.fromTodo(act.name);
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
    if (act.what === "session-stop") {
      const got = await window.ade
        ?.act("session", "/api/cancel", {}, act.name)
        .catch((e: Error) => ({ error: e.message }));
      const stop = real.answered(got);
      if (stop.error) return this.say(stop.error);
      this.say("stopped");
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
    this.say(`starting ${name}…`);
    const got = await window.ade
      .act("launcher", "/api/designs/start", { name })
      .catch((e: Error) => ({ error: e.message }));
    const back = real.answered(got);
    if (back.error) return this.say(back.error);
    await this.read();
    this.say(`started ${name}`);
  }

  private head(): Element[] {
    if (this.tabs.length === 0) return [el("span", "", "Nothing open")];
    return this.tabs.map((tab) => {
      const node = el("div", tab.id === this.active ? "tab on" : "tab");
      node.append(icon(tab.icon, 13), el("span", "nm", tab.title));
      node.title = tab.title;
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
      return node;
    });
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
      // A page reads its address off its own root as it loads, so one
      // built without it cannot be given one later.
      if (!this.sessionAt()) {
        return el("div", "nothing", this.session ? "Waiting for ade…" : "Pick a session.");
      }
      // Nothing serves this session yet, and a page built now would
      // fetch the proxy's 404 and print it. The key below holds it.
      if (this.serving === "starting") {
        return el("div", "nothing", "Starting ade for this checkout…");
      }
      // The chat reaches the session's own server, so it is given that
      // address rather than the launcher's.
      return frame(
        make(this.reach(), this.sessionAt()),
        // Whether there is a page at all, not which state: rebuilding
        // on that flip reloaded the chat under what was being typed.
        `${tab.id}:${this.session}:${this.launcher?.base ?? ""}:${
          this.serving === "starting" ? "starting" : "up"
        }`,
      );
    }
    const view = tab.what;
    if (view.kind === "term") return openTerm(this.session, (why) => this.say(why));
    // Code is monaco: one instance, moved into the tab that is showing
    // a file. Moving a div is free; each file keeps its own model.
    if (view.kind === "code") {
      return mount(view.source, view.title, view.line ?? 0, (line) => void this.blame(line));
    }
    return sheet(
      view,
      (line) => this.blame(line),
      (rule) => void this.forget(rule),
      (asked) => void this.onThread(asked),
    );
  }

  private statusbar(): HTMLElement[] {
    const bar: HTMLElement[] = [];
    const status = this.world.status;

    const branch = el("div", "item");
    branch.append(icon("branch", 13), el("span", "", status.branch));
    branch.title = "Switch branch";
    branch.addEventListener("click", () => this.omnibox.show());

    bar.push(branch);
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
