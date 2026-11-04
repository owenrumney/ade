import { describe, expect, it } from "vitest";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { onScreen, skeleton } from "./panel";
import { declared, read, taken, THEME } from "./paneltheme";

const PANEL = {
  id: "sandbox",
  title: "Sandbox",
  root: "sandbox",
  bundle: "sandbox.js",
  styles: ["sandbox.css", "mounts.css"],
  data: { base: "http://127.0.0.1:7777", note: 'a "quoted" one' },
  async onMessage() {
    return undefined;
  },
};

describe("the host half of a webview", () => {
  // A backtick in the comment inside this template ended it, and the
  // words after it became identifiers: the page built, and every panel
  // threw on open. Nothing in the markup is read by a test otherwise.
  it("is html and nothing else", () => {
    const html = skeleton(PANEL);
    expect(html.startsWith("<!doctype html>")).toBe(true);
    // The head is where an expression that got away would land.
    const head = html.slice(0, html.indexOf("<body>"));
    expect(head).not.toMatch(/NaN|\[object|undefined/);
  });

  it("names the panel's own bundle, styles and root", () => {
    const html = skeleton(PANEL);
    expect(html).toContain("ade-webview://asset/dist/sandbox.js");
    expect(html).toContain("ade-webview://asset/media/sandbox.css");
    expect(html).toContain('id="sandbox"');
  });

  it("allows only the webview scheme and loopback", () => {
    const html = skeleton(PANEL);
    const csp = /content="([^"]*default-src[^"]*)"/.exec(html)?.[1] ?? "";
    expect(csp).toContain("default-src 'none'");
    expect(csp).toMatch(/script-src .nonce-[a-z0-9]+. ade-webview:/);
    expect(csp).toContain("connect-src http://127.0.0.1:* http://localhost:*");
    expect(csp).not.toContain("script-src 'unsafe-inline'");
  });

  it("escapes a quote in the data it writes onto the root", () => {
    expect(skeleton(PANEL)).toContain('data-note="a &quot;quoted&quot; one"');
  });
});

// Refreshing every kept panel on every agent step is where the /api/ship git
// commands were going.
describe("which panels a stir re-reads", () => {
  const node = (hidden: boolean) =>
    ({ closest: (sel: string) => (hidden && sel === "[hidden]" ? {} : null) }) as unknown as HTMLElement;

  it("reads the pane in front", () => {
    expect(onScreen(node(false))).toBe(true);
  });

  it("leaves a pane behind another tab alone", () => {
    expect(onScreen(node(true))).toBe(false);
  });
});

describe("the theme a panel is given", () => {
  // A panel is an iframe and cannot reach theme.css, so nothing set the
  // palette there: every panel drew in the browser's own colours, and
  // the chat's composer was dark text on a dark ground.
  const media = join(__dirname, "..", "media");
  const sheets = readdirSync(media)
    .filter((f) => f.endsWith(".css"))
    .map((f) => readFileSync(join(media, f), "utf8"));

  // A page that styles itself in code reads the palette there, which no
  // stylesheet shows: designedit and explainpanel both do.
  const pages = join(__dirname, "..", "pages");
  const code = readdirSync(pages, { withFileTypes: true })
    .filter((e) => e.isDirectory())
    .map((e) => join(pages, e.name, "main.ts"))
    .filter((f) => existsSync(f))
    .map((f) => readFileSync(f, "utf8"));

  // A sheet that declares a name itself is not asking the theme for it.
  const wanted = new Set<string>();
  for (const css of sheets) {
    const own = new Set(declared(css));
    for (const name of read(css)) if (!own.has(name)) wanted.add(name);
  }
  for (const ts of code) for (const name of taken(ts)) wanted.add(name);

  it("sets every variable the panels read", () => {
    const set = new Set(declared(THEME));
    const missing = [...wanted].filter((v) => !set.has(v)).sort();
    expect(missing, `add these to paneltheme.ts: ${missing.join(", ")}`).toEqual([]);
  });

  it("is in the page before its stylesheets", () => {
    const html = skeleton(PANEL);
    expect(html.indexOf("--ink")).toBeLessThan(html.indexOf("sandbox.css"));
  });
});

describe("the shim the panels run on", () => {
  // It is inline, so the policy has to name it. Refused, it leaves
  // `acquireVsCodeApi` undefined: the page draws and nothing works,
  // which is what an empty chat with a live composer looked like.
  it("is allowed by the page's own policy", () => {
    const html = skeleton(PANEL);
    const nonce = /<script nonce="([^"]+)">/.exec(html)?.[1];
    expect(nonce, "the shim carries no nonce").toBeTruthy();
    expect(html).toContain(`script-src 'nonce-${nonce}'`);
  });

  it("gives the page a different nonce each time", () => {
    const of = (html: string) => /<script nonce="([^"]+)">/.exec(html)?.[1];
    expect(of(skeleton(PANEL))).not.toBe(of(skeleton(PANEL)));
  });

  it("names the panel it speaks for", () => {
    expect(skeleton(PANEL)).toContain('from: "sandbox"');
  });
});

describe("the scrollbar in a panel", () => {
  // Chromium draws a light one by default, which on this background is
  // a white stripe down the side of every list.
  it("is themed, like the window's own", () => {
    const html = skeleton(PANEL);
    expect(html).toContain("::-webkit-scrollbar-thumb");
    expect(html).toContain("scrollbar-color: var(--line)");
  });
});
