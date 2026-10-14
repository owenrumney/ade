import {
  against,
  ago,
  count,
  heat,
  kinds,
  money,
  months,
  percent,
  spark,
  topLanguages,
  weeks,
  type Repo,
} from "./view";

declare function acquireVsCodeApi(): { postMessage(message: unknown): void };
const host = acquireVsCodeApi();
const root = document.getElementById("repo") as HTMLElement;

interface Shown {
  repo?: Repo;
  busy?: boolean;
  note?: string;
}

function el(tag: string, cls?: string, text?: string): HTMLElement {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

function card(title: string, wide = false): HTMLElement {
  const box = el("section", wide ? "card wide" : "card");
  box.append(el("h2", "", title));
  return box;
}

function languages(repo: Repo): HTMLElement {
  const box = card("Languages");
  const total = repo.lines;
  const head = el("div", "big");
  head.append(el("span", "num", total.toLocaleString()));
  head.append(el("span", "unit", `lines · ${repo.files.toLocaleString()} files`));
  box.append(head);

  const bar = el("div", "bar");
  const list = el("div", "legend");
  topLanguages(repo.languages).forEach((lang, i) => {
    const slice = el("span", `slice c${i % 6}`);
    slice.style.width = `${total > 0 ? (lang.lines / total) * 100 : 0}%`;
    slice.title = `${lang.name} · ${lang.lines.toLocaleString()} lines`;
    bar.append(slice);

    const row = el("div", "legend-row");
    row.append(el("span", `dot c${i % 6}`));
    row.append(el("span", "name", lang.name));
    row.append(el("span", "share", percent(lang.lines, total)));
    list.append(row);
  });
  box.append(bar, list);
  return box;
}

function activity(repo: Repo): HTMLElement {
  const box = card("Commit activity", true);
  const busiest = repo.days.reduce((n, d) => Math.max(n, d.commits), 0);
  box.append(
    el(
      "p",
      "sub",
      `${repo.commits} commits in the last year${repo.busiestDay ? ` · busiest ${repo.busiestDay}` : ""}`,
    ),
  );

  const cols = weeks(repo.days);
  const grid = el("div", "heat");
  cols.forEach((column) => {
    const week = el("div", "week");
    column.forEach((day) => {
      const cell = el("span", day ? `cell h${heat(day.commits, busiest)}` : "cell gap");
      if (day) cell.title = `${day.date} · ${day.commits} commits`;
      week.append(cell);
    });
    grid.append(week);
  });
  const scale = el("div", "months");
  months(cols).forEach((name) => scale.append(el("span", "month", name ?? "")));
  // One scroller, so a month label cannot drift from the week it names.
  const scroller = el("div", "scroller");
  scroller.append(grid, scale);
  box.append(scroller);

  if (repo.last) {
    const last = el("div", "last");
    last.append(el("div", "subject", repo.last.subject));
    last.append(
      el("div", "meta", `${repo.last.sha} · ${repo.last.author} · ${ago(repo.last.at)}`),
    );
    box.append(last);
  }
  return box;
}

function people(repo: Repo): HTMLElement {
  const box = card("Contributors");
  const top = repo.contributors.slice(0, 8);
  const most = top.reduce((n, p) => Math.max(n, p.commits), 0);
  for (const person of top) {
    const row = el("div", "person");
    row.append(el("span", "who", person.name));
    const track = el("span", "track");
    const fill = el("span", "fill");
    fill.style.width = `${most > 0 ? (person.commits / most) * 100 : 0}%`;
    track.append(fill);
    row.append(track, el("span", "num", String(person.commits)));
    box.append(row);
  }
  if (top.length === 0) box.append(el("p", "sub", "No commits in the last year."));
  return box;
}

function agentCard(repo: Repo): HTMLElement {
  const box = card("What ade did here", true);
  box.append(el("p", "sub", "This machine's transcripts, last 30 days."));

  const figures = el("div", "figures");
  const figure = (value: string, label: string) => {
    const one = el("div", "figure");
    one.append(el("span", "num", value));
    one.append(el("span", "unit", label));
    figures.append(one);
  };
  figure(money(repo.agent.cost), "API value");
  figure(String(repo.agent.turns), "turns");
  figure(String(repo.agent.sessions), "sessions");
  const tokens = kinds(repo.agent).reduce((n, k) => n + k.value, 0);
  figure(count(tokens), "tokens");
  box.append(figures);

  const chart = el("div", "spark");
  for (const day of spark(repo.agent.byDay)) {
    const bar = el("span", "sparkbar");
    bar.style.height = `${Math.max(day.height * 100, day.cost > 0 ? 4 : 1)}%`;
    bar.title = `${day.key} · ${money(day.cost)}`;
    chart.append(bar);
  }
  box.append(chart);

  const split = el("div", "legend");
  for (const kind of kinds(repo.agent)) {
    const row = el("div", "legend-row");
    row.append(el("span", "name", kind.name));
    row.append(el("span", "share", count(kind.value)));
    split.append(row);
  }
  box.append(split);

  if (repo.agent.tools.length > 0) {
    box.append(el("h3", "", "Busiest tools"));
    const tools = el("div", "legend");
    for (const tool of repo.agent.tools.slice(0, 6)) {
      const row = el("div", "legend-row");
      row.append(el("span", "name", tool.key));
      row.append(el("span", "share", count(tool.sessions)));
      tools.append(row);
    }
    box.append(tools);
  }
  return box;
}

function draw(state: Shown): void {
  root.replaceChildren();
  if (state.note) root.append(el("p", "note", state.note));
  if (!state.repo) {
    root.append(el("p", "sub", state.busy ? "Reading the repository…" : "Nothing to show."));
    return;
  }
  const repo = state.repo;

  const head = el("header", "head");
  const name = el("div", "title");
  name.append(el("span", "repo", repo.repo));
  name.append(el("span", "dir", repo.dir));
  head.append(name);

  const branch = el("div", "branch");
  branch.append(el("span", `pip${repo.ahead || repo.behind ? " off" : ""}`));
  branch.append(el("span", "", against(repo)));
  if (repo.dirty > 0) branch.append(el("span", "dirty", `${repo.dirty} uncommitted`));
  head.append(branch);

  const refresh = el("button", "refresh", state.busy ? "…" : "Refresh");
  refresh.addEventListener("click", () => host.postMessage({ kind: "do", what: "refresh" }));
  head.append(refresh);
  root.append(head);

  const grid = el("div", "grid");
  grid.append(languages(repo), activity(repo), agentCard(repo), people(repo));
  root.append(grid);
}

window.addEventListener("message", (event: MessageEvent<Shown & { kind?: string }>) => {
  if (event.data?.kind === "state") draw(event.data);
});

host.postMessage({ kind: "ready" });
