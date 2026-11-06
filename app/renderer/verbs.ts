// ade's vocabulary: every verb the omnibox offers, in the order it offers
// them. `doing.ts` says what each one does; this says what it is called.

export interface Group {
  group: string;
  verbs: [command: string, name: string][];
}

// Wired but never listed: ade.menu is the box itself.
export const UNLISTED = ["ade.menu"];

export const VOCABULARY: Group[] = [
  {
    group: "Sessions",
    verbs: [
      ["ade.newSession", "New Session"],
      ["ade.newScratch", "Start on something with no repository yet"],
      ["ade.openAgent", "Agent"],
      ["ade.findSession", "Find a session…"],
      ["ade.diffTurn", "Diff"],
      ["ade.openTerminal", "Shell"],
      ["ade.renameSession", "Rename this session…"],
      ["ade.tagSession", "Tag this session…"],
      ["ade.openBrief", "Open this session's brief"],
      ["ade.sendBrief", "Tell the agent to read the brief"],
      ["ade.closeSession", "Close this session"],
      ["ade.portScratch", "Move this scratch session's work into a repository"],
      ["ade.refreshSessions", "Refresh sessions"],
    ],
  },
  {
    group: "Diff",
    verbs: [
      ["ade.refreshChanges", "Refresh the diff"],
      ["ade.setScope", "Diff scope"],
      ["ade.scopeBranch", "Scope: branch"],
      ["ade.scopeCommitted", "Scope: committed"],
      ["ade.scopeUncommitted", "Scope: uncommitted"],
      ["ade.diffRoot", "Choose which repository the Diff shows"],
      ["ade.stageAll", "Stage all"],
      ["ade.unstageAll", "Unstage all"],
      ["ade.pullBase", "Pull the base branch"],
    ],
  },
  {
    group: "Review",
    verbs: [
      ["ade.review", "Review the diff"],
      ["ade.reviewWithAgents", "Review the diff with named agents"],
      ["ade.stop", "Stop the review that is running"],
      ["ade.sendFindings", "Send the open comments to the agent"],
      ["ade.addToReview", "Add the written-up comments to the draft review"],
      ["ade.finishReview", "Send the draft review to the author, with a verdict"],
      ["ade.sendReplies", "Send the waiting replies to GitHub"],
      ["ade.pruneOrphans", "Remove comments whose code has gone"],
      ["ade.removeResolved", "Remove resolved comments"],
      ["ade.removeDeclined", "Delete all declined comments"],
      ["ade.removeAllComments", "Remove every comment"],
    ],
  },
  {
    group: "Commit and pull request",
    verbs: [
      ["ade.writeCommitMessage", "Write a commit message"],
      ["ade.push", "Push"],
      ["ade.openPRLink", "Open a pull request from its link…"],
      ["ade.approvePR", "Approve this pull request"],
      ["ade.requestChanges", "Request changes on this pull request"],
      ["ade.mergePR", "Merge this pull request"],
      ["ade.refreshChecks", "Refresh checks"],
    ],
  },
  {
    group: "Lists",
    verbs: [
      ["ade.well", "Notifications"],
      ["ade.myDay", "My Day"],
      ["ade.pendingPRs", "Pull requests"],
      ["ade.showInbox", "Inbox"],
      ["ade.team", "Team"],
      ["ade.issues", "Issues…"],
      ["ade.spaces", "Spaces"],
      ["ade.openSpace", "Open a space…"],
      ["ade.closeSpace", "Close a space…"],
    ],
  },
  {
    group: "Team",
    verbs: [
      ["ade.askTeamForReview", "Ask the team to review this"],
      ["ade.messageTeamMember", "Send message"],
      ["ade.setPresence", "Show as active or away"],
      ["ade.dismissReply", "Clear this answer"],
      ["ade.dismissOffer", "Dismiss"],
      ["ade.refreshTeam", "Refresh the team roster"],
    ],
  },
  {
    group: "ade itself",
    verbs: [
      ["ade.openSettings", "Settings"],
      ["ade.keys", "Keys and tokens…"],
      ["ade.openPermissions", "What ade refuses an agent"],
      ["ade.toggleSidebar", "Hide or show the sessions"],
      ["ade.startLauncher", "Start the launcher"],
      ["ade.reconnect", "Reconnect"],
      ["ade.showLogs", "Show the log"],
    ],
  },
];
