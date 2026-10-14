import { expect, it } from "vitest";
import { logLine } from "./logline";

it("prints a record as one line, attributes last", () => {
  expect(
    logLine({
      at: "2026-10-03T09:10:19.123Z",
      level: "WARN",
      msg: "net check failed",
      attrs: [
        { key: "target", value: "linear" },
        { key: "took", value: "10s" },
      ],
      seq: 4,
    }),
  ).toBe("09:10:19 WARN  net check failed target=linear took=10s");
});

it("leaves no trailing space on a record with nothing attached", () => {
  expect(
    logLine({ at: "2026-10-03T09:10:19Z", level: "INFO", msg: "up", seq: 1 }),
  ).toBe("09:10:19 INFO  up");
});
