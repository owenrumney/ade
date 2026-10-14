import { createHash } from "node:crypto";
import { createServer, type Server } from "node:http";
import type { Duplex } from "node:stream";
import { afterEach, describe, expect, it } from "vitest";
import { BINARY, decode, TEXT, WS } from "./ws";

const MAGIC = "258EAFA5-E914-47DA-95CA-5AB0DC85B11A";

// A stand-in for ade's endpoint: enough of RFC 6455's server half to prove the
// client's handshake and framing against a real socket rather than a buffer.
function serve(
  onUpgrade: (socket: Duplex, key: string) => void,
  status?: string,
): Promise<{ server: Server; port: number }> {
  const server = createServer((_, res) => res.end("no"));
  server.on("upgrade", (req, socket) => {
    const key = req.headers["sec-websocket-key"] as string;
    if (status) {
      socket.end(`HTTP/1.1 ${status}\r\nContent-Length: 0\r\n\r\n`);
      return;
    }
    const accept = createHash("sha1")
      .update(key + MAGIC)
      .digest("base64");
    socket.write(
      "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\n" +
        `Connection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`,
    );
    onUpgrade(socket, key);
  });
  return new Promise((done) =>
    server.listen(0, "127.0.0.1", () =>
      done({ server, port: (server.address() as { port: number }).port }),
    ),
  );
}

function unmasked(op: number, payload: Buffer, fin = true): Buffer {
  const head = Buffer.from([(fin ? 0x80 : 0) | op, payload.length]);
  return Buffer.concat([head, payload]);
}

let running: Server | undefined;
afterEach(() => running?.close());

describe("WS against a real socket", () => {
  it("handshakes and carries bytes both ways", async () => {
    const heard: Buffer[] = [];
    const { server, port } = await serve((socket) => {
      socket.on("data", (chunk: Buffer) => {
        for (const f of decode(chunk).frames) heard.push(f.payload);
        socket.write(unmasked(BINARY, Buffer.from("$ ")));
      });
    });
    running = server;

    const got: string[] = [];
    const ws = new WS(`ws://127.0.0.1:${port}/api/terminal`, {
      open: () =>
        ws.send(TEXT, Buffer.from('{"type":"resize","cols":80,"rows":24}')),
      data: (chunk) => got.push(chunk.toString()),
      close: () => undefined,
    });
    ws.open();

    await until(() => got.length > 0);
    expect(got.join("")).toBe("$ ");
    expect(heard[0].toString()).toContain('"cols":80');
    ws.close();
  });

  it("sends a masked frame, which is what the server requires", async () => {
    let first: Buffer | undefined;
    const { server, port } = await serve((socket) => {
      socket.on("data", (chunk: Buffer) => (first ??= chunk));
    });
    running = server;

    const ws = new WS(`ws://127.0.0.1:${port}/api/terminal`, {
      open: () => ws.send(BINARY, Buffer.from("ls\n")),
      data: () => undefined,
      close: () => undefined,
    });
    ws.open();

    await until(() => first !== undefined);
    expect(first![1] & 0x80).toBe(0x80);
    ws.close();
  });

  it("reassembles a message the server split in two", async () => {
    const { server, port } = await serve((socket) => {
      socket.write(unmasked(BINARY, Buffer.from("he"), false));
      socket.write(unmasked(0x0, Buffer.from("llo")));
    });
    running = server;

    const got: string[] = [];
    const ws = new WS(`ws://127.0.0.1:${port}/api/terminal`, {
      open: () => undefined,
      data: (chunk) => got.push(chunk.toString()),
      close: () => undefined,
    });
    ws.open();

    await until(() => got.length > 0);
    expect(got).toEqual(["hello"]);
    ws.close();
  });

  it("answers a ping, so an idle terminal is not dropped", async () => {
    let pong: Buffer | undefined;
    const { server, port } = await serve((socket) => {
      socket.on(
        "data",
        (chunk: Buffer) => (pong ??= decode(chunk).frames[0]?.payload),
      );
      socket.write(unmasked(0x9, Buffer.from("hi")));
    });
    running = server;

    const ws = new WS(`ws://127.0.0.1:${port}/api/terminal`, {
      open: () => undefined,
      data: () => undefined,
      close: () => undefined,
    });
    ws.open();

    await until(() => pong !== undefined);
    expect(pong!.toString()).toBe("hi");
    ws.close();
  });

  it("reports a refusal rather than hanging on it", async () => {
    const { server, port } = await serve(() => undefined, "403 Forbidden");
    running = server;

    let why = "";
    let refused = false;
    const ws = new WS(`ws://127.0.0.1:${port}/api/terminal`, {
      open: () => undefined,
      data: () => undefined,
      close: (said, fatal) => {
        why = said;
        refused = fatal;
      },
    });
    ws.open();

    await until(() => why !== "");
    expect(why).toContain("403");
    // A refusal is the token or the route, and asking again gets it again.
    expect(refused).toBe(true);
  });

  it("says so when the shell goes away", async () => {
    const { server, port } = await serve((socket) => {
      const payload = Buffer.alloc(2 + 12);
      payload.writeUInt16BE(1000, 0);
      payload.write("shell exited", 2);
      socket.write(unmasked(0x8, payload));
    });
    running = server;

    let why = "";
    let fatal = true;
    const ws = new WS(`ws://127.0.0.1:${port}/api/terminal`, {
      open: () => undefined,
      data: () => undefined,
      close: (said, over) => {
        why = said;
        fatal = over;
      },
    });
    ws.open();

    await until(() => why !== "");
    expect(why).toBe("shell exited");
    // The shell going is not a refusal: the socket may be worth retrying.
    expect(fatal).toBe(false);
  });
});

async function until(ready: () => boolean): Promise<void> {
  for (let tries = 0; tries < 200; tries++) {
    if (ready()) return;
    await new Promise((r) => setTimeout(r, 10));
  }
  throw new Error("nothing arrived");
}
