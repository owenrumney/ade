import { describe, expect, it } from "vitest";
import { plainly } from "./client";

// A failed claim read "/api/bus/claim: 502 {"error":"check out #78 in …"}".
// The sentence ade wrote for the person was the one part buried.
describe("what a failed route is shown as", () => {
  it("is ade's own words when it sent any", () => {
    expect(
      plainly("/api/bus/claim", 409, '{"error":"infracost/lsp#78 is already merged"}'),
    ).toBe("infracost/lsp#78 is already merged");
  });

  it("keeps the route and the status when there is nothing better", () => {
    expect(plainly("/api/chat", 500, "proxy fell over")).toBe(
      "/api/chat: 500 proxy fell over",
    );
    expect(plainly("/api/chat", 500, "{}")).toBe("/api/chat: 500 {}");
    expect(plainly("/api/chat", 502, '{"error":"  "}')).toBe(
      '/api/chat: 502 {"error":"  "}',
    );
  });
});
