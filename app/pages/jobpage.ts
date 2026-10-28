// Outcome mirrors jobs.Outcome: what came of one fire of a scheduled job.
export interface Outcome {
  firedAt: string;
  millis: number;
  costUsd?: number;
  report?: string;
  error?: string;
  session?: string;
  dir?: string;
  url?: string;
  noHandover?: boolean;
  // Set once the report has been read, which takes it off the unread count.
  seen?: boolean;
}

// newest is the outcomes latest first, whatever order they were kept in.
export function newest(outcomes: Outcome[]): Outcome[] {
  return [...outcomes].sort((a, b) => b.firedAt.localeCompare(a.firedAt));
}

// took is a run's length as a person says it.
export function took(millis: number): string {
  const seconds = Math.round(millis / 1000);
  if (seconds < 60) return `${seconds}s`;
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
}

// page is a job's reports as a document, newest first. A fire with nothing to
// say is still listed: silence from a job that should speak is the news.
export function page(name: string, outcomes: Outcome[]): string {
  const out = [`# ${name}`, ""];
  if (outcomes.length === 0)
    return [...out, "This job has not run yet.", ""].join("\n");
  for (const outcome of newest(outcomes)) {
    const facts = [took(outcome.millis)];
    if (outcome.costUsd) facts.push(`$${outcome.costUsd.toFixed(2)}`);
    if (outcome.dir) facts.push(`\`\``);
    if (outcome.url) facts.push(`[open the session]()`);
    const fresh = unread([outcome]).length ? " · new" : "";
    out.push(`## ${when(outcome.firedAt)}${fresh}`, "", facts.join(" · "), "");
    if (outcome.error) out.push(`**Failed:** ${outcome.error}`, "");
    if (outcome.noHandover)
      out.push("It left no handover, so the next run starts with nothing.", "");
    out.push(outcome.report?.trim() || "_Nothing to report._", "");
  }
  return out.join("\n");
}

// when is a fire time without the seconds and zone nobody reads.
function when(at: string): string {
  return at.slice(0, 16).replace("T", " ");
}

// unread is the outcomes still waiting to be read: ones that said something,
// or failed, and have not been seen. Silence is history, not news.
export function unread(outcomes: Outcome[]): Outcome[] {
  return outcomes.filter(
    (o) => !o.seen && Boolean(o.report?.trim() || o.error),
  );
}
