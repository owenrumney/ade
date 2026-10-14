import { describe, expect, it } from "vitest";
import { page } from "./windowpage";

const now = Date.parse("2026-10-03T12:00:00Z");
const usage = {
  claude: {
    provider: "claude",
    windows: [
      { name: "5h", percent: 68.4, resetsAt: "2026-10-03T14:30:00Z" },
      { name: "7d", percent: 24 },
    ],
    checkedAt: "2026-10-03T11:55:00Z",
  },
};

describe("the plan usage page", () => {
  it("says each window, and how long the short one has left", () => {
    const text = page(usage, {}, now);
    expect(text).toContain("- **5h** 68%, resets in 2h 30m");
    expect(text).toContain("- **7d** 24%");
    expect(text).toContain("Read 2026-10-03 11:55.");
  });

  it("names what spent the window, biggest first", () => {
    const text = page(
      usage,
      {
        since: "2026-10-03T09:00:00Z",
        shares: [
          { dir: "/code/small", tokens: 1000, percent: 4, sessions: 1 },
          { dir: "/code/big", tokens: 2_400_000, percent: 61.25, sessions: 3 },
        ],
      },
      now,
    );
    const rows = text.split("\n").filter((l) => l.startsWith("| /code/"));
    expect(rows[0]).toBe("| /code/big | 61.3% | 2.4M | 3 |");
    expect(rows[1]).toBe("| /code/small | 4.0% | 1k | 1 |");
    expect(text).toContain("since 2026-10-03 09:00");
  });

  it("leaves the table out when nothing has been spent", () => {
    expect(page(usage, { shares: null }, now)).not.toContain("| Checkout |");
  });

  it("says why the numbers stopped, and that there are none", () => {
    const text = page({ error: "401 from the provider" }, {}, now);
    expect(text).toContain("No reading yet.");
    expect(text).toContain("The last read failed: 401 from the provider");
  });

  it("does not promise a reset that has gone by", () => {
    const past = {
      ...usage,
      claude: {
        ...usage.claude,
        windows: [
          { name: "5h", percent: 10, resetsAt: "2026-10-03T11:00:00Z" },
        ],
      },
    };
    expect(page(past, {}, now)).toContain("- **5h** 10%\n");
  });
});

describe("how long a window has left", () => {
  const at = (iso: string) => ({
    claude: {
      provider: "claude",
      windows: [{ name: "w", percent: 1, resetsAt: iso }],
    },
  });

  it("is minutes, then hours, then days", () => {
    expect(page(at("2026-10-03T12:40:00Z"), {}, now)).toContain(
      "resets in 40m",
    );
    expect(page(at("2026-10-04T05:30:00Z"), {}, now)).toContain(
      "resets in 17h 30m",
    );
    expect(page(at("2026-10-10T12:00:00Z"), {}, now)).toContain(
      "resets in 7 days",
    );
  });
});
