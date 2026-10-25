import { describe, expect, it } from "vitest";
import { CHAT_MAX, CHAT_MIN, isChat, sideWidth, SIDEBAR_MIN } from "./dock";

describe("dragging a side wider", () => {
  it("takes the width asked for", () => {
    expect(sideWidth("chat", 460)).toBe(460);
    expect(sideWidth("sidebar", 320)).toBe(320);
  });

  it("never lets the chat collapse itself", () => {
    expect(sideWidth("chat", 40)).toBe(CHAT_MIN);
    expect(sideWidth("chat", -500)).toBe(CHAT_MIN);
    expect(sideWidth("chat", 4_000)).toBe(CHAT_MAX);
  });

  it("holds the sidebar's own limits, which are not the chat's", () => {
    expect(sideWidth("sidebar", 40)).toBe(SIDEBAR_MIN);
    expect(SIDEBAR_MIN).toBeLessThan(CHAT_MIN);
  });

  it("gives whole pixels", () => {
    expect(sideWidth("chat", 460.4)).toBe(460);
  });
});

describe("which panels are the dock", () => {
  it("is every command that used to open a chat tab", () => {
    expect(isChat("ade.openChat")).toBe(true);
    expect(isChat("ade.focusChat")).toBe(true);
    expect(isChat("ade.concierge")).toBe(true);
  });

  it("is not a panel that still is a tab", () => {
    expect(isChat("ade.worktrees")).toBe(false);
    expect(isChat("design:x.md")).toBe(false);
  });
});
