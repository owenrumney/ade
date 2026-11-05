// My Day: the one report ade builds itself. The launcher keeps it as a job
// called my-day, with what came of each build.

// Outcome is one build of the report.
export interface Outcome {
  firedAt: string;
  millis: number;
  report?: string;
  error?: string;
  // Set once the report has been read, which takes it off the unread count.
  seen?: boolean;
}

export interface Job {
  name: string;
  lastFired?: string;
  running?: boolean;
  outcomes?: Outcome[];
}

export const MY_DAY = "my-day";

export function myDay(jobs: Job[]): Job | undefined {
  return jobs.find((job) => job.name === MY_DAY);
}

// newest is the outcomes latest first, whatever order they were kept in.
export function newest(outcomes: Outcome[]): Outcome[] {
  return [...outcomes].sort((a, b) => b.firedAt.localeCompare(a.firedAt));
}

// jobUnread is how many builds said something, or failed, and are unread.
export function jobUnread(job: Job): number {
  return (job.outcomes ?? []).filter((o) => !o.seen && Boolean(o.report?.trim() || o.error)).length;
}

// myDayDue is whether opening the window builds the report: a weekday with
// nothing built today, and no build in flight.
export function myDayDue(job: Job | undefined, now: Date): boolean {
  if (!job || job.running) return false;
  if (now.getDay() === 0 || now.getDay() === 6) return false;
  const last = job.outcomes?.[0]?.firedAt ?? job.lastFired;
  return !last || new Date(last).toDateString() !== now.toDateString();
}

// myDayReport is the newest build on its own: the calendar is today.
export function myDayReport(job: Job | undefined): string {
  if (!job) return "# My Day\n\nThis machine has no My Day report.";
  const latest = newest(job.outcomes ?? [])[0];
  if (!latest) return "# My Day\n\nIt has not been built yet.";
  if (latest.error) return `# My Day\n\n**It failed:** ${latest.error}`;
  // The report writes its own dated heading, so nothing is added here.
  return latest.report?.trim() || "# My Day\n\nNothing waiting, and nothing of yours open.";
}

// myDayTitle carries how old the build is, so yesterday's report is not read
// as this morning's.
export function myDayTitle(job: Job | undefined, now = Date.now()): string {
  const at = Date.parse(newest(job?.outcomes ?? [])[0]?.firedAt ?? "");
  if (Number.isNaN(at)) return "My Day";
  const m = Math.max(0, Math.round((now - at) / 60_000));
  const when = m < 1 ? "just now" : m < 60 ? `${m}m` : m < 2880 ? `${Math.round(m / 60)}h` : `${Math.round(m / 1440)}d`;
  return `My Day · ${when}`;
}
