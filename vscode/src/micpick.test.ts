import { describe, expect, it, vi } from "vitest";
import { inputs } from "./chat/mic";

describe("the microphones offered", () => {
  it("are the inputs, named even before a grant gives them labels", async () => {
    vi.stubGlobal("navigator", {
      mediaDevices: {
        enumerateDevices: async () => [
          { kind: "audioinput", deviceId: "a", label: "" },
          { kind: "audiooutput", deviceId: "b", label: "Speakers" },
          { kind: "audioinput", deviceId: "c", label: "Yeti" },
        ],
      },
    });
    expect(await inputs()).toEqual([
      { id: "a", label: "Microphone 1" },
      { id: "c", label: "Yeti" },
    ]);
    vi.unstubAllGlobals();
  });
});
