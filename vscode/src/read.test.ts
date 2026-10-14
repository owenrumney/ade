import { describe, expect, it } from "vitest";
import { read } from "./client";

describe("read", () => {
  it("takes 204 with no body as nothing to say", async () => {
    await expect(
      read("/api/chat/model", new Response(null, { status: 204 })),
    ).resolves.toBeUndefined();
  });

  it("still parses a body", async () => {
    await expect(
      read("/api/state", new Response(`{"ok":true}`)),
    ).resolves.toEqual({ ok: true });
  });

  it("reports what the server said on a failure", async () => {
    await expect(
      read(
        "/api/chat/new",
        new Response("say what it is for", { status: 400 }),
      ),
    ).rejects.toThrow("say what it is for");
  });
});
