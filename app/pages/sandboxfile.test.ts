import { describe, expect, it } from "vitest";
import { apply, rows, type SandboxFile, validKey } from "./sandboxfile";

const file: SandboxFile = {
  paths: [{ path: "/code/lib" }, { path: "/data", rw: true }],
  env: { NPM_TOKEN: "••••", REGION: "eu-west-2" },
};

describe("apply", () => {
  it("sends back what it did not touch, markers included", () => {
    const body = apply(file, {
      kind: "set",
      key: "REGION",
      value: "us-east-1",
    });
    expect(body.env).toEqual({ NPM_TOKEN: "••••", REGION: "us-east-1" });
    expect(body.paths).toEqual(file.paths);
  });

  it("changes a mount in place rather than listing it twice", () => {
    const body = apply(file, { kind: "mount", path: "/code/lib", rw: true });
    expect(body.paths).toEqual([
      { path: "/data", rw: true },
      { path: "/code/lib", rw: true },
    ]);
  });

  it("leaves rw off a read-only mount, as the file has it", () => {
    const body = apply(file, { kind: "mount", path: "/new", rw: false });
    expect(body.paths.at(-1)).toEqual({ path: "/new" });
  });

  it("removes a mount and a variable", () => {
    expect(apply(file, { kind: "unmount", path: "/data" }).paths).toEqual([
      { path: "/code/lib" },
    ]);
    expect(apply(file, { kind: "unset", key: "NPM_TOKEN" }).env).toEqual({
      REGION: "eu-west-2",
    });
  });

  it("does not change the file it was given", () => {
    apply(file, { kind: "unset", key: "REGION" });
    expect(file.env).toHaveProperty("REGION");
  });

  it("starts from nothing when there is no file yet", () => {
    const body = apply(
      { paths: null, env: null },
      { kind: "set", key: "A", value: "b" },
    );
    expect(body).toEqual({ paths: [], env: { A: "b" } });
  });
});

describe("rows", () => {
  it("lists mounts, then variables, then the two ways to add", () => {
    expect(rows(file).map((row) => row.label)).toEqual([
      "/code/lib",
      "/data",
      "NPM_TOKEN",
      "REGION",
      "Mount a folder…",
      "Set a variable…",
    ]);
  });

  it("never shows a value", () => {
    const text = JSON.stringify(
      rows(file).map((row) => [row.label, row.description]),
    );
    expect(text).not.toContain("eu-west-2");
    expect(text).not.toContain("••••");
  });

  it("says which way a mount is", () => {
    const [ro, rw] = rows(file);
    expect(ro.description).toBe("mounted read-only");
    expect(rw.description).toBe("mounted read-write");
  });
});

describe("validKey", () => {
  it("takes what a shell would", () => {
    expect(validKey("NPM_TOKEN")).toBe(true);
    expect(validKey("_x1")).toBe(true);
    expect(validKey("1X")).toBe(false);
    expect(validKey("A-B")).toBe(false);
    expect(validKey("")).toBe(false);
  });
});
