import { createHash } from "node:crypto";
import type { IncomingMessage } from "node:http";
import type { Duplex } from "node:stream";

const GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11";

/** One WebSocket connection, accepted by a plain HTTP server, with just enough framing for text messages. */
export class Socket {
  private buf = Buffer.alloc(0);
  onText: (text: string) => void = () => undefined;
  onClose: () => void = () => undefined;
  closed = false;

  private readonly s: Duplex;

  constructor(s: Duplex) {
    this.s = s;
    s.on("data", (d: Buffer) => this.feed(d));
    s.on("close", () => this.finish());
    s.on("error", () => this.finish());
  }

  private finish(): void {
    if (this.closed) return;
    this.closed = true;
    this.onClose();
  }

  private feed(d: Buffer): void {
    this.buf = Buffer.concat([this.buf, d]);
    for (;;) {
      if (this.buf.length < 2) return;
      const op = (this.buf[0] as number) & 0x0f;
      const masked = ((this.buf[1] as number) & 0x80) !== 0;
      let len = (this.buf[1] as number) & 0x7f;
      let off = 2;
      if (len === 126) {
        if (this.buf.length < 4) return;
        len = this.buf.readUInt16BE(2);
        off = 4;
      } else if (len === 127) {
        if (this.buf.length < 10) return;
        len = Number(this.buf.readBigUInt64BE(2));
        off = 10;
      }
      const need = off + (masked ? 4 : 0) + len;
      if (this.buf.length < need) return;
      const mask = masked ? this.buf.subarray(off, off + 4) : null;
      const payload = Buffer.from(this.buf.subarray(off + (masked ? 4 : 0), need));
      if (mask) for (let i = 0; i < payload.length; i++) payload[i] = (payload[i] as number) ^ (mask[i % 4] as number);
      this.buf = this.buf.subarray(need);
      if (op === 0x8) {
        this.close();
        return;
      }
      if (op === 0x1) this.onText(payload.toString("utf8"));
      if (op === 0x9) this.write(0xa, payload);
    }
  }

  private write(op: number, payload: Buffer): void {
    if (this.closed) return;
    const head =
      payload.length < 126
        ? Buffer.from([0x80 | op, payload.length])
        : payload.length < 65536
          ? Buffer.from([0x80 | op, 126, payload.length >> 8, payload.length & 0xff])
          : (() => {
              const h = Buffer.alloc(10);
              h[0] = 0x80 | op;
              h[1] = 127;
              h.writeBigUInt64BE(BigInt(payload.length), 2);
              return h;
            })();
    this.s.write(Buffer.concat([head, payload]));
  }

  send(text: string): void {
    this.write(0x1, Buffer.from(text, "utf8"));
  }

  close(): void {
    if (!this.closed) {
      this.write(0x8, Buffer.alloc(0));
      this.s.end();
    }
    this.finish();
  }

  /** The network going away without a close handshake. */
  drop(): void {
    this.s.destroy();
    this.finish();
  }
}

export function accept(req: IncomingMessage, socket: Duplex): Socket | null {
  const key = req.headers["sec-websocket-key"];
  if (typeof key !== "string") {
    socket.destroy();
    return null;
  }
  const accept = createHash("sha1")
    .update(key + GUID)
    .digest("base64");
  socket.write(
    `HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ${accept}\r\n\r\n`,
  );
  return new Socket(socket);
}
