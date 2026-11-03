import { describe, expect, it } from "vitest";
import { docRef } from "./docref";

const HERE = "/Users/o/code/coast";

describe("a design document named in a conversation", () => {
  it("opens through the session when it is this checkout's", () => {
    expect(docRef(`${HERE}/.git/ade/docs/TRK-1337.md`, HERE)).toEqual({
      panel: "design:TRK-1337.md",
      title: "TRK-1337",
    });
  });

  // A reply often names the document the way the instructions do.
  it("takes a path relative to the checkout", () => {
    expect(docRef(".git/ade/docs/project-memory.md", HERE)).toEqual({
      panel: "design:project-memory.md",
      title: "project-memory",
    });
  });

  it("opens through the launcher when it is another checkout's", () => {
    expect(docRef("/Users/o/code/ade/.git/ade/docs/plan.md", HERE)).toEqual({
      panel: "spacedesign:/Users/o/code/ade|plan.md",
      title: "plan",
    });
  });

  it("is nothing for an ordinary file", () => {
    expect(docRef(`${HERE}/internal/web/web.go`, HERE)).toBeUndefined();
    expect(docRef(`${HERE}/docs/plan.md`, HERE)).toBeUndefined();
    expect(docRef(`${HERE}/.git/ade/outbox/shot.png`, HERE)).toBeUndefined();
  });

  it("is nothing for a document not written as markdown", () => {
    expect(docRef(`${HERE}/.git/ade/docs/notes.txt`, HERE)).toBeUndefined();
  });

  // The path came out of a reply, so it is text like any other.
  it("refuses a path that climbs out of the docs directory", () => {
    expect(docRef(`${HERE}/.git/ade/docs/../../../etc/passwd.md`, HERE)).toBeUndefined();
  });
});
