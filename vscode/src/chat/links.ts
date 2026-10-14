// A url printed into the chat should be clickable without the text around it
// becoming html. Ported from web/src/lib/links.ts, which this must agree with.
export interface Span {
  text: string;
  href?: string;
}

// A url runs to the first delimiter that cannot be in one. A bare loopback
// host:port counts: a dev server prints one and it is the thing worth clicking.
const URL_RE =
  /https?:\/\/[^\s<>"'`]+|\b(?:localhost|127\.0\.0\.1|\[::1\]):\d{1,5}(?:\/[^\s<>"'`]*)?/g;

// Trailing punctuation is almost always the sentence's, not the url's.
const TRAILING = /[.,;:!?]+$/;

export function splitLinks(text: string): Span[] {
  const out: Span[] = [];
  let at = 0;
  for (const match of text.matchAll(URL_RE)) {
    const start = match.index ?? 0;
    let href = match[0].replace(TRAILING, "");
    // A closing bracket only counts when the url opened one.
    while (href.endsWith(")") && count(href, ")") > count(href, "(")) {
      href = href.slice(0, -1);
    }
    while (href.endsWith("]") && count(href, "]") > count(href, "[")) {
      href = href.slice(0, -1);
    }
    const bare = !/^https?:\/\//.test(href);
    if (!bare && !/^https?:\/\/[^/\s]/.test(href)) continue;
    if (bare && !inPortRange(href)) continue;
    if (start > at) out.push({ text: text.slice(at, start) });
    out.push({ text: href, href: bare ? `http://${href}` : href });
    at = start + href.length;
  }
  if (at < text.length) out.push({ text: text.slice(at) });
  return out;
}

// openable says whether a webview should hand this href to the editor. Only the
// web: a command: uri from rendered markdown must never be followed.
export function openable(href: string): boolean {
  return /^https?:\/\//i.test(href);
}

function count(haystack: string, needle: string): number {
  let n = 0;
  for (const ch of haystack) if (ch === needle) n++;
  return n;
}

function inPortRange(hostPort: string): boolean {
  const port = Number(hostPort.replace(/^.*:(\d+).*$/, "$1"));
  return port >= 1 && port <= 65535;
}
