import { describe, expect, it } from "vitest";
import { secrets, stateOf } from "./secrets";

describe("the secrets rows", () => {
  it("names all three, set or not", () => {
    expect(secrets({}).map((s) => s.route)).toEqual([
      "linear-key",
      "home-assistant-token",
      "github-packages-token",
    ]);
    expect(secrets({}).every((s) => !s.set)).toBe(true);
  });

  it("says how one ends and never what it is", () => {
    const [linear, home, packages] = secrets({
      linearKeySet: true,
      linearKeyEnd: "a1b2",
      homeAssistantTokenSet: true,
    });
    expect(stateOf(linear)).toBe("set · ends a1b2");
    expect(stateOf(home), "too short to show a tail").toBe("set");
    expect(stateOf(packages)).toBe("not set");
  });
});
