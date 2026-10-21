import { describe, expect, it } from "vitest";
import { edit, empty, envKeys, keyRefused, pathRefused, summary, type Draft } from "./view";

const draft: Draft = {
  paths: [{ path: "/code/lib" }, { path: "/data", rw: true }],
  env: { NPM_TOKEN: "••••", REGION: "eu-west-2" },
};

describe("pathRefused", () => {
  it("takes an absolute path and a home-relative one", () => {
    expect(pathRefused(draft, "/code/other")).toBeUndefined();
    expect(pathRefused(draft, "~/go/pkg/mod")).toBeUndefined();
  });

  it("refuses nothing, a relative path and one already mounted", () => {
    expect(pathRefused(draft, "  ")).toBe("Name a folder.");
    expect(pathRefused(draft, "../lib")).toMatch(/absolute/);
    expect(pathRefused(draft, "/data")).toBe("Already mounted.");
  });
});

describe("keyRefused", () => {
  it("refuses a name the server would, and one already set", () => {
    expect(keyRefused(draft, "AWS_PROFILE")).toBeUndefined();
    expect(keyRefused(draft, "2FA")).toMatch(/digit/);
    expect(keyRefused(draft, "REGION")).toBe("Already set.");
  });
});

describe("edit", () => {
  it("keeps the markers of every variable it did not touch", () => {
    const next = edit(draft, { kind: "mount", path: "/new", rw: true });
    expect(next.env).toEqual(draft.env);
    expect(next.paths.at(-1)).toEqual({ path: "/new", rw: true });
  });
});

describe("summary", () => {
  it("says the checkout alone when there is nothing else", () => {
    expect(summary(empty)).toMatch(/Only the checkout/);
  });

  it("counts folders and variables", () => {
    expect(summary(draft)).toBe("2 folders and 2 variables, on top of the checkout.");
    expect(summary({ paths: [{ path: "/a" }], env: {} })).toBe(
      "1 folder, on top of the checkout.",
    );
  });
});

describe("envKeys", () => {
  it("sorts them, and shows no value", () => {
    expect(envKeys(draft)).toEqual(["NPM_TOKEN", "REGION"]);
  });
});
