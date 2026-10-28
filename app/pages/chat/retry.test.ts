import { describe, expect, it } from "vitest";
import { delayFor, FIRST, LONGEST } from "./retry";

describe("waiting for a server that is still starting", () => {
  it("tries straight away, then doubles", () => {
    expect(delayFor(1)).toBe(1_000);
    expect(delayFor(2)).toBe(2_000);
    expect(delayFor(3)).toBe(4_000);
    expect(delayFor(4)).toBe(8_000);
  });

  it("stops widening, and never stops trying", () => {
    expect(delayFor(5)).toBe(LONGEST);
    expect(delayFor(50)).toBe(LONGEST);
    expect(delayFor(5_000)).toBe(LONGEST);
  });

  // A minute of starting used to be ten reads and then silence for ever.
  it("covers a minute in a handful of reads", () => {
    let waited = 0;
    let tries = 0;
    while (waited < 60_000) {
      tries++;
      waited += delayFor(tries);
    }
    expect(tries).toBeLessThan(10);
  });

  it("treats a count it was never given as the first", () => {
    expect(delayFor(0)).toBe(FIRST);
    expect(delayFor(-1)).toBe(FIRST);
  });
});
