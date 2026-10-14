import type { LauncherRow } from "./omnibox";
import { inboxRowsFor, queueRowsFor } from "./omnibox";
import type { Row } from "./switcher";

// What the bell holds: everything waiting on you, each as one row that acts.
export interface Pending {
  sessions: Row[];
  offers: Parameters<typeof inboxRowsFor>[1];
  waiting: Parameters<typeof queueRowsFor>[1];
  asking: number;
  hereId?: string;
}

export function wellRows(from: Pending): LauncherRow[] {
  const rows: LauncherRow[] = [];
  if (from.asking > 0) {
    rows.push({
      label: "The agent is waiting on you",
      description: from.asking === 1 ? "a tool call needs an answer" : `${from.asking} tool calls need answers`,
      icon: "question",
      command: "ade.focusChat",
    });
  }
  for (const r of from.sessions.filter((s) => s.waiting && s.session.id !== from.hereId)) {
    rows.push(answerRow(r));
  }
  rows.push(...inboxRowsFor("inbox", from.offers, 10));
  rows.push(...queueRowsFor("queue", from.waiting, 1));
  return rows;
}

// answerRow puts the question rather than only the name, and takes the answer
// here. A session whose server has gone can only be opened.
export function answerRow(r: Row): LauncherRow {
  const who = r.session.title || r.session.branch || r.session.name;
  if (!r.session.live) {
    return {
      label: `${who} asked you`,
      description: r.asked || r.repo,
      icon: "bell",
      command: "ade.omnibox.open",
      args: [{ kind: "session", session: r.session }],
    };
  }
  return {
    label: r.asked ? `${who}: ${r.asked}` : `${who} asked you`,
    description: r.asked ? r.repo : "answer it without going there",
    icon: "bell",
    command: "ade.answerSession",
    args: [r],
  };
}

export function wellCount(from: Pending): number {
  return wellRows(from).length;
}
