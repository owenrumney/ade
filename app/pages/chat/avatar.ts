import { escape, type Message } from "./render";

// The same two glyphs the web UI draws, so a conversation read in the editor
// and in the browser looks like the same conversation.
const YOU = `<circle cx="8" cy="5.5" r="3" /><path d="M2.5 14.5c.6-3 2.7-4.5 5.5-4.5s4.9 1.5 5.5 4.5" />`;

const ROBOT =
  `<rect x="2.5" y="5.5" width="11" height="8" rx="2" /><path d="M8 5.5V3M6.5 2.5h3" />` +
  `<circle cx="5.75" cy="9.25" r="0.6" fill="currentColor" />` +
  `<circle cx="10.25" cy="9.25" r="0.6" fill="currentColor" />` +
  `<path d="M6 11.5h4" />`;

// kind is which of the three a message gets. A named agent is neither of the
// regulars: @security's verdict must not read as the conversation's.
export function kind(msg: Message): "you" | "agent" | "named" {
  if (msg.kind === "you") return "you";
  return msg.author ? "named" : "agent";
}

// initial is what a named agent's circle holds. One character, upper case, so
// two voices in a row tell apart at a glance.
export function initial(author: string): string {
  return (author.trim().slice(0, 1) || "?").toUpperCase();
}

// avatar is the markup for one message's circle. The author is escaped: it is
// the server's string and goes into html here.
export function avatar(msg: Message): string {
  const which = kind(msg);
  if (which === "named") {
    const name = msg.author ?? "";
    return `<span class="avatar named" title="@${escape(name)}">${escape(initial(name))}</span>`;
  }
  return `<span class="avatar ${which}">${svg(which === "you" ? YOU : ROBOT)}</span>`;
}

function svg(body: string): string {
  return (
    `<svg viewBox="0 0 16 16" width="16" height="16" fill="none" stroke="currentColor" ` +
    `stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${body}</svg>`
  );
}
