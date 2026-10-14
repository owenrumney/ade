import { describe, expect, it } from "vitest";
import { money, page, tokens } from "./statspage";

describe("tokens and money", () => {
  it("round to what is worth reading", () => {
    expect(tokens(812)).toBe("812");
    expect(tokens(340_400)).toBe("340k");
    expect(tokens(1_240_000)).toBe("1.2M");
    expect(money(3.456)).toBe("$3.46");
  });
});

describe("the stats page", () => {
  const group = (key: string, more = {}) => ({
    key,
    cost: 1,
    tokens: 1000,
    sessions: 1,
    ...more,
  });

  it("leads with the total and how it was reached", () => {
    const text = page({ sessions: 1, requests: 12, tools: 3, cost: 4.5 });
    expect(text).toContain("**$4.50** across 1 session");
    expect(text).toContain("12 requests, 3 tool calls");
    expect(text).not.toContain("## ");
  });

  it("lists the newest days first and only a fortnight of them", () => {
    const days = Array.from({ length: 20 }, (_, i) =>
      group(`2026-09-${String(i + 1).padStart(2, "0")}`),
    );
    const text = page({
      sessions: 1,
      requests: 1,
      tools: 0,
      cost: 0,
      byDay: days,
    });
    const rows = text.split("\n").filter((line) => line.startsWith("| 2026-"));
    expect(rows).toHaveLength(14);
    expect(rows[0]).toContain("2026-09-20");
  });

  it("marks a cost that is only a floor, and says why once", () => {
    const text = page({
      sessions: 2,
      requests: 2,
      tools: 0,
      cost: 2,
      byModel: [group("opus", { unpriced: 3 }), group("sonnet")],
    });
    expect(text).toContain("| opus | $1.00 \\* |");
    expect(text.match(/no price/g)).toHaveLength(1);
  });

  it("orders tools by how often they were called", () => {
    const text = page({
      sessions: 1,
      requests: 1,
      tools: 9,
      cost: 0,
      byTool: [group("Read", { requests: 2 }), group("Bash", { requests: 7 })],
    });
    expect(text.indexOf("| Bash | 7 |")).toBeLessThan(
      text.indexOf("| Read | 2 |"),
    );
  });
});
