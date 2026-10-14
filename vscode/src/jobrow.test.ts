import { describe, expect, it } from "vitest";
import {
  context,
  describe as describeJob,
  docPath,
  editable,
  icon,
  JOB,
  jobNamed,
  line,
  rows,
  skeleton,
} from "./jobrow";
import type { Job } from "./launcher";

const job = (over: Partial<Job> = {}): Job => ({
  name: "morning",
  schedule: "0 9 * * 1-5",
  scope: "user",
  path: "/h/.ade/jobs/morning.md",
  ...over,
});

describe("rows", () => {
  it("lists the unreadable files beside the jobs", () => {
    const got = rows(
      [job()],
      [{ path: "/h/.ade/jobs/bad.md", error: "bad schedule" }],
    );
    expect(got).toHaveLength(2);
    expect("skipped" in got[1]).toBe(true);
  });
});

describe("line", () => {
  it("is the schedule when there is nothing else to say", () => {
    expect(line(job())).toBe("0 9 * * 1-5");
  });

  it("names the project a job belongs to", () => {
    expect(line(job({ project: "owenrumney/ade" }))).toContain(
      "owenrumney/ade",
    );
  });

  it("prefers running over paused, because it is happening now", () => {
    expect(line(job({ running: true, paused: true }))).toContain("running");
    expect(line(job({ running: true, paused: true }))).not.toContain("paused");
  });

  it("says failing for a job that stopped working", () => {
    expect(line(job({ failing: true }))).toContain("failing");
  });
});

describe("icon", () => {
  it("spins while a fire is in flight", () => {
    expect(icon(job({ running: true }))).toBe("sync~spin");
  });

  it("distinguishes paused from failing", () => {
    expect(icon(job({ paused: true }))).toBe("debug-pause");
    expect(icon(job({ failing: true }))).toBe("error");
  });
});

describe("editable", () => {
  // A built-in kind runs Go in the launcher, not a prompt: no file to open.
  it("is false for a built-in", () => {
    expect(editable(job({ kind: "wiki-distil" }))).toBe(false);
  });

  it("is true for a prompt job with a file", () => {
    expect(editable(job())).toBe(true);
  });
});

describe("context", () => {
  it("says whether the row can be resumed or paused", () => {
    expect(context(job())).toContain("live");
    expect(context(job({ paused: true }))).toContain("paused");
  });

  it("marks a built-in, which has no edit or delete", () => {
    expect(context(job({ kind: "wiki-distil" }))).toContain("builtin");
  });

  it("marks a repository job, whose file is not the user's to remove", () => {
    expect(context(job({ scope: "repo" }))).toContain("repo");
    expect(context(job())).not.toContain("repo");
  });
});

describe("skeleton", () => {
  it("names the job in the frontmatter the loader reads", () => {
    expect(skeleton("nightly")).toContain("name: nightly");
  });

  it("is frontmatter then a body, which is what Render writes", () => {
    const lines = skeleton("nightly").split("\n");
    expect(lines[0]).toBe("---");
    expect(lines.indexOf("---", 1)).toBeGreaterThan(1);
  });
});

describe("describe", () => {
  const builtin = job({
    name: "wiki-distil",
    kind: "wiki-distil",
    path: "",
    scope: "builtin",
    description: "Distil the sessions into the wiki",
    schedule: "@every 30m",
  });

  it("says what a built-in does and when, since it has no file to read", () => {
    const text = describeJob(builtin);
    expect(text).toContain("# wiki-distil");
    expect(text).toContain("Distil the sessions into the wiki");
    expect(text).toContain("@every 30m");
  });

  it("says why it cannot be edited", () => {
    expect(describeJob(builtin)).toContain("nothing here to edit");
  });
});

describe("the built-in job's document uri", () => {
  it("is one leading slash and one .md", () => {
    expect(docPath("wiki-distil")).toBe("/wiki-distil.md");
  });

  it("reads back the name the row put in it", () => {
    for (const name of ["wiki-distil", "morning", "a.b"])
      expect(jobNamed(docPath(name))).toBe(name);
  });

  it("is under the scheme the content provider registers", () => {
    expect(JOB).toBe("ade-job");
  });
});

describe("what a job row leads with", () => {
  const base = {
    name: "morning",
    schedule: "0 8 * * 1-5",
    scope: "user",
    path: "/j",
    project: "github.com/owenrumney/ade",
  };

  it("puts unread reports and its state before the schedule", () => {
    const got = line({
      ...base,
      failing: true,
      outcomes: [{ firedAt: "2026-10-01T09:00:00Z", millis: 1, report: "x" }],
    } as never);
    expect(got).toBe(
      "1 unread · failing · 0 8 * * 1-5 · github.com/owenrumney/ade",
    );
  });

  it("is only the schedule and project when nothing needs you", () => {
    expect(line(base as never)).toBe("0 8 * * 1-5 · github.com/owenrumney/ade");
  });
});
