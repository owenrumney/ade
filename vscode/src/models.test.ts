import { describe, expect, it } from "vitest";
import { move, warning } from "./models";

const list = [
  { id: "opus", label: "opus", agent: "claude" },
  { id: "gpt-5.6-terra", label: "terra", agent: "codex" },
];

describe("move", () => {
  it("switches the agent when the model belongs to the other one", () => {
    expect(
      move(list, { provider: "claude", model: "opus" }, "gpt-5.6-terra"),
    ).toEqual({
      kind: "agent",
      provider: "codex",
      model: "gpt-5.6-terra",
    });
  });

  it("only changes the model within one agent", () => {
    expect(move(list, { provider: "claude", model: "sonnet" }, "opus")).toEqual(
      {
        kind: "model",
        model: "opus",
      },
    );
  });

  it("says nothing to do when it is the model already on", () => {
    expect(move(list, { provider: "claude", model: "opus" }, "opus")).toEqual({
      kind: "none",
    });
  });

  it("names the agent in the warning", () => {
    expect(warning("codex")).toContain("Codex");
  });
});
