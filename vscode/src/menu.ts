// One entry instead of fifty. Grouped by hand: which commands belong
// together is a judgement, and a name prefix is not.
export const GROUPS: { title: string; commands: string[] }[] = [
  {
    title: "Conversation",
    commands: [
      "ade.openChat",
      "ade.focusChat",
      "ade.dictate",
      "ade.arrange",
      "ade.dashboard",
      "ade.diffTurn",
      "ade.ask",
      "ade.newChat",
      "ade.clearChat",
      "ade.stop",
      "ade.stopAgent",
      "ade.stopElsewhere",
      "ade.showQueue",
      "ade.runQueued",
      "ade.pickModel",
    ],
  },
  {
    title: "Sessions",
    commands: [
      "ade.switchSession",
      "ade.switchSessionHere",
      "ade.findSession",
      "ade.newSession",
      "ade.closeSession",
      "ade.renameSession",
      "ade.portScratch",
      "ade.newScratch",
      "ade.combineSessions",
      "ade.switchWorktree",
      "ade.worktrees",
      "ade.worktreeGraph",
      "ade.showAllSessions",
      "ade.collapseSessions",
      "ade.refreshSessions",
    ],
  },
  {
    title: "Diff",
    commands: [
      "ade.refreshChanges",
      "ade.scopeBranch",
      "ade.scopeCommitted",
      "ade.scopeUncommitted",
      "ade.stageAll",
      "ade.unstageAll",
      "ade.blame",
      "ade.history",
    ],
  },
  {
    title: "Review",
    commands: [
      "ade.reviewBranch",
      "ade.review",
      "ade.reviewWithAgents",
      "ade.resolveAll",
      "ade.resolveHere",
      "ade.pruneOrphans",
      "ade.removeResolved",
      "ade.removeDeclined",
      "ade.removeAllComments",
      "ade.setScope",
      "ade.openReview",
      "ade.explain",
      "ade.pullBase",
      "ade.diffRoot",
      "ade.draftReplies",
      "ade.sendReplies",
    ],
  },
  {
    title: "Commit and pull request",
    commands: [
      "ade.commitPanel",
      "ade.commitAmend",
      "ade.writeCommitMessage",
      "ade.push",
      "ade.createPR",
      "ade.showPR",
      "ade.checkoutPR",
      "ade.editPR",
      "ade.approvePR",
      "ade.requestChanges",
      "ade.mergePR",
      "ade.refreshChecks",
    ],
  },
  {
    title: "Design and jobs",
    commands: [
      "ade.concierge",
      "ade.conciergeFloat",
      "ade.well",
      "ade.addTodo",
      "ade.todos",
      "ade.newDesign",
      "ade.design",
      "ade.grillDesign",
      "ade.condenseDesign",
      "ade.build",
      "ade.newJob",
      "ade.jobs",
      "ade.searchWiki",
      "ade.runDrift",
      "ade.drift",
      "ade.memory",
      "ade.distilMemory",
    ],
  },
  {
    title: "Issues and pull requests",
    commands: ["ade.issues", "ade.pendingPRs", "ade.fixChecks"],
  },
  {
    title: "Team",
    commands: [
      "ade.askTeamForReview",
      "ade.messageTeamMember",
      "ade.setPresence",
      "ade.dismissReply",
      "ade.dismissOffer",
      "ade.refreshTeam",
    ],
  },
  {
    title: "Machines and ports",
    commands: [
      "ade.machines",
      "ade.addMachine",
      "ade.startElsewhere",
      "ade.exposePort",
      "ade.openTerminal",
      "ade.diagnose",
    ],
  },
  {
    title: "ade itself",
    commands: [
      "ade.showInbox",
      "ade.startLauncher",
      "ade.openSettings",
      "ade.roles",
      "ade.syncModels",
      "ade.toggleSandbox",
      "ade.toggleFree",
      "ade.checks",
      "ade.sandboxFile",
      "ade.previewURL",
      "ade.showStats",
      "ade.showRepo",
      "ade.showUsage",
      "ade.showGraph",
      "ade.syncWorktrees",
      "ade.disk",
      "ade.openPermissions",
      "ade.reconnect",
      "ade.keys",
      "ade.showLogs",
    ],
  },
];

export interface Command {
  command: string;
  title: string;
}

export interface Contributes {
  commands: Command[];
  menus?: { commandPalette?: { command: string; when?: string }[] };
}

// Need a row, thread or resource to act on. Listed because the manifest now
// hides every ade command, so `when: false` no longer tells these apart.
export const ROW_ONLY = [
  "ade.stage",
  "ade.unstage",
  "ade.omnibox",
  "ade.omnibox.answer",
  "ade.omnibox.recent",
  "ade.omnibox.rows",
  "ade.omnibox.open",
  "ade.promoteQueued",
  "ade.dropQueued",
  "ade.openWorktree",
  "ade.pruneWorktree",
  "ade.answerAsk",
  "ade.takeReview",
  "ade.sendBranchPRTo",
  "ade.notifyWhenBack",
  "ade.reply",
  "ade.markDone",
  "ade.todoFromThread",
  "ade.unfoldThread",
  "ade.foldThread",
  "ade.resolveWithAgent",
  "ade.reopenThread",
  "ade.removeComment",
  "ade.convertForGitHub",
  "ade.sendToGitHub",
  "ade.draftReply",
  "ade.sendReply",
  "ade.editOutgoing",
  "ade.archiveSession",
  "ade.unarchiveSession",
  "ade.deleteSession",
  "ade.pruneMachineSessions",
  "ade.uncombineSession",
  "ade.rememberRepo",
  "ade.forgetRepo",
  "ade.dismissSuggestion",
  "ade.showJobReports",
  "ade.openSession",
  "ade.runJob",
  "ade.pauseJob",
  "ade.resumeJob",
  "ade.deleteJob",
  "ade.openPR",
  "ade.openChange",

];

// Live on a view's title bar and nowhere else. Offering them here puts two
// rows a keystroke apart that do different things, and the wrong one wins.
export const TITLE_ONLY = ["ade.commit"];

export function hidden(): Set<string> {
  return new Set([...ROW_ONLY, ...TITLE_ONLY]);
}

// listed is every command the sub-palette should offer: palette-visible, and
// not the entry that opened it.
export function listed(c: Contributes, self: string): Command[] {
  const out = hidden();
  return c.commands.filter(
    (cmd) => cmd.command !== self && !out.has(cmd.command),
  );
}

export interface Entry {
  label: string;
  command?: string;
  separator?: boolean;
}

// Each group under its heading, then anything the table forgot — a new
// command is reachable before someone remembers to file it.
export function entries(c: Contributes, self: string): Entry[] {
  const offer = new Map(listed(c, self).map((cmd) => [cmd.command, cmd]));
  const out: Entry[] = [];

  for (const group of GROUPS) {
    const rows = group.commands
      .map((id) => offer.get(id))
      .filter((cmd) => cmd !== undefined);
    if (rows.length === 0) continue;
    out.push({ label: group.title, separator: true });
    for (const cmd of rows) {
      out.push({ label: strip(cmd.title), command: cmd.command });
      offer.delete(cmd.command);
    }
  }

  if (offer.size > 0) {
    out.push({ label: "Other", separator: true });
    for (const cmd of offer.values())
      out.push({ label: strip(cmd.title), command: cmd.command });
  }
  return out;
}

// strip drops the "ade: " every title carries. Inside a palette that is
// already ade's, it is on every row and distinguishes none of them.
export function strip(title: string): string {
  return title.replace(/^ade:\s*/i, "");
}
