// A document the window draws from one read of ade.
export const REPORTS: Record<string, (got: unknown) => string> = {
  logs: (got) => {
    const lines = (got as { lines?: string[] }).lines ?? [];
    if (lines.length === 0) return "# The log\n\nNothing written yet.";
    return "# The log\n\n```\n" + lines.slice(-400).join("\n") + "\n```";
  },
};
