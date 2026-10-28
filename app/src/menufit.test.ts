import { describe, expect, it } from "vitest";
import { fit } from "./menufit";

const what = (params: Parameters<typeof fit>[0], where?: Parameters<typeof fit>[1]) =>
  fit(params, where)
    .map((i) => i.what ?? "—")
    .join(" ");

describe("the menu at the pointer", () => {
  // Every box in every page had no menu at all: no Copy, no Paste.
  it("edits a field, wherever the field is", () => {
    expect(what({ isEditable: true })).toBe("cut copy paste — selectAll");
  });

  it("copies a selection that is not editable", () => {
    expect(what({ selectionText: "some words", editFlags: { canSelectAll: true } })).toBe(
      "copy selectAll",
    );
    expect(what({ selectionText: "   " })).toBe("");
  });

  it("offers a link where one was clicked", () => {
    expect(what({ linkURL: "https://example.com" })).toBe("open-link copy-link");
    expect(what({ isEditable: true, linkURL: "https://example.com" })).toContain("— open-link");
  });

  // A right-click that draws nothing reads as broken, so a panel offers
  // itself when there is nothing else to say.
  it("offers the panel when there is nothing else", () => {
    expect(what({}, { panel: "ade.commitPanel" })).toBe("reload-panel");
    expect(what({})).toBe("");
  });

  it("adds Inspect only where the build is not packaged", () => {
    expect(what({ isEditable: true }, { dev: true })).toContain("inspect");
    expect(what({ isEditable: true }, { dev: false })).not.toContain("inspect");
  });
});
