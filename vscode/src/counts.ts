import type { Repo } from "./launcher";

// The number after a view's title. Nothing at zero: an empty view already
// reads as empty, and a column of "0" is noise.
export function label(n: number, word = ""): string {
  if (n <= 0) return "";
  return word ? `${n} ${word}` : String(n);
}

// Sessions, not repositories: the root rows are repositories, and how many of
// those there are is not what anybody wants to know.
export function sessionsIn(repos: Repo[]): number {
  return repos.reduce(
    (n, repo) =>
      n +
      (repo.checkouts ?? []).reduce((m, c) => m + (c.sessions ?? []).length, 0),
    0,
  );
}
