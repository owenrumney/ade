// What a place's refresh asks, and what the status bar says it is asking:
// Issues is Linear and Team is agentbus, neither of them GitHub.
export interface Refresh {
  say: string;
  at?: "launcher";
  path?: string;
}

const DASH = (only: string, say: string): Refresh => ({
  say,
  at: "launcher",
  path: `/api/dash/refresh?only=${only}`,
});

const BY: Record<string, Refresh> = {
  // A git repository, so its refresh is a push and a pull.
  designs: { say: "syncing designs…", at: "launcher", path: "/api/designs/sync" },
  prs: DASH("reviews", "asking GitHub…"),
  waiting: DASH("reviews", "asking GitHub…"),
  inbox: DASH("notifications", "asking GitHub…"),
  issues: DASH("tickets", "asking Linear…"),
  team: { say: "asking agentbus…", at: "launcher", path: "/api/bus/refresh" },
};

// A place ade answers for itself is a re-read and nothing more.
export function refreshFor(place: string): Refresh {
  return BY[place] ?? { say: "reading…" };
}
