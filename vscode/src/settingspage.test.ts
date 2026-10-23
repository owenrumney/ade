import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const page = readFileSync(join(__dirname, "settings.ts"), "utf8");
// The form is its own module, shared with ade's app so the two hosts
// draw one page. Assertions about fields read it there.
const body = readFileSync(join(__dirname, "settings", "body.ts"), "utf8");
const script = readFileSync(join(__dirname, "settings", "main.ts"), "utf8");

describe("the settings webview", () => {
  it("keeps the server token out of its html", () => {
    const markup = page.slice(
      page.indexOf("<!doctype html>"),
      page.indexOf("</html>") + "</html>".length,
    );
    expect(markup).not.toContain("server.token");
    expect(markup).not.toContain("data-token");
    // Nor into the page by message: the editor makes the requests now.
    expect(page).not.toContain("token: this.server.token");
    expect(page).toContain('kind: "serving"');
  });

  it("loads settings, patches the form, and writes secrets alone", () => {
    expect(script).toContain('request("/api/settings")');
    expect(script).toContain('method: "PATCH"');
    expect(script).toContain('`/api/settings/${route}`');
    expect(script).toContain('method: "DELETE"');
  });

  it("has dashboard fields without reading its token back", () => {
    expect(body).toContain('id="dashboard-url"');
    expect(body).toContain('data-secret="dashboard-token"');
    expect(script).toContain("dashboardTokenSet");
  });

  it("has a switch for each cheap-model helper", () => {
    expect(body).toContain('id="sharpen-off"');
    expect(body).toContain('id="cleanse-off"');
    expect(body).toContain('id="merge-queued-off"');
    expect(script).toContain("cleanseOff");
    expect(script).toContain("mergeQueuedOff");
  });
});
