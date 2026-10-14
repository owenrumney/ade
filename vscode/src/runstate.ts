import type { Child, Finished, RunState } from "./client";

// What the status bar says about the agent. An icon, a word and a spinner are
// the whole of it: the conversation is where the detail is.
export interface Badge {
  text: string;
  tooltip: string;
  warn: boolean;
}

// kinds the server names that read badly as-is. Anything else is shown as it
// came, so a new kind is a slightly odd word rather than a missing one.
const WORDS: Record<string, string> = {
  "commit-message": "writing a commit message",
  "switch-model": "switching model",
  switch: "switching worktree",
  review: "reviewing",
  chat: "working",
  wake: "carrying on by itself",
};

// ended is the last turn's badge: a run that is over otherwise leaves the bar
// blank, and what it did goes unsaid. A turn that asked you outranks it.
function ended(finished: Finished | null | undefined): Badge | undefined {
  if (!finished) return undefined;
  if (finished.asking) {
    return {
      text: "$(question) ade · waiting on you",
      tooltip: "The turn ended by asking you something.",
      warn: true,
    };
  }
  const files = finished.files ?? 0;
  if (!files) {
    return {
      text: "$(check) ade · nothing changed",
      tooltip: "The last turn changed no files.",
      warn: false,
    };
  }
  const what = `${files} file${files === 1 ? "" : "s"} changed`;
  return {
    text: `$(check) ade · ${what}`,
    tooltip: `The last turn ${what}. Click to write a commit message for them.`,
    warn: false,
  };
}

// offersCommit is whether the badge is a finished turn with something to
// commit, which is the one idle state with an action behind it.
export function offersCommit(state: RunState | null | undefined): boolean {
  return Boolean(
    state &&
      !state.running &&
      !state.children?.length &&
      !state.finished?.asking &&
      (state.finished?.files ?? 0) > 0,
  );
}

// counted names what is working in its own words. A sub-agent and a
// backgrounded command are not the same thing to anyone reading the bar.
function counted(kids: Child[]): string {
  const tasks = kids.filter((k) => k.kind === "task").length;
  if (tasks === kids.length)
    return kids.length === 1 ? "1 task running" : `${kids.length} tasks running`;
  if (tasks === 0)
    return kids.length === 1 ? "1 agent working" : `${kids.length} agents working`;
  return `${kids.length} working`;
}

// childLine is the sub-agent being waited on, or the first of several: its
// name, then its own step. Empty when none is working.
function childLine(state: RunState): string {
  const kids = state.children ?? [];
  if (!kids.length) return "";
  const more = kids.length > 1 ? ` +${kids.length - 1}` : "";
  return ` · ${kids[0].name} › ${kids[0].doing || "starting"}${more}`;
}

// STUCK_AFTER is how many times over the same call has to be the one in hand
// before the run is called round rather than working. Three: twice is a retry.
const STUCK_AFTER = 3;

// going is a run repeating itself. Silence is not the only way to be stuck,
// and the louder way is to look busy doing the same thing over.
export function going(state: RunState, now: number): Badge | undefined {
  const times = state.repeats ?? 0;
  if (times < STUCK_AFTER || !state.doing) return undefined;
  const from = state.repeatedAt ? now - Date.parse(state.repeatedAt) : NaN;
  const age = Number.isNaN(from) ? "" : ` · ${elapsed(from)}`;
  return {
    text: `$(issue-reopened) ade · looks stuck · ${times}× ${state.doing}${age}`,
    tooltip: `The agent has run this ${times} times over with nothing else between. Click to stop it or ask what is happening.`,
    warn: true,
  };
}

// Silence is named after a minute and turns to a warning after five.
const QUIET_SHOWN = 60_000;
const QUIET_WARNED = 5 * 60_000;

export function badge(
  state: RunState | null | undefined,
  since: number,
  quiet = 0,
): Badge | undefined {
  if (!state) return undefined;
  if (state.error) {
    return {
      text: "$(error) ade",
      tooltip: `The last run failed: ${state.error}`,
      warn: true,
    };
  }
  if (!state.running) {
    const kids = state.children ?? [];
    if (kids.length) {
      return {
        text: `$(sync~spin) ade · ${counted(kids)}${childLine(state)}`,
        tooltip: kids
          .map((k) => `${k.name}: ${k.doing || "starting"}`)
          .join("\n"),
        warn: false,
      };
    }
    const over = ended(state.finished);
    if (over) return over;
    const queued = state.queued ?? 0;
    if (queued > 0) {
      return {
        text: `$(clock) ade · ${queued} queued`,
        tooltip: `${queued} turn${queued === 1 ? "" : "s"} waiting. Click to see them, or “ade: Run the queue”.`,
        warn: false,
      };
    }
    return undefined;
  }

  // A paused turn is running but going nowhere alone: a spinner there is a lie.
  if (state.asking?.length) {
    return {
      text: "$(question) ade · waiting on you",
      tooltip: "The agent is holding a tool call until you answer it in the chat.",
      warn: true,
    };
  }

  const round = going(state, Date.now());
  if (round) return round;

  const what = WORDS[state.kind ?? ""] ?? state.kind ?? "working";
  const parts = [what];
  if (since > 0) parts.push(elapsed(since));
  if (state.queued) parts.push(`${state.queued} queued`);
  const age = since > 0 ? ` ${elapsed(since)}` : "";
  const silent = quiet >= QUIET_SHOWN ? ` · quiet ${elapsed(quiet)}` : "";
  const doing = childLine(state) || (state.doing ? ` · ${state.doing}` : "");
  const touched = state.touched?.length
    ? `${state.touched.length} file${state.touched.length === 1 ? "" : "s"} touched: ${state.touched.join(", ")}`
    : "";
  return {
    text: `$(sync~spin) ade · ${what}${age}${silent}${doing}`,
    tooltip: [parts.join(" · "), state.model, state.sandbox ? "sandboxed" : "", touched]
      .filter(Boolean)
      .join("\n"),
    warn: quiet >= QUIET_WARNED,
  };
}

// quiet is how long a running turn has said nothing, in milliseconds.
export function quiet(state: RunState | null | undefined, now: number): number {
  if (!state?.running || !state.lastEventAt) return 0;
  const last = Date.parse(state.lastEventAt);
  if (Number.isNaN(last)) return 0;
  return Math.max(0, now - last);
}

// since is how long the run has been going, in milliseconds, or 0 when the
// server did not say. A clock skew reads as a negative age, which is no age.
export function since(state: RunState | null | undefined, now: number): number {
  if (!state?.running || !state.startedAt) return 0;
  const started = Date.parse(state.startedAt);
  if (Number.isNaN(started)) return 0;
  return Math.max(0, now - started);
}

// elapsed is a duration a glance can read. Seconds below a minute, then
// minutes: a run measured to the second after ten minutes is noise.
export function elapsed(ms: number): string {
  const secs = Math.floor(ms / 1000);
  if (secs < 60) return `${secs}s`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m`;
  return `${Math.floor(mins / 60)}h${mins % 60}m`;
}
