import { describe, expect, it } from "vitest";
import { kept, parse, typed } from "./verify";

describe("the check list as one line", () => {
  it("reads back what it wrote", () => {
    const checks = [
      { name: "build", run: "go build ./..." },
      { name: "format", run: "gofmt -l cmd internal" },
    ];
    expect(parse(typed(checks))).toEqual(checks);
  });

  // A command with an = in it keeps everything after the first one.
  it("splits on the first equals only", () => {
    expect(parse("env=FOO=1 go build ./...")).toEqual([
      { name: "env", run: "FOO=1 go build ./..." },
    ]);
  });

  it("names a bare command after its first word", () => {
    expect(parse("  go build ./...  ")).toEqual([
      { name: "go", run: "go build ./..." },
    ]);
  });

  it("drops the empty and the half-written", () => {
    expect(parse("")).toEqual([]);
    expect(typed([])).toBe("");
    expect(parse(" | build=go build ./... |  | =nothing | named=")).toEqual([
      { name: "build", run: "go build ./..." },
    ]);
  });
});

describe("what the box cannot show", () => {
  it("stays with a check that was not changed", () => {
    const had = [
      {
        name: "go test",
        run: "go test ./...",
        narrow: "go test {{.Packages}}",
      },
      { name: "vitest", run: "npx vitest run", dir: "vscode" },
    ];
    const edited = parse(
      "go test=go test ./... | vitest=npx vitest run --bail 1",
    );
    expect(kept(edited, had)).toEqual([
      {
        name: "go test",
        run: "go test ./...",
        narrow: "go test {{.Packages}}",
      },
      { name: "vitest", run: "npx vitest run --bail 1" },
    ]);
  });
});
