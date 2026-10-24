import { describe, expect, it } from "vitest";
import { Keymap, chordOf, merge, shared, DEFAULTS, type Binding } from "./keymap";
import { BAR } from "../renderer/model";

const on = (bindings: Binding[], mac = true) => new Keymap(bindings, mac);

describe("writing a keystroke as a chord", () => {
  it("puts the modifiers in one order, whatever order they were pressed", () => {
    expect(chordOf({ key: "P", meta: true, shift: true })).toBe("cmd+shift+p");
    expect(chordOf({ key: "p", shift: true, meta: true })).toBe("cmd+shift+p");
  });

  it("names the keys a glyph cannot carry", () => {
    expect(chordOf({ key: "Escape" })).toBe("escape");
    expect(chordOf({ key: "ArrowDown", alt: true })).toBe("alt+down");
    expect(chordOf({ key: " ", meta: true })).toBe("cmd+space");
  });
});

describe("what a keystroke means", () => {
  it("takes mod as cmd on a Mac and ctrl elsewhere", () => {
    const binding = [{ keys: "mod+k", command: "omnibox" }];
    expect(on(binding, true).match({ key: "k", meta: true })).toBe("omnibox");
    expect(on(binding, true).match({ key: "k", control: true })).toBeUndefined();
    expect(on(binding, false).match({ key: "k", control: true })).toBe("omnibox");
  });

  it("is nothing for a key nobody bound, so focus keeps it", () => {
    expect(on(DEFAULTS).match({ key: "a" })).toBeUndefined();
    expect(on(DEFAULTS).match({ key: "x", meta: true })).toBeUndefined();
  });

  it("answers on the press and not the release", () => {
    const map = on([{ keys: "mod+k", command: "omnibox" }]);
    expect(map.match({ key: "k", meta: true, type: "keyDown" })).toBe("omnibox");
    expect(map.match({ key: "k", meta: true, type: "keyUp" })).toBeUndefined();
  });
});

describe("the person's own bindings", () => {
  it("override a default that names the same chord", () => {
    const map = on(merge([{ keys: "mod+k", command: "mine" }]));
    expect(map.match({ key: "k", meta: true })).toBe("mine");
  });

  it("add one the defaults do not have", () => {
    const map = on(merge([{ keys: "mod+j", command: "jump" }]));
    expect(map.match({ key: "j", meta: true })).toBe("jump");
    expect(map.match({ key: "k", meta: true })).toBe("omnibox");
  });

  it("unbind a default by naming no command", () => {
    const map = on(merge([{ keys: "mod+k", command: "" }]));
    expect(map.match({ key: "k", meta: true })).toBeUndefined();
  });

  it("are ignored when they name no chord", () => {
    expect(() => merge([{ keys: "", command: "nowhere" }])).not.toThrow();
    expect(merge([{ keys: "", command: "nowhere" }])).toEqual(DEFAULTS);
  });
});

// ⌘K is the whole reason ade is an app rather than a page.
describe("the shipped table", () => {
  it("opens the omnibox on cmd+k", () => {
    expect(on(DEFAULTS).match({ key: "k", meta: true })).toBe("omnibox");
  });

  // The digits are read off the bar, so a reordered bar with the old keys
  // would send ⌘2 somewhere other than the second icon.
  it("sends each digit to the icon at that position", () => {
    const map = on(DEFAULTS);
    for (const [i, place] of BAR.slice(0, 9).entries()) {
      expect(map.match({ key: String(i + 1), meta: true }), place).toBe(`place.${place}`);
    }
    expect(map.match({ key: "0", meta: true })).toBe("place.review");
    expect(map.match({ key: "0", meta: true, shift: true })).toBe("place.code");
  });

  it("binds no chord twice", () => {
    const seen = new Set<string>();
    for (const binding of on(DEFAULTS).listed()) {
      expect(seen.has(binding.keys), binding.keys).toBe(false);
      seen.add(binding.keys);
    }
  });
});

describe("a chord the page gets too", () => {
  // Escape closes what the page opened — an image in the chat — and the
  // shell used to swallow it, which took the chat away instead.
  it("is escape, and only escape", () => {
    expect(shared("dismiss")).toBe(true);
    for (const binding of DEFAULTS) {
      if (binding.command === "dismiss") continue;
      expect(shared(binding.command), binding.keys).toBe(false);
    }
  });
});
