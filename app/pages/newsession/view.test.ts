import { describe, expect, it } from "vitest";
import { plan } from "./plan";
import { fresh, missing, names, ref, summary, type Chosen } from "./view";

const onTicket = (ticket: string): Chosen => ({
  ...fresh,
  mode: "ticket",
  ticket,
  repos: ["/r/coast"],
});

describe("what the Ticket box accepts", () => {
  it("takes a Linear reference in any case", () => {
    expect(ref("fix-623")).toEqual({ linear: "FIX-623" });
    expect(ref(" TRK-1072 ")).toEqual({ linear: "TRK-1072" });
  });

  it("takes a GitHub issue with or without the hash", () => {
    expect(ref("#12")).toEqual({ issue: 12 });
    expect(ref("12")).toEqual({ issue: 12 });
  });

  it("refuses anything that is not one of those", () => {
    expect(ref("")).toBeUndefined();
    expect(ref("#0")).toBeUndefined();
    expect(ref("cancel-running-process")).toBeUndefined();
  });

  it("will not start until it has one", () => {
    expect(missing(onTicket(""))).toContain("FIX-623");
    expect(missing(onTicket("nonsense"))).toContain("FIX-623");
    expect(missing(onTicket("FIX-623"))).toBeUndefined();
  });
});

describe("what the Ticket row says it would make", () => {
  it("branches a Linear ticket after the ticket", () => {
    const chosen = onTicket("FIX-623");
    expect(names(chosen)).toBe("FIX-623");
    expect(plan(chosen).branch).toBe("fix-623");
  });

  // Only the server can read the title an issue's document is named after.
  it("names no branch for a GitHub issue, and says so", () => {
    const chosen = onTicket("#12");
    expect(names(chosen)).toBe("");
    expect(summary(chosen, plan(chosen))).toBe("Issue #12 in coast.");
  });
});
