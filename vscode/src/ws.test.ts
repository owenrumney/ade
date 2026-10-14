import { describe, expect, it } from "vitest";
import { BINARY, CLOSE, decode, encode, reason, TEXT, upgrade } from "./ws";

// server frames the way ade sends them: unmasked, since only a client masks.
function server(op: number, payload: Buffer, fin = true): Buffer {
  const len = payload.length;
  const extra = len < 126 ? 0 : len < 65536 ? 2 : 8;
  const head = Buffer.alloc(2 + extra);
  head[0] = (fin ? 0x80 : 0) | op;
  head[1] = extra === 0 ? len : extra === 2 ? 126 : 127;
  if (extra === 2) head.writeUInt16BE(len, 2);
  if (extra === 8) head.writeBigUInt64BE(BigInt(len), 2);
  return Buffer.concat([head, payload]);
}

describe("encode", () => {
  it("masks what it sends, because a client must", () => {
    const frame = encode(BINARY, Buffer.from("ls\n"));
    expect(frame[0]).toBe(0x82);
    expect(frame[1] & 0x80).toBe(0x80);
    expect(frame[1] & 0x7f).toBe(3);
  });

  it("round-trips through a decoder that unmasks", () => {
    for (const size of [0, 5, 125, 126, 300, 65535, 65536]) {
      const body = Buffer.alloc(size, 0x61);
      const { frames, rest } = decode(encode(BINARY, body));
      expect(rest.length).toBe(0);
      expect(frames).toHaveLength(1);
      expect(frames[0].payload.equals(body)).toBe(true);
    }
  });
});

describe("decode", () => {
  it("returns a half-arrived frame as leftovers rather than guessing", () => {
    const whole = server(BINARY, Buffer.from("hello"));
    const { frames, rest } = decode(whole.subarray(0, 4));
    expect(frames).toHaveLength(0);
    expect(rest.length).toBe(4);
  });

  it("reads several frames out of one read", () => {
    const buf = Buffer.concat([
      server(BINARY, Buffer.from("a")),
      server(BINARY, Buffer.from("b")),
    ]);
    const { frames, rest } = decode(buf);
    expect(frames.map((f) => f.payload.toString())).toEqual(["a", "b"]);
    expect(rest.length).toBe(0);
  });

  it("keeps the tail of a buffer that ends mid-frame", () => {
    const buf = Buffer.concat([
      server(BINARY, Buffer.from("a")),
      server(BINARY, Buffer.from("bb")).subarray(0, 3),
    ]);
    const { frames, rest } = decode(buf);
    expect(frames).toHaveLength(1);
    expect(rest.length).toBe(3);
  });

  it("carries fin and opcode through", () => {
    const { frames } = decode(server(TEXT, Buffer.from("x"), false));
    expect(frames[0].fin).toBe(false);
    expect(frames[0].op).toBe(TEXT);
  });

  it("refuses a length no buffer could hold", () => {
    const head = Buffer.alloc(10);
    head[0] = 0x82;
    head[1] = 127;
    head.writeBigUInt64BE(BigInt("0xffffffffffffff"), 2);
    expect(() => decode(head)).toThrow(/too long/);
  });
});

describe("upgrade", () => {
  it("waits for the whole header", () => {
    expect(upgrade(Buffer.from("HTTP/1.1 101 Switching"))).toBeUndefined();
  });

  it("hands back the frames that shared the packet", () => {
    const body = server(BINARY, Buffer.from("$ "));
    const buf = Buffer.concat([
      Buffer.from("HTTP/1.1 101 x\r\nUpgrade: websocket\r\n\r\n"),
      body,
    ]);
    const said = upgrade(buf);
    expect(said?.ok).toBe(true);
    expect(decode(said!.rest).frames[0].payload.toString()).toBe("$ ");
  });

  it("reports a refusal rather than reading frames out of an error page", () => {
    const said = upgrade(Buffer.from("HTTP/1.1 403 Forbidden\r\n\r\nno"));
    expect(said?.ok).toBe(false);
    expect(said?.status).toBe(403);
  });
});

describe("reason", () => {
  it("reads the text after the code", () => {
    const payload = Buffer.concat([
      Buffer.alloc(2),
      Buffer.from("shell exited"),
    ]);
    payload.writeUInt16BE(1000, 0);
    expect(reason(payload)).toBe("shell exited");
  });

  it("falls back to the code when there is no text", () => {
    const payload = Buffer.alloc(2);
    payload.writeUInt16BE(1011, 0);
    expect(reason(payload)).toBe("closed 1011");
  });

  it("says something for an empty close", () => {
    expect(reason(Buffer.alloc(0))).not.toBe("");
  });
});

describe("close frames", () => {
  it("are an opcode the reader can tell from output", () => {
    const { frames } = decode(server(CLOSE, Buffer.alloc(2)));
    expect(frames[0].op).toBe(CLOSE);
  });
});
