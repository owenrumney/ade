import { describe, expect, it } from "vitest";
import type { Review } from "./launcher";
import { reviewPage } from "./prview";

const asked = (over: Partial<Review> = {}): Review => ({
  repo: "owenrumney/ade",
  number: 42,
  title: "a pull request",
  author: "someone",
  url: "https://github.com/owenrumney/ade/pull/42",
  updatedAt: "2026-10-01T00:00:00Z",
  ...over,
});

describe("the pending pull request page", () => {
  it("offers ade and the web, and the way to put it aside", () => {
    const html = reviewPage(asked(), "n", Date.parse("2026-10-02T00:00:00Z"));
    expect(html).toContain('data-id="ade"');
    expect(html).toContain("Open PR with ade");
    expect(html).toContain('data-id="web"');
    expect(html).toContain("Open in web");
    expect(html).toContain('data-id="mute"');
  });

  it("offers bringing back what was put aside", () => {
    const html = reviewPage(asked({ muted: true }), "n");
    expect(html).toContain('data-id="unmute"');
    expect(html).not.toContain('data-id="mute"');
  });

  it("says what the list knew: the summary line and the address", () => {
    const html = reviewPage(
      asked({ checks: "1 failing", reviewDecision: "CHANGES_REQUESTED" }),
      "n",
      Date.parse("2026-10-02T00:00:00Z"),
    );
    expect(html).toContain("#42 a pull request");
    expect(html).toContain("owenrumney/ade · @someone");
    expect(html).toContain("checks 1 failing");
    expect(html).toContain("changes requested");
    expect(html).toContain("https://github.com/owenrumney/ade/pull/42");
  });

  it("keeps a title with markup in it a title", () => {
    const html = reviewPage(asked({ title: "<script>x</script>" }), "n");
    expect(html).not.toContain("<script>x");
    expect(html).toContain("&lt;script&gt;x");
  });
});
