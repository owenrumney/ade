import { describe, expect, it } from "vitest";
import {
  detail,
  label,
  type RolesView,
  rows,
  skeleton,
  validName,
} from "./rolerow";

const view = (over: Partial<RolesView> = {}): RolesView => ({
  roles: [
    { name: "oss-dev", prompt: "You are a pragmatic OSS maintainer.\nMore." },
    { name: "infracost", prompt: "You work on Infracost." },
  ],
  default: "oss-dev",
  ...over,
});

describe("the roles pick", () => {
  it("ticks the selected one and nothing else", () => {
    const got = rows(view()).map(label);
    expect(got[0]).toBe("$(check) oss-dev");
    expect(got[1]).toBe("$(blank) infracost");
    expect(got[2]).toBe("$(blank) No role");
  });

  it("ticks No role when none is selected, or the selection has gone", () => {
    expect(rows(view({ default: "" })).map(label)).toContain(
      "$(check) No role",
    );
    expect(rows(view({ default: "deleted" })).map(label)).toContain(
      "$(check) No role",
    );
  });

  it("offers the defaults only when there are none", () => {
    expect(rows(view()).some((r) => r.kind === "install")).toBe(false);
    const empty = rows({ roles: null, default: "" });
    expect(empty.map((r) => r.kind)).toEqual(["none", "new", "install"]);
  });

  it("says what a role is for, from its first line", () => {
    expect(detail(rows(view())[0])).toBe("You are a pragmatic OSS maintainer.");
  });
});

describe("a new role", () => {
  it("takes a name a file can hold, and no other", () => {
    expect(validName("oss-dev")).toBe(true);
    expect(validName("a_1")).toBe(true);
    expect(validName("-leading")).toBe(false);
    expect(validName("has space")).toBe(false);
    expect(validName("")).toBe(false);
    expect(validName("x".repeat(65))).toBe(false);
  });

  it("opens as prose, with the name in it", () => {
    expect(skeleton("reviewer")).toContain("on reviewer");
    expect(skeleton("reviewer")).not.toContain("---");
  });
});
