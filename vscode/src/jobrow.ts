import { unread } from "./jobpage";
import type { Job, Skipped } from "./launcher";

// A job row is either a definition or a file that could not be read as one.
// The second is worth a row: a misspelt schedule is a job that never fires.
export type Row = { job: Job } | { skipped: Skipped };

export function rows(jobs: Job[], skipped: Skipped[]): Row[] {
  return [
    ...jobs.map((j) => ({ job: j })),
    ...skipped.map((s) => ({ skipped: s })),
  ];
}

// line is the schedule and what the state adds to it, which is the whole of
// what a row has to say.
// What needs you comes first: a row is cut off at the view's width, and the
// schedule and project are the parts that can be lost.
export function line(job: Job): string {
  const parts: string[] = [];
  const waiting = unread(job.outcomes ?? []).length;
  if (waiting) parts.push(`${waiting} unread`);
  if (job.running) parts.push("running");
  else if (job.paused) parts.push("paused");
  else if (job.failing) parts.push("failing");
  parts.push(job.schedule);
  if (job.project) parts.push(job.project);
  return parts.join(" · ");
}

export function icon(job: Job): string {
  if (job.running) return "sync~spin";
  if (job.failing) return "error";
  if (job.paused) return "debug-pause";
  return "watch";
}

// editable is whether opening the row means opening a file. A built-in kind is
// Go in the launcher, so there is nothing to edit and the row must say so.
export function editable(job: Job): boolean {
  return !job.kind && Boolean(job.path);
}

// context is what the menus key off: which verbs a row has. A built-in cannot
// be edited or deleted, and a repository job's file is not the user's to remove.
export function context(job: Job): string {
  const bits = ["ade.job"];
  bits.push(job.paused ? "paused" : "live");
  if (!editable(job)) bits.push("builtin");
  if (job.scope !== "user") bits.push("repo");
  return bits.join(".");
}

// skeleton is a new job's file, in the shape jobs.Render writes so the two
// agree on the format without this parsing anything.
export function skeleton(name: string): string {
  return [
    "---",
    `name: ${name}`,
    "schedule: 0 9 * * 1-5",
    "continuity: fresh",
    "allow: reads",
    "---",
    "",
    "What this job should do, as you would ask for it in chat.",
    "",
  ].join("\n");
}

// A built-in has no file, so what it is gets written out for it. Read under
// this scheme, so it opens as a document nobody can save over.
export const JOB = "ade-job";

// docPath and jobNamed are each other's inverse: the row builds the uri and the
// content provider reads the name back out of it.
export function docPath(name: string): string {
  return `/${name}.md`;
}

export function jobNamed(path: string): string {
  return path.replace(/^\//, "").replace(/\.md$/, "");
}

export function describe(job: Job): string {
  const facts = [
    `- Schedule: \`${job.schedule}\``,
    `- Kind: \`${job.kind}\`, built into ade`,
  ];
  if (job.continuity) facts.push(`- Continuity: ${job.continuity}`);
  if (job.allow) facts.push(`- Allowed: ${job.allow}`);
  return [
    `# ${job.name}`,
    "",
    job.description || "A built-in job.",
    "",
    ...facts,
    "",
    "This job is Go in the launcher, not a prompt in a file, so there is",
    "nothing here to edit. It can still be run now, paused and resumed from its row.",
    "",
  ].join("\n");
}
