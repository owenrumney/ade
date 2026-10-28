import { describe, expect, it } from "vitest";
import { base64, downsample, spliced, wav } from "./wav";

describe("wav", () => {
  it("writes a 16 kHz mono 16-bit header and clamped samples", () => {
    const out = wav(new Float32Array([0, 1, -1, 2, 0.5]));
    const header = [
      ..."RIFF".split("").map((c) => c.charCodeAt(0)),
      46, 0, 0, 0,
      ..."WAVE".split("").map((c) => c.charCodeAt(0)),
      ..."fmt ".split("").map((c) => c.charCodeAt(0)),
      16, 0, 0, 0,
      1, 0,
      1, 0,
      0x80, 0x3e, 0, 0,
      0x00, 0x7d, 0, 0,
      2, 0,
      16, 0,
      ..."data".split("").map((c) => c.charCodeAt(0)),
      10, 0, 0, 0,
    ];
    const payload = [0, 0, 0xff, 0x7f, 0x00, 0x80, 0xff, 0x7f, 0xff, 0x3f];
    expect([...out]).toEqual([...header, ...payload]);
  });
});

describe("downsample", () => {
  it("averages each span", () => {
    const got = downsample(new Float32Array([0, 0.5, 1, 0.25, 0.25, 0.25]), 48000, 16000);
    expect([...got]).toEqual([0.5, 0.25]);
  });

  it("leaves a matching rate alone", () => {
    const input = new Float32Array([0.25]);
    expect(downsample(input, 16000)).toBe(input);
  });
});

describe("base64", () => {
  it("encodes bytes", () => {
    expect(base64(new Uint8Array([0, 255, 1]))).toBe("AP8B");
  });
});

describe("spliced", () => {
  it.each([
    ["", 0, 0, "hello", "hello"],
    ["fix the", 7, 7, "parser", "fix the parser"],
    ["fix  bug", 4, 4, "the", "fix the bug"],
    ["fixbug", 3, 3, "the", "fix the bug"],
    ["fix X bug", 4, 5, "the", "fix the bug"],
  ])("puts %j %i-%i with %j", (value, start, end, said, want) => {
    expect(spliced(value, start, end, said).value).toBe(want);
  });

  it("leaves the caret after what was said", () => {
    expect(spliced("ab", 1, 1, "x")).toEqual({ value: "a x b", caret: 4 });
  });
});
