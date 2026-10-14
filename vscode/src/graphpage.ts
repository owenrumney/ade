// GraphRepo mirrors sessions.GraphRepo: one repository, its checkouts, and
// what is serving each right now.
export interface GraphRepo {
  path: string;
  name: string;
  owner?: string;
  checkouts: GraphCheckout[] | null;
}

export interface GraphCheckout {
  path: string;
  name: string;
  branch?: string;
  worktree?: boolean;
  sessions: number;
  server?: { pid: number; agent?: { running?: boolean; kind?: string } } | null;
}

// busy is the repositories with something to show: a server up, or a
// conversation had. A scanned checkout nobody has opened is not news.
export function busy(repos: GraphRepo[]): GraphRepo[] {
  return repos
    .map((repo) => ({
      ...repo,
      checkouts: (repo.checkouts ?? []).filter(
        (c) => c.server || c.sessions > 0,
      ),
    }))
    .filter((repo) => repo.checkouts.length > 0);
}

// state is one checkout in a word or two.
export function state(checkout: GraphCheckout): string {
  if (!checkout.server) return "not running";
  if (checkout.server.agent?.running)
    return checkout.server.agent.kind
      ? `working: ${checkout.server.agent.kind}`
      : "working";
  return "idle";
}

// page is the machine right now: a drawing of what is where, and the same as
// a table for anything the drawing cannot say.
export function page(repos: GraphRepo[]): string {
  const shown = busy(repos);
  const out = ["# What is running", ""];
  if (shown.length === 0)
    return [
      ...out,
      "Nothing is running, and no checkout has a session.",
      "",
    ].join("\n");

  out.push("```mermaid", "flowchart LR");
  shown.forEach((repo, r) => {
    out.push(`  r${r}(["${label(repo.name)}"])`);
    (repo.checkouts ?? []).forEach((checkout, c) => {
      const node = `r${r}c${c}`;
      const text = `${label(checkout.branch || checkout.name)}<br/>${state(checkout)}`;
      out.push(`  r${r} --> ${node}["${text}"]`);
      if (checkout.server?.agent?.running) out.push(`  class ${node} working`);
      else if (checkout.server) out.push(`  class ${node} idle`);
    });
  });
  out.push(
    "  classDef working stroke:#d19a66,stroke-width:2px",
    "  classDef idle stroke:#98c379",
    "```",
    "",
    "| Repository | Checkout | Branch | State | Sessions |",
    "| --- | --- | --- | --- | ---: |",
  );
  for (const repo of shown)
    for (const checkout of repo.checkouts ?? [])
      out.push(
        `| ${repo.name} | ${checkout.name} | ${checkout.branch ?? ""} | ${state(checkout)} | ${checkout.sessions} |`,
      );
  return out.join("\n") + "\n";
}

// label keeps a name from ending the node it is written into.
function label(text: string): string {
  return text.replace(/["\[\]<>]/g, " ").trim();
}

// Running is one checkout with an ade in it, and the repository it belongs to.
export interface Running {
  repo: GraphRepo;
  checkout: GraphCheckout;
}

// running is every checkout with a server up, which are the only ones there is
// anything to stop in. The working ones come first.
export function running(repos: GraphRepo[]): Running[] {
  const out: Running[] = [];
  for (const repo of repos)
    for (const checkout of repo.checkouts ?? [])
      if (checkout.server) out.push({ repo, checkout });
  return out.sort(
    (a, b) =>
      Number(Boolean(b.checkout.server?.agent?.running)) -
      Number(Boolean(a.checkout.server?.agent?.running)),
  );
}
