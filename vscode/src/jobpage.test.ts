import { describe, expect, it } from "vitest";
import { newest, page, took, unread } from "./jobpage";

describe("took", () => {
  it("says seconds, then minutes and seconds", () => {
    expect(took(8_400)).toBe("8s");
    expect(took(125_000)).toBe("2m 5s");
  });
});

describe("a job's reports", () => {
  const early = {
    firedAt: "2026-10-01T09:00:00Z",
    millis: 4000,
    report: "PR 41 has two failing checks.",
  };
  const late = {
    firedAt: "2026-10-02T09:00:00Z",
    millis: 61000,
    costUsd: 0.4,
    error: "exit status 1",
  };

  it("lists the newest first", () => {
    expect(newest([early, late])[0]).toBe(late);
    const text = page("morning", [early, late]);
    expect(text.indexOf("2026-10-02 09:00")).toBeLessThan(
      text.indexOf("2026-10-01 09:00"),
    );
  });

  it("says a failure, what it cost, and that silence was silence", () => {
    const text = page("morning", [late]);
    expect(text).toContain("1m 1s · $0.40");
    expect(text).toContain("**Failed:** exit status 1");
    expect(text).toContain("_Nothing to report._");
  });

  it("says a run left no handover", () => {
    const text = page("m", [{ ...early, noHandover: true }]);
    expect(text).toContain("left no handover");
  });

  it("says so when the job has never run", () => {
    expect(page("morning", [])).toContain("has not run yet");
  });
});

describe("unread", () => {
  const at = "2026-10-01T09:00:00Z";

  it("is what said something and has not been read", () => {
    const got = unread([
      { firedAt: at, millis: 1, report: "look at this" },
      { firedAt: at, millis: 1, report: "old news", seen: true },
      { firedAt: at, millis: 1 },
      { firedAt: at, millis: 1, error: "exit status 1" },
    ]);
    expect(got.map((o) => o.report ?? o.error)).toEqual([
      "look at this",
      "exit status 1",
    ]);
  });

  it("marks the new ones on the page", () => {
    const text = page("m", [
      { firedAt: "2026-10-02T09:00:00Z", millis: 1, report: "fresh" },
      { firedAt: at, millis: 1, report: "read", seen: true },
    ]);
    expect(text).toContain("## 2026-10-02 09:00 · new");
    expect(text).toContain("## 2026-10-01 09:00\n");
  });
});
