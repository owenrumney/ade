import type { BranchPR } from "./client";

// blocked is why a verdict cannot be given, or empty when it can. The server's
// own sentence, because a second wording for the same fact is a second answer.
export function blocked(
  pr: BranchPR,
  verdict: "approve" | "request-changes" | "merge",
): string {
  if (verdict === "approve") return pr.approveBlocked ?? "";
  if (verdict === "request-changes") return pr.requestChangesBlocked ?? "";
  return pr.mergeBlocked ?? "";
}

// label is how a pull request is named back to you before you act on it.
export function label(pr: BranchPR): string {
  return `#${pr.number} ${pr.title}`;
}

// mergeMethods are the two the server accepts. Anything else is refused there,
// so offering a third here would only be a worse error message.
export const mergeMethods = ["rebase", "merge"] as const;

// urlOf reads the pull request's address out of whatever the command was
// handed, the same two shapes numberOf copes with.
export function urlOf(arg: unknown): string {
  const one = arg as { url?: string; pr?: { url?: string } } | null;
  return one?.url ?? one?.pr?.url ?? "";
}

// numberOf reads the pull request out of whatever the command was handed. A
// context menu passes the tree node; a row's own click passes the request.
export function numberOf(arg: unknown): number | undefined {
  const one = arg as { number?: number; pr?: { number?: number } } | null;
  const found = one?.number ?? one?.pr?.number;
  return typeof found === "number" && found > 0 ? found : undefined;
}
