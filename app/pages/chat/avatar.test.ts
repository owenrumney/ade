import { describe, expect, it } from "vitest";
import { avatar, initial, kind } from "./avatar";
import type { Message } from "./render";

const msg = (over: Partial<Message>): Message => ({ id: "m", kind: "agent", at: "x", ...over });

describe("kind", () => {
  it("is you for your own message", () => {
    expect(kind(msg({ kind: "you" }))).toBe("you");
  });

  it("is agent for the main one", () => {
    expect(kind(msg({ kind: "agent" }))).toBe("agent");
  });

  // @security's verdict must not read as the conversation's.
  it("is named for an agent that gave its name", () => {
    expect(kind(msg({ kind: "agent", author: "security" }))).toBe("named");
  });
});

describe("initial", () => {
  it("is one upper-case character", () => {
    expect(initial("security")).toBe("S");
    expect(initial("Architect")).toBe("A");
  });

  it("copes with a name that is spaces or empty", () => {
    expect(initial("   ")).toBe("?");
    expect(initial("")).toBe("?");
  });
});

describe("avatar", () => {
  it("draws the person for you and the robot for the agent", () => {
    expect(avatar(msg({ kind: "you" }))).toContain('class="avatar you"');
    expect(avatar(msg({ kind: "you" }))).toContain("<circle");
    expect(avatar(msg({ kind: "agent" }))).toContain('class="avatar agent"');
    expect(avatar(msg({ kind: "agent" }))).toContain("<rect");
  });

  it("draws a named agent's initial rather than either glyph", () => {
    const got = avatar(msg({ author: "security" }));
    expect(got).toContain(">S<");
    expect(got).not.toContain("<svg");
  });

  // The author is the server's string and goes into html here.
  it("escapes an author that is not a plain name", () => {
    const got = avatar(msg({ author: '<img src=x onerror=alert(1)>' }));
    expect(got).not.toContain("<img");
    expect(got).toContain("&lt;");
  });
});
