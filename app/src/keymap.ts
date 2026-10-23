// The keyboard is ade's, not the platform's: every key is matched here
// before the page or the menu gets it.

// Chord is a key with its modifiers — "cmd+k", "ctrl+shift+p". Order is
// fixed so two spellings cannot disagree.
export type Chord = string;

// Said is what the main process is handed for each keystroke.
export interface Said {
  key: string;
  code?: string;
  control?: boolean;
  meta?: boolean;
  alt?: boolean;
  shift?: boolean;
  type?: string;
}

// Binding is one row of the keymap: the chord, and what ade does.
export interface Binding {
  keys: Chord;
  command: string;
  // when narrows it to a part of the window, as the renderer reports focus.
  when?: string;
}

// DEFAULTS is the whole keyboard, in one place, so rebinding is an edit and
// never a hunt. ~/.ade/keys.json is laid over this.
export const DEFAULTS: Binding[] = [
  { keys: "mod+k", command: "omnibox" },
  { keys: "mod+p", command: "omnibox.files" },
  { keys: "mod+shift+p", command: "omnibox.commands" },
  { keys: "mod+n", command: "session.new" },
  { keys: "mod+shift+l", command: "chat.focus" },
  { keys: "mod+1", command: "place.prs" },
  { keys: "mod+2", command: "place.designs" },
  { keys: "mod+3", command: "place.sessions" },
  { keys: "mod+4", command: "place.todos" },
  { keys: "mod+5", command: "place.issues" },
  { keys: "mod+6", command: "place.inbox" },
  { keys: "mod+7", command: "place.review" },
  { keys: "mod+8", command: "place.code" },
  { keys: "mod+b", command: "sidebar.toggle" },
  { keys: "mod+shift+b", command: "branch.switch" },
  { keys: "escape", command: "dismiss" },
];

// shared is a chord the page gets too. Escape is the only one: a panel
// has its own things to close, such as an image the chat opened.
export function shared(command: string): boolean {
  return command === "dismiss";
}

// mod is ⌘ on a Mac and Ctrl everywhere else, so one table serves both.
export function expand(keys: Chord, mac: boolean): Chord {
  return keys.replace("mod+", mac ? "cmd+" : "ctrl+");
}

// chordOf writes a keystroke the way the table spells one. Modifiers first,
// in a fixed order, then the key in lower case.
export function chordOf(said: Said): Chord {
  const parts: string[] = [];
  if (said.control) parts.push("ctrl");
  if (said.meta) parts.push("cmd");
  if (said.alt) parts.push("alt");
  if (said.shift) parts.push("shift");
  parts.push(named(said.key));
  return parts.join("+");
}

// named is the key as the table writes it: a letter in lower case, and a
// word for the ones a glyph cannot carry.
function named(key: string): string {
  const words: Record<string, string> = {
    " ": "space",
    Escape: "escape",
    Enter: "enter",
    Tab: "tab",
    Backspace: "backspace",
    Delete: "delete",
    ArrowUp: "up",
    ArrowDown: "down",
    ArrowLeft: "left",
    ArrowRight: "right",
  };
  return words[key] ?? key.toLowerCase();
}

// Keymap answers what a keystroke means. Built once from the table, so a
// keystroke is a lookup and not a scan.
export class Keymap {
  private by = new Map<Chord, Binding>();

  constructor(bindings: Binding[], private mac: boolean) {
    // Later wins, which is what lets ~/.ade/keys.json override a default
    // by naming the same chord.
    for (const binding of bindings) {
      this.by.set(expand(binding.keys, mac), binding);
    }
  }

  // match is the command for a keystroke, or nothing, in which case the key
  // belongs to whatever has focus.
  match(said: Said): string | undefined {
    if (said.type && said.type !== "keyDown") return undefined;
    return this.by.get(chordOf(said))?.command;
  }

  // listed is every binding as it stands, for a window that shows them.
  listed(): Binding[] {
    return [...this.by.values()];
  }
}

// merge lays the person's bindings over the defaults. A command of ""
// unbinds the chord instead of rebinding it.
export function merge(mine: Binding[] | undefined): Binding[] {
  const by = new Map<Chord, Binding>();
  for (const binding of [...DEFAULTS, ...(mine ?? [])]) {
    if (!binding.keys) continue;
    if (binding.command === "") by.delete(binding.keys);
    else by.set(binding.keys, binding);
  }
  return [...by.values()];
}
