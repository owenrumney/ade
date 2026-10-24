import { describe, expect, it } from "vitest";
import { refreshFor } from "./refresh";

describe("what a place's refresh asks", () => {
  it("names the service behind the place, not GitHub for all of them", () => {
    expect(refreshFor("issues")).toMatchObject({ say: "asking Linear…", path: "/api/dash/refresh?only=tickets" });
    expect(refreshFor("team")).toMatchObject({ say: "asking agentbus…", path: "/api/bus/refresh" });
    expect(refreshFor("inbox")).toMatchObject({ say: "asking GitHub…", path: "/api/dash/refresh?only=notifications" });
    expect(refreshFor("prs")).toMatchObject({ say: "asking GitHub…", path: "/api/dash/refresh?only=reviews" });
  });

  it("asks nothing for a place ade answers itself", () => {
    for (const place of ["designs", "sessions", "todos", "jobs", "code"]) {
      expect(refreshFor(place)).toEqual({ say: "reading…" });
    }
  });
});
