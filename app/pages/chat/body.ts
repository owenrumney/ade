// The conversation's markup. `chat.js` reads every one of these ids on
// load, and two hosts write the page, so it lives here.

// Nothing of VS Code's: the host supplies the addresses in it.

export interface Chrome {
  // base is the session server this page fetches and streams from.
  base: string;
  // mic is the url of media/mic.js, which the page loads on demand.
  mic: string;
  // desk is the concierge, which reads no repository and says so.
  desk?: boolean;
}

export function body({ base, mic, desk = false }: Chrome): string {
  return `<div id="ade" data-base="${base}" data-mic="${mic}"${desk ? ' class="desk"' : ""}>
      ${desk ? DESK_HEADER : ""}
      <div id="list"></div>
      <div id="composer">
        <div id="tray"></div>
        <textarea id="box" placeholder="${desk ? DESK_ASK : SESSION_ASK}"></textarea>
        <div id="sharpen"></div>
        <div id="row">
          <button id="send">Send</button>
          <button id="mic" aria-label="Dictate" title="Hold to dictate. Cmd+Alt+V or Ctrl+Alt+V toggles it.">${MIC_ICON}</button>
          <button id="stop" title="Cancel the running turn. The session stays up." hidden>Stop</button>
          <span id="hint">Enter to send · Shift+Enter for a new line · paste an image or drop a file</span>
        </div>
        <div id="note"></div>
      </div>
    </div>`;
}

// The same desk bell the activity bar shows, inline so it takes the theme
// colour without a webview uri in the stylesheet.
const DESK_BELL =
  '<svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" ' +
  'stroke-linecap="round" stroke-linejoin="round" stroke-width="1.7">' +
  '<path d="M4.8 14.4 A7.2 7.2 0 0 1 19.2 14.4" />' +
  '<rect x="3" y="14.4" width="18" height="4" rx="1.6" />' +
  '<path d="M12 4.6 V6.6" />' +
  '<circle cx="12" cy="3.1" r="1.4" fill="currentColor" stroke="none" /></svg>';

export const MIC_ICON =
  '<svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" ' +
  'stroke-linecap="round" stroke-linejoin="round" stroke-width="2">' +
  '<path d="M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3z" />' +
  '<path d="M19 11v1a7 7 0 0 1-14 0v-1" />' +
  '<path d="M12 19v3" /><path d="M8 22h8" /></svg>';

// The desk's header. It is the one thing that cannot be mistaken for the
// session's chat at a glance, which is the whole job of drawing it.
const DESK_HEADER =
  '<div id="desk"><span id="deskbell">' +
  DESK_BELL +
  '</span> Concierge' +
  '<span id="deskwhat">the front desk · it cannot read this repository</span></div>';

const DESK_ASK = "Leave a note, ask what is running, or say what to get to.";
const SESSION_ASK =
  "Ask for something. It can read the repository, answer, or make the change.";
