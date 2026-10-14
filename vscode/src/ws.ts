import { randomBytes } from "node:crypto";
import { connect, type Socket } from "node:net";

export const CONT = 0x0;
export const TEXT = 0x1;
export const BINARY = 0x2;
export const CLOSE = 0x8;
export const PING = 0x9;
export const PONG = 0xa;

export interface Frame {
  op: number;
  fin: boolean;
  payload: Buffer;
}

// encode builds one client frame. A client always masks, whatever the peer is:
// an unmasked frame from this side is a protocol error the server may close on.
export function encode(op: number, payload: Buffer): Buffer {
  const mask = randomBytes(4);
  const len = payload.length;
  const extra = len < 126 ? 0 : len < 65536 ? 2 : 8;
  const head = Buffer.alloc(2 + extra + 4);

  head[0] = 0x80 | op;
  head[1] = 0x80 | (extra === 0 ? len : extra === 2 ? 126 : 127);
  if (extra === 2) head.writeUInt16BE(len, 2);
  if (extra === 8) head.writeBigUInt64BE(BigInt(len), 2);
  mask.copy(head, 2 + extra);

  const body = Buffer.allocUnsafe(len);
  for (let i = 0; i < len; i++) body[i] = payload[i] ^ mask[i & 3];
  return Buffer.concat([head, body]);
}

// decode peels whole frames off the buffer and returns what was left over: a
// read boundary falls wherever TCP put it, never where a frame ends.
export function decode(buf: Buffer): { frames: Frame[]; rest: Buffer } {
  const frames: Frame[] = [];
  let at = 0;

  for (;;) {
    if (buf.length - at < 2) break;
    const first = buf[at];
    const second = buf[at + 1];
    const masked = (second & 0x80) !== 0;
    let len = second & 0x7f;
    let head = at + 2;

    if (len === 126) {
      if (buf.length - head < 2) break;
      len = buf.readUInt16BE(head);
      head += 2;
    } else if (len === 127) {
      if (buf.length - head < 8) break;
      const wide = buf.readBigUInt64BE(head);
      if (wide > BigInt(Number.MAX_SAFE_INTEGER))
        throw new Error("websocket frame too long");
      len = Number(wide);
      head += 8;
    }

    const mask = masked ? buf.subarray(head, head + 4) : undefined;
    if (masked) head += 4;
    if (buf.length - head < len) break;

    const payload = Buffer.from(buf.subarray(head, head + len));
    if (mask) for (let i = 0; i < len; i++) payload[i] ^= mask[i & 3];
    frames.push({ op: first & 0x0f, fin: (first & 0x80) !== 0, payload });
    at = head + len;
  }

  return { frames, rest: buf.subarray(at) };
}

// upgrade splits the 101 response from the frames that followed it. The server
// may put the first chunk of output in the same packet as the handshake.
export function upgrade(
  buf: Buffer,
): { ok: boolean; status: number; rest: Buffer } | undefined {
  const end = buf.indexOf("\r\n\r\n");
  if (end < 0) return undefined;
  const head = buf.subarray(0, end).toString("latin1");
  const status = Number(/^HTTP\/1\.\d (\d+)/.exec(head)?.[1] ?? 0);
  return { ok: status === 101, status, rest: buf.subarray(end + 4) };
}

export interface Handlers {
  open: () => void;
  data: (chunk: Buffer) => void;
  // fatal marks a refusal the same request would get again: a dead token, or
  // a route this server does not have. Reconnecting cannot fix either.
  close: (why: string, fatal: boolean) => void;
}

// WS is the small part of RFC 6455 this extension needs: one loopback socket,
// binary in and out, no compression and no subprotocol.
export class WS {
  private socket?: Socket;
  private held: Buffer<ArrayBufferLike> = Buffer.alloc(0);
  private ready = false;
  private done = false;
  private partial?: { op: number; chunks: Buffer[] };

  constructor(
    private url: string,
    private on: Handlers,
  ) {}

  open(): void {
    const at = new URL(this.url);
    const socket = connect(
      { host: at.hostname, port: Number(at.port || 80) },
      () => socket.write(request(at)),
    );
    this.socket = socket;
    socket.setNoDelay(true);
    socket.on("data", (chunk) => this.took(chunk));
    socket.on("error", (err) => this.ended(String(err)));
    socket.on("close", () => this.ended("the connection closed"));
  }

  send(op: number, payload: Buffer): void {
    if (this.ready && this.socket) this.socket.write(encode(op, payload));
  }

  close(): void {
    if (this.done) return;
    this.done = true;
    if (this.ready) this.send(CLOSE, Buffer.alloc(0));
    this.socket?.end();
  }

  private took(chunk: Buffer): void {
    this.held = this.held.length ? Buffer.concat([this.held, chunk]) : chunk;
    if (!this.ready) {
      const said = upgrade(this.held);
      if (!said) return;
      if (!said.ok) {
        this.ended(`ade refused the terminal (${said.status})`, true);
        return;
      }
      this.held = said.rest;
      this.ready = true;
      this.on.open();
    }

    let read;
    try {
      read = decode(this.held);
    } catch (err) {
      this.ended(String(err));
      return;
    }
    this.held = read.rest;
    for (const frame of read.frames) this.frame(frame);
  }

  private frame(frame: Frame): void {
    switch (frame.op) {
      case PING:
        this.send(PONG, frame.payload);
        return;
      case PONG:
        return;
      case CLOSE:
        this.ended(reason(frame.payload));
        return;
    }

    // A fragmented message is rare but legal, and half of one written to the
    // terminal would split a UTF-8 sequence down the middle.
    if (frame.op === CONT) {
      if (!this.partial) return;
      this.partial.chunks.push(frame.payload);
      if (!frame.fin) return;
      const { op, chunks } = this.partial;
      this.partial = undefined;
      this.message(op, Buffer.concat(chunks));
      return;
    }
    if (!frame.fin) {
      this.partial = { op: frame.op, chunks: [frame.payload] };
      return;
    }
    this.message(frame.op, frame.payload);
  }

  private message(op: number, payload: Buffer): void {
    if (op === BINARY || op === TEXT) this.on.data(payload);
  }

  private ended(why: string, fatal = false): void {
    if (this.done) return;
    this.done = true;
    this.socket?.destroy();
    this.on.close(why, fatal);
  }
}

function request(at: URL): string {
  const key = randomBytes(16).toString("base64");
  return [
    `GET ${at.pathname}${at.search} HTTP/1.1`,
    `Host: ${at.host}`,
    "Upgrade: websocket",
    "Connection: Upgrade",
    `Sec-WebSocket-Key: ${key}`,
    "Sec-WebSocket-Version: 13",
    "",
    "",
  ].join("\r\n");
}

// reason is the text of a close frame, which carries a two-byte code first.
export function reason(payload: Buffer): string {
  if (payload.length < 2) return "the shell closed";
  const said = payload.subarray(2).toString("utf8");
  return said || `closed ${payload.readUInt16BE(0)}`;
}
