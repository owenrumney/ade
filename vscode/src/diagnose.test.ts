import { describe, expect, it } from "vitest";
import { netReport } from "./diagnose";

describe("the network report", () => {
  it("puts the hint under the machine it is about", () => {
    const page = netReport({
      results: [],
      hosts: [
        { name: "pi", address: "192.168.1.9:22", reachable: true, took: "4ms" },
        {
          name: "nas",
          address: "192.168.1.7:22",
          reachable: false,
          took: "12ms",
          err: "no route to host",
          hint: "turn on Visual Studio Code under Local Network",
        },
      ],
    });
    const lines = page.split("\n");
    const nas = lines.findIndex((line) => line.includes("**nas**"));
    expect(lines[nas]).toContain("UNREACHABLE at `192.168.1.7:22`");
    expect(lines[nas + 1]).toBe(
      "  - turn on Visual Studio Code under Local Network",
    );
    expect(page).toContain("**pi** — ssh port open");
  });

  it("says so when there is no machine to dial", () => {
    expect(netReport({ results: null, hosts: null })).toContain(
      "No machines bookmarked.",
    );
  });

  it("tells a refused credential from a dead service", () => {
    const page = netReport({
      hosts: [],
      results: [
        { name: "github", reachable: true, status: "200 OK", took: "80ms" },
        {
          name: "linear",
          reachable: true,
          refused: true,
          status: "401 Unauthorized",
          took: "90ms",
        },
        {
          name: "anthropic",
          reachable: false,
          took: "10s",
          reached: "dns@1ms",
          err: "timeout",
        },
      ],
      files: { open: 40, limit: 256 },
    });
    expect(page).toContain("**github** — ok, 200 OK in 80ms");
    expect(page).toContain(
      "**linear** — reachable, but refused the credential",
    );
    expect(page).toContain("(got as far as: dns@1ms): timeout");
    expect(page).toContain("Open files: 40 of 256.");
  });
});
