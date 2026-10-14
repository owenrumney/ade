import { createServer, type Server as HTTP } from "node:http";
import { afterEach, describe, expect, it } from "vitest";
import type { Server } from "./discover";
import { worker } from "./launcher";

let live: HTTP | undefined;

afterEach(() => live?.close());

// A real socket, because what is under test is the request's own timeout:
// a mocked fetch would prove nothing about how long one may be held.
function serve(
  reply: (body: string) => { status: number; text: string; after?: number },
): Promise<Server> {
  return new Promise((resolve) => {
    live = createServer((req, res) => {
      let body = "";
      req.on("data", (chunk) => (body += String(chunk)));
      req.on("end", () => {
        const { status, text, after = 0 } = reply(body);
        setTimeout(() => {
          res.writeHead(status, { "Content-Type": "application/json" });
          res.end(text);
        }, after);
      });
    });
    live.listen(0, "127.0.0.1", () => {
      const port = (live!.address() as { port: number }).port;
      resolve({ base: `http://127.0.0.1:${port}`, token: "t" } as Server);
    });
  });
}

describe("telling a machine's worker something", () => {
  it("names the host in the body and the verb in the path", async () => {
    let seen = "";
    let path = "";
    const at = await serve((body) => {
      seen = body;
      return { status: 204, text: "" };
    });
    live!.on("request", (req) => (path = req.url ?? ""));

    await worker(at, "update", "pi5.local");
    expect(JSON.parse(seen)).toEqual({ host: "pi5.local" });
    expect(path).toContain("/api/worker/update");
  });

  // The server refuses it on a machine that is not connected, and its
  // sentence is the whole point of the refusal.
  it("throws the server's own reason, not its status", async () => {
    const at = await serve(() => ({
      status: 400,
      text: JSON.stringify({ error: "pi5.local is not connected" }),
    }));

    await expect(worker(at, "stop", "pi5.local")).rejects.toThrow(
      "pi5.local is not connected",
    );
  });

  // fetch abandons headers at five minutes; an update holds the request for
  // the server's ten. This only proves the request is not abandoned early.
  it("waits for a reply that is slow to start", async () => {
    const at = await serve(() => ({ status: 204, text: "", after: 300 }));

    await expect(worker(at, "update", "pi5.local")).resolves.toBeUndefined();
  });
});
