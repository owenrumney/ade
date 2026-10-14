import type { Badge } from "./runstate";
import type { VerifyState } from "./client";

// What the status bar says about the repository's own checks. One glyph: the
// conversation holds the failure, and this only says whether to go and read it.
export function badge(
  state: VerifyState | null | undefined,
): Badge | undefined {
  if (!state) return undefined;
  if (state.running) {
    // A narrow pass is feedback, so it never gets the tick the full list earns.
    const narrowed = state.narrow && state.total > 0 && !state.failed;
    return {
      text: "$(sync~spin) checks",
      tooltip: narrowed
        ? `Narrow pass: ${state.total} passed. Running the full list`
        : "Running this repository's checks",
      warn: false,
    };
  }
  if (state.offer?.length) {
    return {
      text: "$(question) checks",
      tooltip: "ade can run this repository's checks after a turn. Set them up?",
      warn: false,
    };
  }
  // Stopping them yourself is not a failure and does not light up red.
  if (state.stopped) {
    return {
      text: "$(circle-slash) checks",
      tooltip: "The checks were stopped",
      warn: false,
    };
  }
  if (state.passed && !state.narrow) {
    return {
      text: `$(check) ${state.total}`,
      tooltip: `All ${state.total} checks passed${when(state.at)}`,
      warn: false,
    };
  }
  if (state.failed) {
    return {
      text: `$(error) ${state.failed}`,
      tooltip: `${state.failed} failed${when(state.at)} — ${state.ran} of ${state.total} ran${state.narrow ? " in the narrow pass" : ""}`,
      warn: true,
    };
  }
  return undefined;
}

// summary is the one line the quick pick is titled with.
export function summary(checks: { name: string }[]): string {
  if (checks.length === 0) return "no checks configured";
  return checks.map((check) => check.name).join(", ");
}

function when(at: string | undefined): string {
  if (!at) return "";
  const stamp = new Date(at);
  if (Number.isNaN(stamp.getTime())) return "";
  return ` at ${stamp.toLocaleTimeString()}`;
}
